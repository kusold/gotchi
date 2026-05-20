package testutil

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/migrations"
)

var testDB *TestDB

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}

	testDB = SetupTestDB(m,
		WithMigrations(
			db.MigrationSource{FS: migrations.Core(), Dir: "."},
			db.MigrationSource{FS: migrations.Auth(), Dir: "."},
		),
	)
	if testDB == nil {
		fmt.Println("Failed to setup test database")
		os.Exit(1)
	}

	code := m.Run()
	testDB.Close()
	os.Exit(code)
}

func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
}

func TestSetupTestDB_WithMigrations(t *testing.T) {
	skipIfShort(t)
	require.NotNil(t, testDB)
	require.NotNil(t, testDB.Pool)

	// Verify core schema was applied: sessions table should exist
	var exists bool
	err := testDB.Pool.QueryRow(t.Context(),
		"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'sessions')").Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "sessions table should exist after core migrations")
}

func TestSetupTestDB_NoMigrations(t *testing.T) {
	skipIfShort(t)
	require.NotNil(t, testDB)

	// Create a fresh database on the existing container to verify that no
	// schema is applied when no migration sources are configured. This avoids
	// spinning up a second container which may share state in some CI
	// environments.
	const dbName = "test_no_migrations"
	_, err := testDB.Pool.Exec(t.Context(),
		fmt.Sprintf("CREATE DATABASE %s", dbName))
	require.NoError(t, err)
	t.Cleanup(func() {
		testDB.Pool.Exec(context.Background(),
			fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	})

	bareURL := strings.Replace(testDB.DatabaseURL, "/testdb", "/"+dbName, 1)
	barePool, err := pgxpool.New(t.Context(), bareURL)
	require.NoError(t, err)
	defer barePool.Close()

	// Verify sessions table does NOT exist (no migrations ran)
	var exists bool
	err = barePool.QueryRow(t.Context(),
		"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'sessions')").Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "sessions table should not exist when no migrations are configured")

	// But basic queries should work
	var result int
	err = barePool.QueryRow(t.Context(), "SELECT 1").Scan(&result)
	require.NoError(t, err)
	assert.Equal(t, 1, result)
}

func TestWithMigrations_AppendsSources(t *testing.T) {
	var cfg setupConfig
	WithMigrations(
		db.MigrationSource{Dir: "first"},
		db.MigrationSource{Dir: "second"},
	)(&cfg)

	require.Len(t, cfg.migrationSources, 2)
	assert.Equal(t, "first", cfg.migrationSources[0].Dir)
	assert.Equal(t, "second", cfg.migrationSources[1].Dir)
}
