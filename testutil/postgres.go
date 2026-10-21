// Package testutil provides utilities for integration testing against a
// real PostgreSQL database running in a Docker container.
//
// # Basic Usage
//
// Use [SetupTestDB] in TestMain when you want to handle setup errors yourself:
//
//	func TestMain(m *testing.M) {
//	    if testing.Short() {
//	        os.Exit(m.Run())
//	    }
//	    testDB := testutil.SetupTestDB(m,
//	        testutil.WithMigrations(
//	            db.MigrationSource{FS: migrations.Core(), Dir: "."},
//	            db.MigrationSource{FS: migrations.Auth(), Dir: "."},
//	        ),
//	    )
//	    if testDB == nil {
//	        fmt.Println("Failed to setup test database")
//	        os.Exit(1)
//	    }
//	    code := m.Run()
//	    testDB.Close()
//	    os.Exit(code)
//	}
//
// Use [RequireTestDB] in test functions where you have a testing.TB to get
// a proper test failure with t.Fatal on error.
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
)

// TestDB holds the resources for an integration test database.
type TestDB struct {
	Pool        *pgxpool.Pool
	DatabaseURL string
	closer      func()
}

// Close releases all Docker and database resources.
func (tdb *TestDB) Close() {
	if tdb.closer != nil {
		tdb.closer()
	}
}

// SetupOption configures [SetupTestDB] behavior.
type SetupOption func(*setupConfig)

type setupConfig struct {
	migrationSources []db.MigrationSource
}

// WithMigrations registers migration sources to apply after the test database
// is created. Migrations are applied in the order provided. If no migration
// sources are provided, the database is created but no schema is applied.
func WithMigrations(sources ...db.MigrationSource) SetupOption {
	return func(cfg *setupConfig) {
		cfg.migrationSources = append(cfg.migrationSources, sources...)
	}
}

// SetupTestDB starts a PostgreSQL container using dockertest and runs any
// registered migrations. It returns a TestDB that must be closed with Close()
// when the test is done. If the container cannot be started or migrations
// fail, it returns nil. Use [RequireTestDB] to fail immediately on error.
//
// Use [WithMigrations] to register migration sources:
//
//	testDB := testutil.SetupTestDB(m,
//	    testutil.WithMigrations(
//	        db.MigrationSource{FS: migrations.Core(), Dir: "."},
//	    ),
//	)
func SetupTestDB(m *testing.M, opts ...SetupOption) *TestDB {
	var cfg setupConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	ctx := context.Background()

	// Connect to Docker using v4 API
	pool, err := dockertest.NewPool(ctx, "",
		dockertest.WithMaxWait(2*time.Minute),
	)
	if err != nil {
		fmt.Printf("Could not connect to docker: %s\n", err)
		return nil
	}

	// Start PostgreSQL container using v4 functional options.
	// Credentials are intentionally trivial — the container is ephemeral and
	// only accessible from the local host.
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
		if dbPool != nil {
			dbPool.Close()
		}
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

	// Run migrations (no-op if no sources configured)
	if err := runMigrations(ctx, databaseURL, cfg.migrationSources); err != nil {
		fmt.Printf("Could not run migrations: %s\n", err)
		dbPool.Close()
		pool.Close(ctx)
		return nil
	}

	return &TestDB{
		Pool:        dbPool,
		DatabaseURL: databaseURL,
		closer: func() {
			if dbPool != nil {
				dbPool.Close()
			}
			pool.Close(ctx)
		},
	}
}

// runMigrations connects to the database and runs the provided migration sources.
// If sources is empty, it returns nil immediately.
func runMigrations(ctx context.Context, databaseURL string, sources []db.MigrationSource) error {
	if len(sources) == 0 {
		return nil
	}

	mgr := db.NewManager(db.Config{
		DatabaseURL: databaseURL,
	})
	for _, src := range sources {
		mgr.AddMigrationSource(src)
	}

	if err := mgr.Connect(ctx); err != nil {
		return fmt.Errorf("could not connect: %w", err)
	}
	defer mgr.Close()

	if err := mgr.RunMigrations(ctx); err != nil {
		return fmt.Errorf("could not run migrations: %w", err)
	}

	return nil
}

// RequireTestDB is like [SetupTestDB] but calls tb.Fatal if setup fails.
func RequireTestDB(tb testing.TB, m *testing.M, opts ...SetupOption) *TestDB {
	testDB := SetupTestDB(m, opts...)
	if testDB == nil {
		tb.Fatal("Failed to setup test database")
	}
	require.NotNil(tb, testDB.Pool, "test database pool should not be nil")
	return testDB
}
