package server

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/anthropic"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/openai"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/state"
	"github.com/sleepysoong/sleepyrouter/internal/upstream"
)

type heldStream struct {
	first   [2]string
	emitted bool
	closed  chan struct{}
	once    sync.Once
}

func (f *heldStream) Next() bool {
	if !f.emitted {
		f.emitted = true
		return true
	}
	<-f.closed
	return false
}
func (f *heldStream) Event() (string, []byte) { return f.first[0], []byte(f.first[1]) }
func (f *heldStream) Err() error              { return nil }
func (f *heldStream) Close() error            { f.once.Do(func() { close(f.closed) }); return nil }

type notifyWriter struct {
	*httptest.ResponseRecorder
	committed chan struct{}
	once      sync.Once
}

func (w *notifyWriter) WriteHeader(status int) {
	w.ResponseRecorder.WriteHeader(status)
	w.once.Do(func() { close(w.committed) })
}

// seqStream replays canned events, then ends cleanly.
type seqStream struct {
	events [][2]string
	idx    int
	closed bool
	err    error
}

func (f *seqStream) Next() bool { return f.idx < len(f.events) }
func (f *seqStream) Event() (string, []byte) {
	e := f.events[f.idx]
	f.idx++
	return e[0], []byte(e[1])
}
func (f *seqStream) Err() error { return f.err }
func (f *seqStream) Close() error {
	f.closed = true
	return nil
}

func streamCandidate() routing.Candidate {
	return routing.Candidate{
		LocalModelID: "openrouter/c", ProviderID: "openrouter", UpstreamModel: "c",
		Model:    config.RuntimeModel{LocalID: "openrouter/c", ProviderID: "openrouter", UpstreamModel: "c"},
		Provider: &config.RuntimeProvider{ID: "openrouter", BaseURL: "https://example.invalid/v1", APIKey: "k", Enabled: true},
	}
}

func TestNonStreamAffinityOnlyForCompletedResponse(t *testing.T) {
	affinity := state.New(nil)
	s := &Server{deps: Deps{Affinity: affinity}}
	snap := testStreamSnapshot(t)
	parsed := openai.ParsedRequest{RequestedModel: "virtual"}
	s.onOpenAISuccess(snap, "req-failed", parsed, streamCandidate(), upstream.Result{ResponseID: "resp_failed", Status: "failed"}, 0, nil)
	if _, ok := affinity.Get("resp_failed"); ok {
		t.Fatal("failed response was affined")
	}
	s.onOpenAISuccess(snap, "req-completed", parsed, streamCandidate(), upstream.Result{ResponseID: "resp_completed", Status: "completed"}, 0, nil)
	if _, ok := affinity.Get("resp_completed"); !ok {
		t.Fatal("completed response has no affinity")
	}
}

func TestOpenAIStreamModelAndTerminalFailures(t *testing.T) {
	snap := testStreamSnapshot(t)
	tests := []struct {
		name           string
		terminal       string
		streamErr      error
		wantSuccess    bool
		wantErrorEvent bool
	}{
		{"complete", "response.completed", nil, true, false},
		{"failed", "response.failed", nil, false, false},
		{"incomplete", "response.incomplete", nil, false, false},
		{"transport", "", errors.New("connection lost"), false, true},
		{"eof", "", nil, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			events := [][2]string{
				{"response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","model":"vendor"}}`},
				{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":1,"delta":"hello"}`},
			}
			if tc.terminal != "" {
				events = append(events, [2]string{tc.terminal, fmt.Sprintf(`{"type":%q,"sequence_number":2,"response":{"id":"resp_1","model":"vendor"}}`, tc.terminal)})
			}
			st := &seqStream{events: events, err: tc.streamErr}
			req := httptest.NewRequest("POST", "/v1/responses", nil)
			w := httptest.NewRecorder()
			parsed := openai.ParsedRequest{RequestedModel: "virtual"}
			committed, res := (&Server{}).precommitAndStreamOpenAI(w, req, snap, "test", parsed, streamCandidate(), st, 1)
			if !committed || res.success != tc.wantSuccess {
				t.Fatalf("committed=%v result=%+v", committed, res)
			}
			body := w.Body.String()
			if strings.Contains(body, `"model":"vendor"`) || strings.Contains(body, `"type":"response.created","model"`) || !strings.Contains(body, `"model":"virtual"`) {
				t.Fatalf("model rewrite: %s", body)
			}
			if (strings.Contains(body, "event: error\n")) != tc.wantErrorEvent {
				t.Fatalf("error event: %s", body)
			}
			if strings.Contains(body, `{"type":"response.failed"}`) {
				t.Fatalf("fabricated failed event: %s", body)
			}
		})
	}
}

