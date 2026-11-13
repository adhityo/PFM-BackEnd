package repository

import (
	"context"

	"github.com/google/uuid"

	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/database"
)

// ActivityLogRepository reads activity_log.
type ActivityLogRepository interface {
	GetByUUID(ctx context.Context, id uuid.UUID) (model.ActivityLog, error)
	List(ctx context.Context, f model.ActivityLogFilter, p model.Paging) ([]model.ActivityLog, int64, error)
}

type activityLogRepository struct{ tx *database.TxManager }

func NewActivityLogRepository(tx *database.TxManager) ActivityLogRepository {
	return &activityLogRepository{tx: tx}
}

func toActivityLog(a sqlc.ActivityLog) model.ActivityLog {
	return model.ActivityLog{UUID: a.Uuid, ActionQuery: a.ActionQuery, IPAddress: a.IpAddress, LogAction: a.LogAction,
		LogDescription: a.LogDescription, LogSessionCode: a.LogSessionCode, LogTime: a.LogTime, LogTitle: a.LogTitle,
		RoleCode: a.RoleCode, Username: a.Username}
}

func (r *activityLogRepository) GetByUUID(ctx context.Context, id uuid.UUID) (model.ActivityLog, error) {
	row, err := r.tx.Queries(ctx).GetActivityLogByUUID(ctx, id)
	if err != nil {
		return model.ActivityLog{}, database.MapError("get activity log", err)
	}
	return toActivityLog(row), nil
}

func (r *activityLogRepository) List(ctx context.Context, f model.ActivityLogFilter, p model.Paging) ([]model.ActivityLog, int64, error) {
	q := r.tx.Queries(ctx)
	rows, err := q.ListActivityLogs(ctx, sqlc.ListActivityLogsParams{Username: f.Username, LogAction: f.LogAction,
		From: f.From, To: f.To, Sort: p.Sort, Limit: p.Limit, Offset: p.Offset})
	if err != nil {
		return nil, 0, database.MapError("list activity logs", err)
	}
	total, err := q.CountActivityLogs(ctx, sqlc.CountActivityLogsParams{Username: f.Username, LogAction: f.LogAction, From: f.From, To: f.To})
	if err != nil {
		return nil, 0, database.MapError("count activity logs", err)
	}
	out := make([]model.ActivityLog, 0, len(rows))
	for _, row := range rows {
		out = append(out, toActivityLog(row))
	}
	return out, total, nil
}
