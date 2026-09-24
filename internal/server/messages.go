package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/anthropic"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/openai"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/upstream"
	"github.com/sleepysoong/sleepyrouter/internal/usage"
)

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	reqID := RequestIDFrom(r.Context())
	snap := s.deps.Store.Current()
	if snap == nil {
		anthropic.WriteError(w, http.StatusServiceUnavailable, "api_error", "server not ready")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		anthropic.WriteError(w, http.StatusRequestEntityTooLarge, "invalid_request", "body too large")
		return
	}
	parsed, err := anthropic.Parse(raw)
	if err != nil {
		anthropic.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	parsed.SessionID = r.Header.Get("X-Claude-Code-Session-Id")
	if s.deps.Logger != nil {
		s.deps.Logger.Info("request_received", "request_id", reqID, "protocol", "anthropic", "requested_model", parsed.RequestedModel, "stream", parsed.Stream, "session", parsed.SessionID)
	}

	candidates, reason, err := routing.ResolveCandidates(snap, routing.RouteRequest{RequestedModel: parsed.RequestedModel, Requirements: parsed.Requirements})
	if err != nil {
		if routing.IsUnknownModel(err) {
			anthropic.WriteError(w, http.StatusNotFound, "not_found_error", "unknown model")
		} else {
			anthropic.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		}
		return
	}
	if s.deps.Logger != nil {
		names := make([]string, 0, len(candidates))
		for _, c := range candidates {
			names = append(names, c.LocalModelID)
		}
		s.deps.Logger.Info("route_resolved", "request_id", reqID, "reason", string(reason), "candidates", names)
	}
	if len(candidates) == 0 {
		anthropic.WriteError(w, http.StatusServiceUnavailable, "api_error", "no usable candidates")
		s.recordUsage(reqID, "anthropic", parsed.RequestedModel, "", "", 0, 0, 0, false, "no_usable", 0, parsed.SessionID, snap.Generation, nil)
		return
	}

	pool := upstream.NewPool(snap)
	caller := &openai.SDKCaller{Pool: pool, Registry: s.deps.Registry}
	start := time.Now()

	if !parsed.Stream {
		s.serveMessagesNonStream(w, r, snap, reqID, parsed, candidates, caller, start)
		return
	}
	s.serveMessagesStream(w, r, snap, reqID, parsed, candidates, caller, start)
}

