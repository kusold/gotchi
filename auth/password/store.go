package password

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kusold/gotchi/auth"
	"github.com/kusold/gotchi/internal/db"
)

// RegisterRequest holds the input for user registration.
type RegisterRequest struct {
	Email    string
	Password string
	Username string // optional
	Name     string // optional
}

// PasswordIdentityStore implements [auth.IdentityStore] for local
// username/password authentication. It delegates user/tenant operations
// to an underlying [auth.PostgresIdentityStore] and adds password-specific
// credential management.
type PasswordIdentityStore struct {
	inner   *auth.PostgresIdentityStore
	pool    *pgxpool.Pool
	queries *db.Queries
	hasher  Hasher
	cfg     PasswordConfig
	logger  *slog.Logger
}

// NewPasswordIdentityStore creates a new PasswordIdentityStore that wraps the
// given PostgresIdentityStore for user/tenant operations and adds password
// credential management.
func NewPasswordIdentityStore(pool *pgxpool.Pool, inner *auth.PostgresIdentityStore, cfg PasswordConfig, logger *slog.Logger) (*PasswordIdentityStore, error) {
	if pool == nil {
		return nil, fmt.Errorf("postgres pool is required")
	}
	if inner == nil {
		return nil, fmt.Errorf("identity store is required")
	}

	conf := cfg.WithDefaults()
	if logger == nil {
		logger = slog.Default()
	}

	return &PasswordIdentityStore{
		inner:   inner,
		pool:    pool,
		queries: db.New(pool),
		hasher:  NewArgon2idHasher(conf.Hashing),
		cfg:     conf,
		logger:  logger,
	}, nil
}

// --- IdentityStore delegation ---

// ResolveOrProvisionUser delegates to the underlying PostgresIdentityStore.
func (s *PasswordIdentityStore) ResolveOrProvisionUser(ctx context.Context, identity auth.Identity) (auth.UserRef, error) {
	return s.inner.ResolveOrProvisionUser(ctx, identity)
}

// ListMemberships delegates to the underlying PostgresIdentityStore.
func (s *PasswordIdentityStore) ListMemberships(ctx context.Context, userID uuid.UUID) ([]auth.Membership, error) {
	return s.inner.ListMemberships(ctx, userID)
}

// GetTenantDisplay delegates to the underlying PostgresIdentityStore.
func (s *PasswordIdentityStore) GetTenantDisplay(ctx context.Context, tenantID uuid.UUID) (auth.TenantDisplay, error) {
	return s.inner.GetTenantDisplay(ctx, tenantID)
}

// --- Password-specific operations ---

// Register creates a new user with the given credentials. The entire
// operation — user lookup, creation, and credential storage — runs inside a
// single database transaction so that a failure at any step rolls back cleanly
// without leaving orphan records.
//
// Returns ErrEmailAlreadyRegistered if a user with the same email and local
// issuer already has a password credential.
func (s *PasswordIdentityStore) Register(ctx context.Context, req RegisterRequest) (auth.UserRef, error) {
	if req.Email == "" || req.Password == "" {
		return auth.UserRef{}, &PasswordError{
			Err:    ErrPasswordPolicyViolation,
			Status: 400,
			Detail: "email and password are required",
		}
	}

	// Validate password policy
	contextWords := []string{req.Email}
	if req.Username != "" {
		contextWords = append(contextWords, req.Username)
	}
	if err := s.cfg.Policy.Validate(req.Password, contextWords...); err != nil {
		return auth.UserRef{}, err
	}

	// Hash the password before opening the transaction so the expensive Argon2id
	// computation doesn't hold a database lock.
	hash, err := s.hasher.Hash(req.Password)
	if err != nil {
		return auth.UserRef{}, fmt.Errorf("failed to hash password: %w", err)
	}

	// Begin a transaction so user creation + credential storage are atomic.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return auth.UserRef{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	txQueries := s.queries.WithTx(tx)
	txInner := s.inner.WithTx(tx)

	// Check if a local user with this email already has a password credential
	existingUser, err := txQueries.GetUserByEmailAndIssuer(ctx, db.GetUserByEmailAndIssuerParams{
		Email:  req.Email,
		Issuer: s.cfg.Issuer,
	})
	if err == nil {
		// User exists — check if they already have a password credential
		_, credErr := txQueries.GetPasswordCredential(ctx, existingUser.ID)
		if credErr == nil {
			return auth.UserRef{}, &PasswordError{
				Err:    ErrEmailAlreadyRegistered,
				Status: 409,
				Detail: "an account with this email already exists",
			}
		}
		// User exists but has no password credential (e.g. OIDC-only).
		// Fall through to attach a password credential.
	}

	// Create user via shared identity store (within the same transaction)
	identity := auth.Identity{
		Issuer:        s.cfg.Issuer,
		Subject:       req.Email,
		Email:         req.Email,
		EmailVerified: false,
		Username:      req.Username,
		Name:          req.Name,
	}

	userRef, err := txInner.ResolveOrProvisionUser(ctx, identity)
	if err != nil {
		// Handle unique constraint violation from concurrent registration
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return auth.UserRef{}, &PasswordError{
				Err:    ErrEmailAlreadyRegistered,
				Status: 409,
				Detail: "an account with this email already exists",
			}
		}
		return auth.UserRef{}, fmt.Errorf("failed to create user: %w", err)
	}

	// Store the password credential within the same transaction
	err = txQueries.UpsertPasswordCredential(ctx, db.UpsertPasswordCredentialParams{
		UserID:        userRef.UserID,
		PasswordHash:  hash,
		HashAlgorithm: "argon2id",
	})
	if err != nil {
		return auth.UserRef{}, fmt.Errorf("failed to store credential: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return auth.UserRef{}, fmt.Errorf("failed to commit registration: %w", err)
	}

	s.logger.Info("password user registered",
		"user_id", userRef.UserID,
		"issuer", s.cfg.Issuer,
	)

	return userRef, nil
}

