package auth

import (
	"context"

	"github.com/google/uuid"
)

type claimsContextKey struct{}

// ClaimsContextValue is a placeholder type for claims context values.
type ClaimsContextValue struct{}

var claimsContextToken = claimsContextKey{}

// WithSessionClaims returns a new context with the given SessionClaims attached.
func WithSessionClaims(ctx context.Context, claims SessionClaims) context.Context {
	return context.WithValue(ctx, claimsContextToken, claims)
}

// SessionClaimsFromContext extracts SessionClaims from the context. Returns
// the claims and true if present, or a zero-value SessionClaims and false
// otherwise.
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
