package service

import (
	"context"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
)

// Change request approval codes (*_data_history.approval_status).
const (
	ChangePending  int16 = 0
	ChangeApproved int16 = 1
	ChangeRejected int16 = 2
)

// ChangeRequestService implements maker-checker changes of investor and
// marketing agent data (PRD §10.5): a request stores the new values next to
// the current ones; approval by another user applies them in one transaction.
type ChangeRequestService struct {
	d     Deps
	tx    *database.TxManager
	audit *audit.Recorder
}

func NewChangeRequestService(d Deps, rec *audit.Recorder) *ChangeRequestService {
	return &ChangeRequestService{d: d, tx: d.TX, audit: rec}
}

// lockTarget locks the investor/agent and refuses a second pending request.
func (s *ChangeRequestService) lockTarget(ctx context.Context, lock func(context.Context, uuid.UUID) (int64, error),
	pending func(context.Context, *int64) (int64, error), id uuid.UUID) (int64, error) {
	target, err := lock(ctx, id)
	if err != nil {
		return 0, database.MapError("lock change target", err)
	}
	n, err := pending(ctx, &target)
	if err != nil {
		return 0, database.MapError("count pending changes", err)
	}
	if n > 0 {
		return 0, apperr.Conflict("masih ada pengajuan perubahan yang menunggu persetujuan")
	}
	return target, nil
}

// CreateInvestor files a change request for an investor.
func (s *ChangeRequestService) CreateInvestor(ctx context.Context, investor uuid.UUID, req dto.InvestorRequest) (uuid.UUID, error) {
	var out uuid.UUID
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.tx.Queries(ctx)
		target, err := s.lockTarget(ctx, q.LockInvestor, q.CountPendingInvestorChangeRequests, investor)
		if err != nil {
			return err
		}
		refs := map[string]**int64{}
		p := sqlc.CreateInvestorChangeRequestParams{Uuid: crud.NewID(), InvestorID: target, Actor: actorName(ctx),
			Status: req.Status, Address: crud.Text(req.Address), AddressCity: crud.Text(req.AddressCity),
			AddressPostalCode: crud.Text(req.AddressPostalCode), Bank: crud.Text(req.Bank),
			BankAccountName: crud.Text(req.BankAccountName), BankAccountNumber: crud.Text(req.BankAccountNumber),
			BankBranch: crud.Text(req.BankBranch), CorrespondingAddress: crud.Text(req.CorrespondingAddress),
			CorrespondingAddressCity:       crud.Text(req.CorrespondingAddressCity),
			CorrespondingAddressPostalCode: crud.Text(req.CorrespondingAddressPostalCode),
			Description:                    crud.Text(req.Description), Email: crud.Text(req.Email),
			IdentityNumber: crud.Text(req.IdentityNumber), InvestorName: crud.Text(req.InvestorName),
			MobilePhone: crud.Text(req.MobilePhone), Npwp: crud.Text(req.NPWP), Telephone: crud.Text(req.Telephone),
			MarketingAgentCode: crud.Text(req.MarketingAgentCode)}
		refs["city"], refs["country"] = &p.CityID, &p.CountryID
		refs["investor_category"], refs["province"] = &p.InvestorCategoryID, &p.ProvinceID
		raw := map[string]string{"city": req.CityUUID, "country": req.CountryUUID,
			"investor_category": req.InvestorCategoryUUID, "province": req.ProvinceUUID}
		for table, dst := range refs {
			if *dst, err = database.RefID(ctx, s.tx, table, raw[table], table+"_uuid"); err != nil {
				return err
			}
		}
		if out, err = q.CreateInvestorChangeRequest(ctx, p); err != nil {
			return database.MapError("create investor change", err)
		}
		return s.audit.Record(ctx, audit.Entry{Action: "investor_data_history.create", Entity: "investor_data_history",
			EntityUUID: out, Route: "POST /investors/{uuid}/change-requests"})
	})
	return out, err
}

// CreateAgent files a change request for a marketing agent.
func (s *ChangeRequestService) CreateAgent(ctx context.Context, agent uuid.UUID, req dto.MarketingAgentChangeRequest) (uuid.UUID, error) {
	var out uuid.UUID
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.tx.Queries(ctx)
		target, err := s.lockTarget(ctx, q.LockMarketingAgent, q.CountPendingAgentChangeRequests, agent)
		if err != nil {
			return err
		}
		out, err = q.CreateAgentChangeRequest(ctx, sqlc.CreateAgentChangeRequestParams{Uuid: crud.NewID(), AgentID: target,
			Actor: actorName(ctx), Status: req.Status, Address: crud.Text(req.Address), Bank: crud.Text(req.Bank),
			BankAccountName: crud.Text(req.BankAccountName), BankAccountNumber: crud.Text(req.BankAccountNumber),
			BankBranch: crud.Text(req.BankBranch), Birthdate: crud.Date(req.Birthdate), Description: crud.Text(req.Description),
			Email: crud.Text(req.Email), Handphone: crud.Text(req.Handphone), IdentityNumber: crud.Text(req.IdentityNumber),
			Location: crud.Text(req.Location), MarketingAgentName: crud.Text(req.MarketingAgentName), Npwp: crud.Text(req.NPWP),
			Telephone: crud.Text(req.Telephone)})
		if err != nil {
			return database.MapError("create agent change", err)
		}
		return s.audit.Record(ctx, audit.Entry{Action: "marketing_agent_data_history.create", Entity: "marketing_agent_data_history",
			EntityUUID: out, Route: "POST /marketing-agents/{uuid}/change-requests"})
	})
	return out, err
}