// Authenticate verifies the email/password combination and returns the user
// reference on success. It implements timing attack mitigation for non-existent
// users, sliding-window lockout, and transparent password rehashing.
//
// The lockout check, credential fetch, and login attempt recording run inside a
// single database transaction to prevent concurrent attempts from racing past
// the lockout threshold.
func (s *PasswordIdentityStore) Authenticate(ctx context.Context, email, password, ipAddress string) (auth.UserRef, error) {
	// Look up user by email + local issuer
	user, err := s.queries.GetUserByEmailAndIssuer(ctx, db.GetUserByEmailAndIssuerParams{
		Email:  email,
		Issuer: s.cfg.Issuer,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			// Timing attack mitigation: perform a dummy hash so response time
			// is similar to a real password check
			_, _ = s.hasher.Hash("dummy-password-for-timing")
			return auth.UserRef{}, &PasswordError{
				Err:    ErrInvalidCredentials,
				Status: 401,
			}
		}
		return auth.UserRef{}, fmt.Errorf("failed to query user: %w", err)
	}

	// Begin a transaction so the lockout check + credential verify + attempt
	// recording are atomic. This prevents concurrent login attempts from
	// racing past the lockout threshold.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return auth.UserRef{}, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	txQueries := s.queries.WithTx(tx)

	// Check lockout status
	failedCount, err := txQueries.CountRecentFailedAttempts(ctx, db.CountRecentFailedAttemptsParams{
		UserID:      user.ID,
		AttemptedAt: time.Now().Add(-s.cfg.Lockout.Window),
	})
	if err != nil {
		return auth.UserRef{}, fmt.Errorf("failed to check lockout: %w", err)
	}

	if IsLockedOut(int(failedCount), s.cfg.Lockout) {
		// Timing mitigation: perform a dummy hash so that response time for a
		// locked account is indistinguishable from a wrong-password attempt,
		// preventing timing-based account enumeration.
		_, _ = s.hasher.Hash("dummy-password-for-timing")
		return auth.UserRef{}, &PasswordError{
			Err:    ErrAccountLocked,
			Status: 423,
		}
	}

	// Fetch the stored credential
	cred, err := txQueries.GetPasswordCredential(ctx, user.ID)
	if err != nil {
		if err == pgx.ErrNoRows {
			// User exists but has no password credential
			return auth.UserRef{}, &PasswordError{
				Err:    ErrInvalidCredentials,
				Status: 401,
			}
		}
		return auth.UserRef{}, fmt.Errorf("failed to fetch credential: %w", err)
	}

	// Verify the password
	match, needsRehash, verifyErr := s.hasher.Verify(password, cred.PasswordHash)
	if verifyErr != nil {
		return auth.UserRef{}, fmt.Errorf("failed to verify password: %w", verifyErr)
	}

	// Record the login attempt
	var ipAddr *netip.Addr
	if ipAddress != "" {
		if addr, parseErr := netip.ParseAddr(ipAddress); parseErr == nil {
			ipAddr = &addr
		}
	}

	if !match {
		if recordErr := txQueries.RecordLoginAttempt(ctx, db.RecordLoginAttemptParams{
			UserID:    user.ID,
			IpAddress: ipAddr,
			Success:   false,
		}); recordErr != nil {
			s.logger.Error("failed to record failed login attempt",
				"user_id", user.ID,
				"error", recordErr,
			)
		}
		// Commit so the failed attempt is persisted for lockout tracking.
		// After Commit, the deferred Rollback is a no-op.
		if commitErr := tx.Commit(ctx); commitErr != nil {
			s.logger.Error("failed to commit failed login attempt",
				"user_id", user.ID,
				"error", commitErr,
			)
		}
		return auth.UserRef{}, &PasswordError{
			Err:    ErrInvalidCredentials,
			Status: 401,
		}
	}

	// Record successful login attempt
	if recordErr := txQueries.RecordLoginAttempt(ctx, db.RecordLoginAttemptParams{
		UserID:    user.ID,
		IpAddress: ipAddr,
		Success:   true,
	}); recordErr != nil {
		s.logger.Error("failed to record successful login attempt",
			"user_id", user.ID,
			"error", recordErr,
		)
	}

	// Check email verification if required. Commit the successful attempt
	// first so the lockout window resets for this user.
	if s.cfg.RequireEmailVerification && !user.EmailVerified {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			s.logger.Error("failed to commit login attempt for unverified user",
				"user_id", user.ID,
				"error", commitErr,
			)
		}
		return auth.UserRef{}, &PasswordError{
			Err:    ErrEmailNotVerified,
			Status: 403,
		}
	}

	// Update last login
	if updateErr := txQueries.UpdateLastLoginAt(ctx, user.ID); updateErr != nil {
		s.logger.Error("failed to update last login time",
			"user_id", user.ID,
			"error", updateErr,
		)
	}

	// Commit the login attempt + last_login update before the expensive
	// rehash so the transaction doesn't hold a DB lock during Argon2id.
	if needsRehash {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return auth.UserRef{}, fmt.Errorf("failed to commit transaction: %w", commitErr)
		}

		// Rehash outside the transaction — Argon2id is CPU-bound and
		// should not hold a database connection.
		newHash, hashErr := s.hasher.Hash(password)
		if hashErr != nil {
			s.logger.Error("failed to rehash password",
				"user_id", user.ID,
				"error", hashErr,
			)
			// Rehash failure is non-fatal; login still succeeds.
		} else if updateErr := s.queries.UpdatePasswordHash(ctx, db.UpdatePasswordHashParams{
			UserID:       user.ID,
			PasswordHash: newHash,
		}); updateErr != nil {
			s.logger.Error("failed to update rehashed password",
				"user_id", user.ID,
				"error", updateErr,
			)
		}
		// tx.Rollback was deferred but tx is already committed, so it's a no-op.
	} else {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return auth.UserRef{}, fmt.Errorf("failed to commit transaction: %w", commitErr)
		}
	}

	s.logger.Info("password login succeeded",
		"user_id", user.ID,
		"ip", ipAddress,
	)

	return auth.UserRef{
		UserID:  user.ID,
		Issuer:  user.Issuer,
		Subject: user.IdentifierSubject,
	}, nil
}

