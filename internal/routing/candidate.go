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
}
