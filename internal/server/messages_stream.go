package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/anthropic"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/openai"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/upstream"
	"github.com/sleepysoong/sleepyrouter/internal/usage"
)

func (s *Server) serveMessagesStream(w http.ResponseWriter, r *http.Request, snap *config.RuntimeSnapshot, reqID string, parsed anthropic.Parsed, candidates []routing.Candidate, caller *openai.SDKCaller, start time.Time) {
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
		attemptStart := time.Now()
		st, err := caller.DoStream(r.Context(), c, upBody)
		if err != nil {
			ae := extractAttempt(c, err, time.Since(attemptStart))
			attempts = append(attempts, ae)
			if ae.Class == routing.ErrorAuth && (ae.StatusCode == 401 || ae.StatusCode == 403) {
				authFailed[c.ProviderID] = true
			}
			if ae.Class == routing.ErrorClient {
				anthropic.WriteError(w, http.StatusBadRequest, "invalid_request_error", ae.SafeMessage)
				s.logAttempts(reqID, attempts)
				return
			}
			continue
		}
		committed, res := s.precommitAndStreamAnthropic(w, r, snap, reqID, parsed, c, st, len(attempts)+1)
		dur := time.Since(attemptStart)
		if !committed {
			ae := res.failErr
			ae.Candidate = c.LocalModelID
			ae.Provider = c.ProviderID
			ae.Duration = dur
			if ae.SafeMessage == "" {
				ae.Class = routing.ErrorUpstream
				ae.SafeMessage = "upstream stream ended before meaningful event"
			}
			attempts = append(attempts, ae)
			_ = st.Close()
			if r.Context().Err() != nil {
				return
			}
			continue
		}
		if res.success && res.continuationFingerprint != "" {
			s.continuations.Store(parsed.SessionID, c, res.continuationFingerprint, anthropic.ReasoningContinuation{
				ChatCompletions: res.reasoningContent,
				ResponsesItems:  res.reasoningItems,
			})
		}
		var rows []usage.Attempt
		for i, a := range attempts {
			rows = append(rows, usage.Attempt{RequestID: reqID, Index: i + 1, Model: a.Candidate, Provider: a.Provider, DurationMs: a.Duration.Milliseconds(), StatusCode: a.StatusCode, ErrorClass: a.Class.String()})
		}
		rows = append(rows, usage.Attempt{RequestID: reqID, Index: len(attempts) + 1, Model: c.LocalModelID, Provider: c.ProviderID, DurationMs: dur.Milliseconds(), Success: res.success, ErrorClass: res.errClass})
		s.recordUsageWithCache(reqID, "anthropic", parsed.RequestedModel, c.LocalModelID, c.ProviderID,
			res.inTok, res.outTok, res.cachedInputTok, res.cacheWriteInputTok, len(rows), res.success,
			res.errClass, time.Since(start).Milliseconds(), parsed.SessionID, snap.Generation, rows)
		if s.deps.Logger != nil {
			s.deps.Logger.Info("request_completed", "request_id", reqID, "protocol", "anthropic", "routed_model", c.LocalModelID, "success", res.success)
		}
		s.logAttempts(reqID, attempts)
		return
	}
	status := http.StatusBadGateway
	if len(attempts) > 0 && allKeyMissing(attempts) {
		status = http.StatusServiceUnavailable
	}
	anthropic.WriteError(w, status, "api_error", "All configured candidates failed")
	s.recordUsage(reqID, "anthropic", parsed.RequestedModel, "", "", 0, 0, len(attempts), false, "upstream", time.Since(start).Milliseconds(), parsed.SessionID, snap.Generation, toUsageAttempts(reqID, attempts))
	if s.deps.Logger != nil {
		s.deps.Logger.Info("request_failed", "request_id", reqID, "attempts", len(attempts))
	}
	s.logAttempts(reqID, attempts)
}

