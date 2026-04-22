package password

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	inner  *auth.PostgresIdentityStore
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

// Register creates a new user with the given credentials. It creates the user
// via the shared PostgresIdentityStore (which handles tenant provisioning) and
// then stores the hashed password credential.
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

	// Create user via shared identity store
	identity := auth.Identity{
		Issuer:        s.cfg.Issuer,
		Subject:       req.Email,
		Email:         req.Email,
		EmailVerified: false,
		Username:      req.Username,
		Name:          req.Name,
	}

	userRef, err := s.inner.ResolveOrProvisionUser(ctx, identity)
	if err != nil {
		return auth.UserRef{}, fmt.Errorf("failed to create user: %w", err)
	}

	// Hash and store the password
	hash, err := s.hasher.Hash(req.Password)
	if err != nil {
		return auth.UserRef{}, fmt.Errorf("failed to hash password: %w", err)
	}

	err = s.queries.UpsertPasswordCredential(ctx, db.UpsertPasswordCredentialParams{
		UserID:        userRef.UserID,
		PasswordHash:  hash,
		HashAlgorithm: "argon2id",
	})
	if err != nil {
		return auth.UserRef{}, fmt.Errorf("failed to store credential: %w", err)
	}

	s.logger.Info("password user registered",
		"user_id", userRef.UserID,
		"issuer", s.cfg.Issuer,
	)

	return userRef, nil
}

// Authenticate verifies the email/password combination and returns the user
// reference on success. It implements timing attack mitigation for non-existent
// users, progressive lockout, and transparent password rehashing.
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

	// Check lockout status
	failedCount, err := s.queries.CountRecentFailedAttempts(ctx, db.CountRecentFailedAttemptsParams{
		UserID:      user.ID,
		AttemptedAt: time.Now().Add(-s.cfg.Lockout.Window),
	})
	if err != nil {
		return auth.UserRef{}, fmt.Errorf("failed to check lockout: %w", err)
	}

	lockoutDuration := CalculateLockoutDuration(int(failedCount), s.cfg.Lockout)
	if lockoutDuration > 0 {
		return auth.UserRef{}, &PasswordError{
			Err:    ErrAccountLocked,
			Status: 423,
			Detail: fmt.Sprintf("account locked, retry after %s", lockoutDuration),
		}
	}

	// Fetch the stored credential
	cred, err := s.queries.GetPasswordCredential(ctx, user.ID)
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
		_ = s.queries.RecordLoginAttempt(ctx, db.RecordLoginAttemptParams{
			UserID:    user.ID,
			IpAddress: ipAddr,
			Success:   false,
		})
		return auth.UserRef{}, &PasswordError{
			Err:    ErrInvalidCredentials,
			Status: 401,
		}
	}

	// Successful login
	_ = s.queries.RecordLoginAttempt(ctx, db.RecordLoginAttemptParams{
		UserID:    user.ID,
		IpAddress: ipAddr,
		Success:   true,
	})

	// Check email verification if required
	if s.cfg.RequireEmailVerification && !user.EmailVerified {
		return auth.UserRef{}, &PasswordError{
			Err:    ErrEmailNotVerified,
			Status: 403,
		}
	}

	// Transparent rehash if needed
	if needsRehash {
		newHash, hashErr := s.hasher.Hash(password)
		if hashErr != nil {
			s.logger.Error("failed to rehash password",
				"user_id", user.ID,
				"error", hashErr,
			)
		} else {
			_ = s.queries.UpdatePasswordHash(ctx, db.UpdatePasswordHashParams{
				UserID:       user.ID,
				PasswordHash: newHash,
			})
		}
	}

	// Update last login
	_ = s.queries.UpdateLastLoginAt(ctx, user.ID)

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

	// Validate new password policy
	if policyErr := s.cfg.Policy.Validate(newPassword); policyErr != nil {
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
	_ = s.queries.InvalidateUserTokens(ctx, db.InvalidateUserTokensParams{
		UserID:    userID,
		TokenType: "password_reset",
	})

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
	_ = s.queries.InvalidateUserTokens(ctx, db.InvalidateUserTokensParams{
		UserID:    user.ID,
		TokenType: "password_reset",
	})

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

	// Validate the new password
	if policyErr := s.cfg.Policy.Validate(newPassword); policyErr != nil {
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
	_ = s.queries.InvalidateUserTokens(ctx, db.InvalidateUserTokensParams{
		UserID:    userID,
		TokenType: "email_verification",
	})

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
