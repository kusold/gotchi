package password

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsLockedOut(t *testing.T) {
	cfg := LockoutConfig{MaxAttempts: 5, Window: 15 * 60 * 1e9} // 15 minutes

	tests := []struct {
		attempts int
		want     bool
	}{
		{0, false},
		{1, false},
		{4, false},
		{5, true},
		{6, true},
		{20, true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, IsLockedOut(tt.attempts, cfg),
			"IsLockedOut(%d)", tt.attempts)
	}
}
