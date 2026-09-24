package server

import (
	"encoding/json"
	"fmt"
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

func (s *Server) serveResponsesStream(w http.ResponseWriter, r *http.Request, snap *config.RuntimeSnapshot, reqID string, parsed openai.ParsedRequest, candidates []routing.Candidate, caller *openai.SDKCaller, start time.Time) {
	authFailed := map[string]bool{}
	var attempts []routing.AttemptError
	var attemptRows []usage.Attempt

	for _, c := range candidates {
		if authFailed[c.ProviderID] {
			attempts = append(attempts, routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Class: routing.ErrorAuth, SafeMessage: "provider skipped after auth failure", Skipped: true, SkipReason: "provider_auth_failed"})
			continue
		}
		if c.Provider == nil || c.Provider.APIKey == "" {
			ae := routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Class: routing.ErrorUnknown, SafeMessage: "API key missing for provider " + c.ProviderID, Skipped: true, SkipReason: "missing_api_key"}
			attempts = append(attempts, ae)
			continue
		}
		if s.deps.Logger != nil {
			s.deps.Logger.Info("candidate_attempt", "request_id", reqID, "candidate", c.LocalModelID)
		}
		attemptStart := time.Now()
		st, err := caller.DoStream(r.Context(), c, parsed.Raw)
		if err != nil {
			ae := extractAttempt(c, err, time.Since(attemptStart))
			attempts = append(attempts, ae)
			if ae.Class == routing.ErrorAuth && (ae.StatusCode == 401 || ae.StatusCode == 403) {
				authFailed[c.ProviderID] = true
			}
			if ae.Class == routing.ErrorClient {
				s.recordUsage(reqID, "openai", parsed.RequestedModel, "", "", 0, 0, len(attempts), false, ae.Class.String(), time.Since(start).Milliseconds(), "", snap.Generation, toUsageAttempts(reqID, attempts))
				openai.WriteError(w, http.StatusBadRequest, "invalid_request", ae.SafeMessage)
				return
			}
			continue
		}

		// Precommit phase: buffer until meaningful event or failure.
		committed, result := s.precommitAndStreamOpenAI(w, r, snap, reqID, parsed, c, st, len(attempts)+1)
		dur := time.Since(attemptStart)
		if !committed {
			// Pre-commit failure -> failover. result.err holds cause.
			ae := result.failErr
			if ae.SafeMessage == "" {
				ae = routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Class: routing.ErrorUpstream, SafeMessage: "upstream stream ended before meaningful event"}
			}
			ae.Candidate = c.LocalModelID
			ae.Provider = c.ProviderID
			ae.Duration = dur
			attempts = append(attempts, ae)
			if ae.Class == routing.ErrorAuth && (ae.StatusCode == 401 || ae.StatusCode == 403) {
				authFailed[c.ProviderID] = true
			}
			if ae.Class == routing.ErrorClient {
				s.recordUsage(reqID, "openai", parsed.RequestedModel, "", "", 0, 0, len(attempts), false, ae.Class.String(), time.Since(start).Milliseconds(), "", snap.Generation, toUsageAttempts(reqID, attempts))
				openai.WriteError(w, http.StatusBadRequest, "invalid_request", ae.SafeMessage)
				return
			}
			_ = st.Close()
			if r.Context().Err() != nil {
				s.recordUsage(reqID, "openai", parsed.RequestedModel, "", "", 0, 0, len(attempts), false, "client", time.Since(start).Milliseconds(), "", snap.Generation, toUsageAttempts(reqID, attempts))
				return
			}
			continue
		}
		// Committed: stream finished inside precommitAndStream (post-commit, no failover).
		for i, a := range attempts {
			attemptRows = append(attemptRows, usage.Attempt{RequestID: reqID, Index: i + 1, Model: a.Candidate, Provider: a.Provider, DurationMs: a.Duration.Milliseconds(), StatusCode: a.StatusCode, ErrorClass: a.Class.String()})
		}
		attemptRows = append(attemptRows, usage.Attempt{RequestID: reqID, Index: len(attempts) + 1, Model: c.LocalModelID, Provider: c.ProviderID, DurationMs: dur.Milliseconds(), Success: result.success, StatusCode: result.statusCode, ErrorClass: result.errClass})
		if result.success && result.responseID != "" {
			s.deps.Affinity.Set(state.ResponseAffinity{ResponseID: result.responseID, ProviderID: c.ProviderID, LocalModelID: c.LocalModelID, UpstreamModel: c.UpstreamModel, CreatedAt: time.Now()})
		}
		s.recordUsageWithCache(reqID, "openai", parsed.RequestedModel, c.LocalModelID, c.ProviderID,
			result.inTok, result.outTok, result.cachedInputTok, result.cacheWriteInputTok,
			len(attemptRows), result.success, result.errClass, time.Since(start).Milliseconds(), "", snap.Generation, attemptRows)
		if s.deps.Logger != nil {
			s.deps.Logger.Info("request_completed", "request_id", reqID, "protocol", "openai", "routed_model", c.LocalModelID, "success", result.success)
		}
		s.logAttempts(reqID, attempts)
		return
	}

	// All failed pre-commit.
	dur := time.Since(start)
	status := http.StatusBadGateway
	code := "all_candidates_failed"
	if len(attempts) > 0 && allKeyMissing(attempts) {
		status = http.StatusServiceUnavailable
		code = "no_usable_candidates"
	}
	s.recordUsage(reqID, "openai", parsed.RequestedModel, "", "", 0, 0, len(attempts), false, "upstream", dur.Milliseconds(), "", snap.Generation, toUsageAttempts(reqID, attempts))
	if s.deps.Logger != nil {
		s.deps.Logger.Info("request_failed", "request_id", reqID, "attempts", len(attempts))
	}
	s.logAttempts(reqID, attempts)
	openai.WriteError(w, status, code, "All configured candidates failed")
}

