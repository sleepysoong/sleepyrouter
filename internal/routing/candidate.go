package routing

import "time"

// AttemptError records one failed candidate attempt (no secrets).
type AttemptError struct {
	Candidate   string
	Provider    string
	StatusCode  int
	Class       ErrorClass
	SafeMessage string
	Duration    time.Duration
	// Skipped marks candidates never attempted (missing key, auth-skipped
	// provider, disabled). Logged as candidate_skipped, not candidate_failed.
	Skipped bool
	// SkipReason briefly explains Skipped (e.g. "missing_api_key").
	SkipReason string
}
