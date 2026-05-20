package testutil

import (
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/migrations"
)

var testDB *TestDB

func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Short() {
		testDB = SetupTestDB(m,
			WithMigrations(
				db.MigrationSource{FS: migrations.Core(), Dir: "."},
				db.MigrationSource{FS: migrations.Auth(), Dir: "."},
			),
		)
		if testDB == nil {
			fmt.Println("Integration tests require a container runtime")
			os.Exit(1)
		}
	}

	code := m.Run()
	if testDB != nil {
		testDB.Close()
	}
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

	// SetupTestDB with no migration sources should create a working database
	// but with no application schema.
	bareDB := SetupTestDB(nil)
	if bareDB == nil {
		t.Fatal("Failed to setup bare test database")
	}
	defer bareDB.Close()

	// Verify sessions table does NOT exist (no migrations ran)
	var exists bool
	err := bareDB.Pool.QueryRow(t.Context(),
		"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'sessions')").Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "sessions table should not exist when no migrations are configured")

	// But basic queries should work
	var result int
	err = bareDB.Pool.QueryRow(t.Context(), "SELECT 1").Scan(&result)
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
