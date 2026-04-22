package password

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/internal/testutil"
)

// Integration tests require Docker. Run without -short to include them:
//
//	go test ./auth/password/...              # unit + integration (requires Docker)
//	go test ./auth/password/... -short       # unit tests only

var integrationDB *testutil.TestDB

func TestMain(m *testing.M) {
	flag.Parse()
	if !testing.Short() {
		integrationDB = testutil.SetupTestDB(m)
		if integrationDB == nil {
			fmt.Println("Integration tests require a container runtime")
			os.Exit(1)
		}
	}

	code := m.Run()
	if integrationDB != nil {
		integrationDB.Close()
	}
	os.Exit(code)
}

func requireIntegrationDB(t *testing.T) *testutil.TestDB {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	return integrationDB
}

func newTestPasswordStore(t *testing.T) (*PasswordIdentityStore, *auth.PostgresIdentityStore) {
	t.Helper()
	db := requireIntegrationDB(t)

	inner, err := auth.NewPostgresIdentityStore(db.Pool, auth.PostgresStoreConfig{
		DefaultTenantName: "Test Tenant " + t.Name(),
	})
	require.NoError(t, err)

	cfg := PasswordConfig{
		Hashing: HashingConfig{
			Memory:      4096, // lower for test speed
			Iterations:  1,
			Parallelism: 1,
			SaltLength:  8,
			KeyLength:   16,
		},
		Lockout: LockoutConfig{
			MaxAttempts: 3,
			Window:      15 * time.Minute,
		},
	}

	store, err := NewPasswordIdentityStore(db.Pool, inner, cfg, nil)
	require.NoError(t, err)
	return store, inner
}

func uniqueEmail(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test-%s@example.com", uuid.New().String()[:8])
}

func TestRegister_Success(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)
	email := uniqueEmail(t)

	userRef, err := store.Register(ctx, RegisterRequest{
		Email:    email,
		Password: "secure-password-123",
		Username: "testuser",
		Name:     "Test User",
	})
	require.NoError(t, err)
	assert.NotEqual(t, uuid.UUID{}, userRef.UserID)
	assert.Equal(t, "local", userRef.Issuer)
	assert.Equal(t, email, userRef.Subject)

	// User should have a membership
	memberships, err := store.ListMemberships(ctx, userRef.UserID)
	require.NoError(t, err)
	assert.Len(t, memberships, 1)
}

func TestRegister_DuplicateEmail(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)
	email := uniqueEmail(t)

	_, err := store.Register(ctx, RegisterRequest{
		Email:    email,
		Password: "first-password-123",
	})
	require.NoError(t, err)

	// Second registration should be rejected.
	_, err = store.Register(ctx, RegisterRequest{
		Email:    email,
		Password: "second-password-456",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrEmailAlreadyRegistered))
}

func TestRegister_PasswordPolicyViolation(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)

	_, err := store.Register(ctx, RegisterRequest{
		Email:    uniqueEmail(t),
		Password: "short",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPasswordPolicyViolation))
}

func TestRegister_MissingFields(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)

	_, err := store.Register(ctx, RegisterRequest{Email: "", Password: "password-123"})
	assert.True(t, errors.Is(err, ErrPasswordPolicyViolation))

	_, err = store.Register(ctx, RegisterRequest{Email: "test@example.com", Password: ""})
	assert.True(t, errors.Is(err, ErrPasswordPolicyViolation))
}

func TestAuthenticate_Success(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)
	email := uniqueEmail(t)
	password := "correct-horse-battery-staple"

	_, err := store.Register(ctx, RegisterRequest{
		Email:    email,
		Password: password,
	})
	require.NoError(t, err)

	userRef, err := store.Authenticate(ctx, email, password, "127.0.0.1")
	require.NoError(t, err)
	assert.NotEqual(t, uuid.UUID{}, userRef.UserID)
}

func TestAuthenticate_WrongPassword(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)
	email := uniqueEmail(t)

	_, err := store.Register(ctx, RegisterRequest{
		Email:    email,
		Password: "correct-password",
	})
	require.NoError(t, err)

	_, err = store.Authenticate(ctx, email, "wrong-password", "127.0.0.1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidCredentials))
}

func TestAuthenticate_NonexistentEmail(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)

	_, err := store.Authenticate(ctx, "nonexistent@example.com", "any-password", "127.0.0.1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidCredentials))
}

