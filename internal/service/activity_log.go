package service

import (
	"context"

	"github.com/google/uuid"

	"pfm-backend/internal/dto"
	"pfm-backend/internal/model"
	"pfm-backend/internal/repository"
)

// ActivityLogService reads the audit trail.
type ActivityLogService struct {
	logs repository.ActivityLogRepository
}

func NewActivityLogService(logs repository.ActivityLogRepository) *ActivityLogService {
	return &ActivityLogService{logs: logs}
}

// ActivityLogSorts is the sort whitelist for activity logs.
var ActivityLogSorts = []string{"-log_time", "log_time"}

func (s *ActivityLogService) List(ctx context.Context, f model.ActivityLogFilter, p model.Paging) ([]dto.ActivityLogResponse, int64, error) {
	logs, total, err := s.logs.List(ctx, f, p)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.ActivityLogResponse, 0, len(logs))
	for _, l := range logs {
		out = append(out, dto.ActivityLogFromModel(l))
	}
	return out, total, nil
}

func (s *ActivityLogService) Get(ctx context.Context, id uuid.UUID) (dto.ActivityLogResponse, error) {
	l, err := s.logs.GetByUUID(ctx, id)
	if err != nil {
		return dto.ActivityLogResponse{}, err
	}
	return dto.ActivityLogFromModel(l), nil
}