func (s *Server) precommitAndStreamAnthropic(w http.ResponseWriter, r *http.Request, snap *config.RuntimeSnapshot, reqID string, parsed anthropic.Parsed, c routing.Candidate, st openai.EventStream, attemptNo int) (bool, streamResult) {
	cfg := upstream.DefaultPrecommit()
	type buffered struct {
		typ     string
		payload []byte
	}
	var buf []buffered
	bufBytes := 0
	firstTimer := time.NewTimer(snap.Timeouts.FirstEvent)
	defer firstTimer.Stop()
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

	meaningful := false
collect:
	for {
		if len(buf) >= cfg.MaxEvents || bufBytes >= cfg.MaxBytes {
			meaningful = true
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
			meaningful = true
			break collect
		case <-idleTimer.C:
			return false, streamResult{failErr: routing.AttemptError{Class: routing.ErrorTimeout, SafeMessage: "stream idle timeout"}}
		case stp, ok := <-steps:
			idleTimer.Stop()
			if !ok {
				if err := st.Err(); err != nil {
					return false, streamResult{failErr: routing.AttemptError{Class: routing.ErrorUpstream, SafeMessage: trunc(err.Error(), 300)}}
				}
				if !meaningful {
					return false, streamResult{failErr: routing.AttemptError{Class: routing.ErrorUpstream, SafeMessage: "upstream stream ended before meaningful event"}}
				}
				break collect
			}
			buf = append(buf, buffered{typ: stp.typ, payload: stp.payload})
			bufBytes += len(stp.payload)
			if stp.typ == "response.failed" || stp.typ == "error" {
				return false, streamResult{failErr: routing.AttemptError{Class: routing.ErrorUpstream, SafeMessage: "upstream response failed before output"}}
			}
			if upstream.IsMeaningfulEvent(stp.typ, string(stp.payload)) {
				meaningful = true
				break collect
			}
		}
	}

	// Commit: encode buffered + stream rest through stateful encoder.
	setDebugHeaders(w, reqID, c, attemptNo, snap.Generation)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	enc := anthropic.NewStreamEncoder(parsed.RequestedModel)
	var continuationFingerprint string
	var continuation anthropic.ReasoningContinuation
	for _, ev := range enc.StartEvents() {
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Event, ev.Data)
	}
	flushEncode := func(typ string, payload []byte) {
		if typ == "response.completed" || typ == "response.incomplete" {
			fingerprint, value := anthropicContinuationFromEvent(typ, payload, parsed.RequestedModel)
			if providerStream, ok := st.(interface{ ProviderReasoningContent() string }); ok {
				value.ChatCompletions = providerStream.ProviderReasoningContent()
			}
			if fingerprint != "" && (value.ChatCompletions != "" || len(value.ResponsesItems) > 0) {
				continuationFingerprint, continuation = fingerprint, value
			}
		}
		for _, ev := range enc.HandleResponsesEvent(typ, string(payload)) {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Event, ev.Data)
		}
		if fl != nil {
			fl.Flush()
		}
	}
	streamResultWithContinuation := func(success bool, errClass string) streamResult {
		result := anthropicStreamResult(enc, success, errClass)
		if success {
			result.continuationFingerprint = continuationFingerprint
			result.reasoningContent = continuation.ChatCompletions
			result.reasoningItems = continuation.ResponsesItems
		}
		return result
	}
	for _, b := range buf {
		flushEncode(b.typ, b.payload)
	}
	if fl != nil {
		fl.Flush()
	}
	if s.deps.Logger != nil {
		s.deps.Logger.Info("stream_committed", "request_id", reqID, "candidate", c.LocalModelID, "protocol", "anthropic")
	}
	if enc.Terminal {
		_ = st.Close()
		errClass := ""
		if !enc.Success {
			errClass = "upstream"
			if enc.StopReason == "max_tokens" {
				errClass = "incomplete"
			}
		}
		return true, streamResultWithContinuation(enc.Success, errClass)
	}
	// Drain rest post-commit (no failover). The producer above stays the
	// sole st reader; we drain steps in order.
	success := false
	errClass := ""
	// Idle means "no event for stream_idle", not total elapsed.
	idleTimeout := snap.Timeouts.StreamIdle
	idleTimer := time.NewTimer(idleTimeout)
	defer idleTimer.Stop()
	pingTicker := time.NewTicker(anthropicPingInterval(idleTimeout))
	defer pingTicker.Stop()
	for {
		select {
		case <-r.Context().Done():
			success = false
			errClass = "client"
			_ = st.Close()
			return true, streamResultWithContinuation(success, errClass)
		case stp, ok := <-steps:
			if ok {
				flushEncode(stp.typ, stp.payload)
				if enc.Terminal {
					_ = st.Close()
					if !enc.Success {
						errClass = "upstream"
						if enc.StopReason == "max_tokens" {
							errClass = "incomplete"
						}
					}
					return true, streamResultWithContinuation(enc.Success, errClass)
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
				errClass = "upstream"
			} else if !enc.Terminal {
				errClass = "upstream_eof"
			} else if !enc.Success {
				errClass = "upstream"
				if enc.StopReason == "max_tokens" {
					errClass = "incomplete"
				}
			}
			if !enc.Terminal {
				for _, ev := range enc.Fail("api_error", "upstream stream ended before completion") {
					_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Event, ev.Data)
				}
				if fl != nil {
					fl.Flush()
				}
			}
			success = enc.Success && errClass == ""
			_ = st.Close()
			return true, streamResultWithContinuation(success, errClass)
		case <-idleTimer.C:
			success = false
			errClass = "timeout"
			_ = st.Close()
			if !enc.Terminal {
				for _, ev := range enc.Fail("api_error", "upstream stream idle timeout") {
					_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Event, ev.Data)
				}
				if fl != nil {
					fl.Flush()
				}
			}
			return true, streamResultWithContinuation(success, errClass)
		case <-pingTicker.C:
			// Keep Claude Code's byte-level watchdog alive without extending
			// our own no-upstream-event idle ceiling.
			_, _ = fmt.Fprint(w, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
			if fl != nil {
				fl.Flush()
			}
		}
	}
}

