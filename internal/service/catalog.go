package service

import (
	"context"

	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/repository"
)

// DefaultRoles are the roles present in the legacy data (PRD §4).
var DefaultRoles = []struct{ Code, Name string }{
	{auth.RoleAdmin, "ADMINISTRATOR"},
	{"FM", "FUND MANAGER"},
	{"MEMBER", "MEMBER"},
	{"AGENT", "MARKETING AGENT"},
	{"CLIENT-MNG", "CLIENT MANAGEMENT"},
	{"CLIENT-ADMIN", "CLIENT ADMINISTRATION"},
	{"CSPV", "CLIENT SUPERVISOR"},
}

// SyncCatalog makes sure every permission known to the code exists in `action`
// and that the default roles exist. Used by `cmd/migrate seed`.
func SyncCatalog(ctx context.Context, tx *database.TxManager, actions repository.ActionRepository, roles repository.RoleRepository, catalog []auth.Permission) error {
	return tx.WithinTx(ctx, func(ctx context.Context) error {
		for _, p := range catalog {
			if err := actions.EnsureCode(ctx, p.Code, p.Name, "system"); err != nil {
				return err
			}
		}
		for _, r := range DefaultRoles {
			if _, err := roles.UpsertByCode(ctx, r.Code, r.Name, "system"); err != nil {
				return err
			}
		}
		return nil
	})
}
