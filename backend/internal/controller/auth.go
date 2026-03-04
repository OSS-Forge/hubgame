package controller

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID   string `json:"uid"`
	TenantID string `json:"tid"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type AuthController struct {
	secret []byte
	issuer string
}

func NewAuthController(secret, issuer string) *AuthController {
	return &AuthController{secret: []byte(secret), issuer: issuer}
}

func (a *AuthController) IssueToken(userID, tenantID, role string, ttl time.Duration) (string, error) {
	claims := Claims{
		UserID:   userID,
		TenantID: tenantID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    a.issuer,
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.secret)
}

func (a *AuthController) ParseToken(tokenString string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("invalid signing method")
		}
		return a.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	if claims.Issuer != a.issuer {
		return nil, errors.New("invalid issuer")
	}
	return claims, nil
}

type contextKey string

const claimsCtxKey contextKey = "auth.claims"

func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsCtxKey).(*Claims)
	return claims, ok
}

func (a *AuthController) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		claims, err := a.ParseToken(parts[1])
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), claimsCtxKey, claims))
		next.ServeHTTP(w, r)
	})
}
