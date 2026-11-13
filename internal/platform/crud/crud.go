// Package crud is the shared service + HTTP layer for MASTER/CHILD resources
// (PRD §10.4). Each resource supplies a Store (its sqlc repository), DTOs and
// optional business hooks; transactions, locking, reference checks, audit,
// ETags and routing are handled here once.
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

// Store is the repository contract of a resource.
type Store[Row, Req any] interface {
	Get(ctx context.Context, id uuid.UUID) (Row, error)
	List(ctx context.Context, q httpx.ListQuery) ([]Row, int64, error)
	Create(ctx context.Context, req Req, actor string) (uuid.UUID, error)
	// Update and SoftDelete must return apperr.ErrPreconditionFailed when
	// updated_at no longer equals expected.
	Update(ctx context.Context, id uuid.UUID, req Req, actor string, expected time.Time) error
	SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) error
	Meta(row Row) Meta
}

// Nested exposes GET <ParentPath>/{parent_uuid}<Path> as a list filtered by Filter.
type Nested struct {
	ParentPath string
	Filter     string
}

// Resource describes one CRUD endpoint family backed by a table.
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
	Check  func(ctx context.Context, req *Req, cur *Row) error
	Nested []Nested
}

// Env carries the shared dependencies needed to mount resources.
type Env struct {
	Router    *auth.Router
	Validator httpx.Validator
	TX        *database.TxManager
	Audit     *audit.Recorder
}

// Registrar is implemented by every Resource instantiation.
type Registrar interface {
	Permissions() []auth.Permission
	Register(env Env)
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
	g.Get("/:uuid", read, h.get)
	g.Put("/:uuid", update, h.update)
	g.Delete("/:uuid", del, h.delete)
	for _, n := range r.Nested {
		env.Router.Group(n.ParentPath).Get("/:parent_uuid"+r.Path, read, h.nestedList(n.Filter))
	}
}

func (r Resource[Row, Req, Resp]) sortOptions() []string {
	out := make([]string, 0, len(r.Sorts)*2)
	for _, s := range r.Sorts {
		out = append(out, s, "-"+s)
	}
	return out
}
