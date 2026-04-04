package db_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/internal/testutil"
	"github.com/kusold/gotchi/tenantctx"
)

const tenantSettingSQL = "SELECT COALESCE(current_setting('app.current_tenant', true), '')"

var testDB *testutil.TestDB

func TestMain(m *testing.M) {
	testDB = testutil.SetupTestDB(m)
	if testDB == nil {
		fmt.Println("Failed to setup test database")
		os.Exit(1)
	}

	code := m.Run()
	testDB.Close()
	os.Exit(code)
}

func newManagedPool(t *testing.T) *db.Manager {
	t.Helper()
	mgr := db.NewManager(db.Config{
		DatabaseURL: testDB.DatabaseURL,
	})
	require.NoError(t, mgr.Connect(context.Background()))
	t.Cleanup(mgr.Close)
	return mgr
}

func TestSetTenantFunction_SetsAndClearsConfig(t *testing.T) {
	ctx := context.Background()
	tenantID := uuid.New().String()

	_, err := testDB.Pool.Exec(ctx, "SELECT set_tenant($1)", tenantID)
	require.NoError(t, err)

	var val string
	require.NoError(t, testDB.Pool.QueryRow(ctx, tenantSettingSQL).Scan(&val))
	assert.Equal(t, tenantID, val)

	_, err = testDB.Pool.Exec(ctx, "SELECT set_tenant($1)", "")
	require.NoError(t, err)

	require.NoError(t, testDB.Pool.QueryRow(ctx, tenantSettingSQL).Scan(&val))
	assert.Equal(t, "", val)
}

func TestBeforeAcquire_SetsTenantOnConnection(t *testing.T) {
	mgr := newManagedPool(t)
	ctx := context.Background()
	tenantID := uuid.New()

	conn, err := mgr.Pool().Acquire(tenantctx.WithTenantID(ctx, tenantID))
	require.NoError(t, err)
	defer conn.Release()

	var val string
	require.NoError(t, conn.QueryRow(ctx, tenantSettingSQL).Scan(&val))
	assert.Equal(t, tenantID.String(), val)
}

func TestAfterRelease_ClearsTenantOnConnection(t *testing.T) {
	mgr := newManagedPool(t)
	ctx := context.Background()
	tenantID := uuid.New()

	conn, err := mgr.Pool().Acquire(tenantctx.WithTenantID(ctx, tenantID))
	require.NoError(t, err)
	conn.Release()

	conn2, err := mgr.Pool().Acquire(ctx)
	require.NoError(t, err)
	defer conn2.Release()

	var val string
	require.NoError(t, conn2.QueryRow(ctx, tenantSettingSQL).Scan(&val))
	assert.Equal(t, "", val)
}

func TestSystemTenant_DoesNotSetTenant(t *testing.T) {
	mgr := newManagedPool(t)
	ctx := context.Background()

	conn, err := mgr.Pool().Acquire(tenantctx.WithSystemTenant(ctx))
	require.NoError(t, err)
	defer conn.Release()

	var val string
	require.NoError(t, conn.QueryRow(ctx, tenantSettingSQL).Scan(&val))
	assert.Equal(t, "", val, "system tenant should not set app.current_tenant")
}

func TestNoTenantContext_DoesNotSetTenant(t *testing.T) {
	mgr := newManagedPool(t)
	ctx := context.Background()

	conn, err := mgr.Pool().Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	var val string
	require.NoError(t, conn.QueryRow(ctx, tenantSettingSQL).Scan(&val))
	assert.Equal(t, "", val, "no tenant context should not set app.current_tenant")
}

func TestTenantSwitchingOnConnection(t *testing.T) {
	mgr := newManagedPool(t)
	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()

	conn, err := mgr.Pool().Acquire(tenantctx.WithTenantID(ctx, tenantA))
	require.NoError(t, err)

	var val string
	require.NoError(t, conn.QueryRow(ctx, tenantSettingSQL).Scan(&val))
	assert.Equal(t, tenantA.String(), val)
	conn.Release()

	conn, err = mgr.Pool().Acquire(tenantctx.WithTenantID(ctx, tenantB))
	require.NoError(t, err)
	defer conn.Release()

	require.NoError(t, conn.QueryRow(ctx, tenantSettingSQL).Scan(&val))
	assert.Equal(t, tenantB.String(), val)
}

func TestConcurrentTenantIsolation(t *testing.T) {
	mgr := newManagedPool(t)
	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()

	adminCtx := db.AdminContext(ctx)
	_, err := mgr.Pool().Exec(adminCtx, "INSERT INTO tenants (tenant_id, name) VALUES ($1, $2)", tenantA, "Tenant A")
	require.NoError(t, err)
	_, err = mgr.Pool().Exec(adminCtx, "INSERT INTO tenants (tenant_id, name) VALUES ($1, $2)", tenantB, "Tenant B")
	require.NoError(t, err)

	const goroutines = 10
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*2)

	for i := 0; i < goroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			conn, err := mgr.Pool().Acquire(tenantctx.WithTenantID(ctx, tenantA))
			if err != nil {
				errCh <- err
				return
			}
			defer conn.Release()

			var val string
			if err := conn.QueryRow(ctx, tenantSettingSQL).Scan(&val); err != nil {
				errCh <- err
				return
			}
			if val != tenantA.String() {
				errCh <- fmt.Errorf("expected tenant A (%s), got %s", tenantA.String(), val)
			}
		}()
		go func() {
			defer wg.Done()
			conn, err := mgr.Pool().Acquire(tenantctx.WithTenantID(ctx, tenantB))
			if err != nil {
				errCh <- err
				return
			}
			defer conn.Release()

			var val string
			if err := conn.QueryRow(ctx, tenantSettingSQL).Scan(&val); err != nil {
				errCh <- err
				return
			}
			if val != tenantB.String() {
				errCh <- fmt.Errorf("expected tenant B (%s), got %s", tenantB.String(), val)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestAdminContext_SetsSystemTenant(t *testing.T) {
	ctx := context.Background()
	adminCtx := db.AdminContext(ctx)

	tenantID, ok := tenantctx.TenantIDString(adminCtx)
	require.True(t, ok)
	assert.Equal(t, tenantctx.SystemTenant, tenantID)
	assert.True(t, tenantctx.IsSystemTenant(adminCtx))
}
