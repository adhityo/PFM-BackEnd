package service

import (
	"context"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/repository"
)

// Anti-escalation rule (PRD §12.2): a non-ADMIN actor may only manage roles
// and users whose permissions it holds itself, and may only grant or assign
// permissions it holds. Raw role_action rows are used, regardless of the
// role status, so activating a role later cannot bypass the rule. ADMIN
// (implicitly every permission) is protected separately.

// lacking returns the codes the actor does not hold (none for ADMIN).
func lacking(actor auth.Actor, codes []string) []string {
	if actor.IsAdmin() {
		return nil
	}
	var out []string
	for _, c := range codes {
		if !actor.Can(c) {
			out = append(out, c)
		}
	}
	return out
}

// roleCodes returns every permission code attached to a role.
func roleCodes(ctx context.Context, roles repository.RoleRepository, roleID int64) ([]string, error) {
	actions, err := roles.Actions(ctx, roleID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		out = append(out, a.ActionCode)
	}
	return out, nil
}

// beyondActor is the 403 for permissions the actor would need to hold.
func beyondActor(msg string, codes []string) error {
	e := apperr.MissingPermission(codes...)
	e.Message = msg
	return e
}

// requireCovers fails when the role holds permissions the actor lacks.
func requireCovers(ctx context.Context, roles repository.RoleRepository, actor auth.Actor, roleID int64, msg string) error {
	if actor.IsAdmin() {
		return nil
	}
	codes, err := roleCodes(ctx, roles, roleID)
	if err != nil {
		return err
	}
	if missing := lacking(actor, codes); len(missing) > 0 {
		return beyondActor(msg, missing)
	}
	return nil
}
