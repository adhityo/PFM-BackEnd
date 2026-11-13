package service

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/repository"
)

// PortfolioTxService holds the multi-row portfolio transactions that do not
// fit plain CRUD: placement header-detail and deposito rollover/withdraw.
// Journal posting for these is added with the accounting module (M3).
type PortfolioTxService struct {
	tx         *database.TxManager
	audit      *audit.Recorder
	placements repository.PlacementRepository
	depositos  repository.DepositoPortfolioRepository
}

func NewPortfolioTxService(tx *database.TxManager, rec *audit.Recorder) *PortfolioTxService {
	return &PortfolioTxService{tx: tx, audit: rec,
		placements: repository.NewPlacementRepository(tx), depositos: repository.NewDepositoPortfolioRepository(tx)}
}

// PlacementDetails returns a placement and its active details.
func (s *PortfolioTxService) PlacementDetails(ctx context.Context, id uuid.UUID) (sqlc.GetPlacementByUUIDRow, []sqlc.ListPlacementDetailsRow, error) {
	p, err := s.placements.Get(ctx, id)
	if err != nil {
		return p, nil, err
	}
	rows, err := s.tx.Queries(ctx).ListPlacementDetails(ctx, &p.ID)
	return p, rows, database.MapError("list placement details", err)
}

// ReplacePlacementDetails swaps every detail in one transaction. Dates must
// fall inside the placement period and the total may not exceed its value.
func (s *PortfolioTxService) ReplacePlacementDetails(ctx context.Context, id uuid.UUID, req dto.PlacementDetailsRequest,
	expected time.Time) (sqlc.GetPlacementByUUIDRow, []sqlc.ListPlacementDetailsRow, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return sqlc.GetPlacementByUUIDRow{}, nil, err
	}
	var (
		p    sqlc.GetPlacementByUUIDRow
		rows []sqlc.ListPlacementDetailsRow
	)
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := database.LockForUpdate(ctx, s.tx, "placement", id); err != nil {
			return err
		}
		if p, err = s.placements.Get(ctx, id); err != nil {
			return err
		}
		if !p.UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		total := decimal.Zero
		for i, d := range req.Details {
			date := crud.Date(d.PlacementDetailDate)
			if (p.StartDate != nil && date.Before(*p.StartDate)) || (p.EndDate != nil && date.After(*p.EndDate)) {
				return apperr.Invalid("details["+strconv.Itoa(i)+"].placement_detail_date", "range", "tanggal di luar periode placement")
			}
			total = total.Add(*crud.Decimal(d.PlacementValue))
		}
		if p.PlacementValue != nil && total.GreaterThan(*p.PlacementValue) {
			return apperr.Invalid("details", "total", "total detail melebihi nilai placement")
		}
		q := s.tx.Queries(ctx)
		name := actor.AuditName()
		if err := q.SoftDeletePlacementDetails(ctx, sqlc.SoftDeletePlacementDetailsParams{PlacementID: &p.ID, Actor: &name}); err != nil {
			return database.MapError("clear placement details", err)
		}
		for _, d := range req.Details {
			if err := q.InsertPlacementDetail(ctx, sqlc.InsertPlacementDetailParams{Uuid: crud.NewID(), PlacementID: &p.ID,
				Description: crud.Text(d.Description), PlacementDetailDate: crud.Date(d.PlacementDetailDate),
				PlacementValue: crud.Decimal(d.PlacementValue), Status: d.Status, Actor: &name}); err != nil {
				return database.MapError("insert placement detail", err)
			}
		}
		if err := q.TouchPlacement(ctx, sqlc.TouchPlacementParams{ID: p.ID, Actor: &name}); err != nil {
			return database.MapError("touch placement", err)
		}
		if p, err = s.placements.Get(ctx, id); err != nil {
			return err
		}
		if rows, err = q.ListPlacementDetails(ctx, &p.ID); err != nil {
			return database.MapError("list placement details", err)
		}
		return s.audit.Record(ctx, audit.Entry{Action: "placement.replace_details", Entity: "placement", EntityUUID: id,
			Description: "jumlah detail=" + strconv.Itoa(len(rows)), Route: "PUT /placements/{uuid}/details"})
	})
	return p, rows, err
}

// lockDeposito locks a placement and checks scope, ETag and that it is still open.
func (s *PortfolioTxService) lockDeposito(ctx context.Context, id uuid.UUID, expected time.Time) (sqlc.GetDepositoPortfolioForUpdateRow, error) {
	cur, err := s.tx.Queries(ctx).GetDepositoPortfolioForUpdate(ctx, id)
	if err != nil {
		return cur, database.MapError("lock deposito portfolio", err)
	}
	if _, err := s.depositos.Get(ctx, id); err != nil { // tenant scope
		return cur, err
	}
	if !cur.UpdatedAt.Equal(expected) {
		return cur, apperr.ErrPreconditionFailed
	}
	if cur.NextPortfolioID != nil {
		return cur, apperr.Conflict("penempatan deposito sudah di-rollover")
	}
	return cur, nil
}