// ChangePassword updates the user's password after verifying the old password.
func (s *PasswordIdentityStore) ChangePassword(ctx context.Context, userID uuid.UUID, oldPassword, newPassword string) error {
	cred, err := s.queries.GetPasswordCredential(ctx, userID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return &PasswordError{Err: ErrUserNotFound, Status: 404}
		}
		return fmt.Errorf("failed to fetch credential: %w", err)
	}

	match, _, err := s.hasher.Verify(oldPassword, cred.PasswordHash)
	if err != nil {
		return fmt.Errorf("failed to verify password: %w", err)
	}
	if !match {
		return &PasswordError{Err: ErrInvalidCredentials, Status: 401}
	}

	// Look up user to provide context words (email, username) for policy validation.
	user, err := s.queries.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to fetch user: %w", err)
	}

	contextWords := []string{user.Email}
	if user.Username.Valid && user.Username.String != "" {
		contextWords = append(contextWords, user.Username.String)
	}

	// Validate new password policy
	if policyErr := s.cfg.Policy.Validate(newPassword, contextWords...); policyErr != nil {
		return policyErr
	}

	newHash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	err = s.queries.UpdatePasswordHash(ctx, db.UpdatePasswordHashParams{
		UserID:       userID,
		PasswordHash: newHash,
	})
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	// Invalidate all password reset tokens
	if invalidateErr := s.queries.InvalidateUserTokens(ctx, db.InvalidateUserTokensParams{
		UserID:    userID,
		TokenType: "password_reset",
	}); invalidateErr != nil {
		s.logger.Error("failed to invalidate password reset tokens",
			"user_id", userID,
			"error", invalidateErr,
		)
	}

	s.logger.Info("password changed", "user_id", userID)
	return nil
}

