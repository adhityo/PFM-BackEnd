package service

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/httpx"
)

const setupRoute = "POST /portfolios/setup"

// PortfolioSetupService creates a portfolio ready for trading in one
// transaction: the portfolio, its account mapping per asset class, cash
// accounts per trade type and bank accounts. Every row goes through the
// pipeline of its own CRUD endpoint (rules, scope, hooks, audit).
type PortfolioSetupService struct {
	env        crud.Env
	v          httpx.Validator
	portfolios crud.Resource[sqlc.GetPortfolioByUUIDRow, dto.PortfolioRequest, dto.PortfolioResponse]
	mappings   crud.Resource[sqlc.GetAssetClassFinanceAccountByUUIDRow, dto.AssetClassFinanceAccountRequest, dto.AssetClassFinanceAccountResponse]
	structures crud.Resource[sqlc.GetPortfolioAccountingStructureByUUIDRow, dto.PortfolioAccountingStructureRequest, dto.PortfolioAccountingStructureResponse]
	banks      crud.Resource[sqlc.GetPortfolioBankByUUIDRow, dto.PortfolioBankRequest, dto.PortfolioBankResponse]
}

func NewPortfolioSetupService(d Deps, rec *audit.Recorder, v httpx.Validator) *PortfolioSetupService {
	return &PortfolioSetupService{env: crud.Env{TX: d.TX, Audit: rec, Files: d.Files}, v: v,
		portfolios: PortfolioResource(d), mappings: AssetClassFinanceAccountResource(d),
		structures: PortfolioAccountingStructureResource(d), banks: PortfolioBankResource(d)}
}

// authorize requires the create permission of every kind of row in the request
// (portfolio:create is checked by the route).
func (s *PortfolioSetupService) authorize(ctx context.Context, req dto.PortfolioSetupRequest) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	need := map[string]bool{}
	for _, a := range req.AssetClasses {
		need[s.mappings.Table+":create"] = true
		if len(a.CashAccounts) > 0 {
			need[s.structures.Table+":create"] = true
		}
	}
	if len(req.Banks) > 0 {
		need[s.banks.Table+":create"] = true
	}
	var missing []string
	for _, code := range []string{s.mappings.Table + ":create", s.structures.Table + ":create", s.banks.Table + ":create"} {
		if need[code] && !actor.Can(code) {
			missing = append(missing, code)
		}
	}
	if len(missing) > 0 {
		return apperr.MissingPermission(missing...)
	}
	return nil
}

// validate checks every item with the rules of its CRUD endpoint before
// anything is written, so all field errors are reported at once.
func (s *PortfolioSetupService) validate(req dto.PortfolioSetupRequest) error {
	placeholder := uuid.Nil.String() // the portfolio does not exist yet
	var fields []apperr.FieldError
	check := func(path string, r any, skip ...string) {
		err := s.v.Validate(r)
		if err == nil {
			return
		}
		for _, f := range prefixFields(path, err) {
			if !slices.ContainsFunc(skip, func(suffix string) bool { return strings.HasSuffix(f.Field, suffix) }) {
				fields = append(fields, f)
			}
		}
	}
	for i, a := range req.AssetClasses {
		path := fmt.Sprintf("asset_classes[%d]", i)
		m := a.Mapping(placeholder)
		check(path, &m)
		for j, c := range a.CashAccounts {
			st := c.Structure(placeholder, a.AssetClassUUID)
			// asset_class_uuid is reported once, on the asset class itself.
			check(fmt.Sprintf("%s.cash_accounts[%d]", path, j), &st, ".asset_class_uuid")
		}
	}
	for i, b := range req.Banks {
		r := b.Request(placeholder)
		check(fmt.Sprintf("banks[%d]", i), &r)
	}
	if len(fields) > 0 {
		return apperr.Validation(fields...)
	}
	return nil
}

// prefixFields places the field errors of an item at its path in the request.
func prefixFields(path string, err error) []apperr.FieldError {
	e, ok := apperr.As(err)
	if !ok || len(e.Fields) == 0 {
		msg := "data tidak valid"
		if ok {
			msg = e.Message
		}
		return []apperr.FieldError{{Field: path, Rule: "invalid", Message: msg}}
	}
	out := make([]apperr.FieldError, 0, len(e.Fields))
	for _, f := range e.Fields {
		f.Field = path + "." + f.Field
		out = append(out, f)
	}
	return out
}

// at attributes a write-time error (reference, rule, uniqueness) to its item.
func at(path string, err error) error {
	e, ok := apperr.As(err)
	if !ok {
		return err
	}
	switch e.Kind {
	case apperr.KindValidation:
		return apperr.Validation(prefixFields(path, err)...)
	case apperr.KindConflict:
		return apperr.Conflict(path + ": " + e.Message)
	default:
		return err
	}
}

// Setup creates everything or nothing. It returns the created rows and the
// portfolio's updated_at (ETag).
func (s *PortfolioSetupService) Setup(ctx context.Context, req dto.PortfolioSetupRequest) (dto.PortfolioSetupResponse, time.Time, error) {
	out := dto.PortfolioSetupResponse{AssetClassFinanceAccounts: []dto.AssetClassFinanceAccountResponse{},
		PortfolioAccountingStructures: []dto.PortfolioAccountingStructureResponse{}, PortfolioBanks: []dto.PortfolioBankResponse{}}
	if err := s.authorize(ctx, req); err != nil {
		return out, time.Time{}, err
	}
	if err := s.validate(req); err != nil {
		return out, time.Time{}, err
	}
	var updatedAt time.Time
	err := s.env.TX.WithinTx(ctx, func(ctx context.Context) error {
		p, meta, err := s.portfolios.Insert(ctx, s.env, req.Portfolio, setupRoute)
		if err != nil {
			return at("portfolio", err)
		}
		out.Portfolio, updatedAt = p, meta.UpdatedAt
		id := meta.UUID.String()
		for i, a := range req.AssetClasses {
			path := fmt.Sprintf("asset_classes[%d]", i)
			m, _, err := s.mappings.Insert(ctx, s.env, a.Mapping(id), setupRoute)
			if err != nil {
				return at(path, err)
			}
			out.AssetClassFinanceAccounts = append(out.AssetClassFinanceAccounts, m)
			for j, c := range a.CashAccounts {
				st, _, err := s.structures.Insert(ctx, s.env, c.Structure(id, a.AssetClassUUID), setupRoute)
				if err != nil {
					return at(fmt.Sprintf("%s.cash_accounts[%d]", path, j), err)
				}
				out.PortfolioAccountingStructures = append(out.PortfolioAccountingStructures, st)
			}
		}
		for i, b := range req.Banks {
			r, _, err := s.banks.Insert(ctx, s.env, b.Request(id), setupRoute)
			if err != nil {
				return at(fmt.Sprintf("banks[%d]", i), err)
			}
			out.PortfolioBanks = append(out.PortfolioBanks, r)
		}
		return nil
	})
	return out, updatedAt, err
}
