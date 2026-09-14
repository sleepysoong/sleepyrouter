package server

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/openai"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
)

// seqStream replays canned events, then ends cleanly.
type seqStream struct {
	events [][2]string
	idx    int
	closed bool
}

func (f *seqStream) Next() bool { return f.idx < len(f.events) }
func (f *seqStream) Event() (string, []byte) {
	e := f.events[f.idx]
	f.idx++
	return e[0], []byte(e[1])
}
func (f *seqStream) Err() error { return nil }
func (f *seqStream) Close() error {
	f.closed = true
	return nil
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
