package dto

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/platform/httpx"
)

// PlacementDetailItem is one tranche of a placement.
type PlacementDetailItem struct {
	Description         string `json:"description"           validate:"omitempty,max=255,safe_text"`
	PlacementDetailDate string `json:"placement_detail_date" validate:"required,datetime=2006-01-02"`
	PlacementValue      string `json:"placement_value"       validate:"required,decimal=20.4,dgt=0"`
	Status              *int16 `json:"status"                validate:"omitempty,min=0"`
}

// PlacementDetailsRequest replaces every detail of a placement at once.
type PlacementDetailsRequest struct {
	Details []PlacementDetailItem `json:"details" validate:"required,max=500,dive"`
}

func (r *PlacementDetailsRequest) Normalize() {
	for i := range r.Details {
		r.Details[i].Description = strings.TrimSpace(r.Details[i].Description)
		r.Details[i].PlacementDetailDate = strings.TrimSpace(r.Details[i].PlacementDetailDate)
		r.Details[i].PlacementValue = strings.TrimSpace(r.Details[i].PlacementValue)
	}
}

type PlacementDetailResponse struct {
	UUID                uuid.UUID        `json:"uuid"`
	Description         *string          `json:"description"`
	PlacementDetailDate *httpx.Date      `json:"placement_detail_date"`
	PlacementValue      *decimal.Decimal `json:"placement_value"`
	Status              *int16           `json:"status"`
	CreatedAt           time.Time        `json:"created_at"`
}

type PlacementDetailsResponse struct {
	PlacementUUID uuid.UUID                 `json:"placement_uuid"`
	Details       []PlacementDetailResponse `json:"details"`
}

func PlacementDetailsFromRows(placement uuid.UUID, rows []sqlc.ListPlacementDetailsRow) PlacementDetailsResponse {
	out := PlacementDetailsResponse{PlacementUUID: placement, Details: make([]PlacementDetailResponse, 0, len(rows))}
	for _, r := range rows {
		out.Details = append(out.Details, PlacementDetailResponse{UUID: r.Uuid, Description: r.Description,
			PlacementDetailDate: httpx.DateOf(r.PlacementDetailDate), PlacementValue: r.PlacementValue,
			Status: r.Status, CreatedAt: r.CreatedAt})
	}
	return out
}

// DepositoRolloverRequest overrides terms of the next placement; empty
// fields inherit from the current one.
type DepositoRolloverRequest struct {
	StartDate               string `json:"start_date"                validate:"omitempty,datetime=2006-01-02"`
	Tenor                   *int32 `json:"tenor"                     validate:"omitempty,min=1,max=600"`
	TotalDepositoAllocation string `json:"total_deposito_allocation" validate:"omitempty,decimal=20.4,dgt=0"`
	NetInterestRate         string `json:"net_interest_rate"         validate:"omitempty,decimal=12.6,dgte=0,dlte=100"`
}

// DepositoWithdrawRequest closes a placement on WithdrawalDate.
type DepositoWithdrawRequest struct {
	WithdrawalDate string `json:"withdrawal_date" validate:"required,datetime=2006-01-02"`
}
