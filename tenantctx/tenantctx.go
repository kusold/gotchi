package tenantctx

import (
	"context"

	"github.com/google/uuid"
)

const SystemTenant = "ADMIN_SYSTEM"

type tenantContextKey struct{}

var tenantKey = tenantContextKey{}

func WithTenantID(ctx context.Context, tenantID uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantKey, tenantID.String())
}

func WithTenantIDString(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey, tenantID)
}

func WithSystemTenant(ctx context.Context) context.Context {
	return WithTenantIDString(ctx, SystemTenant)
}

func TenantIDString(ctx context.Context) (string, bool) {
	tenantID, ok := ctx.Value(tenantKey).(string)
	if !ok || tenantID == "" {
		return "", false
	}
	return tenantID, true
}

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

func IsSystemTenant(ctx context.Context) bool {
	tenantID, ok := TenantIDString(ctx)
	if !ok {
		return false
	}
	return tenantID == SystemTenant
}
