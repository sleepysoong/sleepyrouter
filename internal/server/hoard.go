package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/openai"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/tidwall/sjson"
)

// The Hoard endpoint (POST /hoard/v1/responses) is the OpenAI Responses
// pipeline of /v1/responses — same parsing, routing, failover, precommit and
// stream semantics — plus a routing trace telling the caller which candidates
// were tried, in order, and why each one failed or was skipped.
//
// Wire additions (everything else is the unmodified Responses contract):
//   - JSON bodies (success and error): top-level "sleepyrouter": {"routing": trace}
//   - SSE: one `event: sleepyrouter.routing` right after the stream commits
//     (before the first upstream event), and the trace on the gateway-generated
//     `error` event when a committed stream breaks.
//
// The trace rides in the request context so /v1/responses is untouched: every
// hook below is a no-op when no trace is present.

const (
	routingEventName = "sleepyrouter.routing"
	routingBodyKey   = "sleepyrouter"
)

type routeTraceKey struct{}

// routeTrace is the caller-visible routing record. Reasons come from
// routing.AttemptError.SafeMessage and are additionally scrubbed of provider
// credentials before serialization.
type routeTrace struct {
	mu sync.Mutex

	RequestedModel   string         `json:"requested_model"`
	RouteReason      string         `json:"route_reason,omitempty"`
	Candidates       []string       `json:"candidates"`
	SelectedModel    string         `json:"selected_model,omitempty"`
	SelectedProvider string         `json:"selected_provider,omitempty"`
	Attempts         []traceAttempt `json:"attempts"`

	secrets []string
}

type traceAttempt struct {
	Index         int    `json:"index"`
	Model         string `json:"model"`
	Provider      string `json:"provider"`
	UpstreamModel string `json:"upstream_model,omitempty"`
	// Outcome: "succeeded", "failed", "skipped" (never sent upstream), or
	// "streaming" (committed stream still in progress when reported).
	Outcome    string `json:"outcome"`
	ErrorClass string `json:"error_class,omitempty"`
	StatusCode int    `json:"status_code,omitempty"`
	Reason     string `json:"reason,omitempty"`
	// FailedOver reports whether the router moved on to the next candidate
	// after this failure (false = the error stopped routing, e.g. a client error).
	FailedOver *bool `json:"failed_over,omitempty"`
	DurationMs int64 `json:"duration_ms"`
}

func withRouteTrace(ctx context.Context, t *routeTrace) context.Context {
	return context.WithValue(ctx, routeTraceKey{}, t)
}

func routeTraceFrom(ctx context.Context) *routeTrace {
	t, _ := ctx.Value(routeTraceKey{}).(*routeTrace)
	return t
}

func (s *Server) handleHoardResponses(w http.ResponseWriter, r *http.Request) {
	s.handleResponses(w, r.WithContext(withRouteTrace(r.Context(), &routeTrace{Candidates: []string{}, Attempts: []traceAttempt{}})))
}

// setRoute records the resolved candidate plan and remembers provider keys to scrub.
func (t *routeTrace) setRoute(snap *config.RuntimeSnapshot, requested string, reason routing.RouteReason, cands []routing.Candidate) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.RequestedModel = requested
	t.RouteReason = string(reason)
	t.Candidates = make([]string, 0, len(cands))
	for _, c := range cands {
		t.Candidates = append(t.Candidates, c.LocalModelID)
	}
	t.secrets = t.secrets[:0]
	if snap != nil {
		for _, p := range snap.Providers {
			if p != nil && len(p.APIKey) >= 4 {
				t.secrets = append(t.secrets, p.APIKey)
			}
		}
	}
}

func (t *routeTrace) setRequested(requested string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.RequestedModel = requested
	t.mu.Unlock()
}

