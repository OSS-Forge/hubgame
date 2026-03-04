package api

import (
	"context"

	"hubgame/backend/internal/controller"
)

type storageClaimsKey struct{}

func withClaimsForStorage(ctx context.Context, claims *controller.Claims) context.Context {
	return context.WithValue(ctx, storageClaimsKey{}, claims)
}

// StorageClaimsExtractor is passed to the storage tenant controller.
func StorageClaimsExtractor(ctx context.Context) (*controller.Claims, bool) {
	claims, ok := ctx.Value(storageClaimsKey{}).(*controller.Claims)
	return claims, ok
}
