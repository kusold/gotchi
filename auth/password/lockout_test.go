package password

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func defaultLockoutConfig() LockoutConfig {
	return LockoutConfig{}.withDefaults()
}

func TestCalculateLockoutDuration_NoLockout(t *testing.T) {
	cfg := defaultLockoutConfig()
	for _, attempts := range []int{0, 1, 2, 3, 4} {
		assert.Equal(t, time.Duration(0), CalculateLockoutDuration(attempts, cfg),
			"no lockout for %d attempts", attempts)
	}
}

func TestCalculateLockoutDuration_ProgressiveBackoff(t *testing.T) {
	cfg := defaultLockoutConfig()

	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{5, 1 * time.Minute},
		{6, 2 * time.Minute},
		{7, 4 * time.Minute},
		{8, 8 * time.Minute},
		{9, 16 * time.Minute},
		{10, 30 * time.Minute}, // capped at MaxLockout
		{20, 30 * time.Minute}, // still capped
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := CalculateLockoutDuration(tt.attempts, cfg)
			assert.Equal(t, tt.want, got, "lockout for %d attempts", tt.attempts)
		})
	}
}

func TestCalculateLockoutDuration_CustomConfig(t *testing.T) {
	cfg := LockoutConfig{
		MaxAttempts:   3,
		BaseLockout:   30 * time.Second,
		MaxLockout:    5 * time.Minute,
		BackoffFactor: 3.0,
	}

	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{2, 0},
		{3, 30 * time.Second},
		{4, 90 * time.Second},    // 30s * 3
		{5, 270 * time.Second},   // 30s * 9
		{6, 300 * time.Second},   // 30s * 27 = 810s, capped at 5min
	}
	for _, tt := range tests {
		got := CalculateLockoutDuration(tt.attempts, cfg)
		assert.Equal(t, tt.want, got, "lockout for %d attempts", tt.attempts)
	}
}
