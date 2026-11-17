package audit_test

import (
	"context"
	"errors"
	"testing"

	"pfm-backend/internal/db"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/testutil"
)

// Events follow the business transaction (a rollback leaves no trace), reach
// pfm_log only through the shipper, and a replay after a crash between the
// log insert and the outbox delete is ignored.
func TestOutboxShipsCommittedEventsOnce(t *testing.T) {
	core := database.NewTxManager(testutil.NewDB(t, db.Core))
	logs := database.NewTxManager(testutil.NewDB(t, db.Log))
	rec := audit.NewRecorder(core, db.Core)
	ctx := context.Background()
	count := func(tx *database.TxManager, q string) int {
		t.Helper()
		var n int
		if err := tx.Executor(ctx).QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if err := core.WithinTx(ctx, func(ctx context.Context) error {
		return rec.Record(ctx, audit.Entry{Action: "x.commit", Entity: "x"})
	}); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	_ = core.WithinTx(ctx, func(ctx context.Context) error {
		if err := rec.Record(ctx, audit.Entry{Action: "x.rollback", Entity: "x"}); err != nil {
			return err
		}
		return boom
	})
	if n := count(logs, "SELECT count(*) FROM activity_log"); n != 0 {
		t.Fatalf("events must not reach pfm_log before shipping, got %d", n)
	}

	s := audit.NewShipper(logs, map[string]*database.TxManager{db.Core: core})
	if n, err := s.ShipAll(ctx); err != nil || n != 1 {
		t.Fatalf("ship = %d, %v", n, err)
	}
	if n, err := s.ShipAll(ctx); err != nil || n != 0 {
		t.Fatalf("second ship = %d, %v", n, err)
	}
	var id string
	if err := logs.Executor(ctx).QueryRow(ctx, "SELECT uuid::text FROM activity_log WHERE log_action = 'x.commit' AND source = 'core'").Scan(&id); err != nil {
		t.Fatalf("shipped event: %v", err)
	}
	if n := count(logs, "SELECT count(*) FROM activity_log WHERE log_action = 'x.rollback'"); n != 0 {
		t.Fatal("rolled back event was logged")
	}
	if n := count(core, "SELECT count(*) FROM audit_outbox"); n != 0 {
		t.Fatalf("outbox not drained: %d", n)
	}

	// Replay: the same event again in the outbox is shipped without a duplicate.
	if _, err := core.Executor(ctx).Exec(ctx, "INSERT INTO audit_outbox (uuid, log_action, source) VALUES ($1, 'x.commit', 'core')", id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ShipAll(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(logs, "SELECT count(*) FROM activity_log WHERE log_action = 'x.commit'"); n != 1 {
		t.Fatalf("replayed event duplicated: %d", n)
	}
}