// decide validates a pending request against the checker (maker-checker and
// "target unchanged since the request") and runs apply/decide.
func (s *ChangeRequestService) decide(ctx context.Context, table, route string, id uuid.UUID, approve bool, note string,
	lock func(context.Context) (reqID int64, status *int16, maker *string, stale bool, err error),
	apply func(context.Context, int64, *string) error,
	mark func(context.Context, int64, *int16, *string, *string, *auth.Actor) error) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		reqID, status, maker, stale, err := lock(ctx)
		if err != nil {
			return database.MapError("lock change request", err)
		}
		if statusOf(status) != ChangePending {
			return apperr.Conflict("pengajuan perubahan sudah diputuskan")
		}
		if maker != nil && *maker == actor.AuditName() {
			return apperr.Forbidden("pengaju perubahan tidak boleh memutuskannya (maker-checker)")
		}
		name := actor.AuditName()
		decision, action := ChangeRejected, "reject"
		if approve {
			if stale {
				return apperr.Conflict("data sudah berubah sejak pengajuan; tolak dan ajukan ulang")
			}
			if err := apply(ctx, reqID, &name); err != nil {
				return database.MapError("apply change request", err)
			}
			decision, action = ChangeApproved, "approve"
		}
		if err := mark(ctx, reqID, &decision, crud.Text(note), &name, &actor); err != nil {
			return database.MapError("decide change request", err)
		}
		return s.audit.Record(ctx, audit.Entry{Action: table + "." + action, Entity: table, EntityUUID: id,
			Description: note, Route: "POST " + route + "/{uuid}/" + action})
	})
}

// DecideInvestor approves (applies) or rejects an investor change request.
func (s *ChangeRequestService) DecideInvestor(ctx context.Context, id uuid.UUID, approve bool, note string) error {
	q := func(ctx context.Context) *sqlc.Queries { return s.tx.Queries(ctx) }
	return s.decide(ctx, "investor_data_history", "/investor-data-histories", id, approve, note,
		func(ctx context.Context) (int64, *int16, *string, bool, error) {
			r, err := q(ctx).LockInvestorChangeRequest(ctx, id)
			return r.ID, r.ApprovalStatus, r.CreatedBy, r.TargetUpdatedAt.After(r.CreatedAt), err
		},
		func(ctx context.Context, reqID int64, actor *string) error {
			return q(ctx).ApplyInvestorChangeRequest(ctx, sqlc.ApplyInvestorChangeRequestParams{Actor: actor, ID: reqID})
		},
		func(ctx context.Context, reqID int64, decision *int16, note, name *string, a *auth.Actor) error {
			return q(ctx).DecideInvestorChangeRequest(ctx, sqlc.DecideInvestorChangeRequestParams{Decision: decision,
				Notes: note, Actor: name, Ip: a.IP, ID: reqID})
		})
}

// DecideAgent approves (applies) or rejects a marketing agent change request.
func (s *ChangeRequestService) DecideAgent(ctx context.Context, id uuid.UUID, approve bool, note string) error {
	q := func(ctx context.Context) *sqlc.Queries { return s.tx.Queries(ctx) }
	return s.decide(ctx, "marketing_agent_data_history", "/marketing-agent-data-histories", id, approve, note,
		func(ctx context.Context) (int64, *int16, *string, bool, error) {
			r, err := q(ctx).LockAgentChangeRequest(ctx, id)
			return r.ID, r.ApprovalStatus, r.CreatedBy, r.TargetUpdatedAt.After(r.CreatedAt), err
		},
		func(ctx context.Context, reqID int64, actor *string) error {
			return q(ctx).ApplyAgentChangeRequest(ctx, sqlc.ApplyAgentChangeRequestParams{Actor: actor, ID: reqID})
		},
		func(ctx context.Context, reqID int64, decision *int16, note, name *string, a *auth.Actor) error {
			return q(ctx).DecideAgentChangeRequest(ctx, sqlc.DecideAgentChangeRequestParams{Decision: decision,
				Notes: note, Actor: name, Ip: a.IP, ID: reqID})
		})
}

// View returns a change request (PII-masked) and its ETag time.
func (s *ChangeRequestService) View(ctx context.Context, table string, id uuid.UUID) (any, error) {
	if table == "investor_data_history" {
		r, _, err := InvestorDataHistoryResource(s.d).View(ctx, s.audit, id)
		return r, err
	}
	r, _, err := MarketingAgentDataHistoryResource(s.d).View(ctx, s.audit, id)
	return r, err
}
