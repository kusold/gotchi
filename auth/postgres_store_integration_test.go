package auth

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/db"
	"github.com/kusold/gotchi/migrations"
)

var (
	pool     *dockertest.Pool
	resource *dockertest.Resource
	dbPool   *pgxpool.Pool
)

func TestMain(m *testing.M) {
	var err error

	// Connect to Docker
	pool, err = dockertest.NewPool("")
	if err != nil {
		fmt.Printf("Could not connect to docker: %s\n", err)
		os.Exit(1)
	}

	// Start PostgreSQL container
	resource, err = pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "15-alpine",
		Env: []string{
			"POSTGRES_PASSWORD=secret",
			"POSTGRES_DB=testdb",
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})
	if err != nil {
		fmt.Printf("Could not start postgres container: %s\n", err)
		os.Exit(1)
	}

	// Get host:port
	hostPort := resource.GetHostPort("5432/tcp")
	databaseURL := fmt.Sprintf("postgres://postgres:secret@%s/testdb?sslmode=disable", hostPort)

	// Wait for PostgreSQL to be ready
	ctx := context.Background()
	if err = pool.Retry(func() error {
		var err error
		dbPool, err = pgxpool.New(ctx, databaseURL)
		if err != nil {
			return err
		}
		return dbPool.Ping(ctx)
	}); err != nil {
		fmt.Printf("Could not connect to postgres: %s\n", err)
		_ = resource.Close()
		os.Exit(1)
	}

	// Run migrations using v2 API
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
		dbPool.Close()
		_ = resource.Close()
		os.Exit(1)
	}
	
	if err := mgr.RunMigrations(ctx); err != nil {
		fmt.Printf("Could not run migrations: %s\n", err)
		dbPool.Close()
		_ = resource.Close()
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Cleanup
	dbPool.Close()
	_ = resource.Close()
	os.Exit(code)
}

func TestNewPostgresIdentityStore_Success(t *testing.T) {
	store := NewPostgresIdentityStore(dbPool, PostgresStoreConfig{
		Schema:            "public",
		DefaultTenantName: "Default Tenant",
	})
	require.NotNil(t, store)
}

func TestNewPostgresIdentityStore_ValidatesSchema(t *testing.T) {
	ctx := context.Background()

	// Test invalid schema name (SQL injection attempt) via ResolveOrProvisionUser
	store := NewPostgresIdentityStore(dbPool, PostgresStoreConfig{
		Schema:            "public; DROP TABLE users;--",
		DefaultTenantName: "Default Tenant",
	})

	// This should fail when trying to use the invalid schema
	_, err := store.ResolveOrProvisionUser(ctx, Identity{
		Issuer:            "test",
		Subject:           "test",
		Email:             "test@example.com",
		EmailVerified:     true,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid schema name")
}

func TestResolveOrProvisionUser_NewUser(t *testing.T) {
	ctx := context.Background()

	store := NewPostgresIdentityStore(dbPool, PostgresStoreConfig{
		Schema:            "public",
		DefaultTenantName: "Test Tenant",
	})

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
	assert.NotEqual(t, uuid.Nil, userRef.TenantID)
	assert.Equal(t, "test-issuer", userRef.Issuer)
	assert.Equal(t, "test-subject-123", userRef.Subject)

	// Verify user was created
	retrieved, err := store.ResolveOrProvisionUser(ctx, identity)
	require.NoError(t, err)
	assert.Equal(t, userRef.UserID, retrieved.UserID)
	assert.Equal(t, userRef.TenantID, retrieved.TenantID)
}

func TestResolveOrProvisionUser_ExistingUser(t *testing.T) {
	ctx := context.Background()

	store := NewPostgresIdentityStore(dbPool, PostgresStoreConfig{
		Schema:            "public",
		DefaultTenantName: "Test Tenant 2",
	})

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
	assert.Equal(t, userRef1.TenantID, userRef2.TenantID)
}

func TestResolveOrProvisionUser_MultipleUsers(t *testing.T) {
	ctx := context.Background()

	store := NewPostgresIdentityStore(dbPool, PostgresStoreConfig{
		Schema:            "public",
		DefaultTenantName: "Test Tenant 3",
	})

	// Create multiple users
	for i := 0; i < 5; i++ {
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

func TestValidateSchemaName_Integration(t *testing.T) {
	// Test valid schemas
	validSchemas := []string{"", "public", "my_schema", "schema123", "Schema_Name"}
	for _, schema := range validSchemas {
		err := validateSchemaName(schema)
		assert.NoError(t, err, "schema %q should be valid", schema)
	}

	// Test invalid schemas
	invalidSchemas := []string{
		"1schema",              // starts with digit
		"my-schema",            // contains hyphen
		"my.schema",            // contains dot
		"schema!",              // special char
		"public; DROP TABLE",   // SQL injection
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", // 64 chars - too long
	}
	for _, schema := range invalidSchemas {
		err := validateSchemaName(schema)
		assert.Error(t, err, "schema %q should be invalid", schema)
	}
}
