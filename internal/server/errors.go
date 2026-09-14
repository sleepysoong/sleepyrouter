package server

import (
	"encoding/json"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/routing"
)

// logAttempts emits candidate_failed / candidate_skipped per attempt.
// Skips are observability, not failures: the candidate was never called.
func (s *Server) logAttempts(reqID string, attempts []routing.AttemptError) {
	if s.deps.Logger == nil {
		return
	}
	for _, a := range attempts {
		if a.Skipped {
			s.deps.Logger.Info("candidate_skipped",
				"request_id", reqID, "candidate", a.Candidate,
				"provider", a.Provider, "reason", a.SkipReason)
			continue
		}
		s.deps.Logger.Info("candidate_failed",
			"request_id", reqID, "candidate", a.Candidate,
			"provider", a.Provider, "class", a.Class.String(),
			"status", a.StatusCode, "error", a.SafeMessage)
	}
}

// Error helpers for non-protocol endpoints.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
