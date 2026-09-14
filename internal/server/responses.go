package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/openai"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/state"
	"github.com/sleepysoong/sleepyrouter/internal/upstream"
	"github.com/sleepysoong/sleepyrouter/internal/usage"
)

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	reqID := RequestIDFrom(r.Context())
	snap := s.deps.Store.Current()
	if snap == nil {
		openai.WriteError(w, http.StatusServiceUnavailable, "no_config", "server not ready")
		return
	}
	limit := int64(snap.Server.RequestBodyLimitMB) * 1024 * 1024
	if limit <= 0 {
		limit = 32 * 1024 * 1024
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		openai.WriteError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body too large")
		return
	}
	parsed, err := openai.Parse(raw)
	if err != nil {
		if isMissingModelErr(err) {
			openai.WriteBadRequest(w, "missing_model", "model is required")
		} else {
			openai.WriteBadRequest(w, "invalid_request", "malformed JSON")
		}
		return
	}
	if s.deps.Logger != nil {
		s.deps.Logger.Info("request_received", "request_id", reqID, "protocol", "openai", "requested_model", parsed.RequestedModel, "stream", parsed.Stream)
	}

	candidates, reason, err := s.resolveWithAffinity(snap, parsed.RequestedModel, parsed.Requirements, parsed.PreviousResponseID)
	if err != nil {
		if routing.IsUnknownModel(err) {
			openai.WriteError(w, http.StatusNotFound, "model_not_found", "unknown model")
		} else {
			openai.WriteBadRequest(w, "invalid_request", err.Error())
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
		openai.WriteError(w, http.StatusServiceUnavailable, "no_usable_candidates", "no usable candidates")
		s.recordUsage(reqID, "openai", parsed.RequestedModel, "", "", 0, 0, 0, false, "no_usable", 0, "", snap.Generation, nil)
		return
	}

	pool := upstream.NewPool(snap)
	caller := &openai.SDKCaller{Pool: pool, Registry: s.deps.Registry}
	start := time.Now()

	if !parsed.Stream {
		ctx, cancel := withTimeout(r.Context(), snap.Timeouts.Request)
		defer cancel()
		rewrite := func(b []byte) []byte { return openai.RewriteResponseModel(b, parsed.RequestedModel) }
		outcome, lastErr := openai.ExecuteNonStream(ctx, caller, candidates, parsed.Raw, rewrite)
		dur := time.Since(start)
		if outcome.Result != nil {
			s.onOpenAISuccess(snap, reqID, parsed, outcome.Candidate, *outcome.Result, dur, outcome.Attempts)
			setDebugHeaders(w, reqID, outcome.Candidate, len(outcome.Attempts)+1, snap.Generation)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(outcome.Result.RawBody)
			return
		}
		s.onOpenAIFailure(w, reqID, parsed, dur, outcome.Attempts, lastErr, snap.Generation)
		return
	}

	s.serveResponsesStream(w, r, snap, reqID, parsed, candidates, caller, start)
}

func (s *Server) resolveWithAffinity(snap *config.RuntimeSnapshot, requested string, req routing.Requirements, prevID string) ([]routing.Candidate, routing.RouteReason, error) {
	if prevID != "" {
		if aff, ok := s.deps.Affinity.Get(prevID); ok {
			// Sticky: only the affined model, no cross-provider failover.
			m, ok := snap.Models[aff.LocalModelID]
			if !ok || !m.Enabled {
				return nil, "", errAffinityGone()
			}
			p, ok := snap.Providers[aff.ProviderID]
			if !ok || !p.Enabled {
				return nil, "", errAffinityGone()
			}
			c := routing.Candidate{LocalModelID: aff.LocalModelID, ProviderID: aff.ProviderID, UpstreamModel: m.UpstreamModel, Model: m, Provider: p}
			// Still respect explicit capability unsupported.
			filtered := routing.FilterCandidates(snap, []string{aff.LocalModelID}, req)
			if len(filtered) == 0 {
				return nil, "", errAffinityGone()
			}
			return []routing.Candidate{c}, routing.ReasonDirect, nil
		}
		// Unknown previous ID: route normally but only first candidate gets the ID.
		rr := routing.RouteRequest{RequestedModel: requested, Requirements: req}
		cands, reason, err := routing.ResolveCandidates(snap, rr)
		if err != nil {
			return nil, "", err
		}
		if len(cands) > 1 {
			cands = cands[:1]
		}
		return cands, reason, nil
	}
	return routing.ResolveCandidates(snap, routing.RouteRequest{RequestedModel: requested, Requirements: req})
}

