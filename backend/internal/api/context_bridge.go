package api

import (
	"context"

	"hubgame/backend/internal/controller"
)

// WithClaimsForStorage bridges auth claims into a context key that storage controllers can use.
func WithClaimsForStorage(ctx context.Context, claims *controller.Claims) context.Context {
	return context.WithValue(ctx, contextKeyClaims{}, claims)
}

func claimsFromStorageContext(ctx context.Context) (*controller.Claims, bool) {
	claims, ok := ctx.Value(contextKeyClaims{}).(*controller.Claims)
	return claims, ok
}