type streamResult struct {
	success                 bool
	inTok                   int64
	cachedInputTok          int64
	cacheWriteInputTok      int64
	outTok                  int64
	responseID              string
	statusCode              int
	errClass                string
	failErr                 routing.AttemptError
	continuationFingerprint string
	reasoningContent        string
	reasoningItems          []json.RawMessage
}

// precommitAndStreamOpenAI buffers until meaningful event, then commits and streams rest.
// Returns committed=false if failover should continue.
func (s *Server) precommitAndStreamOpenAI(w http.ResponseWriter, r *http.Request, snap *config.RuntimeSnapshot, reqID string, parsed openai.ParsedRequest, c routing.Candidate, st openai.EventStream, attemptNo int) (bool, streamResult) {
	cfg := upstream.DefaultPrecommit()
	type buffered struct {
		typ     string
		payload []byte
	}
	var buf []buffered
	bufBytes := 0
	// First-event timeout.
	firstTimer := time.NewTimer(snap.Timeouts.FirstEvent)
	defer firstTimer.Stop()
	// Precommit max delay after first event.
	var precommitTimer *time.Timer
	var precommitCh <-chan time.Time

	type step struct {
		ok      bool
		typ     string
		payload []byte
	}
	steps := make(chan step, 1)
	stopReader := make(chan struct{})
	defer close(stopReader)
	// Single producer: the ONLY reader of st. Post-commit draining also
	// goes through steps, so events are never split or lost.
	go func() {
		defer close(steps)
		for st.Next() {
			t, p := st.Event()
			cp := append([]byte{}, p...)
			select {
			case steps <- step{ok: true, typ: t, payload: cp}:
			case <-stopReader:
				return
			}
		}
	}()

	var firstEventAt time.Time
	meaningful := false
	for {
		if len(buf) >= cfg.MaxEvents || bufBytes >= cfg.MaxBytes {
			meaningful = true // buffer limit forces commit
			break
		}
		var timeoutCh <-chan time.Time
		if len(buf) == 0 {
			timeoutCh = firstTimer.C
		} else {
			if precommitTimer == nil {
				precommitTimer = time.NewTimer(2 * time.Second)
				precommitCh = precommitTimer.C
				defer precommitTimer.Stop()
			}
			timeoutCh = precommitCh
		}
		// Idle timeout applies post-first-byte too; use StreamIdle if no event.
		idleTimer := time.NewTimer(snap.Timeouts.StreamIdle)
		select {
		case <-r.Context().Done():
			idleTimer.Stop()
			return false, streamResult{failErr: routing.AttemptError{Class: routing.ErrorClient, SafeMessage: "request cancelled by client"}}
		case <-timeoutCh:
			idleTimer.Stop()
			if len(buf) == 0 {
				return false, streamResult{failErr: routing.AttemptError{Class: routing.ErrorTimeout, SafeMessage: "first event timeout"}}
			}
			meaningful = true // precommit delay reached -> commit
		case <-idleTimer.C:
			return false, streamResult{failErr: routing.AttemptError{Class: routing.ErrorTimeout, SafeMessage: "stream idle timeout"}}
		case stp, ok := <-steps:
			idleTimer.Stop()
			if !ok {
				// Stream ended. Check SDK error.
				if err := st.Err(); err != nil {
					return false, streamResult{failErr: routing.AttemptError{Class: routing.ErrorUpstream, SafeMessage: trunc(err.Error(), 300)}}
				}
				if len(buf) == 0 || !meaningful {
					return false, streamResult{failErr: routing.AttemptError{Class: routing.ErrorUpstream, SafeMessage: "upstream stream ended before meaningful event"}}
				}
				break
			}
			if firstEventAt.IsZero() {
				firstEventAt = time.Now()
			}
			buf = append(buf, buffered{typ: stp.typ, payload: stp.payload})
			bufBytes += len(stp.payload)
			if stp.typ == "response.failed" || stp.typ == "error" {
				return false, streamResult{failErr: routing.AttemptError{Class: routing.ErrorUpstream, SafeMessage: "upstream response failed before output"}}
			}
			if upstream.IsMeaningfulEvent(stp.typ, string(stp.payload)) {
				meaningful = true
			}
			if meaningful {
				goto COMMIT
			}
			continue
		}
		if meaningful {
			break
		}
	}

COMMIT:
	// Commit headers + buffered events.
	setDebugHeaders(w, reqID, c, attemptNo, snap.Generation)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	var inTok, outTok int64
	var cachedInputTok, cacheWriteInputTok int64
	var respID string
	var lastSequence int64 = -1
	terminal := false
	success := false
	errClass := ""
	observe := func(typ string, payload []byte) {
		var meta struct {
			SequenceNumber *int64 `json:"sequence_number"`
		}
		if json.Unmarshal(payload, &meta) == nil && meta.SequenceNumber != nil {
			lastSequence = *meta.SequenceNumber
		}
		switch typ {
		case "response.completed":
			terminal, success = true, true
		case "response.failed", "response.incomplete", "error":
			terminal, success, errClass = true, false, typ
		}
	}
	for _, b := range buf {
		observe(b.typ, b.payload)
		inT, outT, cachedT, cacheWriteT, rid := parseOpenAIEventMeta(string(b.payload))
		inTok = max64(inTok, inT)
		outTok = max64(outTok, outT)
		cachedInputTok = max64(cachedInputTok, cachedT)
		cacheWriteInputTok = max64(cacheWriteInputTok, cacheWriteT)
		if rid != "" {
			respID = rid
		}
		payload := openai.RewriteStreamEventModel(string(b.payload), parsed.RequestedModel)
		if b.typ != "" {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", b.typ, payload)
		} else {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		}
	}
	if fl != nil {
		fl.Flush()
	}
	if s.deps.Logger != nil {
		s.deps.Logger.Info("stream_committed", "request_id", reqID, "candidate", c.LocalModelID)
	}
	if terminal {
		_ = st.Close()
		return true, streamResult{success: success, inTok: inTok, outTok: outTok, cachedInputTok: cachedInputTok, cacheWriteInputTok: cacheWriteInputTok, responseID: respID, errClass: errClass}
	}
	// Continue streaming rest (post-commit: no failover).
	// The producer above stays the sole st reader; we drain steps in order.
	statusCode := 0
	forward := func(typ string, payload []byte) {
		observe(typ, payload)
		inT, outT, cachedT, cacheWriteT, rid := parseOpenAIEventMeta(string(payload))
		inTok = max64(inTok, inT)
		outTok = max64(outTok, outT)
		cachedInputTok = max64(cachedInputTok, cachedT)
		cacheWriteInputTok = max64(cacheWriteInputTok, cacheWriteT)
		if rid != "" {
			respID = rid
		}
		body := openai.RewriteStreamEventModel(string(payload), parsed.RequestedModel)
		if typ != "" {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, body)
		} else {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
		}
		if fl != nil {
			fl.Flush()
		}
	}
	// Idle means "no event for stream_idle", not total elapsed: the timer
	// resets on every forwarded event.
	idleTimeout := snap.Timeouts.StreamIdle
	idleTimer := time.NewTimer(idleTimeout)
	defer idleTimer.Stop()
	for {
		select {
		case <-r.Context().Done():
			success = false
			errClass = "client"
			_ = st.Close()
			return true, streamResult{success: success, inTok: inTok, outTok: outTok, cachedInputTok: cachedInputTok, cacheWriteInputTok: cacheWriteInputTok, responseID: respID, statusCode: statusCode, errClass: errClass}
		case stp, ok := <-steps:
			if ok {
				forward(stp.typ, stp.payload)
				if terminal {
					_ = st.Close()
					return true, streamResult{success: success, inTok: inTok, outTok: outTok, cachedInputTok: cachedInputTok, cacheWriteInputTok: cacheWriteInputTok, responseID: respID, statusCode: statusCode, errClass: errClass}
				}
				if !idleTimer.Stop() {
					select {
					case <-idleTimer.C:
					default:
					}
				}
				idleTimer.Reset(idleTimeout)
				continue
			}
			if err := st.Err(); err != nil {
				success = false
				errClass = "upstream"
			} else if !terminal {
				success = false
				errClass = "upstream_eof"
			}
			if !terminal {
				_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", openai.StreamErrorEvent("upstream stream ended before completion", lastSequence+1))
				if fl != nil {
					fl.Flush()
				}
			}
			_ = st.Close()
			return true, streamResult{success: success, inTok: inTok, outTok: outTok, cachedInputTok: cachedInputTok, cacheWriteInputTok: cacheWriteInputTok, responseID: respID, statusCode: statusCode, errClass: errClass}
		case <-idleTimer.C:
			success = false
			errClass = "timeout"
			_ = st.Close()
			if !terminal {
				_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", openai.StreamErrorEvent("upstream stream idle timeout", lastSequence+1))
				if fl != nil {
					fl.Flush()
				}
			}
			return true, streamResult{success: success, inTok: inTok, outTok: outTok, cachedInputTok: cachedInputTok, cacheWriteInputTok: cacheWriteInputTok, responseID: respID, statusCode: statusCode, errClass: errClass}
		}
	}
}

