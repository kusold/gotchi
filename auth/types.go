package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

const (
	DefaultSessionKey         = "auth"
	DefaultLoginPath          = "/auth/login"
	DefaultTenantPickerPath   = "/auth/tenants"
	DefaultPostLoginRedirect  = "/ui/profile"
	DefaultAuthorizePath      = "/oidc/authorize"
	DefaultCallbackPath       = "/oidc/callback"
	DefaultTenantsPath        = "/tenants"
	DefaultTenantSelectPath   = "/tenant/select"
	DefaultStateCookieName    = "oidc_state"
	DefaultCookieMaxAgeSecond = 3600
)

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type Identity struct {
	Issuer            string
	Subject           string
	Email             string
	EmailVerified     bool
	Username          string
	Name              string
	PreferredUsername string
	RawClaims         map[string]any
}

type UserRef struct {
	UserID  uuid.UUID
	Issuer  string
	Subject string
}

type Membership struct {
	TenantID   uuid.UUID
	TenantName string
	Role       Role
}

type TenantDisplay struct {
	TenantID uuid.UUID
	Name     string
}

type IdentityStore interface {
	ResolveOrProvisionUser(ctx context.Context, identity Identity) (UserRef, error)
	ListMemberships(ctx context.Context, userID uuid.UUID) ([]Membership, error)
	GetTenantDisplay(ctx context.Context, tenantID uuid.UUID) (TenantDisplay, error)
}

// Hooks is kept as a compatibility alias.
type Hooks = IdentityStore

type SessionClaims struct {
	Authenticated  bool
	UserID         uuid.UUID
	Issuer         string
	Subject        string
	ActiveTenantID *uuid.UUID
}

var ErrTenantSelectionRequired = errors.New("tenant selection required")
