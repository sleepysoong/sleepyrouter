package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/upstream"
)

// EventStream abstracts a Responses SSE stream for failover orchestration.
type EventStream interface {
	Next() bool
	// Event returns SSE event name and JSON payload.
	Event() (string, []byte)
	Err() error
	Close() error
}

// UpstreamCaller performs one candidate attempt.
type UpstreamCaller interface {
	DoNonStream(ctx context.Context, c routing.Candidate, rawBody []byte) (upstream.Result, error)
	DoStream(ctx context.Context, c routing.Candidate, rawBody []byte) (EventStream, error)
}

// Failover orchestrates ordered candidates. It is shared logic used by HTTP
// handlers and tests. On success it returns the winning candidate, result or
// stream (exactly one non-nil), and the attempt history.
type Outcome struct {
	Candidate routing.Candidate
	Result    *upstream.Result
	Stream    EventStream
	Attempts  []routing.AttemptError
	Committed bool // stream committed (only for stream path)
}

// ExecuteNonStream tries candidates in order until success.
func ExecuteNonStream(ctx context.Context, caller UpstreamCaller, candidates []routing.Candidate, rawBody []byte, rewriteModel func([]byte) []byte) (Outcome, *routing.AttemptError) {
	authFailed := map[string]bool{}
	var attempts []routing.AttemptError
	for i, c := range candidates {
		if authFailed[c.ProviderID] {
			attempts = append(attempts, routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Class: routing.ErrorAuth, SafeMessage: "provider skipped after auth failure", Skipped: true, SkipReason: "provider_auth_failed"})
			continue
		}
		if c.Provider == nil || c.Provider.APIKey == "" {
			ae := routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Class: routing.ErrorUnknown, SafeMessage: "API key missing for provider " + c.ProviderID, Skipped: true, SkipReason: "missing_api_key"}
			attempts = append(attempts, ae)
			continue
		}
		start := time.Now()
		res, err := caller.DoNonStream(ctx, c, rawBody)
		dur := time.Since(start)
		if err == nil {
			if rewriteModel != nil && res.RawBody != nil {
				res.RawBody = rewriteModel(res.RawBody)
			}
			return Outcome{Candidate: c, Result: &res, Attempts: attempts}, nil
		}
		// Classify for auth-skip optimization.
		ae := classifyCallerError(c, err, dur)
		attempts = append(attempts, ae)
		if ae.Class == routing.ErrorAuth && (ae.StatusCode == 401 || ae.StatusCode == 403) {
			authFailed[c.ProviderID] = true
		}
		if ae.Class == routing.ErrorClient {
			return Outcome{Attempts: attempts}, &attempts[len(attempts)-1:][0]
		}
		_ = i
		if ctx.Err() != nil {
			ce := routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Class: routing.ErrorClient, SafeMessage: "request cancelled by client"}
			attempts = append(attempts, ce)
			return Outcome{Attempts: attempts}, &ce
		}
	}
	if len(attempts) == 0 {
		ae := routing.AttemptError{Class: routing.ErrorUnknown, SafeMessage: "no usable candidates"}
		return Outcome{Attempts: attempts}, &ae
	}
	last := attempts[len(attempts)-1]
	return Outcome{Attempts: attempts}, &last
}

func classifyCallerError(c routing.Candidate, err error, dur time.Duration) routing.AttemptError {
	if ae, ok := asAttemptError(err); ok {
		ae.Candidate = c.LocalModelID
		ae.Provider = c.ProviderID
		ae.Duration = dur
		return ae
	}
	msg := strings.TrimSpace(err.Error())
	class := routing.ErrorUnknown
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "context canceled"), strings.Contains(lower, "client closed"):
		class = routing.ErrorClient
	case strings.Contains(lower, "deadline exceeded"), strings.Contains(lower, "timeout"):
		class = routing.ErrorTimeout
	}
	return routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Class: class, SafeMessage: msg, Duration: dur}
}

type attemptErrorCarrier interface {
	AttemptError() routing.AttemptError
}

func asAttemptError(err error) (routing.AttemptError, bool) {
	var ac attemptErrorCarrier
	if e, ok := err.(attemptErrorCarrier); ok {
		_ = ac
		return e.AttemptError(), true
	}
	return routing.AttemptError{}, false
}

// SliceStream reads all events (test helper).
func SliceStream(s EventStream) ([]string, error) {
	var out []string
	for s.Next() {
		_, p := s.Event()
		out = append(out, string(p))
	}
	return out, s.Err()
}

var (
	_ = json.Marshal
	_ = io.EOF
	_ = http.StatusOK
)
