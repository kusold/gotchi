package tenantctx

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestTenantID_InvalidUUID(t *testing.T) {
	ctx := WithTenantIDString(context.Background(), "not-a-uuid")

	_, ok := TenantID(ctx)
	assert.False(t, ok, "TenantID should return false for invalid UUID")

	// But TenantIDString should still return the raw value
	val, ok := TenantIDString(ctx)
	assert.True(t, ok)
	assert.Equal(t, "not-a-uuid", val)
}

func TestTenantID_SystemTenantIsNotUUID(t *testing.T) {
	ctx := WithSystemTenant(context.Background())

	_, ok := TenantID(ctx)
	assert.False(t, ok, "TenantID should return false for system tenant (not a valid UUID)")

	val, ok := TenantIDString(ctx)
	assert.True(t, ok)
	assert.Equal(t, SystemTenant, val)
}

func TestTenantIDString_EmptyString(t *testing.T) {
	ctx := WithTenantIDString(context.Background(), "")

	_, ok := TenantIDString(ctx)
	assert.False(t, ok, "TenantIDString should return false for empty string")

	_, ok = TenantID(ctx)
	assert.False(t, ok, "TenantID should return false for empty string")
}

func TestTenantIDString_MissingFromContext(t *testing.T) {
	ctx := context.Background()

	_, ok := TenantIDString(ctx)
	assert.False(t, ok, "TenantIDString should return false when not set")

	_, ok = TenantID(ctx)
	assert.False(t, ok, "TenantID should return false when not set")

	assert.False(t, IsSystemTenant(ctx), "IsSystemTenant should be false when not set")
}

func TestWithTenantID_RoundTrip(t *testing.T) {
	id := uuid.New()
	ctx := WithTenantID(context.Background(), id)

	got, ok := TenantID(ctx)
	assert.True(t, ok)
	assert.Equal(t, id, got)

	gotStr, ok := TenantIDString(ctx)
	assert.True(t, ok)
	assert.Equal(t, id.String(), gotStr)
}

func TestIsSystemTenant(t *testing.T) {
	ctx := WithSystemTenant(context.Background())
	assert.True(t, IsSystemTenant(ctx))

	ctx = WithTenantID(context.Background(), uuid.New())
	assert.False(t, IsSystemTenant(ctx))

	ctx = context.Background()
	assert.False(t, IsSystemTenant(ctx))
}