func (s *PortfolioTxService) snapshot(ctx context.Context, id int64, actor string) error {
	err := s.tx.Queries(ctx).SnapshotDepositoPortfolio(ctx, sqlc.SnapshotDepositoPortfolioParams{
		Uuid: crud.NewID(), Actor: actor, ID: id})
	return database.MapError("snapshot deposito portfolio", err)
}

// RolloverDeposito opens the next placement (tenor in months), links both and
// snapshots history, atomically. It returns the new placement.
func (s *PortfolioTxService) RolloverDeposito(ctx context.Context, id uuid.UUID, req dto.DepositoRolloverRequest,
	expected time.Time) (sqlc.GetDepositoPortfolioByUUIDRow, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return sqlc.GetDepositoPortfolioByUUIDRow{}, err
	}
	var out sqlc.GetDepositoPortfolioByUUIDRow
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.lockDeposito(ctx, id, expected)
		if err != nil {
			return err
		}
		start := crud.Date(req.StartDate)
		if start == nil {
			start = cur.PortfolioEndDate
		}
		if start == nil {
			return apperr.Invalid("start_date", "required", "wajib diisi karena penempatan lama belum memiliki tanggal akhir")
		}
		if cur.PortfolioStartDate != nil && start.Before(*cur.PortfolioStartDate) {
			return apperr.Invalid("start_date", "range", "tanggal mulai rollover sebelum penempatan lama dimulai")
		}
		tenor := req.Tenor
		if tenor == nil {
			tenor = cur.Tenor
		}
		if tenor == nil {
			return apperr.Invalid("tenor", "required", "wajib diisi")
		}
		end := start.AddDate(0, int(*tenor), 0)
		allocation := crud.Decimal(req.TotalDepositoAllocation)
		if allocation == nil {
			allocation = cur.TotalDepositoAllocation
		}
		rate := crud.Decimal(req.NetInterestRate)
		if rate == nil {
			rate = cur.NetInterestRate
		}
		year := int32(start.Year())
		name := actor.AuditName()
		next, err := s.tx.Queries(ctx).InsertDepositoRollover(ctx, sqlc.InsertDepositoRolloverParams{
			Uuid: crud.NewID(), DepositoID: cur.DepositoID, PortfolioID: cur.PortfolioID, TotalDepositoAllocation: allocation,
			Tenor: tenor, NetInterestRate: rate, YieldPercentage: cur.YieldPercentage, PreviousID: &cur.ID,
			PortfolioYear: &year, PortfolioStartDate: start, PortfolioEndDate: &end, Actor: &name,
		})
		if err != nil {
			return database.MapError("insert rollover", err)
		}
		closeAt := cur.PortfolioEndDate
		if closeAt == nil {
			closeAt = start
		}
		if err := s.tx.Queries(ctx).LinkDepositoRollover(ctx, sqlc.LinkDepositoRolloverParams{
			NextID: &next.ID, EndDate: closeAt, Actor: &name, ID: cur.ID}); err != nil {
			return database.MapError("link rollover", err)
		}
		for _, rid := range []int64{cur.ID, next.ID} {
			if err := s.snapshot(ctx, rid, name); err != nil {
				return err
			}
		}
		if out, err = s.depositos.Get(ctx, next.Uuid); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "deposito_portfolio.rollover", Entity: "deposito_portfolio",
			EntityUUID: id, Description: "penempatan baru=" + next.Uuid.String(), Route: "POST /deposito-portfolios/{uuid}/rollover"})
	})
	return out, err
}

// WithdrawDeposito closes an open placement on the withdrawal date.
func (s *PortfolioTxService) WithdrawDeposito(ctx context.Context, id uuid.UUID, req dto.DepositoWithdrawRequest,
	expected time.Time) (sqlc.GetDepositoPortfolioByUUIDRow, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return sqlc.GetDepositoPortfolioByUUIDRow{}, err
	}
	var out sqlc.GetDepositoPortfolioByUUIDRow
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.lockDeposito(ctx, id, expected)
		if err != nil {
			return err
		}
		date := crud.Date(req.WithdrawalDate)
		if cur.PortfolioStartDate != nil && date.Before(*cur.PortfolioStartDate) {
			return apperr.Invalid("withdrawal_date", "range", "tanggal pencairan sebelum tanggal penempatan")
		}
		name := actor.AuditName()
		if err := s.tx.Queries(ctx).CloseDepositoPortfolio(ctx, sqlc.CloseDepositoPortfolioParams{
			EndDate: date, Actor: &name, ID: cur.ID}); err != nil {
			return database.MapError("close deposito portfolio", err)
		}
		if err := s.snapshot(ctx, cur.ID, name); err != nil {
			return err
		}
		if out, err = s.depositos.Get(ctx, id); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "deposito_portfolio.withdraw", Entity: "deposito_portfolio",
			EntityUUID: id, Route: "POST /deposito-portfolios/{uuid}/withdraw"})
	})
	return out, err
}
