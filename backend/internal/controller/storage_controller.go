package controller

import (
	"context"
	"errors"

	"hubgame/backend/internal/database"
)

type ClaimsExtractor func(context.Context) (*Claims, bool)

type StorageTenantController struct {
	extract ClaimsExtractor
}

func NewStorageTenantController(extract ClaimsExtractor) StorageTenantController {
	return StorageTenantController{extract: extract}
}

func (c StorageTenantController) Name() string { return "storage_tenant_controller" }

func (c StorageTenantController) BeforeInsert(ctx context.Context, e *database.Entity) error {
	return c.enforce(ctx, e.TenantID)
}

func (c StorageTenantController) BeforeUpdate(ctx context.Context, current, next *database.Entity) error {
	if err := c.enforce(ctx, current.TenantID); err != nil {
		return err
	}
	if current.TenantID != next.TenantID {
		return errors.New("tenant mutation is forbidden")
	}
	return nil
}

func (c StorageTenantController) BeforeDelete(ctx context.Context, e *database.Entity) error {
	return c.enforce(ctx, e.TenantID)
}

func (c StorageTenantController) BeforeAppendEvent(ctx context.Context, event *database.Event) error {
	return c.enforce(ctx, event.TenantID)
}

func (c StorageTenantController) enforce(ctx context.Context, tenantID string) error {
	claims, ok := c.extract(ctx)
	if !ok {
		return errors.New("missing auth context")
	}
	if claims.TenantID != tenantID {
		return errors.New("cross-tenant access denied")
	}
	return nil
}
