package srv

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
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
	logger := func(event ServerLogEvent) {}
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
	_, _ = store.UpdateModelGroup("default", []string{"model-a:free", "model-b:free"})
	_ = store.WriteModelCache(types.ModelCache{
		Models: []types.SleepyRouterModel{
			{ID: "model-a:free", Name: "Model A", Provider: "test"},
			{ID: "model-b:free", Name: "Model B", Provider: "test"},
		},
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	})
	return store, func() { os.RemoveAll(root) }
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, _ := json.Marshal(value)
	return data
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
			"model":    "sleepyrouter/balanced",
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

func TestServer_NonFreeModelRejected(t *testing.T) {
	root, err := os.MkdirTemp("", "sleepyrouter-paid-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	store := cfg.NewConfigStore(root)
	_, _ = store.UpdateModelGroup("default", []string{"paid/model"})
	_ = store.WriteModelCache(types.ModelCache{
		Models: []types.SleepyRouterModel{
			{ID: "paid/model", Name: "Paid", Provider: "paid", Raw: mustJSON(t, map[string]any{
				"id": "paid/model", "pricing": map[string]any{"prompt": "1", "completion": "1"},
			})},
		},
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	})
	called := false
	mock := utils.HTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return mockResponse(200, map[string]any{}), nil
	})
	withTestServerHandler(store, mock, utils.Environment{"OPENROUTER_API_KEY": "key"}, func(handler http.Handler) {
		reqBody, _ := json.Marshal(map[string]any{
			"model":    "paid/model",
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		})
		w := testRequest(handler, "POST", "/v1/chat/completions", bytes.NewReader(reqBody))
		if w.Code != 400 {
			t.Fatalf("expected 400, got %d", w.Code)
		}
		if !bytes.Contains(w.Body.Bytes(), []byte("무료 모델")) {
			t.Fatalf("body: %s", w.Body.String())
		}
		if called {
			t.Fatal("provider should not have been called")
		}
	})
}

