package password

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPasswordPolicy_MinLength(t *testing.T) {
	policy := PasswordPolicy{MinLength: 8, MaxLength: 128}

	err := policy.Validate("short")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPasswordPolicyViolation))

	err = policy.Validate("long-enough-password")
	assert.NoError(t, err)
}

func TestPasswordPolicy_MaxLength(t *testing.T) {
	policy := PasswordPolicy{MinLength: 8, MaxLength: 10}

	err := policy.Validate("this-is-too-long")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrPasswordPolicyViolation))

	err = policy.Validate("exactly10!")
	assert.NoError(t, err)
}

func TestPasswordPolicy_ContextualReject(t *testing.T) {
	policy := PasswordPolicy{MinLength: 8, MaxLength: 128, RejectContextual: true}

	tests := []struct {
		name    string
		pw      string
		words   []string
		wantErr bool
	}{
		{
			name:    "contains email",
			pw:      "user@example.com-password",
			words:   []string{"user@example.com"},
			wantErr: true,
		},
		{
			name:    "contains username",
			pw:      "my-jdoe-secret",
			words:   []string{"jdoe"},
			wantErr: true,
		},
		{
			name:    "case-insensitive match",
			pw:      "JDOE-secret-pass",
			words:   []string{"jdoe"},
			wantErr: true,
		},
		{
			name:    "no contextual match",
			pw:      "correct-horse-battery-staple",
			words:   []string{"jdoe", "user@example.com"},
			wantErr: false,
		},
		{
			name:    "empty words are skipped",
			pw:      "correct-horse-battery-staple",
			words:   []string{"", ""},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := policy.Validate(tt.pw, tt.words...)
			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, errors.Is(err, ErrPasswordPolicyViolation))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPasswordPolicy_NoContextualReject(t *testing.T) {
	policy := PasswordPolicy{MinLength: 8, MaxLength: 128, RejectContextual: false}

	err := policy.Validate("user@example.com-password", "user@example.com")
	assert.NoError(t, err, "should not reject when RejectContextual is false")
}
