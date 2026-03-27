// Package testutil provides shared test utilities for integration testing.
package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	dockertest "github.com/ory/dockertest/v4"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/migrations"
)

// TestDB holds the resources for an integration test database.
type TestDB struct {
	Pool   *pgxpool.Pool
	closer func()
}

// Close releases all Docker and database resources.
func (tdb *TestDB) Close() {
	if tdb.closer != nil {
		tdb.closer()
	}
}

// SetupTestDB starts a PostgreSQL container using dockertest and runs migrations.
// It returns a TestDB that must be closed with Close() when the test is done.
//
// Usage:
//
//	func TestMain(m *testing.M) {
//	    testDB := testutil.SetupTestDB(m)
//	    defer testDB.Close()
//	    // use testDB.Pool for tests
//	}
func SetupTestDB(m *testing.M) *TestDB {
	ctx := context.Background()

	// Connect to Docker using v4 API
	pool, err := dockertest.NewPool(ctx, "",
		dockertest.WithMaxWait(2*time.Minute),
	)
	if err != nil {
		fmt.Printf("Could not connect to docker: %s\n", err)
		return nil
	}

	// Start PostgreSQL container using v4 functional options
	resource, err := pool.Run(ctx, "postgres",
		dockertest.WithTag("18-alpine"),
		dockertest.WithEnv([]string{
			"POSTGRES_PASSWORD=secret",
			"POSTGRES_DB=testdb",
		}),
	)
	if err != nil {
		fmt.Printf("Could not start postgres container: %s\n", err)
		pool.Close(ctx)
		return nil
	}

	// Get host:port
	hostPort := resource.GetHostPort("5432/tcp")
	databaseURL := fmt.Sprintf("postgres://postgres:secret@%s/testdb?sslmode=disable", hostPort)

	// Wait for PostgreSQL to be ready - v4 API requires context and timeout
	var dbPool *pgxpool.Pool
	if err = pool.Retry(ctx, 30*time.Second, func() error {
		var err error
		dbPool, err = pgxpool.New(ctx, databaseURL)
		if err != nil {
			return err
		}
		return dbPool.Ping(ctx)
	}); err != nil {
		fmt.Printf("Could not connect to postgres: %s\n", err)
		pool.Close(ctx)
		return nil
	}

	// Run migrations
	if err := runMigrations(ctx, databaseURL); err != nil {
		fmt.Printf("Could not run migrations: %s\n", err)
		dbPool.Close()
		pool.Close(ctx)
		return nil
	}

	return &TestDB{
		Pool: dbPool,
		closer: func() {
			if dbPool != nil {
				dbPool.Close()
			}
			pool.Close(ctx)
		},
	}
}

// runMigrations connects to the database and runs all migrations
func runMigrations(ctx context.Context, databaseURL string) error {
	mgr := db.NewManager(db.Config{
		DatabaseURL: databaseURL,
		Schema:      "public",
	})
	mgr.AddMigrationSource(db.MigrationSource{
		FS:  migrations.Core(),
		Dir: ".",
	})
	mgr.AddMigrationSource(db.MigrationSource{
		FS:  migrations.Auth(),
		Dir: ".",
	})

	if err := mgr.Connect(ctx); err != nil {
		return fmt.Errorf("could not connect: %w", err)
	}

	if err := mgr.RunMigrations(ctx); err != nil {
		return fmt.Errorf("could not run migrations: %w", err)
	}

	return nil
}

// RequireTestDB is like SetupTestDB but fails the test immediately if setup fails.
// Use this in TestMain when you want the test to fail fast on setup errors.
func RequireTestDB(tb testing.TB, m *testing.M) *TestDB {
	testDB := SetupTestDB(m)
	if testDB == nil {
		tb.Fatal("Failed to setup test database")
	}
	require.NotNil(tb, testDB.Pool, "test database pool should not be nil")
	return testDB
}
