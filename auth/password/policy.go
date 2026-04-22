package password

import (
	"fmt"
	"strings"
	"unicode"
)

// Validate checks the password against the configured policy rules.
// contextWords are checked against the password for contextual matches
// (e.g. email address, username).
func (p *PasswordPolicy) Validate(password string, contextWords ...string) error {
	if len(password) < p.MinLength {
		return &PasswordError{
			Err:    ErrPasswordPolicyViolation,
			Status: 400,
			Detail: fmt.Sprintf("password must be at least %d characters", p.MinLength),
		}
	}
	if len(password) > p.MaxLength {
		return &PasswordError{
			Err:    ErrPasswordPolicyViolation,
			Status: 400,
			Detail: fmt.Sprintf("password must be at most %d characters", p.MaxLength),
		}
	}

	if p.RejectContextual {
		lowerPassword := strings.ToLower(password)
		for _, word := range contextWords {
			if word == "" {
				continue
			}
			if strings.Contains(lowerPassword, strings.ToLower(word)) {
				return &PasswordError{
					Err:    ErrPasswordPolicyViolation,
					Status: 400,
					Detail: "password must not contain your email or username",
				}
			}
		}
	}

	// Check for control characters
	for _, r := range password {
		if unicode.IsControl(r) {
			return &PasswordError{
				Err:    ErrPasswordPolicyViolation,
				Status: 400,
				Detail: "password must not contain control characters",
			}
		}
	}

	return nil
}