func TestAnthropicStreamTerminatesOrErrors(t *testing.T) {
	snap := testStreamSnapshot(t)
	tests := []struct {
		name, terminal, payload, wantStop string
		wantSuccess                       bool
	}{
		{"complete", "response.completed", `{"response":{"usage":{"input_tokens":1,"output_tokens":2}}}`, "end_turn", true},
		{"max_tokens", "response.incomplete", `{"response":{"incomplete_details":{"reason":"max_output_tokens"}}}`, "max_tokens", false},
		{"other_incomplete", "response.incomplete", `{"response":{"incomplete_details":{"reason":"content_filter"}}}`, "", false},
		{"failed", "response.failed", `{}`, "", false},
		{"eof", "", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			events := [][2]string{{"response.output_text.delta", `{"delta":"hello"}`}}
			if tc.terminal != "" {
				events = append(events, [2]string{tc.terminal, tc.payload})
			}
			st := &seqStream{events: events}
			w := httptest.NewRecorder()
			committed, res := (&Server{}).precommitAndStreamAnthropic(w, httptest.NewRequest("POST", "/v1/messages", nil), snap, "test", anthropic.Parsed{RequestedModel: "virtual"}, streamCandidate(), st, 1)
			if !committed || res.success != tc.wantSuccess {
				t.Fatalf("committed=%v result=%+v", committed, res)
			}
			body := w.Body.String()
			if tc.wantStop == "" {
				if !strings.Contains(body, "event: error\n") || strings.Contains(body, "event: message_stop\n") {
					t.Fatalf("failed stream: %s", body)
				}
			} else if !strings.Contains(body, `"stop_reason":"`+tc.wantStop+`"`) || !strings.Contains(body, "event: message_stop\n") {
				t.Fatalf("missing stop: %s", body)
			}
		})
	}
}

func TestFailedBeforeOutputAllowsFailover(t *testing.T) {
	snap := testStreamSnapshot(t)
	events := [][2]string{{"response.created", `{"type":"response.created"}`}, {"response.failed", `{"type":"response.failed"}`}}
	openaiStream := &seqStream{events: events}
	committed, _ := (&Server{}).precommitAndStreamOpenAI(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", nil), snap, "test", openai.ParsedRequest{RequestedModel: "virtual"}, streamCandidate(), openaiStream, 1)
	_ = openaiStream.Close()
	if committed {
		t.Fatal("OpenAI failure before output committed")
	}
	anthropicStream := &seqStream{events: events}
	committed, _ = (&Server{}).precommitAndStreamAnthropic(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", nil), snap, "test", anthropic.Parsed{RequestedModel: "virtual"}, streamCandidate(), anthropicStream, 1)
	_ = anthropicStream.Close()
	if committed {
		t.Fatal("Anthropic failure before output committed")
	}
}

func TestCommittedStreamIdleAndClientCancellation(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		for _, scenario := range []string{"idle", "cancel"} {
			t.Run(protocol+"/"+scenario, func(t *testing.T) {
				snap := testStreamSnapshot(t)
				snap.Timeouts.StreamIdle = 20 * time.Millisecond
				st := &heldStream{first: [2]string{"response.output_text.delta", `{"type":"response.output_text.delta","delta":"hello"}`}, closed: make(chan struct{})}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				writer := &notifyWriter{ResponseRecorder: httptest.NewRecorder(), committed: make(chan struct{})}
				var result streamResult
				done := make(chan struct{})
				go func() {
					defer close(done)
					if protocol == "openai" {
						_, result = (&Server{}).precommitAndStreamOpenAI(writer, httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx), snap, "test", openai.ParsedRequest{RequestedModel: "virtual"}, streamCandidate(), st, 1)
					} else {
						_, result = (&Server{}).precommitAndStreamAnthropic(writer, httptest.NewRequest("POST", "/v1/messages", nil).WithContext(ctx), snap, "test", anthropic.Parsed{RequestedModel: "virtual"}, streamCandidate(), st, 1)
					}
				}()
				select {
				case <-writer.committed:
				case <-time.After(time.Second):
					t.Fatal("stream did not commit")
				}
				if scenario == "cancel" {
					cancel()
				}
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("stream did not stop")
				}
				if result.success || !st.emitted {
					t.Fatalf("result=%+v emitted=%v", result, st.emitted)
				}
				if scenario == "idle" && !strings.Contains(writer.Body.String(), "event: error\n") {
					t.Fatalf("missing error: %s", writer.Body.String())
				}
			})
		}
	}
}

