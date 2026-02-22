package auth

import (
	"context"

	"github.com/google/uuid"
)

type claimsContextKey struct{}

type ClaimsContextValue struct{}

var claimsContextToken = claimsContextKey{}

func WithSessionClaims(ctx context.Context, claims SessionClaims) context.Context {
	return context.WithValue(ctx, claimsContextToken, claims)
}

func SessionClaimsFromContext(ctx context.Context) (SessionClaims, bool) {
	claims, ok := ctx.Value(claimsContextToken).(SessionClaims)
	if !ok {
		return SessionClaims{}, false
	}
	return claims, true
}

func activeTenantFromClaims(claims SessionClaims) (uuid.UUID, bool) {
	if claims.ActiveTenantID == nil {
		return uuid.UUID{}, false
	}
	return *claims.ActiveTenantID, true
}
