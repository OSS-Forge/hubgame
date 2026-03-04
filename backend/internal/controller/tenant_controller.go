package controller

import (
	"context"
	"errors"

	"hubgame/backend/internal/database"
)

// TenantGuardController embeds auth-aware safety checks in DB operations.
type TenantGuardController struct{}

func (TenantGuardController) Name() string { return "tenant_guard" }

func (TenantGuardController) BeforeInsert(ctx context.Context, e *database.Entity) error {
	return enforceTenant(ctx, e.TenantID)
}

func (TenantGuardController) BeforeUpdate(ctx context.Context, current, next *database.Entity) error {
	if err := enforceTenant(ctx, current.TenantID); err != nil {
		return err
	}
	if current.TenantID != next.TenantID {
		return errors.New("tenant mutation is forbidden")
	}
	return nil
}

func (TenantGuardController) BeforeDelete(ctx context.Context, e *database.Entity) error {
	return enforceTenant(ctx, e.TenantID)
}

func (TenantGuardController) BeforeAppendEvent(ctx context.Context, event *database.Event) error {
	return enforceTenant(ctx, event.TenantID)
}

func enforceTenant(ctx context.Context, tenantID string) error {
	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		return errors.New("missing auth context")
	}
	if claims.TenantID != tenantID {
		return errors.New("cross-tenant access denied")
	}
	return nil
}
