package password

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPasswordConfig_WithDefaults(t *testing.T) {
	cfg := PasswordConfig{}
	got := cfg.WithDefaults()

	assert.Equal(t, "/auth/password", got.PathPrefix)
	assert.Equal(t, "local", got.Issuer)
	assert.Equal(t, "auth", got.SessionKey)
	assert.Equal(t, "Default", got.DefaultTenantName)
	assert.False(t, got.RequireEmailVerification)
}

func TestPasswordConfig_WithDefaults_PreservesCustom(t *testing.T) {
	cfg := PasswordConfig{
		PathPrefix:               "/api/auth",
		Issuer:                   "custom",
		SessionKey:               "custom-auth",
		RequireEmailVerification: true,
		DefaultTenantName:        "My Org",
	}
	got := cfg.WithDefaults()

	assert.Equal(t, "/api/auth", got.PathPrefix)
	assert.Equal(t, "custom", got.Issuer)
	assert.Equal(t, "custom-auth", got.SessionKey)
	assert.True(t, got.RequireEmailVerification)
	assert.Equal(t, "My Org", got.DefaultTenantName)
}

func TestPasswordConfig_WithDefaults_Idempotent(t *testing.T) {
	cfg := PasswordConfig{}.WithDefaults()
	again := cfg.WithDefaults()
	assert.Equal(t, cfg, again)
}

func TestHashingConfig_Defaults(t *testing.T) {
	cfg := HashingConfig{}.withDefaults()
	assert.Equal(t, uint32(19456), cfg.Memory)
	assert.Equal(t, uint32(2), cfg.Iterations)
	assert.Equal(t, uint8(1), cfg.Parallelism)
	assert.Equal(t, uint32(16), cfg.SaltLength)
	assert.Equal(t, uint32(32), cfg.KeyLength)
}

func TestLockoutConfig_Defaults(t *testing.T) {
	cfg := LockoutConfig{}.withDefaults()
	assert.Equal(t, 5, cfg.MaxAttempts)
	assert.Equal(t, 15*time.Minute, cfg.Window)
}

func TestTokenConfig_Defaults(t *testing.T) {
	cfg := TokenConfig{}.withDefaults()
	assert.Equal(t, 1*time.Hour, cfg.ResetTokenExpiry)
	assert.Equal(t, 24*time.Hour, cfg.VerificationTokenExpiry)
	assert.Equal(t, 32, cfg.TokenLength)
}

func TestPasswordPolicy_Defaults(t *testing.T) {
	cfg := PasswordPolicy{}.withDefaults()
	assert.Equal(t, 8, cfg.MinLength)
	assert.Equal(t, 128, cfg.MaxLength)
	assert.False(t, cfg.RejectContextual) // zero value is false, not defaulted
}
