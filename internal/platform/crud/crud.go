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
	// CheckDelete runs inside the delete transaction before the soft delete.
	CheckDelete func(ctx context.Context, cur *Row) error
	// AfterWrite runs inside the write transaction after create (before == nil),
	// update, and delete (after == nil); e.g. to refresh a parent's latest_* columns.
	AfterWrite func(ctx context.Context, before, after *Row) error
	// Files are upload references attached (pending -> active) before writing.
	Files []FileField[Row, Req]
	// Import enables POST <Path>/import (CSV, all-or-nothing).
	Import bool
	Nested []Nested
	// Mask hides PII for actors without pii:read (PRD §12.2).
	Mask func(ctx context.Context, r *Resp)
}

// FileField describes a *_file_uuid request field backed by file_object.
type FileField[Row, Req any] struct {
	Field   string // JSON field name, used in error messages
	Purpose string // upload purpose, e.g. "cash_settlement.document"
	Get     func(*Req) string
	Current func(*Row) *uuid.UUID
}

// FileAttacher validates a pending upload and marks it active (service.FileService).
// Files live in pfm_document: the claim commits there before the caller's
// transaction, so a reference can never point to a file that cleanup removes.
type FileAttacher interface {
	Attach(ctx context.Context, fileUUID uuid.UUID, purpose, field string) error
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
	Mask        func(ctx context.Context, r *Resp)
}

// Env carries the shared dependencies needed to mount resources.
type Env struct {
	Router    *auth.Router
	Validator httpx.Validator
	TX        *database.TxManager
	Audit     *audit.Recorder
	Files     FileAttacher
}

// Registrar is implemented by every Resource and ReadResource instantiation.
type Registrar interface {
	Permissions() []auth.Permission
	Register(env Env)
}

// CanReadPII reports whether the actor in ctx may see unmasked PII.
func CanReadPII(ctx context.Context) bool {
	a, ok := auth.ActorFrom(ctx)
	return ok && a.Can(auth.PermPIIRead)
}

// present applies PII masking and logs unmasked access (PRD §12.2).
func present[Resp any](ctx context.Context, rec *audit.Recorder, table string, mask func(context.Context, *Resp), items ...*Resp) {
	if mask == nil {
		return
	}
	if CanReadPII(ctx) {
		if rec != nil {
			_ = rec.Record(ctx, audit.Entry{Action: table + ".pii_read", Entity: table,
				Description: "akses data pribadi tanpa masking"})
		}
		return
	}
	for _, it := range items {
		mask(ctx, it)
	}
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
	s := &service[Row, Req, Resp]{res: r, tx: env.TX, audit: env.Audit, files: env.Files}
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
	h := &readHandler[Row, Resp]{res: r, audit: env.Audit}
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

// View returns the (PII-masked) API representation of one row and its ETag
// time, for action endpoints that answer with the updated record.
func (r Resource[Row, Req, Resp]) View(ctx context.Context, rec *audit.Recorder, id uuid.UUID) (Resp, time.Time, error) {
	return view(ctx, rec, r.Table, r.Store, r.Response, r.Mask, id)
}

// View is Resource.View for read-only resources.
func (r ReadResource[Row, Resp]) View(ctx context.Context, rec *audit.Recorder, id uuid.UUID) (Resp, time.Time, error) {
	return view(ctx, rec, r.Table, r.Store, r.Response, r.Mask, id)
}

func view[Row, Resp any](ctx context.Context, rec *audit.Recorder, table string, store Reader[Row], response func(Row) Resp,
	mask func(context.Context, *Resp), id uuid.UUID) (Resp, time.Time, error) {
	row, err := store.Get(ctx, id)
	if err != nil {
		var zero Resp
		return zero, time.Time{}, err
	}
	out := response(row)
	present(ctx, rec, table, mask, &out)
	return out, store.Meta(row).UpdatedAt, nil
}
