package srv

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/handler"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

type testResponseRecorder struct {
	HeaderMap http.Header
	Body      bytes.Buffer
	Code      int
}

func newTestRecorder() *testResponseRecorder {
	return &testResponseRecorder{
		HeaderMap: make(http.Header),
		Code:      200,
	}
}

func (r *testResponseRecorder) Header() http.Header {
	return r.HeaderMap
}

func (r *testResponseRecorder) Write(data []byte) (int, error) {
	if r.Code == 0 {
		r.Code = 200
	}
	return r.Body.Write(data)
}

func (r *testResponseRecorder) WriteHeader(code int) {
	r.Code = code
}

func (r *testResponseRecorder) Flush() {}

func withTestServerHandler(store *cfg.ConfigStore, client types.HTTPDoer, env utils.Environment, fn func(handler http.Handler)) {
	logger := func(event handler.ServerLogEvent) {}
	opts := ServerOptions{
		Store:         store,
		FetchImpl:     client,
		Env:           env,
		RequestLogger: logger,
	}
	if opts.Env == nil {
		opts.Env = utils.Environment{}
	}
	server := CreateSleepyRouterServer(opts)
	handler := server.Handler
	fn(handler)
}

func testRequest(handler http.Handler, method, path string, body io.Reader) *testResponseRecorder {
	req, _ := http.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := newTestRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func tempServerStore(t *testing.T) (*cfg.ConfigStore, func()) {
	t.Helper()
	root, err := os.MkdirTemp("", "sleepyrouter-server-test-")
	if err != nil {
		t.Fatal(err)
	}
	store := cfg.NewConfigStore(root)
	config := types.SleepyRouterConfig{
		Port:        4567,
		ModelGroups: types.ModelGroups{"default": {"model-a:free", "model-b:free"}},
		Models: map[string]types.ModelDefinition{
			"model-a:free": {Provider: "openrouter", Name: "model-a-free-upstream"},
			"model-b:free": {Provider: "openrouter", Name: "model-b-free-upstream"},
		},
	}
	_ = store.WriteConfig(config)
	return store, func() { _ = os.RemoveAll(root) }
}

func TestServer_RouteReasonInLogEvent(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	_, _ = store.UpdateModelGroup("fast", []string{"nvidia/fast-model:free", "openrouter/fast-alt:free"})
	func() {
		c, err := store.ReadConfig()
		if err != nil {
			t.Fatal(err)
		}
		c.Models["nvidia/fast-model:free"] = types.ModelDefinition{Provider: "nvidia", Name: "fast-model:free"}
		c.Models["openrouter/fast-alt:free"] = types.ModelDefinition{Provider: "openrouter", Name: "fast-alt:free"}
		if err := store.WriteConfig(c); err != nil {
			t.Fatal(err)
		}
	}()
	tests := []struct {
		name         string
		requestModel string
		wantReason   string
	}{
		{"explicit group match", "fast", "model-group"},
		{"auto falls back", "auto", "fallback-order"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured handler.ServerLogEvent
			logger := func(event handler.ServerLogEvent) {
				if event.Type == "response" {
					captured = event
				}
			}
			opts := ServerOptions{
				Store:         store,
				FetchImpl:     nil,
				Env:           utils.Environment{"NVIDIA_API_KEY": "key", "OPENROUTER_API_KEY": "key"},
				RequestLogger: logger,
			}
			server := CreateSleepyRouterServer(opts)
			reqBody, _ := json.Marshal(map[string]any{
				"model":    tt.requestModel,
				"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			})
			w := testRequest(server.Handler, "POST", "/v1/chat/completions", bytes.NewReader(reqBody))
			if captured.RouteReason == "" {
				t.Fatalf("empty RouteReason (status %d): log not captured", w.Code)
			}
			if captured.RouteReason != tt.wantReason {
				t.Fatalf("RouteReason: got %q, want %q", captured.RouteReason, tt.wantReason)
			}
		})
	}
}

func TestServer_HealthNoKey(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	withTestServerHandler(store, nil, utils.Environment{}, func(handler http.Handler) {
		w := testRequest(handler, "GET", "/health", nil)
		if w.Code != 200 {
			t.Fatalf("status: %d", w.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("json: %v", err)
		}
		if body["ok"] != true {
			t.Fatalf("ok: %v", body["ok"])
		}
	})
}

func TestServer_AnthropicCountTokens(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	withTestServerHandler(store, nil, utils.Environment{}, func(handler http.Handler) {
		reqBody, _ := json.Marshal(map[string]any{
			"model":    "balanced",
			"messages": []any{map[string]any{"role": "user", "content": "hello world"}},
		})
		w := testRequest(handler, "POST", "/anthropic/v1/messages/count_tokens", bytes.NewReader(reqBody))
		if w.Code != 200 {
			t.Fatalf("status: %d", w.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("json: %v", err)
		}
		tokens, ok := body["input_tokens"].(float64)
		if !ok || int(tokens) <= 0 {
			t.Fatalf("input_tokens: %v", body["input_tokens"])
		}
	})
}

func TestServer_ReturnsSelectedModels(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	withTestServerHandler(store, nil, utils.Environment{"OPENROUTER_API_KEY": "key"}, func(handler http.Handler) {
		w := testRequest(handler, "GET", "/v1/models", nil)
		if w.Code != 200 {
			t.Fatalf("status: %d", w.Code)
		}
		var resp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json: %v", err)
		}
		if len(resp.Data) != 2 || resp.Data[0].ID != "model-a:free" || resp.Data[1].ID != "model-b:free" {
			t.Fatalf("models: %v", resp.Data)
		}
	})
}
