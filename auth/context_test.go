package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithSessionClaims(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	claims := SessionClaims{
		Authenticated:  true,
		UserID:         userID,
		Issuer:         "test-issuer",
		Subject:        "test-subject",
		ActiveTenantID: &tenantID,
	}

	ctx := context.Background()
	ctxWithClaims := WithSessionClaims(ctx, claims)

	// Verify the claims are in the context
	retrievedClaims, ok := SessionClaimsFromContext(ctxWithClaims)
	require.True(t, ok, "should be able to retrieve claims from context")
	assert.Equal(t, claims, retrievedClaims)
}

func TestSessionClaimsFromContext(t *testing.T) {
	t.Run("returns claims when present", func(t *testing.T) {
		userID := uuid.New()
		tenantID := uuid.New()
		claims := SessionClaims{
			Authenticated:  true,
			UserID:         userID,
			Issuer:         "test-issuer",
			Subject:        "test-subject",
			ActiveTenantID: &tenantID,
		}

		ctx := WithSessionClaims(context.Background(), claims)
		retrievedClaims, ok := SessionClaimsFromContext(ctx)

		assert.True(t, ok, "should return true when claims are present")
		assert.Equal(t, claims.Authenticated, retrievedClaims.Authenticated)
		assert.Equal(t, claims.UserID, retrievedClaims.UserID)
		assert.Equal(t, claims.Issuer, retrievedClaims.Issuer)
		assert.Equal(t, claims.Subject, retrievedClaims.Subject)
		require.NotNil(t, retrievedClaims.ActiveTenantID)
		assert.Equal(t, tenantID, *retrievedClaims.ActiveTenantID)
	})

	t.Run("returns false when claims not present", func(t *testing.T) {
		ctx := context.Background()
		claims, ok := SessionClaimsFromContext(ctx)

		assert.False(t, ok, "should return false when claims are not present")
		assert.Equal(t, SessionClaims{}, claims)
	})

	t.Run("returns false when wrong type in context", func(t *testing.T) {
		// Use a different context key to simulate wrong type
		ctx := context.WithValue(context.Background(), struct{}{}, "not-a-claims-struct")
		claims, ok := SessionClaimsFromContext(ctx)

		assert.False(t, ok, "should return false when wrong type is in context")
		assert.Equal(t, SessionClaims{}, claims)
	})
}

func TestSessionClaims_RoundTrip(t *testing.T) {
	testCases := []struct {
		name   string
		claims SessionClaims
	}{
		{
			name: "full claims with tenant",
			claims: SessionClaims{
				Authenticated:  true,
				UserID:         uuid.New(),
				Issuer:         "https://example.com",
				Subject:        "user-123",
				ActiveTenantID: ptrUUID(uuid.New()),
			},
		},
		{
			name: "claims without tenant",
			claims: SessionClaims{
				Authenticated:  true,
				UserID:         uuid.New(),
				Issuer:         "https://example.com",
				Subject:        "user-456",
				ActiveTenantID: nil,
			},
		},
		{
			name: "unauthenticated claims",
			claims: SessionClaims{
				Authenticated:  false,
				UserID:         uuid.UUID{},
				Issuer:         "",
				Subject:        "",
				ActiveTenantID: nil,
			},
		},
		{
			name:   "empty claims",
			claims: SessionClaims{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = WithSessionClaims(ctx, tc.claims)

			retrieved, ok := SessionClaimsFromContext(ctx)
			require.True(t, ok, "should successfully retrieve claims")
			assert.Equal(t, tc.claims, retrieved, "round trip should preserve all fields")
		})
	}
}

func TestActiveTenantFromClaims(t *testing.T) {
	t.Run("returns tenant ID when present", func(t *testing.T) {
		tenantID := uuid.New()
		claims := SessionClaims{
			Authenticated:  true,
			UserID:         uuid.New(),
			ActiveTenantID: &tenantID,
		}

		result, ok := activeTenantFromClaims(claims)

		assert.True(t, ok, "should return true when ActiveTenantID is present")
		assert.Equal(t, tenantID, result)
	})

	t.Run("returns false when tenant ID is nil", func(t *testing.T) {
		claims := SessionClaims{
			Authenticated:  true,
			UserID:         uuid.New(),
			ActiveTenantID: nil,
		}

		result, ok := activeTenantFromClaims(claims)

		assert.False(t, ok, "should return false when ActiveTenantID is nil")
		assert.Equal(t, uuid.UUID{}, result)
	})

	t.Run("returns false for empty claims", func(t *testing.T) {
		claims := SessionClaims{}

		result, ok := activeTenantFromClaims(claims)

		assert.False(t, ok)
		assert.Equal(t, uuid.UUID{}, result)
	})
}

// Helper function to create a pointer to a uuid.UUID
func ptrUUID(id uuid.UUID) *uuid.UUID {
	return &id
}
