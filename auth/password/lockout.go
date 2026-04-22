package password

import "time"

// CalculateLockoutDuration returns the lockout duration based on the number of
// recent failed attempts. It implements progressive backoff: no lockout for
// attempts below MaxAttempts, then exponential backoff capped at MaxLockout.
//
// Lockout schedule (default MaxAttempts=5, BaseLockout=1min, MaxLockout=30min):
//
//	1-4 attempts: no lockout
//	5   attempts: 1 minute
//	6   attempts: 2 minutes
//	7   attempts: 4 minutes
//	8   attempts: 8 minutes
//	9   attempts: 16 minutes
//	10+ attempts: 30 minutes (cap)
func CalculateLockoutDuration(failedAttempts int, cfg LockoutConfig) time.Duration {
	if failedAttempts < cfg.MaxAttempts {
		return 0
	}

	excess := failedAttempts - cfg.MaxAttempts
	duration := time.Duration(float64(cfg.BaseLockout) * powFloat(cfg.BackoffFactor, excess))
	if duration > cfg.MaxLockout {
		return cfg.MaxLockout
	}
	return duration
}

// powFloat calculates base^exp as a float64, used for exponential backoff.
func powFloat(base float64, exp int) float64 {
	result := 1.0
	for i := 0; i < exp; i++ {
		result *= base
	}
	return result
}