func parseOpenAIEventMeta(payload string) (inT, outT, cachedT, cacheWriteT int64, respID string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return 0, 0, 0, 0, ""
	}
	if u, ok := raw["usage"]; ok {
		var uu struct {
			InputTokens        int64 `json:"input_tokens"`
			OutputTokens       int64 `json:"output_tokens"`
			InputTokensDetails struct {
				CachedTokens     int64 `json:"cached_tokens"`
				CacheWriteTokens int64 `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
		}
		if err := json.Unmarshal(u, &uu); err == nil {
			inT, outT = uu.InputTokens, uu.OutputTokens
			cachedT, cacheWriteT = uu.InputTokensDetails.CachedTokens, uu.InputTokensDetails.CacheWriteTokens
		}
	}
	if r, ok := raw["response"]; ok {
		var rr struct {
			ID    string `json:"id"`
			Usage *struct {
				InputTokens        int64 `json:"input_tokens"`
				OutputTokens       int64 `json:"output_tokens"`
				InputTokensDetails struct {
					CachedTokens     int64 `json:"cached_tokens"`
					CacheWriteTokens int64 `json:"cache_write_tokens"`
				} `json:"input_tokens_details"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(r, &rr); err == nil {
			respID = rr.ID
			if rr.Usage != nil {
				inT, outT = rr.Usage.InputTokens, rr.Usage.OutputTokens
				cachedT = rr.Usage.InputTokensDetails.CachedTokens
				cacheWriteT = rr.Usage.InputTokensDetails.CacheWriteTokens
			}
		}
	}
	if id, ok := raw["id"]; ok {
		var s string
		if err := json.Unmarshal(id, &s); err == nil && respID == "" {
			// Only treat resp_ IDs as response IDs.
			if len(s) > 5 && s[:5] == "resp_" {
				respID = s
			}
		}
	}
	return inT, outT, cachedT, cacheWriteT, respID
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func extractAttempt(c routing.Candidate, err error, dur time.Duration) routing.AttemptError {
	if ae, ok := err.(interface{ AttemptError() routing.AttemptError }); ok {
		a := ae.AttemptError()
		a.Candidate = c.LocalModelID
		a.Provider = c.ProviderID
		a.Duration = dur
		return a
	}
	return routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Class: routing.ErrorUnknown, SafeMessage: trunc(err.Error(), 300), Duration: dur}
}

// Avoid unused import.
var _ = strconv.Itoa
