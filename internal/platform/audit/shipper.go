package audit

import (
	"context"
	"fmt"

	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/platform/database"
)

// Shipper moves events from the outboxes of the source databases into
// pfm_log.activity_log. A batch is inserted into the log first and removed
// from the outbox afterwards; a crash in between only replays events, which
// the log ignores by uuid. Rows are claimed with SKIP LOCKED, so several
// workers may ship concurrently.
type Shipper struct {
	log     *database.TxManager
	sources map[string]*database.TxManager
	batch   int32
}

func NewShipper(log *database.TxManager, sources map[string]*database.TxManager) *Shipper {
	return &Shipper{log: log, sources: sources, batch: 500}
}

// ShipAll drains every outbox and returns the number of events shipped.
func (s *Shipper) ShipAll(ctx context.Context) (int, error) {
	total := 0
	for name, src := range s.sources {
		for {
			n, err := s.shipBatch(ctx, src)
			total += n
			if err != nil {
				return total, fmt.Errorf("ship %s outbox: %w", name, err)
			}
			if n < int(s.batch) {
				break
			}
		}
	}
	return total, nil
}

func (s *Shipper) shipBatch(ctx context.Context, src *database.TxManager) (int, error) {
	n := 0
	err := src.WithinTx(ctx, func(ctx context.Context) error {
		rows, err := src.Queries(ctx).ClaimAuditOutbox(ctx, s.batch)
		if err != nil || len(rows) == 0 {
			return err
		}
		if err := s.log.WithinTx(ctx, func(ctx context.Context) error {
			q := s.log.Queries(ctx)
			for _, r := range rows {
				if err := q.ShipActivityLog(ctx, sqlc.ShipActivityLogParams{Uuid: r.Uuid, ActionQuery: r.ActionQuery,
					IpAddress: r.IpAddress, LogAction: r.LogAction, LogDescription: r.LogDescription,
					LogSessionCode: r.LogSessionCode, LogTime: &r.LogTime, LogTitle: r.LogTitle, RoleCode: r.RoleCode,
					Username: r.Username, Source: &r.Source}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		ids := make([]int64, len(rows))
		for i, r := range rows {
			ids[i] = r.ID
		}
		n = len(rows)
		return src.Queries(ctx).DeleteAuditOutbox(ctx, ids)
	})
	return n, err
}