func (s *Server) serveMessagesNonStream(w http.ResponseWriter, r *http.Request, snap *config.RuntimeSnapshot, reqID string, parsed anthropic.Parsed, candidates []routing.Candidate, caller *openai.SDKCaller, start time.Time) {
	authFailed := map[string]bool{}
	var attempts []routing.AttemptError
	for _, c := range candidates {
		if authFailed[c.ProviderID] {
			attempts = append(attempts, routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Class: routing.ErrorAuth, SafeMessage: "provider skipped after auth failure", Skipped: true, SkipReason: "provider_auth_failed"})
			continue
		}
		if c.Provider == nil || c.Provider.APIKey == "" {
			attempts = append(attempts, routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Class: routing.ErrorUnknown, SafeMessage: "API key missing for provider " + c.ProviderID, Skipped: true, SkipReason: "missing_api_key"})
			continue
		}
		upBody, err := anthropic.ToResponsesWithContinuations(parsed, c.UpstreamModel, s.reasoningForRequest(parsed, c), c.Provider.WireAPI == "chat_completions")
		if err != nil {
			anthropic.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
			return
		}
		ctx, cancel := withTimeout(r.Context(), snap.Timeouts.Request)
		res, err := caller.DoNonStream(ctx, c, upBody)
		cancel()
		if err != nil {
			ae := extractAttempt(c, err, 0)
			attempts = append(attempts, ae)
			if ae.Class == routing.ErrorAuth && (ae.StatusCode == 401 || ae.StatusCode == 403) {
				authFailed[c.ProviderID] = true
			}
			if ae.Class == routing.ErrorClient {
				anthropic.WriteError(w, http.StatusBadRequest, "invalid_request_error", ae.SafeMessage)
				s.recordUsage(reqID, "anthropic", parsed.RequestedModel, "", "", 0, 0, len(attempts), false, ae.Class.String(), time.Since(start).Milliseconds(), parsed.SessionID, snap.Generation, toUsageAttempts(reqID, attempts))
				s.logAttempts(reqID, attempts)
				return
			}
			continue
		}
		out, err := anthropic.ToMessage(res.RawBody, parsed.RequestedModel)
		if err != nil {
			ae := routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Class: routing.ErrorUpstream, SafeMessage: "response mapping failed"}
			attempts = append(attempts, ae)
			continue
		}
		s.rememberReasoning(parsed, c, anthropicMessageContent(out), anthropic.ReasoningContinuation{
			ChatCompletions: res.ReasoningContent,
			ResponsesItems:  res.ReasoningItems,
		})
		inT, outT, cacheWriteT, cachedT := anthropicUsage(out)
		setDebugHeaders(w, reqID, c, len(attempts)+1, snap.Generation)
		var rows []usage.Attempt
		for i, a := range attempts {
			rows = append(rows, usage.Attempt{RequestID: reqID, Index: i + 1, Model: a.Candidate, Provider: a.Provider, DurationMs: a.Duration.Milliseconds(), StatusCode: a.StatusCode, ErrorClass: a.Class.String()})
		}
		rows = append(rows, usage.Attempt{RequestID: reqID, Index: len(attempts) + 1, Model: c.LocalModelID, Provider: c.ProviderID, DurationMs: time.Since(start).Milliseconds(), Success: true})
		s.recordUsageWithCache(reqID, "anthropic", parsed.RequestedModel, c.LocalModelID, c.ProviderID,
			inT+cacheWriteT+cachedT, outT, cachedT, cacheWriteT, len(rows), true, "",
			time.Since(start).Milliseconds(), parsed.SessionID, snap.Generation, rows)
		if s.deps.Logger != nil {
			s.deps.Logger.Info("request_completed", "request_id", reqID, "protocol", "anthropic", "routed_model", c.LocalModelID)
		}
		s.logAttempts(reqID, attempts)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
		return
	}
	status := http.StatusBadGateway
	if len(attempts) > 0 && allKeyMissing(attempts) {
		status = http.StatusServiceUnavailable
	}
	s.recordUsage(reqID, "anthropic", parsed.RequestedModel, "", "", 0, 0, len(attempts), false, "upstream", time.Since(start).Milliseconds(), parsed.SessionID, snap.Generation, toUsageAttempts(reqID, attempts))
	if s.deps.Logger != nil {
		s.deps.Logger.Info("request_failed", "request_id", reqID, "attempts", len(attempts))
	}
	s.logAttempts(reqID, attempts)
	anthropic.WriteError(w, status, "api_error", "All configured candidates failed")
}

func anthropicUsage(body []byte) (input, output, cacheWrite, cacheRead int64) {
	var v struct {
		Usage struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return 0, 0, 0, 0
	}
	return v.Usage.InputTokens, v.Usage.OutputTokens, v.Usage.CacheCreationInputTokens, v.Usage.CacheReadInputTokens
}

func toUsageAttempts(reqID string, atts []routing.AttemptError) []usage.Attempt {
	out := make([]usage.Attempt, 0, len(atts))
	for i, a := range atts {
		out = append(out, usage.Attempt{RequestID: reqID, Index: i + 1, Model: a.Candidate, Provider: a.Provider, DurationMs: a.Duration.Milliseconds(), StatusCode: a.StatusCode, ErrorClass: a.Class.String()})
	}
	return out
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	snap := s.deps.Store.Current()
	_ = snap
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	if err != nil {
		anthropic.WriteError(w, http.StatusRequestEntityTooLarge, "invalid_request", "body too large")
		return
	}
	counter := anthropic.NewCounter()
	n, err := counter.CountRequest(raw)
	if err != nil {
		anthropic.WriteError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"input_tokens": n})
	var _ = fmt.Sprint
}