func TestServer_RoutesOpenAIChat(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	var seenBody map[string]any
	mock := utils.HTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := utils.ReadBody(req)
		seenBody = body
		return mockResponse(200, map[string]any{
			"id":    "chatcmpl_1",
			"model": body["model"],
			"choices": []any{map[string]any{
				"message":       map[string]any{"content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 3},
		}), nil
	})
	withTestServerHandler(store, mock, utils.Environment{"OPENROUTER_API_KEY": "key"}, func(handler http.Handler) {
		reqBody, _ := json.Marshal(map[string]any{
			"model":    "auto",
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		})
		w := testRequest(handler, "POST", "/v1/chat/completions", bytes.NewReader(reqBody))
		if w.Code != 200 {
			t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("json: %v", err)
		}
		if body["model"] != "model-a:free" {
			t.Fatalf("model: %v", body["model"])
		}
		if seenBody == nil || seenBody["model"] != "model-a:free" {
			t.Fatalf("seenBody: %v", seenBody)
		}
		// Check usage logging
		logs, err := store.ReadUsageLogs()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, l := range logs {
			if l.Model == "model-a:free" && l.InputTokens == 2 && l.OutputTokens == 3 && l.Success {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("usage log not found: %v", logs)
		}
	})
}



func mockResponse(status int, body any) *http.Response {
	data, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(data)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func TestModelUpstreamID_MultiSlash(t *testing.T) {
	// NVIDIA: "nvidia/b/c" → upstream "b/c"
	nvidiaModel := types.SleepyRouterModel{ID: "nvidia/b/c", Provider: "nvidia", Source: types.SourceNVIDIA}
	if got := modelUpstreamID(nvidiaModel); got != "b/c" {
		t.Fatalf("nvidia modelUpstreamID: got %q, want b/c", got)
	}

	// OpenRouter: uses UpstreamID if present
	orModel := types.SleepyRouterModel{ID: "openrouter/b/c", UpstreamID: "b/c", Provider: "openrouter", Source: types.SourceOpenRouter}
	if got := modelUpstreamID(orModel); got != "b/c" {
		t.Fatalf("openrouter modelUpstreamID: got %q, want b/c", got)
	}

	// Copilot: "copilot/b/c" → upstream "b/c"
	copilotModel := types.SleepyRouterModel{ID: "copilot/b/c", Provider: "copilot", Source: types.SourceCopilot}
	if got := modelUpstreamID(copilotModel); got != "b/c" {
		t.Fatalf("copilot modelUpstreamID: got %q, want b/c", got)
	}
}

func TestServer_RoutesNVIDIAAnthropic(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	// Override group to include an nvidia model
	_, _ = store.UpdateModelGroup("default", []string{"nvidia/meta/llama-4"})
	_ = store.WriteModelCache(types.ModelCache{
		Models: []types.SleepyRouterModel{
			{ID: "nvidia/meta/llama-4", Name: "Meta Llama 4", Provider: "nvidia", Source: types.SourceNVIDIA, UsageID: "nvidia/llama-4"},
		},
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	})

	var seenBody map[string]any
	mock := utils.HTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := utils.ReadBody(req)
		seenBody = body
		return mockResponse(200, map[string]any{
			"id":    "chatcmpl_n1",
			"model": body["model"],
			"choices": []any{map[string]any{
				"message":       map[string]any{"content": "nvidia response"},
				"finish_reason": "stop",
			}},
			"content": []any{map[string]any{"type": "text", "text": "nvidia response"}},
			"usage":   map[string]any{"prompt_tokens": 5, "completion_tokens": 10},
		}), nil
	})
	withTestServerHandler(store, mock, utils.Environment{"NVIDIA_API_KEY": "nkey"}, func(handler http.Handler) {
		reqBody, _ := json.Marshal(map[string]any{
			"model":      "auto",
			"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
			"max_tokens": 100,
		})
		w := testRequest(handler, "POST", "/anthropic/v1/messages", bytes.NewReader(reqBody))
		if w.Code != 200 {
			t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
		}
		// Should call NVIDIA API directly (not OpenRouter Anthropic skin)
		if seenBody == nil {
			t.Fatal("upstream not called")
		}
		if s, _ := seenBody["model"].(string); s != "meta/llama-4" {
			t.Fatalf("upstream model: want meta/llama-4, got %v", seenBody["model"])
		}
		// Anthropic response shape (OpenAIToAnthropic translation)
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("response json: %v\nbody: %s", err, w.Body.String())
		}
		if _, ok := body["content"].([]any); !ok {
			t.Fatalf("expected anthropic content array, body: %s", w.Body.String())
		}
		// Usage logged
		logs, _ := store.ReadUsageLogs()
		found := false
		for _, l := range logs {
			if l.Model == "nvidia/llama-4" && l.InputTokens == 5 && l.OutputTokens == 10 && l.Success {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("usage log not found for nvidia/llama-4: %v", logs)
		}
	})
}

func TestServer_RejectsEmptyChoicesAndRetries(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	// Three models: bad returns empty choices, good returns real response
	_, _ = store.UpdateModelGroup("default", []string{"model-empty:free", "model-good:free"})
	_ = store.WriteModelCache(types.ModelCache{
		Models: []types.SleepyRouterModel{
			{ID: "model-empty:free", Name: "Empty", Provider: "test"},
			{ID: "model-good:free", Name: "Good", Provider: "test"},
		},
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	})
	callCount := 0
	mock := utils.HTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		callCount++
		body, _ := utils.ReadBody(req)
		model := body["model"].(string)
		if model == "model-empty:free" {
			// Empty choices → should be treated as failure and retried
			return mockResponse(200, map[string]any{
				"id":      "empty_1",
				"model":   model,
				"choices": []any{}, // empty
			}), nil
		}
		// Second model returns valid response
		return mockResponse(200, map[string]any{
			"id":    "good_1",
			"model": model,
			"choices": []any{map[string]any{
				"message":       map[string]any{"content": "retry ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		}), nil
	})
	withTestServerHandler(store, mock, utils.Environment{"OPENROUTER_API_KEY": "key"}, func(handler http.Handler) {
		reqBody, _ := json.Marshal(map[string]any{
			"model":    "auto",
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
		})
		w := testRequest(handler, "POST", "/v1/chat/completions", bytes.NewReader(reqBody))
		if w.Code != 200 {
			t.Fatalf("expected 200 after retry, got %d: %s", w.Code, w.Body.String())
		}
		var body map[string]any
		json.Unmarshal(w.Body.Bytes(), &body)
		if body["model"] != "model-good:free" {
			t.Fatalf("model: %v", body["model"])
		}
		// Both tried (empty model first, then good model)
		if callCount != 2 {
			t.Fatalf("expected 2 upstream calls, got %d", callCount)
		}
		// First model's usage logged as failure (0 tokens, success=false)
		logs, _ := store.ReadUsageLogs()
		emptyFail := false
		goodSuccess := false
		for _, l := range logs {
			if l.Model == "model-empty:free" && !l.Success {
				emptyFail = true
			}
			if l.Model == "model-good:free" && l.Success && l.InputTokens == 1 {
				goodSuccess = true
			}
		}
		if !emptyFail || !goodSuccess {
			t.Fatalf("usage logs mismatch: %v", logs)
		}
	})
}


func TestFormatServerLogEvent(t *testing.T) {
	event := ServerLogEvent{
		Type:           "response",
		ID:             1,
		Method:         "POST",
		Path:           "/v1/chat/completions",
		StatusCode:     200,
		DurationMs:     42,
		RequestedModel: "auto",
		ModelID:        "model-a:free",
		RouteReason:    "fallback-order",
	}
	text := FormatServerLogEvent(event, false)
	if text == "" {
		t.Fatal("empty log")
	}
	// Must not contain ANSI codes when color is false
	for _, c := range "\x1b" {
		if len(text) > 0 && text[0] == byte(c) {
			t.Fatalf("has ANSI codes: %q", text)
		}
	}
}

func TestSafeLogValue(t *testing.T) {
	if got := safeLogValue("hello"); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := safeLogValue(strings.Repeat("x", 300)); len(got) > 203 {
		t.Fatalf("too long: %d", len(got))
	}
}
