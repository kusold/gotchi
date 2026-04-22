package password

import (
	"context"
	"time"
)

// Default configuration values for password authentication.
const (
	// DefaultPathPrefix is the default URL prefix for password auth routes.
	DefaultPathPrefix = "/auth/password"
	// DefaultIssuer is the issuer string stored in the users table for
	// password-authenticated users. Distinguishes them from OIDC users.
	DefaultIssuer = "local"
	// DefaultMinPasswordLength is the minimum password length (NIST recommendation).
	DefaultMinPasswordLength = 8
	// DefaultMaxPasswordLength prevents Argon2 abuse via extremely long inputs.
	DefaultMaxPasswordLength = 128
)

// PasswordConfig holds all configuration for password authentication.
// Use [PasswordConfig.WithDefaults] to populate defaults.
type PasswordConfig struct {
	// PathPrefix is the base path for password auth routes.
	// Defaults to [DefaultPathPrefix].
	PathPrefix string
	// Issuer is the issuer string stored in the users table for
	// password-authenticated users. Defaults to [DefaultIssuer].
	Issuer string
	// SessionKey is the key used to store claims in the session.
	// Defaults to "auth" (same as OIDC).
	SessionKey string
	// RequireEmailVerification blocks login until the user's email is verified.
	// Defaults to false.
	RequireEmailVerification bool
	// DefaultTenantName is the name used when creating the first tenant for a
	// new user who has no existing tenant. Defaults to "Default".
	DefaultTenantName string
	// Hashing configures Argon2id parameters.
	Hashing HashingConfig
	// Policy configures password complexity rules.
	Policy PasswordPolicy
	// Lockout configures account lockout behavior.
	Lockout LockoutConfig
	// Tokens configures reset and verification token behavior.
	Tokens TokenConfig
	// EmailSender sends emails for password reset and verification flows.
	// If nil, tokens are returned in HTTP responses (development mode).
	EmailSender EmailSender
}

// WithDefaults returns a copy of PasswordConfig with empty fields populated
// by their default values.
func (c PasswordConfig) WithDefaults() PasswordConfig {
	cfg := c
	if cfg.PathPrefix == "" {
		cfg.PathPrefix = DefaultPathPrefix
	}
	if cfg.Issuer == "" {
		cfg.Issuer = DefaultIssuer
	}
	if cfg.SessionKey == "" {
		cfg.SessionKey = "auth"
	}
	if cfg.DefaultTenantName == "" {
		cfg.DefaultTenantName = "Default"
	}
	cfg.Hashing = cfg.Hashing.withDefaults()
	cfg.Lockout = cfg.Lockout.withDefaults()
	cfg.Tokens = cfg.Tokens.withDefaults()
	cfg.Policy = cfg.Policy.withDefaults()
	return cfg
}

// HashingConfig configures Argon2id password hashing parameters.
// Defaults follow OWASP 2024 minimum recommendations.
type HashingConfig struct {
	// Memory in KiB. Default: 19456 (19 MiB).
	Memory uint32
	// Iterations (time cost). Default: 2.
	Iterations uint32
	// Parallelism (degree of parallelism). Default: 1.
	Parallelism uint8
	// SaltLength in bytes. Default: 16.
	SaltLength uint32
	// KeyLength (output hash length) in bytes. Default: 32.
	KeyLength uint32
}

func (c HashingConfig) withDefaults() HashingConfig {
	cfg := c
	if cfg.Memory == 0 {
		cfg.Memory = 19456
	}
	if cfg.Iterations == 0 {
		cfg.Iterations = 2
	}
	if cfg.Parallelism == 0 {
		cfg.Parallelism = 1
	}
	if cfg.SaltLength == 0 {
		cfg.SaltLength = 16
	}
	if cfg.KeyLength == 0 {
		cfg.KeyLength = 32
	}
	return cfg
}

// LockoutConfig configures account lockout behavior after failed login attempts.
// Uses a sliding-window model: accounts are locked when failed attempts within
// the Window reach MaxAttempts. The lockout clears when enough old failures age
// out of the Window.
type LockoutConfig struct {
	// MaxAttempts before lockout. Default: 5.
	MaxAttempts int
	// Window is the time window for counting failed attempts. Default: 15 minutes.
	Window time.Duration
}

func (c LockoutConfig) withDefaults() LockoutConfig {
	cfg := c
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 5
	}
	if cfg.Window == 0 {
		cfg.Window = 15 * time.Minute
	}
	return cfg
}

// TokenConfig configures reset and verification token behavior.
type TokenConfig struct {
	// ResetTokenExpiry is how long password reset tokens are valid.
	// Default: 1 hour.
	ResetTokenExpiry time.Duration
	// VerificationTokenExpiry is how long email verification tokens are valid.
	// Default: 24 hours.
	VerificationTokenExpiry time.Duration
	// TokenLength in bytes (before base64url encoding). Default: 32.
	TokenLength int
}

func (c TokenConfig) withDefaults() TokenConfig {
	cfg := c
	if cfg.ResetTokenExpiry == 0 {
		cfg.ResetTokenExpiry = 1 * time.Hour
	}
	if cfg.VerificationTokenExpiry == 0 {
		cfg.VerificationTokenExpiry = 24 * time.Hour
	}
	if cfg.TokenLength == 0 {
		cfg.TokenLength = 32
	}
	return cfg
}

// PasswordPolicy configures password complexity rules. v1 supports min/max
// length and contextual word checks only (no zxcvbn).
type PasswordPolicy struct {
	// MinLength is the minimum password length. Default: 8.
	MinLength int
	// MaxLength is the maximum password length. Default: 128.
	MaxLength int
	// RejectContextual rejects passwords containing the user's email or
	// username. Default: true.
	RejectContextual bool
}

func (c PasswordPolicy) withDefaults() PasswordPolicy {
	cfg := c
	if cfg.MinLength == 0 {
		cfg.MinLength = DefaultMinPasswordLength
	}
	if cfg.MaxLength == 0 {
		cfg.MaxLength = DefaultMaxPasswordLength
	}
	return cfg
}

// EmailSender abstracts email delivery for password reset and verification
// flows. If nil on PasswordConfig, tokens are returned in HTTP responses
// (development mode).
type EmailSender interface {
	// SendPasswordReset sends a password reset email containing the token.
	SendPasswordReset(ctx context.Context, email, token string) error
	// SendEmailVerification sends an email verification link containing the token.
	SendEmailVerification(ctx context.Context, email, token string) error
	// SendPasswordChanged sends a notification that the password was changed.
	SendPasswordChanged(ctx context.Context, email string) error
}
