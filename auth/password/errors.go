package password

import "errors"

// Sentinel errors for password authentication operations. Handlers should use
// these with errors.Is to determine the appropriate HTTP response.
var (
	// ErrInvalidCredentials is returned when the email/password combination is
	// incorrect. The error message is intentionally vague to prevent user
	// enumeration.
	ErrInvalidCredentials = errors.New("invalid email or password")
	// ErrAccountLocked is returned when the account is temporarily locked due
	// to repeated failed login attempts.
	ErrAccountLocked = errors.New("account temporarily locked due to repeated failed attempts")
	// ErrPasswordPolicyViolation is returned when a password does not meet the
	// configured complexity requirements.
	ErrPasswordPolicyViolation = errors.New("password does not meet complexity requirements")
	// ErrEmailAlreadyRegistered is returned when registration is attempted with
	// an email that already has a local account.
	ErrEmailAlreadyRegistered = errors.New("email is already registered")
	// ErrEmailNotVerified is returned when login is attempted and email
	// verification is required but has not been completed.
	ErrEmailNotVerified = errors.New("email address has not been verified")
	// ErrTokenExpired is returned when a reset or verification token has passed
	// its expiry time.
	ErrTokenExpired = errors.New("token has expired")
	// ErrTokenConsumed is returned when a reset or verification token has
	// already been used.
	ErrTokenConsumed = errors.New("token has already been used")
	// ErrTokenInvalid is returned when a token cannot be found or does not
	// match.
	ErrTokenInvalid = errors.New("token is invalid")
	// ErrUserNotFound is returned when a user cannot be found by ID.
	ErrUserNotFound = errors.New("user not found")
)

// PasswordError wraps a sentinel error with an HTTP status code and optional
// detail string. Handlers can type-assert errors to PasswordError to extract
// the appropriate HTTP status without a large switch statement.
type PasswordError struct {
	Err    error
	Status int
	Detail string
}

func (e *PasswordError) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return e.Err.Error()
}

// Unwrap returns the underlying sentinel error so errors.Is works correctly.
func (e *PasswordError) Unwrap() error {
	return e.Err
}
