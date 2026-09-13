package server

import (
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
			continue
		}
		if c.Provider == nil || c.Provider.APIKey == "" {
			attempts = append(attempts, routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Class: routing.ErrorUnknown, SafeMessage: "API key missing for provider " + c.ProviderID})
			continue
		}
		upBody, err := anthropic.ToResponses(parsed, c.UpstreamModel)
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
		var rows []usage.Attempt
		for i, a := range attempts {
			rows = append(rows, usage.Attempt{RequestID: reqID, Index: i + 1, Model: a.Candidate, Provider: a.Provider, DurationMs: a.Duration.Milliseconds(), StatusCode: a.StatusCode, ErrorClass: a.Class.String()})
		}
		rows = append(rows, usage.Attempt{RequestID: reqID, Index: len(attempts) + 1, Model: c.LocalModelID, Provider: c.ProviderID, DurationMs: dur.Milliseconds(), Success: res.success, ErrorClass: res.errClass})
		s.recordUsage(reqID, "anthropic", parsed.RequestedModel, c.LocalModelID, c.ProviderID, res.inTok, res.outTok, len(rows), res.success, res.errClass, time.Since(start).Milliseconds(), parsed.SessionID, snap.Generation, rows)
		return
	}
	anthropic.WriteError(w, http.StatusBadGateway, "api_error", "All configured candidates failed")
	s.recordUsage(reqID, "anthropic", parsed.RequestedModel, "", "", 0, 0, len(attempts), false, "upstream", time.Since(start).Milliseconds(), parsed.SessionID, snap.Generation, toUsageAttempts(reqID, attempts))
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
	doneRead := make(chan bool, 1)
	go func() {
		for st.Next() {
			t, p := st.Event()
			cp := append([]byte{}, p...)
			steps <- step{ok: true, typ: t, payload: cp}
		}
		doneRead <- false
		close(steps)
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
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	fl, _ := w.(http.Flusher)
	enc := anthropic.NewStreamEncoder(parsed.RequestedModel)
	for _, ev := range enc.StartEvents() {
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Event, ev.Data)
	}
	var inTok, outTok int64
	flushEncode := func(typ string, payload []byte) {
		for _, ev := range enc.HandleResponsesEvent(typ, string(payload)) {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Event, ev.Data)
		}
		if fl != nil {
			fl.Flush()
		}
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
	// Drain rest post-commit (no failover).
	success := true
	errClass := ""
	restDone := make(chan struct{})
	go func() {
		defer close(restDone)
		for {
			select {
			case stp, ok := <-steps:
				if !ok {
					return
				}
				flushEncode(stp.typ, stp.payload)
			case <-doneRead:
				return
			}
		}
	}()
	select {
	case <-r.Context().Done():
		success = false
		errClass = "client"
	case <-restDone:
		if err := st.Err(); err != nil {
			success = false
			errClass = "upstream"
		}
	case <-time.After(snap.Timeouts.StreamIdle):
		success = false
		errClass = "timeout"
	}
	_ = st.Close()
	inTok = enc.InputTokens
	outTok = enc.OutputTokens
	return true, streamResult{success: success, inTok: inTok, outTok: outTok, errClass: errClass}
}