// InitiatePasswordReset generates a password reset token. It always returns
// a nil error to prevent user enumeration -- if the user doesn't exist, no
// token is generated but no error is returned. The returned plaintext token
// should be sent to the user via email (or returned in dev mode).
func (s *PasswordIdentityStore) InitiatePasswordReset(ctx context.Context, email string) (string, error) {
	user, err := s.queries.GetUserByEmailAndIssuer(ctx, db.GetUserByEmailAndIssuerParams{
		Email:  email,
		Issuer: s.cfg.Issuer,
	})
	if err != nil {
		// Return nil error to prevent enumeration
		return "", nil
	}

	// Invalidate any existing reset tokens
	if invalidateErr := s.queries.InvalidateUserTokens(ctx, db.InvalidateUserTokensParams{
		UserID:    user.ID,
		TokenType: "password_reset",
	}); invalidateErr != nil {
		s.logger.Error("failed to invalidate existing reset tokens",
			"user_id", user.ID,
			"error", invalidateErr,
		)
	}

	// Generate a new token
	plaintext, hash, err := GenerateToken(s.cfg.Tokens.TokenLength)
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	err = s.queries.InsertAuthToken(ctx, db.InsertAuthTokenParams{
		UserID:    user.ID,
		TokenHash: hash,
		TokenType: "password_reset",
		ExpiresAt: time.Now().Add(s.cfg.Tokens.ResetTokenExpiry),
	})
	if err != nil {
		return "", fmt.Errorf("failed to store token: %w", err)
	}

	return plaintext, nil
}

// CompletePasswordReset verifies the token and updates the user's password.
func (s *PasswordIdentityStore) CompletePasswordReset(ctx context.Context, token, newPassword string) error {
	tokenHash := sha256Sum(token)

	userID, err := s.queries.ConsumeAuthToken(ctx, db.ConsumeAuthTokenParams{
		TokenHash: tokenHash,
		TokenType: "password_reset",
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return &PasswordError{Err: ErrTokenInvalid, Status: 400}
		}
		return fmt.Errorf("failed to consume token: %w", err)
	}

	// Look up user to provide context words (email, username) for policy validation.
	user, err := s.queries.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to fetch user: %w", err)
	}

	contextWords := []string{user.Email}
	if user.Username.Valid && user.Username.String != "" {
		contextWords = append(contextWords, user.Username.String)
	}

	// Validate the new password
	if policyErr := s.cfg.Policy.Validate(newPassword, contextWords...); policyErr != nil {
		return policyErr
	}

	// Hash and store the new password
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	err = s.queries.UpdatePasswordHash(ctx, db.UpdatePasswordHashParams{
		UserID:       userID,
		PasswordHash: hash,
	})
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	s.logger.Info("password reset completed", "user_id", userID)
	return nil
}

// InitiateEmailVerification generates an email verification token for the user.
func (s *PasswordIdentityStore) InitiateEmailVerification(ctx context.Context, userID uuid.UUID) (string, error) {
	// Invalidate any existing verification tokens
	if invalidateErr := s.queries.InvalidateUserTokens(ctx, db.InvalidateUserTokensParams{
		UserID:    userID,
		TokenType: "email_verification",
	}); invalidateErr != nil {
		s.logger.Error("failed to invalidate existing verification tokens",
			"user_id", userID,
			"error", invalidateErr,
		)
	}

	plaintext, hash, err := GenerateToken(s.cfg.Tokens.TokenLength)
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	err = s.queries.InsertAuthToken(ctx, db.InsertAuthTokenParams{
		UserID:    userID,
		TokenHash: hash,
		TokenType: "email_verification",
		ExpiresAt: time.Now().Add(s.cfg.Tokens.VerificationTokenExpiry),
	})
	if err != nil {
		return "", fmt.Errorf("failed to store token: %w", err)
	}

	return plaintext, nil
}

// VerifyEmail verifies the user's email address using the provided token.
func (s *PasswordIdentityStore) VerifyEmail(ctx context.Context, token string) error {
	tokenHash := sha256Sum(token)

	userID, err := s.queries.ConsumeAuthToken(ctx, db.ConsumeAuthTokenParams{
		TokenHash: tokenHash,
		TokenType: "email_verification",
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return &PasswordError{Err: ErrTokenInvalid, Status: 400}
		}
		return fmt.Errorf("failed to consume token: %w", err)
	}

	err = s.queries.UpdateEmailVerified(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to update email verified: %w", err)
	}

	s.logger.Info("email verified", "user_id", userID)
	return nil
}
