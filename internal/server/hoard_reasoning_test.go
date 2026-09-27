package server_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/provider"
	"github.com/sleepysoong/sleepyrouter/internal/server"
	"github.com/sleepysoong/sleepyrouter/internal/state"
)

// Chat Completions reasoning_content on the Hoard endpoint: streamed live as a
// Responses reasoning item; plain endpoints keep it private; a stream that
// only reasons and never answers ends in an error with the selected model
// marked failed instead of an empty success.

func chatSSEUpstream(chunks []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			fl.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
}

func chatDelta(delta string) string {
	return `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"k","choices":[{"index":0,"delta":` + delta + `,"finish_reason":null}]}`
}

const chatStop = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"k","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`

func chatReasoningServer(t *testing.T, base string) *server.Server {
	t.Helper()
	cfg, err := config.Parse([]byte(`
version = 1
[server]
host = "127.0.0.1"
port = 4567
[routing]
default_group = "coding"
[timeouts]
request = "10s"
first_event = "5s"
stream_idle = "5s"
[providers.nv]
base_url = "` + base + `"
api_key_env = "H_NV"
wire_api = "chat_completions"
[models."nv/k"]
provider = "nv"
upstream_model = "vendor/k"
[groups]
coding = ["nv/k"]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	store, err := config.NewStore(cfg, map[string]string{"H_NV": "nv-test-key-0000000000"}, 1)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return server.New(server.Deps{Store: store, Affinity: state.New(nil), Registry: provider.DefaultRegistry()})
}

func eventNames(evs []sseEvent) []string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.name)
	}
	return out
}

func indexOf(names []string, want string) int {
	for i, n := range names {
		if n == want {
			return i
		}
	}
	return -1
}

func TestHoardStreamsChatReasoningBeforeAnswer(t *testing.T) {
	up := chatSSEUpstream([]string{
		chatDelta(`{"role":"assistant","reasoning_content":"think "}`),
		chatDelta(`{"reasoning_content":"hard"}`),
		chatDelta(`{"content":"answer"}`),
		chatStop,
	})
	defer up.Close()
	srv := chatReasoningServer(t, up.URL+"/v1")

	_, raw, evs := streamThroughGateway(t, srv, "/hoard/v1/responses")
	names := eventNames(evs)
	r, txt := indexOf(names, "response.reasoning_text.delta"), indexOf(names, "response.output_text.delta")
	if r < 0 || txt < 0 || r > txt {
		t.Fatalf("want reasoning deltas before text: %v %s", names, raw)
	}
	var reasoning strings.Builder
	var completed string
	for _, e := range evs {
		if e.name == "response.reasoning_text.delta" {
			reasoning.WriteString(e.data)
		}
		if e.name == "response.completed" {
			completed = e.data
		}
	}
	if !strings.Contains(reasoning.String(), `"think "`) || !strings.Contains(reasoning.String(), `"hard"`) {
		t.Fatalf("reasoning deltas = %s", reasoning.String())
	}
	if !strings.Contains(completed, `"type":"reasoning"`) || !strings.Contains(completed, `"text":"think hard"`) || !strings.Contains(completed, `"text":"answer"`) {
		t.Fatalf("completed output = %s", completed)
	}
	// Reasoning item closes before the message item opens.
	if done := indexOf(names, "response.reasoning_text.done"); done < 0 || done > txt {
		t.Fatalf("reasoning not closed before text: %v", names)
	}
}

func TestPlainResponsesKeepsChatReasoningPrivate(t *testing.T) {
	up := chatSSEUpstream([]string{chatDelta(`{"reasoning_content":"secret"}`), chatDelta(`{"content":"answer"}`), chatStop})
	defer up.Close()
	srv := chatReasoningServer(t, up.URL+"/v1")

	_, raw, evs := streamThroughGateway(t, srv, "/v1/responses")
	for _, e := range evs {
		if strings.Contains(e.name, "reasoning") || strings.Contains(e.data, "secret") {
			t.Fatalf("reasoning leaked on plain endpoint: %v", evs)
		}
	}
	if indexOf(eventNames(evs), "response.completed") < 0 {
		t.Fatalf("no completion: %v %s", evs, raw)
	}
}

func TestHoardReasoningOnlyStreamEndsInError(t *testing.T) {
	up := chatSSEUpstream([]string{chatDelta(`{"reasoning_content":"!!!!"}`), chatStop})
	defer up.Close()
	srv := chatReasoningServer(t, up.URL+"/v1")

	_, raw, evs := streamThroughGateway(t, srv, "/hoard/v1/responses")
	names := eventNames(evs)
	if indexOf(names, "response.completed") >= 0 || len(evs) == 0 || evs[len(evs)-1].name != "error" {
		t.Fatalf("reasoning-only stream must end in error, got %v %s", names, raw)
	}
	last := evs[len(evs)-1].data
	if !strings.Contains(last, "empty response") {
		t.Fatalf("error = %s", last)
	}
	if tr := traceOf(t, []byte(last)); tr.outcomes() != "nv/k=failed/upstream/over=false" {
		t.Fatalf("trace = %s", tr.outcomes())
	}
}