func anthropicContinuationFromEvent(typ string, payload []byte, requestedModel string) (string, anthropic.ReasoningContinuation) {
	if typ != "response.completed" && typ != "response.incomplete" {
		return "", anthropic.ReasoningContinuation{}
	}
	var event struct {
		Response json.RawMessage `json:"response"`
	}
	if json.Unmarshal(payload, &event) != nil || len(event.Response) == 0 {
		return "", anthropic.ReasoningContinuation{}
	}
	message, err := anthropic.ToMessage(event.Response, requestedModel)
	if err != nil {
		return "", anthropic.ReasoningContinuation{}
	}
	content := anthropicMessageContent(message)
	if len(content) == 0 {
		return "", anthropic.ReasoningContinuation{}
	}
	return anthropic.ContentFingerprint(content), anthropic.ReasoningContinuation{
		ResponsesItems: upstream.ResponsesReasoningItems(event.Response),
	}
}

func anthropicPingInterval(idleTimeout time.Duration) time.Duration {
	const maxInterval = 15 * time.Second
	interval := idleTimeout / 3
	if interval <= 0 {
		interval = time.Millisecond
	}
	if interval > maxInterval {
		return maxInterval
	}
	return interval
}

func anthropicStreamResult(enc *anthropic.StreamEncoder, success bool, errClass string) streamResult {
	return streamResult{
		success:            success,
		inTok:              enc.InputTokens + enc.CacheCreationInputTokens + enc.CacheReadInputTokens,
		outTok:             enc.OutputTokens,
		cachedInputTok:     enc.CacheReadInputTokens,
		cacheWriteInputTok: enc.CacheCreationInputTokens,
		errClass:           errClass,
	}
}