func TestAuthenticate_TransparentRehash(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)
	email := uniqueEmail(t)
	password := "rehash-test-password"
	db := requireIntegrationDB(t)

	_, err := store.Register(ctx, RegisterRequest{
		Email:    email,
		Password: password,
	})
	require.NoError(t, err)

	userRef, err := store.Authenticate(ctx, email, password, "127.0.0.1")
	require.NoError(t, err)

	// Create a new store with different hash params to trigger rehash
	cfg := PasswordConfig{
		Hashing: HashingConfig{
			Memory:      8192, // different from original 4096
			Iterations:  2,
			Parallelism: 1,
			SaltLength:  8,
			KeyLength:   16,
		},
	}
	inner, err := auth.NewPostgresIdentityStore(db.Pool, auth.PostgresStoreConfig{
		DefaultTenantName: "Rehash Tenant",
	})
	require.NoError(t, err)
	newStore, err := NewPasswordIdentityStore(db.Pool, inner, cfg, nil)
	require.NoError(t, err)

	userRef2, err := newStore.Authenticate(ctx, email, password, "127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, userRef.UserID, userRef2.UserID)

	userRef3, err := newStore.Authenticate(ctx, email, password, "127.0.0.1")
	require.NoError(t, err)
	assert.Equal(t, userRef.UserID, userRef3.UserID)
}

func TestChangePassword_Success(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)
	email := uniqueEmail(t)
	oldPassword := "old-password-123"
	newPassword := "new-password-456"

	userRef, err := store.Register(ctx, RegisterRequest{
		Email:    email,
		Password: oldPassword,
	})
	require.NoError(t, err)

	err = store.ChangePassword(ctx, userRef.UserID, oldPassword, newPassword)
	require.NoError(t, err)

	// Old password should no longer work
	_, err = store.Authenticate(ctx, email, oldPassword, "127.0.0.1")
	assert.True(t, errors.Is(err, ErrInvalidCredentials))

	// New password should work
	_, err = store.Authenticate(ctx, email, newPassword, "127.0.0.1")
	require.NoError(t, err)
}

func TestChangePassword_WrongOldPassword(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)

	userRef, err := store.Register(ctx, RegisterRequest{
		Email:    uniqueEmail(t),
		Password: "original-password",
	})
	require.NoError(t, err)

	err = store.ChangePassword(ctx, userRef.UserID, "wrong-old-password", "new-password")
	assert.True(t, errors.Is(err, ErrInvalidCredentials))
}

func TestPasswordReset_FullFlow(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)
	email := uniqueEmail(t)
	oldPassword := "old-password-123"
	newPassword := "new-password-456"

	_, err := store.Register(ctx, RegisterRequest{
		Email:    email,
		Password: oldPassword,
	})
	require.NoError(t, err)

	// Initiate password reset
	token, err := store.InitiatePasswordReset(ctx, email)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	// Complete password reset
	err = store.CompletePasswordReset(ctx, token, newPassword)
	require.NoError(t, err)

	// Old password should no longer work
	_, err = store.Authenticate(ctx, email, oldPassword, "127.0.0.1")
	assert.True(t, errors.Is(err, ErrInvalidCredentials))

	// New password should work
	_, err = store.Authenticate(ctx, email, newPassword, "127.0.0.1")
	require.NoError(t, err)
}

func TestPasswordReset_InvalidToken(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)

	err := store.CompletePasswordReset(ctx, "invalid-token", "new-password")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTokenInvalid))
}

func TestPasswordReset_NonexistentEmail(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)

	// Should return empty token and nil error (enumeration protection)
	token, err := store.InitiatePasswordReset(ctx, "nonexistent@example.com")
	assert.NoError(t, err)
	assert.Empty(t, token)
}

func TestEmailVerification_FullFlow(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)
	email := uniqueEmail(t)

	userRef, err := store.Register(ctx, RegisterRequest{
		Email:    email,
		Password: "password-123",
	})
	require.NoError(t, err)

	token, err := store.InitiateEmailVerification(ctx, userRef.UserID)
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	err = store.VerifyEmail(ctx, token)
	require.NoError(t, err)
}

func TestEmailVerification_InvalidToken(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)

	err := store.VerifyEmail(ctx, "invalid-token")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTokenInvalid))
}

func TestAuthenticate_LockoutAfterFailedAttempts(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestPasswordStore(t)
	email := uniqueEmail(t)
	correctPassword := "correct-password-123"

	_, err := store.Register(ctx, RegisterRequest{
		Email:    email,
		Password: correctPassword,
	})
	require.NoError(t, err)

	// MaxAttempts is 3 in test config. Fail 3 times.
	for i := 0; i < 3; i++ {
		_, err = store.Authenticate(ctx, email, "wrong-password", "127.0.0.1")
		require.Error(t, err, "attempt %d should fail", i+1)
		assert.True(t, errors.Is(err, ErrInvalidCredentials))
	}

	// The 4th attempt should be locked out even with the correct password.
	_, err = store.Authenticate(ctx, email, correctPassword, "127.0.0.1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAccountLocked), "should be locked out after %d failed attempts, got: %v", 3, err)
}
