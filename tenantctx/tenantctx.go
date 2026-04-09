// Package tenantctx provides utilities for storing and retrieving tenant
// identifiers within a [context.Context]. It is designed for use in
// multi-tenant applications where each request must be associated with a
// specific tenant.
//
// Tenant IDs are stored internally as strings, but convenience functions are
// provided for working with [github.com/google/uuid.UUID] values. The package
// also defines a special [SystemTenant] constant that represents an
// administrative system-level tenant.
//
// # Setting a Tenant ID
//
// Use [WithTenantID] to store a UUID-based tenant ID, or [WithTenantIDString]
// for an arbitrary string value. [WithSystemTenant] is a shorthand that sets
// the tenant to [SystemTenant].
//
// # Retrieving a Tenant ID
//
// Use [TenantIDString] to retrieve the raw string value, or [TenantID] to
// parse it back into a UUID. Both functions return a boolean indicating whether
// a valid tenant ID was present in the context.
//
// # Checking for System Tenant
//
// Use [IsSystemTenant] to check whether the context carries the system tenant
// identifier. This is useful for authorizing administrative operations.
package tenantctx

import (
	"context"

	"github.com/google/uuid"
)

// SystemTenant is the tenant identifier used for administrative system-level
// operations. It is not a valid UUID and will cause [TenantID] to return
// false; use [TenantIDString] or [IsSystemTenant] instead when working with
// the system tenant.
const SystemTenant = "ADMIN_SYSTEM"

type tenantContextKey struct{}

var tenantKey = tenantContextKey{}

// WithTenantID returns a new [context.Context] with the given tenant ID stored
// as its string representation. The UUID is converted to a string internally
// so that both [TenantIDString] and [TenantID] can retrieve it.
func WithTenantID(ctx context.Context, tenantID uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantKey, tenantID.String())
}

// WithTenantIDString returns a new [context.Context] with the given tenant ID
// string. This is useful when the tenant ID is already in string form, such as
// when parsing from a request header or a JWT claim.
func WithTenantIDString(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey, tenantID)
}

// WithSystemTenant returns a new [context.Context] with the tenant ID set to
// [SystemTenant]. This is a convenience wrapper around [WithTenantIDString]
// for administrative contexts.
func WithSystemTenant(ctx context.Context) context.Context {
	return WithTenantIDString(ctx, SystemTenant)
}

// TenantIDString retrieves the tenant ID as a string from the given context.
// It returns the tenant ID and true if a non-empty tenant ID is present, or an
// empty string and false if the tenant ID is missing or empty.
func TenantIDString(ctx context.Context) (string, bool) {
	tenantID, ok := ctx.Value(tenantKey).(string)
	if !ok || tenantID == "" {
		return "", false
	}
	return tenantID, true
}

// TenantID retrieves the tenant ID as a [uuid.UUID] from the given context.
// It returns the parsed UUID and true if a valid UUID tenant ID is present, or
// a zero UUID and false if the tenant ID is missing, empty, or not a valid
// UUID. Note that [SystemTenant] is not a valid UUID and will cause this
// function to return false; use [TenantIDString] for the raw value.
func TenantID(ctx context.Context) (uuid.UUID, bool) {
	tenantID, ok := TenantIDString(ctx)
	if !ok {
		return uuid.UUID{}, false
	}
	parsed, err := uuid.Parse(tenantID)
	if err != nil {
		return uuid.UUID{}, false
	}
	return parsed, true
}

// IsSystemTenant reports whether the context carries the [SystemTenant]
// identifier. It returns false if no tenant ID is present in the context or if
// the tenant ID is any value other than [SystemTenant].
func IsSystemTenant(ctx context.Context) bool {
	tenantID, ok := TenantIDString(ctx)
	if !ok {
		return false
	}
	return tenantID == SystemTenant
}
