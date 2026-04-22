package password

// IsLockedOut returns true if the number of failed attempts within the
// configured window has reached or exceeded MaxAttempts. The lockout is
// enforced by the sliding window: once failed count >= MaxAttempts, the
// account remains locked until enough old failures age out of the window.
func IsLockedOut(failedAttempts int, cfg LockoutConfig) bool {
	return failedAttempts >= cfg.MaxAttempts
}
