// Package audit records audit events (PRD §12.6). Events are written to the
// audit_outbox of the database whose transaction caused them, so they commit
// or roll back with the business change; Shipper then moves them to the
// separate, append-only pfm_log database.
package audit

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"

	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/httpx"
)

// Recorder persists audit events into one database's outbox.
type Recorder struct {
	tx     *database.TxManager
	source string
}

// NewRecorder writes to the outbox of tx's database; source is its name
// ("core", "security" or "document").
func NewRecorder(tx *database.TxManager, source string) *Recorder {
	return &Recorder{tx: tx, source: source}
}

// Entry describes one audited action. Description must not contain PII or secrets.
type Entry struct {
	Action      string // e.g. "app_user.create"
	Entity      string // table name
	EntityUUID  uuid.UUID
	Description string
	Route       string
}

// Record writes the entry using the actor from ctx (or the system actor).
func (r *Recorder) Record(ctx context.Context, e Entry) error {
	actor, ok := auth.ActorFrom(ctx)
	if !ok {
		actor = auth.SystemActor()
	}
	return r.RecordAs(ctx, actor, e)
}

// RecordAs writes the entry for an explicit actor (used before an actor exists, e.g. login).
func (r *Recorder) RecordAs(ctx context.Context, actor auth.Actor, e Entry) error {
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	username := actor.Username
	if actor.UserUUID != uuid.Nil {
		username = actor.UserUUID.String()
	}
	title := e.Entity
	if e.EntityUUID != uuid.Nil {
		title = e.Entity + ":" + e.EntityUUID.String()
	}
	rid := actor.RequestID
	if rid == "" {
		rid = httpx.RequestIDFromContext(ctx)
	}
	err = r.tx.Queries(ctx).InsertAuditOutbox(ctx, sqlc.InsertAuditOutboxParams{
		Uuid:           id,
		ActionQuery:    nonEmpty(clip(e.Route)),
		IpAddress:      actor.IP,
		LogAction:      &e.Action,
		LogDescription: nonEmpty(clip(e.Description)),
		LogSessionCode: nonEmpty(rid),
		LogTitle:       nonEmpty(clip(title)),
		RoleCode:       nonEmpty(actor.RoleCode),
		Username:       nonEmpty(username),
		Source:         r.source,
	})
	if err != nil {
		return fmt.Errorf("insert audit_outbox: %w", err)
	}
	return nil
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// clip keeps values within VARCHAR(255) without splitting a UTF-8 rune.
func clip(s string) string {
	if len(s) <= 255 {
		return s
	}
	s = s[:255]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
