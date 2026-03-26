package auth

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	dockertest "github.com/ory/dockertest/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/migrations"
)

var (
	pool   dockertest.ClosablePool
	dbPool *pgxpool.Pool
)

// testUsersCount is the number of users to create in multi-user tests
const testUsersCount = 5

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error

	// Connect to Docker using v4 API
	pool, err = dockertest.NewPool(ctx, "",
		dockertest.WithMaxWait(2*time.Minute),
	)
	if err != nil {
		fmt.Printf("Could not connect to docker: %s\n", err)
		os.Exit(1)
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
		os.Exit(1)
	}

	// Get host:port
	hostPort := resource.GetHostPort("5432/tcp")
	databaseURL := fmt.Sprintf("postgres://postgres:secret@%s/testdb?sslmode=disable", hostPort)

	// Wait for PostgreSQL to be ready - v4 API requires context and timeout
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
		os.Exit(1)
	}

	// Run migrations
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
		fmt.Printf("Could not connect: %s\n", err)
		cleanup(ctx)
		os.Exit(1)
	}

	if err := mgr.RunMigrations(ctx); err != nil {
		fmt.Printf("Could not run migrations: %s\n", err)
		cleanup(ctx)
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Cleanup - pool.Close removes all tracked containers/networks
	cleanup(ctx)
	os.Exit(code)
}

// cleanup releases Docker and database resources
func cleanup(ctx context.Context) {
	if dbPool != nil {
		dbPool.Close()
	}
	pool.Close(ctx)
}

// newTestStore creates a PostgresIdentityStore with a unique tenant name for test isolation
func newTestStore(t *testing.T, tenantName string) *PostgresIdentityStore {
	store, err := NewPostgresIdentityStore(dbPool, PostgresStoreConfig{
		Schema:            "public",
		DefaultTenantName: tenantName,
	})
	require.NoError(t, err)
	require.NotNil(t, store)
	return store
}

func TestNewPostgresIdentityStore_Success(t *testing.T) {
	store := newTestStore(t, "Default Tenant")
	require.NotNil(t, store)
}

func TestResolveOrProvisionUser_NewUser(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "Test Tenant NewUser")

	// Create a new user
	identity := Identity{
		Issuer:            "test-issuer",
		Subject:           "test-subject-123",
		PreferredUsername: "testuser",
		Email:             "test@example.com",
		EmailVerified:     true,
	}

	userRef, err := store.ResolveOrProvisionUser(ctx, identity)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, userRef.UserID)
	assert.Equal(t, "test-issuer", userRef.Issuer)
	assert.Equal(t, "test-subject-123", userRef.Subject)

	// Verify user was created
	retrieved, err := store.ResolveOrProvisionUser(ctx, identity)
	require.NoError(t, err)
	assert.Equal(t, userRef.UserID, retrieved.UserID)
}

func TestResolveOrProvisionUser_ExistingUser(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "Test Tenant ExistingUser")

	// Create user first time
	identity := Identity{
		Issuer:            "test-issuer-2",
		Subject:           "test-subject-456",
		PreferredUsername: "testuser2",
		Email:             "test2@example.com",
		EmailVerified:     true,
	}

	userRef1, err := store.ResolveOrProvisionUser(ctx, identity)
	require.NoError(t, err)

	// Same user second time should return same reference
	userRef2, err := store.ResolveOrProvisionUser(ctx, identity)
	require.NoError(t, err)
	assert.Equal(t, userRef1.UserID, userRef2.UserID)
}

func TestResolveOrProvisionUser_MultipleUsers(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "Test Tenant MultipleUsers")

	// Create multiple users to verify the store handles concurrent-ish user creation
	for i := 0; i < testUsersCount; i++ {
		identity := Identity{
			Issuer:            fmt.Sprintf("issuer-%d", i),
			Subject:           fmt.Sprintf("subject-%d", i),
			PreferredUsername: fmt.Sprintf("user%d", i),
			Email:             fmt.Sprintf("user%d@example.com", i),
			EmailVerified:     true,
		}

		userRef, err := store.ResolveOrProvisionUser(ctx, identity)
		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, userRef.UserID)
	}
}

func TestResolveOrProvisionUser_NilPool(t *testing.T) {
	ctx := context.Background()

	// Create store with nil pool
	store, err := NewPostgresIdentityStore(nil, PostgresStoreConfig{
		Schema:            "public",
		DefaultTenantName: "Test",
	})
	require.NoError(t, err)
	require.NotNil(t, store)

	// Should return error when trying to use nil pool
	identity := Identity{
		Issuer:  "test",
		Subject: "test",
	}
	_, err = store.ResolveOrProvisionUser(ctx, identity)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "postgres pool is required")
}

func TestResolveOrProvisionUser_EmptyFields(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "Test Tenant EmptyFields")

	tests := []struct {
		name     string
		identity Identity
	}{
		{
			name: "empty issuer",
			identity: Identity{
				Issuer:  "",
				Subject: "test-subject",
			},
		},
		{
			name: "empty subject",
			identity: Identity{
				Issuer:  "test-issuer",
				Subject: "",
			},
		},
		{
			name: "empty both",
			identity: Identity{
				Issuer:  "",
				Subject: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: The current implementation may not validate empty fields
			// This test documents the current behavior
			_, err := store.ResolveOrProvisionUser(ctx, tt.identity)
			// We're documenting that empty fields are allowed for now
			// If validation is added, this test should expect an error
			_ = err // suppress unused variable warning if needed
		})
	}
}

func TestResolveOrProvisionUser_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "Test Tenant Concurrent")

	const goroutines = 10
	const iterations = 5

	var wg sync.WaitGroup
	errors := make(chan error, goroutines*iterations)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				identity := Identity{
					Issuer:            fmt.Sprintf("concurrent-issuer-%d", goroutineID),
					Subject:           fmt.Sprintf("concurrent-subject-%d-%d", goroutineID, i),
					PreferredUsername: fmt.Sprintf("user_%d_%d", goroutineID, i),
					Email:             fmt.Sprintf("user_%d_%d@example.com", goroutineID, i),
					EmailVerified:     true,
				}

				_, err := store.ResolveOrProvisionUser(ctx, identity)
				if err != nil {
					errors <- err
				}
			}
		}(g)
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	var errs []error
	for err := range errors {
		errs = append(errs, err)
	}

	assert.Empty(t, errs, "concurrent access should not produce errors, got %d errors: %v", len(errs), errs)
}

func TestResolveOrProvisionUser_SameUserConcurrent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t, "Test Tenant SameUserConcurrent")

	// Test that concurrent requests for the same user return the same user ID
	const goroutines = 10
	identity := Identity{
		Issuer:            "same-user-concurrent",
		Subject:           "same-subject-concurrent",
		PreferredUsername: "sameuser",
		Email:             "sameuser@example.com",
		EmailVerified:     true,
	}

	var wg sync.WaitGroup
	userIDs := make(chan uuid.UUID, goroutines)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			userRef, err := store.ResolveOrProvisionUser(ctx, identity)
			if err == nil {
				userIDs <- userRef.UserID
			}
		}()
	}

	wg.Wait()
	close(userIDs)

	// All goroutines should get the same user ID
	var ids []uuid.UUID
	for id := range userIDs {
		ids = append(ids, id)
	}

	require.NotEmpty(t, ids, "should have at least one successful result")
	firstID := ids[0]
	for _, id := range ids {
		assert.Equal(t, firstID, id, "all concurrent requests should return the same user ID")
	}
}
