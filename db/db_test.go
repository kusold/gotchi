package db_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/migrations"
	"github.com/kusold/gotchi/tenantctx"
)

func TestNewManager(t *testing.T) {
	mgr := db.NewManager(db.Config{DatabaseURL: "postgres://user:pass@localhost:5432/testdb"})
	require.NotNil(t, mgr)
}

func TestManager_Pool_BeforeConnect(t *testing.T) {
	mgr := db.NewManager(db.Config{DatabaseURL: "postgres://localhost/test"})
	assert.Nil(t, mgr.Pool())
}

func TestManager_Ping_BeforeConnect(t *testing.T) {
	mgr := db.NewManager(db.Config{DatabaseURL: "postgres://localhost/test"})
	err := mgr.Ping(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not initialized")
}

func TestManager_RunMigrations_BeforeConnect(t *testing.T) {
	mgr := db.NewManager(db.Config{DatabaseURL: "postgres://localhost/test"})
	err := mgr.RunMigrations(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not initialized")
}

func TestManager_Close_NilPool(t *testing.T) {
	mgr := db.NewManager(db.Config{DatabaseURL: "postgres://localhost/test"})
	assert.NotPanics(t, mgr.Close)
}

func TestManager_Connect_InvalidURL(t *testing.T) {
	mgr := db.NewManager(db.Config{DatabaseURL: "://not-a-valid-url"})
	err := mgr.Connect(context.Background())
	require.Error(t, err)
}

func TestManager_Connect_CancelledContext(t *testing.T) {
	mgr := db.NewManager(db.Config{DatabaseURL: "postgres://user:pass@nonexistent-host.example.com:5432/testdb?sslmode=disable"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := mgr.Connect(ctx)
	require.Error(t, err)
}

func TestAdminContext_NilInput(t *testing.T) {
	adminCtx := db.AdminContext(nil)
	require.NotNil(t, adminCtx)

	tenantID, ok := tenantctx.TenantIDString(adminCtx)
	require.True(t, ok)
	assert.Equal(t, tenantctx.SystemTenant, tenantID)
}

func TestAddMigrationSource_DefaultDir(t *testing.T) {
	mgr := db.NewManager(db.Config{DatabaseURL: "postgres://localhost/test"})
	mgr.AddMigrationSource(db.MigrationSource{
		FS:  fstest.MapFS{},
		Dir: "",
	})
	mgr.AddMigrationSource(db.MigrationSource{
		FS:  fstest.MapFS{},
		Dir: "subdir",
	})
}

func TestManager_Connect_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mgr := db.NewManager(db.Config{DatabaseURL: testDB.DatabaseURL})
	require.NoError(t, mgr.Connect(context.Background()))
	t.Cleanup(mgr.Close)
	assert.NotNil(t, mgr.Pool())
}

func TestManager_Connect_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mgr := db.NewManager(db.Config{DatabaseURL: testDB.DatabaseURL})
	require.NoError(t, mgr.Connect(context.Background()))
	t.Cleanup(mgr.Close)

	require.NoError(t, mgr.Connect(context.Background()))
	assert.NotNil(t, mgr.Pool())
}

func TestManager_Connect_WithSearchPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()

	adminConn, err := testDB.Pool.Acquire(ctx)
	require.NoError(t, err)
	_, err = adminConn.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS test_search_schema")
	require.NoError(t, err)
	adminConn.Release()

	mgr := db.NewManager(db.Config{
		DatabaseURL: testDB.DatabaseURL,
		SearchPath:  "test_search_schema",
	})
	require.NoError(t, mgr.Connect(ctx))
	t.Cleanup(mgr.Close)

	conn, err := mgr.Pool().Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	var searchPath string
	require.NoError(t, conn.QueryRow(ctx, "SELECT current_setting('search_path', false)").Scan(&searchPath))
	assert.Contains(t, searchPath, "test_search_schema")
}

func TestManager_Ping_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mgr := db.NewManager(db.Config{DatabaseURL: testDB.DatabaseURL})
	require.NoError(t, mgr.Connect(context.Background()))
	t.Cleanup(mgr.Close)

	require.NoError(t, mgr.Ping(context.Background()))
}

func TestManager_Close_DoubleClose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mgr := db.NewManager(db.Config{DatabaseURL: testDB.DatabaseURL})
	require.NoError(t, mgr.Connect(context.Background()))

	assert.NotPanics(t, func() {
		mgr.Close()
		mgr.Close()
	})
}

func TestManager_RunMigrations_EmptySources(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mgr := db.NewManager(db.Config{DatabaseURL: testDB.DatabaseURL})
	require.NoError(t, mgr.Connect(context.Background()))
	t.Cleanup(mgr.Close)

	require.NoError(t, mgr.RunMigrations(context.Background()))
}

func TestManager_RunMigrations_SingleSource(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mgr := db.NewManager(db.Config{DatabaseURL: testDB.DatabaseURL})
	mgr.AddMigrationSource(db.MigrationSource{
		FS:  migrations.Core(),
		Dir: ".",
	})
	require.NoError(t, mgr.Connect(context.Background()))
	t.Cleanup(mgr.Close)

	require.NoError(t, mgr.RunMigrations(context.Background()))
}

func TestManager_RunMigrations_MultipleSources(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mgr := db.NewManager(db.Config{DatabaseURL: testDB.DatabaseURL})
	mgr.AddMigrationSource(db.MigrationSource{
		FS:  migrations.Core(),
		Dir: ".",
	})
	mgr.AddMigrationSource(db.MigrationSource{
		FS:  migrations.Auth(),
		Dir: ".",
	})
	require.NoError(t, mgr.Connect(context.Background()))
	t.Cleanup(mgr.Close)

	require.NoError(t, mgr.RunMigrations(context.Background()))
}

func TestManager_RunMigrations_Idempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	mgr := db.NewManager(db.Config{DatabaseURL: testDB.DatabaseURL})
	mgr.AddMigrationSource(db.MigrationSource{
		FS:  migrations.Core(),
		Dir: ".",
	})
	mgr.AddMigrationSource(db.MigrationSource{
		FS:  migrations.Auth(),
		Dir: ".",
	})
	require.NoError(t, mgr.Connect(context.Background()))
	t.Cleanup(mgr.Close)

	require.NoError(t, mgr.RunMigrations(context.Background()))
	require.NoError(t, mgr.RunMigrations(context.Background()))
}

func TestManager_RunMigrations_FailedMigration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	badMigrationFS := fstest.MapFS{
		"20990101000000_bad.sql": &fstest.MapFile{
			Data: []byte("-- +goose Up\nSELECT * FROM nonexistent_table_xyz;\n\n-- +goose Down\nSELECT 1;"),
		},
	}

	mgr := db.NewManager(db.Config{DatabaseURL: testDB.DatabaseURL})
	mgr.AddMigrationSource(db.MigrationSource{
		FS:  badMigrationFS,
		Dir: ".",
	})
	require.NoError(t, mgr.Connect(context.Background()))
	t.Cleanup(mgr.Close)

	err := mgr.RunMigrations(context.Background())
	require.Error(t, err, "migration with invalid SQL should fail")
}
