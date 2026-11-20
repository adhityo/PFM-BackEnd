package mail

import (
	"context"
	"fmt"
	"log/slog"
	"unicode/utf8"

	"github.com/google/uuid"

	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/platform/database"
)

// Queue writes messages to the mail_outbox of one database, inside the
// caller's transaction when there is one (the email commits with the change).
type Queue struct {
	tx *database.TxManager
}

func NewQueue(tx *database.TxManager) *Queue { return &Queue{tx: tx} }

func (q *Queue) Enqueue(ctx context.Context, m Message) error {
	if err := m.validate(); err != nil {
		return err
	}
	var html *string
	if m.HTML != "" {
		html = &m.HTML
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	err = q.tx.Queries(ctx).InsertMailOutbox(ctx, sqlc.InsertMailOutboxParams{Uuid: id, Recipient: m.To, Subject: m.Subject,
		BodyText: m.Text, BodyHtml: html})
	if err != nil {
		return fmt.Errorf("insert mail_outbox: %w", err)
	}
	return nil
}

// MaxAttempts is how often a message is tried before it is given up.
const MaxAttempts = 8

// Deliverer sends due messages of the source databases (worker job).
type Deliverer struct {
	sources map[string]*database.TxManager
	sender  Sender
	log     *slog.Logger
	batch   int32
}

func NewDeliverer(sources map[string]*database.TxManager, sender Sender, log *slog.Logger) *Deliverer {
	return &Deliverer{sources: sources, sender: sender, log: log, batch: 50}
}

// DeliverDue sends every due message once and returns how many were sent.
// A failed message is retried later with backoff; its error is recorded.
func (d *Deliverer) DeliverDue(ctx context.Context) (int, error) {
	sent := 0
	for name, src := range d.sources {
		var rows []sqlc.MailOutbox
		err := src.WithinTx(ctx, func(ctx context.Context) error {
			var err error
			rows, err = src.Queries(ctx).ClaimMailOutbox(ctx, sqlc.ClaimMailOutboxParams{MaxAttempts: MaxAttempts, Batch: d.batch})
			return err
		})
		if err != nil {
			return sent, fmt.Errorf("claim %s mail_outbox: %w", name, err)
		}
		for _, r := range rows {
			m := Message{To: r.Recipient, Subject: r.Subject, Text: r.BodyText}
			if r.BodyHtml != nil {
				m.HTML = *r.BodyHtml
			}
			if err := d.sender.Send(ctx, m); err != nil {
				d.log.Warn("kirim email gagal", "db", name, "mail", r.Uuid, "attempt", r.Attempts, "err", err)
				if err := src.Queries(ctx).RecordMailFailure(ctx, sqlc.RecordMailFailureParams{Error: clip500(err.Error()),
					MaxAttempts: MaxAttempts, ID: r.ID}); err != nil {
					return sent, err
				}
				continue
			}
			if err := src.Queries(ctx).DeleteMailOutbox(ctx, r.ID); err != nil {
				return sent, err
			}
			sent++
		}
	}
	return sent, nil
}

func clip500(s string) *string {
	if len(s) > 500 {
		s = s[:500]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return &s
}