func testStreamSnapshot(t *testing.T) *config.RuntimeSnapshot {
	t.Helper()
	cfg, err := config.Parse([]byte(`
version = 1
[timeouts]
request = "10s"
first_event = "5s"
stream_idle = "5s"
[routing]
default_group = "coding"
[providers.openrouter]
enabled = true
base_url = "https://example.invalid/v1"
api_key_env = "TEST_OR_KEY"
[models."openrouter/c"]
provider = "openrouter"
upstream_model = "c"
[groups]
coding = ["openrouter/c"]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return config.BuildSnapshot(cfg, map[string]string{"TEST_OR_KEY": "k"}, 1)
}

// TestPrecommitForwardsEveryEventInOrder pins the single-consumer invariant:
// after commit, the producer stays the sole stream reader and every event
// reaches the client exactly once, in order. The pre-fix code raced a second
// Next() loop against the producer and dropped events.
func TestPrecommitForwardsEveryEventInOrder(t *testing.T) {
	snap := testStreamSnapshot(t)
	s := &Server{}
	c := routing.Candidate{
		LocalModelID: "openrouter/c", ProviderID: "openrouter", UpstreamModel: "c",
		Model:    config.RuntimeModel{LocalID: "openrouter/c", ProviderID: "openrouter", UpstreamModel: "c"},
		Provider: &config.RuntimeProvider{ID: "openrouter", BaseURL: "https://example.invalid/v1", APIKey: "k", Enabled: true},
	}
	const n = 50
	events := [][2]string{{"response.created", `{"type":"response.created"}`}}
	for i := 0; i < n; i++ {
		events = append(events, [2]string{
			"response.output_text.delta",
			fmt.Sprintf(`{"type":"response.output_text.delta","delta":"tok-%d"}`, i),
		})
	}
	events = append(events, [2]string{"response.completed", `{"type":"response.completed"}`})
	st := &seqStream{events: events}

	req := httptest.NewRequest("POST", "/v1/responses", nil)
	w := httptest.NewRecorder()
	parsed := openai.ParsedRequest{Raw: []byte(`{"model":"coding","input":"hi"}`), RequestedModel: "coding"}

	committed, res := s.precommitAndStreamOpenAI(w, req, snap, "test-req", parsed, c, st, 1)
	if !committed {
		t.Fatalf("not committed: %+v", res)
	}
	if !res.success {
		t.Fatalf("not success: %+v", res)
	}
	body := w.Body.String()
	if got := strings.Count(body, "data: "); got != len(events) {
		t.Fatalf("forwarded %d events, want %d", got, len(events))
	}
	// Order preserved.
	last := -1
	for i := 0; i < n; i++ {
		marker := fmt.Sprintf(`"tok-%d"`, i)
		pos := strings.Index(body, marker)
		if pos < 0 || pos < last {
			t.Fatalf("event tok-%d missing or out of order", i)
		}
		last = pos
	}
	if !st.closed {
		t.Fatal("stream not closed")
	}
	_ = time.Now
}