func (s *Server) onOpenAISuccess(snap *config.RuntimeSnapshot, reqID string, parsed openai.ParsedRequest, c routing.Candidate, res upstream.Result, dur time.Duration, attempts []routing.AttemptError) {
	if res.ResponseID != "" {
		s.deps.Affinity.Set(state.ResponseAffinity{
			ResponseID: res.ResponseID, ProviderID: c.ProviderID,
			LocalModelID: c.LocalModelID, UpstreamModel: c.UpstreamModel, CreatedAt: time.Now(),
		})
	}
	var atts []usage.Attempt
	for i, a := range attempts {
		atts = append(atts, usage.Attempt{RequestID: reqID, Index: i + 1, Model: a.Candidate, Provider: a.Provider, DurationMs: a.Duration.Milliseconds(), StatusCode: a.StatusCode, ErrorClass: a.Class.String()})
	}
	atts = append(atts, usage.Attempt{RequestID: reqID, Index: len(attempts) + 1, Model: c.LocalModelID, Provider: c.ProviderID, DurationMs: dur.Milliseconds(), Success: true})
	s.recordUsage(reqID, "openai", parsed.RequestedModel, c.LocalModelID, c.ProviderID, res.InputTokens, res.OutputTokens, len(atts), true, "", dur.Milliseconds(), "", snap.Generation, atts)
	if s.deps.Logger != nil {
		s.deps.Logger.Info("candidate_success", "request_id", reqID, "candidate", c.LocalModelID, "attempt", len(atts), "duration_ms", dur.Milliseconds())
		s.deps.Logger.Info("request_completed", "request_id", reqID, "protocol", "openai", "routed_model", c.LocalModelID)
	}
	s.logAttempts(reqID, attempts)
}

func (s *Server) onOpenAIFailure(w http.ResponseWriter, reqID string, parsed openai.ParsedRequest, dur time.Duration, attempts []routing.AttemptError, lastErr *routing.AttemptError, gen uint64) {
	status := http.StatusBadGateway
	code := "all_candidates_failed"
	msg := "All configured candidates failed"
	if lastErr != nil {
		switch lastErr.Class {
		case routing.ErrorClient:
			status = http.StatusBadRequest
			code = "invalid_request"
			msg = lastErr.SafeMessage
		case routing.ErrorTimeout:
			status = http.StatusGatewayTimeout
		case routing.ErrorAuth, routing.ErrorUnknown:
			if len(attempts) > 0 && allKeyMissing(attempts) {
				status = http.StatusServiceUnavailable
				code = "no_usable_candidates"
				msg = "no usable candidates"
			}
		}
	}
	var atts []usage.Attempt
	errClass := ""
	if lastErr != nil {
		errClass = lastErr.Class.String()
	}
	for i, a := range attempts {
		atts = append(atts, usage.Attempt{RequestID: reqID, Index: i + 1, Model: a.Candidate, Provider: a.Provider, DurationMs: a.Duration.Milliseconds(), StatusCode: a.StatusCode, ErrorClass: a.Class.String()})
	}
	s.recordUsage(reqID, "openai", parsed.RequestedModel, "", "", 0, 0, len(atts), false, errClass, dur.Milliseconds(), "", gen, atts)
	if s.deps.Logger != nil {
		s.deps.Logger.Info("request_failed", "request_id", reqID, "attempts", len(atts), "error", msg)
	}
	s.logAttempts(reqID, attempts)
	openai.WriteError(w, status, code, msg)
}

func allKeyMissing(atts []routing.AttemptError) bool {
	if len(atts) == 0 {
		return false
	}
	for _, a := range atts {
		if !a.Skipped {
			return false
		}
	}
	return true
}

func setDebugHeaders(w http.ResponseWriter, reqID string, c routing.Candidate, attempts int, gen uint64) {
	w.Header().Set("X-SleepyRouter-Model", c.LocalModelID)
	w.Header().Set("X-SleepyRouter-Provider", c.ProviderID)
	w.Header().Set("X-SleepyRouter-Attempts", strconv.Itoa(attempts))
	w.Header().Set("X-SleepyRouter-Config-Generation", strconv.FormatUint(gen, 10))
}

func (s *Server) recordUsage(reqID, protocol, requested, routed, provider string, inT, outT int64, attempts int, success bool, errClass string, durMs int64, session string, gen uint64, atts []usage.Attempt) {
	if s.deps.Usage == nil {
		return
	}
	s.deps.Usage.RecordRequest(usage.Record{
		RequestID: reqID, Protocol: protocol, RequestedModel: requested,
		RoutedModel: routed, Provider: provider, Attempts: attempts,
		InputTokens: inT, OutputTokens: outT, Success: success,
		ErrorClass: errClass, DurationMs: durMs, SessionID: session, ConfigGen: gen,
	}, atts)
}

type affinityGoneError struct{}

func (affinityGoneError) Error() string { return "previous response is pinned to an unavailable model" }
func errAffinityGone() error            { return affinityGoneError{} }

func isMissingModelErr(err error) bool {
	if routing.IsMissingModel(err) {
		return true
	}
	return err != nil && (contains(err.Error(), "model is required"))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

var _ = json.Marshal
var _ = context.Background
