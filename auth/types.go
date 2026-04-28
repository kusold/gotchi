// Package auth provides OpenID Connect (OIDC) authentication and multi-tenant
// identity management for gotchi applications.
//
// The package handles the complete OIDC authorization code flow: redirecting
// users to an identity provider, exchanging authorization codes for tokens,
// resolving or provisioning local user records, and managing session state
// including tenant selection for multi-tenant applications.
//
// # Quick Start
//
// The typical flow is orchestrated by the [app] package, but you can use the
// auth package directly:
//
//	// Create a PostgreSQL identity store.
//	store, err := auth.NewPostgresIdentityStore(pool, auth.PostgresStoreConfig{})
//
//	// Create the OIDC handler to manage authentication routes.
//	handler, err := auth.NewOIDCHandler(auth.Config{
//	    Enabled:      true,
//	    IssuerURL:    "https://auth.example.com",
//	    ClientID:     "my-client",
//	    ClientSecret: "my-secret",
//	    RedirectURL:  "https://myapp.com/oidc/callback",
//	}, sessionMgr, store)
//
//	// Register OIDC routes on your Chi router.
//	handler.RegisterRoutes(r)
//
//	// Protect routes with authentication middleware.
//	r.Group(func(protected chi.Router) {
//	    protected.Use(auth.RequireAuthenticated(sessionMgr, auth.MiddlewareConfig{}))
//	    protected.Get("/profile", profileHandler)
//	})
//
// # Key Concepts
//
//   - [OIDCHandler] manages the OIDC authorization code flow, including the
//     authorize redirect, callback handling, and tenant selection.
//   - [IdentityStore] is the interface for resolving OIDC identities to local
//     users and managing tenant memberships. Use [PostgresIdentityStore] for
//     PostgreSQL-backed storage.
//   - [SessionClaims] holds the authenticated user's session data, including
//     the active tenant ID, and is stored in the session.
//   - [RequireAuthenticated] is middleware that enforces authentication and
//     optionally requires tenant selection before allowing access.
package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Default configuration values for auth paths and settings.
const (
	// DefaultSessionKey is the default session key used to store auth claims.
	DefaultSessionKey = "auth"
	// DefaultLoginPath is the default path for the login page.
	DefaultLoginPath = "/auth/login"
	// DefaultTenantPickerPath is the default path for the tenant selection UI.
	DefaultTenantPickerPath = "/auth/tenants"
	// DefaultPostLoginRedirect is the default URL to redirect to after login.
	DefaultPostLoginRedirect = "/ui/profile"
	// DefaultAuthorizePath is the default path that initiates the OIDC authorize flow.
	DefaultAuthorizePath = "/oidc/authorize"
	// DefaultCallbackPath is the default OIDC callback path.
	DefaultCallbackPath = "/oidc/callback"
	// DefaultTenantsPath is the default API path for listing a user's tenants.
	DefaultTenantsPath = "/tenants"
	// DefaultTenantSelectPath is the default API path for selecting an active tenant.
	DefaultTenantSelectPath = "/tenant/select"
	// DefaultLogoutPath is the default path for the OIDC logout endpoint.
	DefaultLogoutPath = "/oidc/logout"
	// DefaultPostLogoutRedirect is the default URL to redirect to after logout.
	DefaultPostLogoutRedirect = "/"
	// DefaultStateCookieName is the default cookie name for the OIDC state parameter.
	DefaultStateCookieName = "oidc_state"
	// DefaultCookieMaxAgeSecond is the default max age (in seconds) for the state cookie.
	DefaultCookieMaxAgeSecond = 3600
)

// Role represents a user's role within a tenant.
type Role string

const (
	// RoleOwner is the highest-privilege role, typically the tenant creator.
	RoleOwner Role = "owner"
	// RoleAdmin can manage users and settings within a tenant.
	RoleAdmin Role = "admin"
	// RoleMember is the standard role for tenant members.
	RoleMember Role = "member"
)

// Identity represents a user's identity as extracted from OIDC claims. It is
// the input to [IdentityStore.ResolveOrProvisionUser].
type Identity struct {
	Issuer            string         // The OIDC issuer URL (e.g. "https://auth.example.com").
	Subject           string         // The unique subject identifier from the IDP.
	Email             string         // The user's email address.
	EmailVerified     bool           // Whether the email has been verified by the IDP.
	Username          string         // The user's username (nickname).
	Name              string         // The user's display name.
	PreferredUsername string         // The user's preferred username.
	RawClaims         map[string]any // Any additional claims from the ID token.
}

// UserRef is a reference to a resolved local user, containing both the local
// user ID and the OIDC issuer/subject pair that uniquely identifies the user
// across identity providers.
type UserRef struct {
	UserID  uuid.UUID // The local database user ID.
	Issuer  string    // The OIDC issuer URL.
	Subject string    // The OIDC subject identifier.
}

// Membership represents a user's membership in a tenant, including their role.
type Membership struct {
	TenantID   uuid.UUID // The tenant's unique ID.
	TenantName string    // The human-readable tenant name.
	Role       Role      // The user's role within the tenant.
}

// TenantDisplay holds basic display information for a tenant.
type TenantDisplay struct {
	TenantID uuid.UUID // The tenant's unique ID.
	Name     string    // The human-readable tenant name.
}

// IdentityStore is the interface for resolving OIDC identities to local users
// and managing tenant memberships. Implementations handle user provisioning
// (auto-creating users on first login) and membership queries.
//
// The built-in [PostgresIdentityStore] implements this interface using
// PostgreSQL. For testing or custom backends, implement this interface directly.
type IdentityStore interface {
	// ResolveOrProvisionUser looks up an existing user by their OIDC issuer and
	// subject. If no user exists, it creates one along with a default tenant
	// membership.
	ResolveOrProvisionUser(ctx context.Context, identity Identity) (UserRef, error)
	// ListMemberships returns all tenant memberships for the given user.
	ListMemberships(ctx context.Context, userID uuid.UUID) ([]Membership, error)
	// GetTenantDisplay returns display information for a specific tenant.
	GetTenantDisplay(ctx context.Context, tenantID uuid.UUID) (TenantDisplay, error)
}

// Hooks is a compatibility alias for [IdentityStore].
type Hooks = IdentityStore

// SessionClaims holds the authenticated user's session data. It is stored in
// the session under [DefaultSessionKey] and can be retrieved from context
// using [SessionClaimsFromContext].
type SessionClaims struct {
	Authenticated  bool       // True after successful OIDC authentication.
	UserID         uuid.UUID  // The local database user ID.
	Issuer         string     // The OIDC issuer URL.
	Subject        string     // The OIDC subject identifier.
	ActiveTenantID *uuid.UUID // The currently selected tenant, or nil if not yet selected.
}

// ErrTenantSelectionRequired is returned when a user has multiple tenant
// memberships but has not yet selected an active tenant.
var ErrTenantSelectionRequired = errors.New("tenant selection required")
