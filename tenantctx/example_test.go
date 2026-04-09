package tenantctx_test

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/kusold/gotchi/tenantctx"
)

func ExampleWithTenantID() {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	ctx := tenantctx.WithTenantID(context.Background(), id)

	tenantID, ok := tenantctx.TenantIDString(ctx)
	fmt.Println(ok)
	fmt.Println(tenantID)
	// Output:
	// true
	// 550e8400-e29b-41d4-a716-446655440000
}

func ExampleWithTenantIDString() {
	ctx := tenantctx.WithTenantIDString(context.Background(), "acme-corp")

	tenantID, ok := tenantctx.TenantIDString(ctx)
	fmt.Println(ok)
	fmt.Println(tenantID)
	// Output:
	// true
	// acme-corp
}

func ExampleWithSystemTenant() {
	ctx := tenantctx.WithSystemTenant(context.Background())

	fmt.Println(tenantctx.IsSystemTenant(ctx))

	tenantID, ok := tenantctx.TenantIDString(ctx)
	fmt.Println(ok)
	fmt.Println(tenantID)
	// Output:
	// true
	// true
	// ADMIN_SYSTEM
}

func ExampleTenantID() {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	ctx := tenantctx.WithTenantID(context.Background(), id)

	parsed, ok := tenantctx.TenantID(ctx)
	fmt.Println(ok)
	fmt.Println(parsed == id)
	// Output:
	// true
	// true
}

func ExampleTenantIDString_missing() {
	ctx := context.Background()

	_, ok := tenantctx.TenantIDString(ctx)
	fmt.Println(ok)
	// Output:
	// false
}

func ExampleIsSystemTenant() {
	// A regular tenant context is not the system tenant.
	id := uuid.New()
	ctx := tenantctx.WithTenantID(context.Background(), id)
	fmt.Println(tenantctx.IsSystemTenant(ctx))

	// A system tenant context is recognized.
	sysCtx := tenantctx.WithSystemTenant(context.Background())
	fmt.Println(tenantctx.IsSystemTenant(sysCtx))

	// A context with no tenant ID is not the system tenant.
	emptyCtx := context.Background()
	fmt.Println(tenantctx.IsSystemTenant(emptyCtx))
	// Output:
	// false
	// true
	// false
}