// record replaces the attempt list: failures/skips so far, then optionally the
// selected candidate with its outcome ("succeeded", "failed", "streaming").
func (t *routeTrace) record(cands []routing.Candidate, failed []routing.AttemptError, selected *routing.Candidate, outcome string, sel routing.AttemptError) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	upstreamOf := map[string]string{}
	for _, c := range cands {
		upstreamOf[c.LocalModelID] = c.UpstreamModel
	}
	t.Attempts = make([]traceAttempt, 0, len(failed)+1)
	for i, a := range failed {
		ta := traceAttempt{
			Index: i + 1, Model: a.Candidate, Provider: a.Provider, UpstreamModel: upstreamOf[a.Candidate],
			ErrorClass: a.Class.String(), StatusCode: a.StatusCode, DurationMs: a.Duration.Milliseconds(),
			Reason: t.reason(a),
		}
		if a.Skipped {
			ta.Outcome = "skipped"
			if a.SkipReason != "" {
				ta.Reason = a.SkipReason + ": " + ta.Reason
			}
		} else {
			ta.Outcome = "failed"
			// Routing continues past a failure unless it was a client error
			// (or the last candidate); report what actually happened.
			moved := i+1 < len(failed) || selected != nil
			ta.FailedOver = &moved
		}
		t.Attempts = append(t.Attempts, ta)
	}
	t.SelectedModel, t.SelectedProvider = "", ""
	if selected != nil {
		ta := traceAttempt{
			Index: len(failed) + 1, Model: selected.LocalModelID, Provider: selected.ProviderID,
			UpstreamModel: selected.UpstreamModel, Outcome: outcome, DurationMs: sel.Duration.Milliseconds(),
		}
		if outcome == "failed" {
			ta.ErrorClass = sel.Class.String()
			ta.StatusCode = sel.StatusCode
			ta.Reason = t.reason(sel)
			no := false
			ta.FailedOver = &no // committed streams never fail over
		}
		t.Attempts = append(t.Attempts, ta)
		t.SelectedModel, t.SelectedProvider = selected.LocalModelID, selected.ProviderID
	}
}

// failSelected marks the selected (committed) candidate failed after commit.
func (t *routeTrace) failSelected(class, reason string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n := len(t.Attempts); n > 0 && t.Attempts[n-1].Outcome == "streaming" {
		no := false
		t.Attempts[n-1].Outcome = "failed"
		t.Attempts[n-1].ErrorClass = class
		t.Attempts[n-1].Reason = t.scrub(reason)
		t.Attempts[n-1].FailedOver = &no
	}
}

// reason is the caller-facing failure explanation. The SDK leaves type/code/
// message empty when the upstream body is not an OpenAI error envelope (HTML
// error pages, `{}`), which NormalizeError turns into ": : " — fall back to the
// HTTP status so a failure is never unexplained.
func (t *routeTrace) reason(a routing.AttemptError) string {
	msg := strings.TrimSpace(a.SafeMessage)
	if strings.Trim(msg, ": ") == "" {
		switch {
		case a.StatusCode != 0:
			msg = fmt.Sprintf("HTTP %d %s from upstream (no error details in body)", a.StatusCode, http.StatusText(a.StatusCode))
		default:
			msg = a.Class.String() + " error (no details from upstream)"
		}
	}
	return t.scrub(msg)
}

func (t *routeTrace) scrub(msg string) string {
	for _, k := range t.secrets {
		msg = strings.ReplaceAll(msg, k, "[redacted]")
	}
	return msg
}

func (t *routeTrace) json() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	b, _ := json.Marshal(struct {
		Routing *routeTrace `json:"routing"`
	}{t})
	return b
}

// injectTrace adds the trace to a JSON body (response or error envelope).
func injectTrace(body []byte, t *routeTrace) []byte {
	if t == nil {
		return body
	}
	out, err := sjson.SetRawBytes(body, routingBodyKey, t.json())
	if err != nil {
		return body
	}
	return out
}

// routingEventPayload is the data of the `sleepyrouter.routing` SSE event.
func routingEventPayload(t *routeTrace) []byte {
	b, _ := sjson.SetBytes(t.json(), "type", routingEventName)
	return b
}

// writeResponsesError writes the OpenAI error envelope, plus the trace on the Hoard endpoint.
func writeResponsesError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	t := routeTraceFrom(r.Context())
	if t == nil {
		if status == http.StatusBadRequest {
			openai.WriteBadRequest(w, code, message)
		} else {
			openai.WriteError(w, status, code, message)
		}
		return
	}
	typ := "upstream_error"
	if status == http.StatusBadRequest {
		typ = "invalid_request_error"
	}
	body, _ := json.Marshal(openai.ErrorEnvelope{Error: openai.ErrorBody{Message: message, Type: typ, Code: code}})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(injectTrace(body, t))
}

type streamClassifierKey struct{}

// withStreamClassifier stores the request's SDK caller so precommit code can
// normalize lazily surfaced stream errors with the right provider hook.
func withStreamClassifier(ctx context.Context, caller *openai.SDKCaller) context.Context {
	return context.WithValue(ctx, streamClassifierKey{}, caller)
}

// classifyStreamErr turns a stream Err() into an AttemptError like the non-stream
// path would (status, class, message without the upstream URL). Without a caller
// (unit tests of precommit) it falls back to the previous generic classification.
func classifyStreamErr(ctx context.Context, c routing.Candidate, err error) routing.AttemptError {
	if caller, ok := ctx.Value(streamClassifierKey{}).(*openai.SDKCaller); ok && caller != nil {
		return caller.NormalizeStreamError(c, err)
	}
	return routing.AttemptError{Class: routing.ErrorUpstream, SafeMessage: trunc(err.Error(), 300)}
}
