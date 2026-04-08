package db_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/tenantctx"
)

const tenantSettingSQL = "SELECT COALESCE(current_setting('app.current_tenant', true), '')"

func newManagedPool(t *testing.T) *db.Manager {
	t.Helper()
	mgr := db.NewManager(db.Config{
		DatabaseURL: testDB.DatabaseURL,
	})
	require.NoError(t, mgr.Connect(context.Background()))
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

func TestSetTenantFunction_SetsAndClearsConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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
	if testing.Short() {
		t.Skip("skipping integration test")
	}
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

	assertTenantContext := func(tenantID uuid.UUID) {
		defer wg.Done()
		conn, err := mgr.Pool().Acquire(tenantctx.WithTenantID(ctx, tenantID))
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
		if val != tenantID.String() {
			errCh <- fmt.Errorf("expected tenant %s, got %s", tenantID.String(), val)
		}
	}

	for i := 0; i < goroutines; i++ {
		wg.Add(2)
		go assertTenantContext(tenantA)
		go assertTenantContext(tenantB)
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

// setupRLSTable creates a test table with RLS enabled and a policy restricting
// rows to the current tenant. Because PostgreSQL superusers bypass RLS, this
// also creates a non-superuser role ("rls_app_user") and returns a db.Manager
// connected as that role. The table and role are cleaned up when the test ends.
func setupRLSTable(t *testing.T) *db.Manager {
	t.Helper()
	ctx := db.AdminContext(context.Background())

	// Use the superuser pool to create the table, role, and policy
	pool := testDB.Pool

	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS rls_test_items (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			tenant_id UUID NOT NULL,
			name TEXT NOT NULL
		)
	`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `ALTER TABLE rls_test_items ENABLE ROW LEVEL SECURITY`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `ALTER TABLE rls_test_items FORCE ROW LEVEL SECURITY`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		DO $$ BEGIN
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'rls_app_user') THEN
				CREATE ROLE rls_app_user LOGIN PASSWORD 'testpass';
			END IF;
		END $$
	`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `GRANT ALL ON rls_test_items TO rls_app_user`)
	require.NoError(t, err)

	// Grant EXECUTE on set_tenant so the app user can call it
	_, err = pool.Exec(ctx, `GRANT EXECUTE ON FUNCTION set_tenant(text) TO rls_app_user`)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		DROP POLICY IF EXISTS tenant_isolation ON rls_test_items;
		CREATE POLICY tenant_isolation ON rls_test_items
			USING (tenant_id::text = current_setting('app.current_tenant', true))
	`)
	require.NoError(t, err)

	// Build a connection URL for the non-superuser role
	appURL := strings.Replace(testDB.DatabaseURL, "postgres:secret", "rls_app_user:testpass", 1)
	mgr := db.NewManager(db.Config{DatabaseURL: appURL})
	require.NoError(t, mgr.Connect(context.Background()))

	t.Cleanup(func() {
		_ = mgr.Close()
		_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS rls_test_items`)
	})

	return mgr
}

func TestRLS_EnforcesTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mgr := setupRLSTable(t)

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()

	// Insert a row as tenant A
	ctxA := tenantctx.WithTenantID(ctx, tenantA)
	conn, err := mgr.Pool().Acquire(ctxA)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO rls_test_items (tenant_id, name) VALUES ($1, 'item-a')`, tenantA)
	require.NoError(t, err)
	conn.Release()

	// Insert a row as tenant B
	ctxB := tenantctx.WithTenantID(ctx, tenantB)
	conn, err = mgr.Pool().Acquire(ctxB)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO rls_test_items (tenant_id, name) VALUES ($1, 'item-b')`, tenantB)
	require.NoError(t, err)
	conn.Release()

	// Query as tenant A: should only see tenant A's row
	conn, err = mgr.Pool().Acquire(ctxA)
	require.NoError(t, err)
	rows, err := conn.Query(ctx, `SELECT name FROM rls_test_items`)
	require.NoError(t, err)
	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	conn.Release()

	assert.Equal(t, []string{"item-a"}, names, "tenant A should only see its own rows")

	// Query as tenant B: should only see tenant B's row
	conn, err = mgr.Pool().Acquire(ctxB)
	require.NoError(t, err)
	rows, err = conn.Query(ctx, `SELECT name FROM rls_test_items`)
	require.NoError(t, err)
	var namesB []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		namesB = append(namesB, name)
	}
	require.NoError(t, rows.Err())
	conn.Release()

	assert.Equal(t, []string{"item-b"}, namesB, "tenant B should only see its own rows")
}

func TestRLS_AdminBypassesIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mgr := setupRLSTable(t)

	ctx := context.Background()
	tenantA := uuid.New()
	tenantB := uuid.New()

	// Insert rows as each tenant
	ctxA := tenantctx.WithTenantID(ctx, tenantA)
	conn, err := mgr.Pool().Acquire(ctxA)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO rls_test_items (tenant_id, name) VALUES ($1, 'a-row')`, tenantA)
	require.NoError(t, err)
	conn.Release()

	ctxB := tenantctx.WithTenantID(ctx, tenantB)
	conn, err = mgr.Pool().Acquire(ctxB)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO rls_test_items (tenant_id, name) VALUES ($1, 'b-row')`, tenantB)
	require.NoError(t, err)
	conn.Release()

	// Query as superuser (bypasses RLS): should see all rows.
	// Use the testDB pool directly since it connects as the postgres superuser,
	// which is the expected behavior for admin operations in production.
	superConn, err := testDB.Pool.Acquire(ctx)
	require.NoError(t, err)
	var count int
	require.NoError(t, superConn.QueryRow(ctx, `SELECT count(*) FROM rls_test_items`).Scan(&count))
	superConn.Release()

	assert.Equal(t, 2, count, "superuser should bypass RLS and see all rows")
}

func TestRLS_NoTenantSeesNoRows(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mgr := setupRLSTable(t)

	ctx := context.Background()
	tenantA := uuid.New()

	// Insert a row as tenant A
	ctxA := tenantctx.WithTenantID(ctx, tenantA)
	conn, err := mgr.Pool().Acquire(ctxA)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO rls_test_items (tenant_id, name) VALUES ($1, 'secret')`, tenantA)
	require.NoError(t, err)
	conn.Release()

	// Query with an unrelated tenant: should see 0 rows
	unrelated := uuid.New()
	ctxU := tenantctx.WithTenantID(ctx, unrelated)
	conn, err = mgr.Pool().Acquire(ctxU)
	require.NoError(t, err)
	var count int
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM rls_test_items`).Scan(&count))
	conn.Release()

	assert.Equal(t, 0, count, "unrelated tenant should see no rows")
}

func TestBeforeAcquire_GracefulDegradation_MissingFunction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()

	// Create a manager with search_path pointing to a schema that has no set_tenant function
	mgr := db.NewManager(db.Config{
		DatabaseURL: testDB.DatabaseURL,
		SearchPath:  "no_rls_schema,public",
	})
	require.NoError(t, mgr.Connect(ctx))
	t.Cleanup(func() { _ = mgr.Close() })

	// Acquiring with a tenant context should not fail — the undefined_function
	// error from calling set_tenant() is tolerated
	tenantID := uuid.New()
	conn, err := mgr.Pool().Acquire(tenantctx.WithTenantID(ctx, tenantID))
	require.NoError(t, err, "acquire should succeed even when set_tenant function is missing")
	conn.Release()
}
