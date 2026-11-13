// Package crud is the shared service + HTTP layer for MASTER, CHILD,
// TIMESERIES and read-only resources (PRD §10.4). Each resource supplies a
// store (its sqlc repository), DTOs and optional hooks; transactions,
// locking, tenant scope, reference checks, audit, ETags, CSV import and
// routing are handled here once.
package crud

import (
	"context"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/httpx"
)

// Meta identifies a row for the generic layer.
type Meta struct {
	ID        int64
	UUID      uuid.UUID
	UpdatedAt time.Time
}

// Reader is the read side of a repository. Implementations apply the tenant
// scope of the actor in ctx (see ScopeClientID) to Get and List.
type Reader[Row any] interface {
	Get(ctx context.Context, id uuid.UUID) (Row, error)
	List(ctx context.Context, q httpx.ListQuery) ([]Row, int64, error)
	Meta(row Row) Meta
}

// Store is the full repository contract of a writable resource.
type Store[Row, Req any] interface {
	Reader[Row]
	Create(ctx context.Context, req Req, actor string) (uuid.UUID, error)
	// Update and SoftDelete must return apperr.ErrPreconditionFailed when
	// updated_at no longer equals expected.
	Update(ctx context.Context, id uuid.UUID, req Req, actor string, expected time.Time) error
	SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) error
}

// Nested exposes GET <ParentPath>/{parent_uuid}<Path> as a list filtered by Filter.
type Nested struct {
	ParentPath string
	Filter     string
}

// Resource describes one writable CRUD endpoint family backed by a table.
type Resource[Row, Req, Resp any] struct {
	Table       string // permission prefix and audit entity
	Label       string
	Path        string
	Sorts       []string // sortable fields; "-" variants are added automatically
	DefaultSort string
	Filters     []string // allowed query parameters besides page/per_page/sort
	Store       Store[Row, Req]
	Response    func(Row) Resp
	// Check runs inside the write transaction before create (cur == nil) and update.
	Check func(ctx context.Context, req *Req, cur *Row) error
	// AfterWrite runs inside the write transaction after create (before == nil),
	// update, and delete (after == nil); e.g. to refresh a parent's latest_* columns.
	AfterWrite func(ctx context.Context, before, after *Row) error
	// Import enables POST <Path>/import (CSV, all-or-nothing).
	Import bool
	Nested []Nested
}

// ReadResource describes a read-only endpoint family (positions, history).
type ReadResource[Row, Resp any] struct {
	Table       string
	Label       string
	Path        string
	Sorts       []string
	DefaultSort string
	Filters     []string
	Store       Reader[Row]
	Response    func(Row) Resp
	Nested      []Nested
}

// Env carries the shared dependencies needed to mount resources.
type Env struct {
	Router    *auth.Router
	Validator httpx.Validator
	TX        *database.TxManager
	Audit     *audit.Recorder
}

// Registrar is implemented by every Resource and ReadResource instantiation.
type Registrar interface {
	Permissions() []auth.Permission
	Register(env Env)
}

// ScopeClientID returns the tenant filter of the actor in ctx (nil = global).
func ScopeClientID(ctx context.Context) *int64 {
	a, ok := auth.ActorFrom(ctx)
	if !ok {
		return nil
	}
	return a.ScopeClientID()
}

// Permissions returns <table>:read|create|update|delete.
func (r Resource[Row, Req, Resp]) Permissions() []auth.Permission {
	return auth.CRUD(r.Table, r.Label)
}

// Register mounts the standard routes of PRD §10.3.
func (r Resource[Row, Req, Resp]) Register(env Env) {
	s := &service[Row, Req, Resp]{res: r, tx: env.TX, audit: env.Audit}
	h := &handler[Row, Req, Resp]{svc: s, v: env.Validator}
	read, create, update, del := r.Table+":read", r.Table+":create", r.Table+":update", r.Table+":delete"

	g := env.Router.Group(r.Path)
	g.Get("", read, h.list)
	g.Post("", create, h.create)
	if r.Import {
		g.Post("/import", create, h.importCSV)
	}
	g.Get("/:uuid", read, h.get)
	g.Put("/:uuid", update, h.update)
	g.Delete("/:uuid", del, h.delete)
	for _, n := range r.Nested {
		env.Router.Group(n.ParentPath).Get("/:parent_uuid"+r.Path, read, h.nestedList(n.Filter))
	}
}

// Permissions returns only <table>:read.
func (r ReadResource[Row, Resp]) Permissions() []auth.Permission {
	return []auth.Permission{{Code: r.Table + ":read", Name: "Lihat " + r.Label}}
}

// Register mounts list, detail and nested list routes.
func (r ReadResource[Row, Resp]) Register(env Env) {
	h := &readHandler[Row, Resp]{res: r}
	read := r.Table + ":read"
	g := env.Router.Group(r.Path)
	g.Get("", read, h.list)
	g.Get("/:uuid", read, h.get)
	for _, n := range r.Nested {
		env.Router.Group(n.ParentPath).Get("/:parent_uuid"+r.Path, read, h.nestedList(n.Filter))
	}
}

func sortOptions(sorts []string) []string {
	out := make([]string, 0, len(sorts)*2)
	for _, s := range sorts {
		out = append(out, s, "-"+s)
	}
	return out
}
