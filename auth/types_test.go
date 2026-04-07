package auth

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestDefaultConstants(t *testing.T) {
	// Verify all default constants are set correctly
	assert.Equal(t, "auth", DefaultSessionKey)
	assert.Equal(t, "/auth/login", DefaultLoginPath)
	assert.Equal(t, "/auth/tenants", DefaultTenantPickerPath)
	assert.Equal(t, "/ui/profile", DefaultPostLoginRedirect)
	assert.Equal(t, "/oidc/authorize", DefaultAuthorizePath)
	assert.Equal(t, "/oidc/callback", DefaultCallbackPath)
	assert.Equal(t, "/tenants", DefaultTenantsPath)
	assert.Equal(t, "/tenant/select", DefaultTenantSelectPath)
	assert.Equal(t, "oidc_state", DefaultStateCookieName)
	assert.Equal(t, 3600, DefaultCookieMaxAgeSecond)
}

func TestRoleConstants(t *testing.T) {
	// Verify role constants
	assert.Equal(t, Role("owner"), RoleOwner)
	assert.Equal(t, Role("admin"), RoleAdmin)
	assert.Equal(t, Role("member"), RoleMember)
}

func TestErrTenantSelectionRequired(t *testing.T) {
	// Verify the error exists and has the correct message
	assert.NotNil(t, ErrTenantSelectionRequired)
	assert.Equal(t, "tenant selection required", ErrTenantSelectionRequired.Error())

	// Verify it can be used with errors.Is
	err := ErrTenantSelectionRequired
	assert.True(t, errors.Is(err, ErrTenantSelectionRequired))
}

func TestRoleType(t *testing.T) {
	// Verify Role type is a string
	var r Role = "custom"
	assert.Equal(t, Role("custom"), r)

	// Verify role constants can be used as Role type
	var owner Role = RoleOwner
	var admin Role = RoleAdmin
	var member Role = RoleMember

	assert.Equal(t, RoleOwner, owner)
	assert.Equal(t, RoleAdmin, admin)
	assert.Equal(t, RoleMember, member)
}

func TestSessionClaims_Fields(t *testing.T) {
	// This test ensures all fields on SessionClaims are accessible
	// and have the expected types (compilation check)
	var emptyUserID interface{} = uuid.UUID{}
	_ = emptyUserID

	claims := SessionClaims{
		Authenticated:  true,
		UserID:         uuid.UUID{},
		Issuer:         "test",
		Subject:        "sub",
		ActiveTenantID: nil,
	}

	assert.True(t, claims.Authenticated)
	assert.Equal(t, "test", claims.Issuer)
	assert.Equal(t, "sub", claims.Subject)
	assert.Nil(t, claims.ActiveTenantID)
}

func TestIdentity_Fields(t *testing.T) {
	// This test ensures all fields on Identity are accessible
	identity := Identity{
		Issuer:            "issuer",
		Subject:           "subject",
		Email:             "test@example.com",
		EmailVerified:     true,
		Username:          "testuser",
		Name:              "Test User",
		PreferredUsername: "preferred",
		RawClaims:         map[string]any{"key": "value"},
	}

	assert.Equal(t, "issuer", identity.Issuer)
	assert.Equal(t, "subject", identity.Subject)
	assert.Equal(t, "test@example.com", identity.Email)
	assert.True(t, identity.EmailVerified)
	assert.Equal(t, "testuser", identity.Username)
	assert.Equal(t, "Test User", identity.Name)
	assert.Equal(t, "preferred", identity.PreferredUsername)
	assert.NotNil(t, identity.RawClaims)
}

func TestMembership_Fields(t *testing.T) {
	// This test ensures all fields on Membership are accessible
	membership := Membership{
		TenantID:   uuid.UUID{},
		TenantName: "test-tenant",
		Role:       RoleAdmin,
	}

	assert.Equal(t, "test-tenant", membership.TenantName)
	assert.Equal(t, RoleAdmin, membership.Role)
}

func TestTenantDisplay_Fields(t *testing.T) {
	// This test ensures all fields on TenantDisplay are accessible
	display := TenantDisplay{
		TenantID: uuid.UUID{},
		Name:     "My Tenant",
	}

	assert.Equal(t, "My Tenant", display.Name)
}
