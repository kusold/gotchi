package password

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSentinelErrors(t *testing.T) {
	sentinels := []error{
		ErrInvalidCredentials,
		ErrAccountLocked,
		ErrPasswordPolicyViolation,
		ErrEmailAlreadyRegistered,
		ErrEmailNotVerified,
		ErrTokenInvalid,
		ErrUserNotFound,
	}
	for _, err := range sentinels {
		assert.Error(t, err, "sentinel error should be non-nil: %v", err)
	}
}

func TestPasswordError_Error(t *testing.T) {
	tests := []struct {
		name    string
		err     *PasswordError
		want    string
		wantIs  error
		wantNot error
	}{
		{
			name:   "uses detail when present",
			err:    &PasswordError{Err: ErrInvalidCredentials, Status: 401, Detail: "bad creds"},
			want:   "bad creds",
			wantIs: ErrInvalidCredentials,
		},
		{
			name:   "falls back to inner error message",
			err:    &PasswordError{Err: ErrAccountLocked, Status: 423},
			want:   ErrAccountLocked.Error(),
			wantIs: ErrAccountLocked,
		},
		{
			name:    "does not match unrelated error",
			err:     &PasswordError{Err: ErrTokenInvalid, Status: 400},
			want:    ErrTokenInvalid.Error(),
			wantNot: ErrInvalidCredentials,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.err.Error())
			if tt.wantIs != nil {
				assert.True(t, errors.Is(tt.err, tt.wantIs))
			}
			if tt.wantNot != nil {
				assert.False(t, errors.Is(tt.err, tt.wantNot))
			}
		})
	}
}

func TestPasswordError_Unwrap(t *testing.T) {
	inner := ErrTokenInvalid
	pe := &PasswordError{Err: inner, Status: 400}
	assert.Equal(t, inner, pe.Unwrap())
	assert.True(t, errors.Is(pe, ErrTokenInvalid))
}
