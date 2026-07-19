package srv

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

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
	config := types.SleepyRouterConfig{
		Port:        4567,
		ModelGroups: types.ModelGroups{"default": {"model-a:free", "model-b:free"}},
		Models: map[string]types.ModelDefinition{
			"model-a:free": {Provider: "openrouter", Name: "model-a-free-upstream"},
			"model-b:free": {Provider: "openrouter", Name: "model-b-free-upstream"},
		},
	}
	_ = store.WriteConfig(config)
	return store, func() { os.RemoveAll(root) }
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
			var captured ServerLogEvent
			logger := func(event ServerLogEvent) {
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

func TestServer_OpenAIStreamResponse(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	mock := utils.HTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		body := `data: {"id":"1","object":"chat.completion.chunk","choices":[{"delta":{"content":"hello"}}]}

data: [DONE]

`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader([]byte(body))),
			Header:     http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}},
		}, nil
	})
	withTestServerHandler(store, mock, utils.Environment{"OPENROUTER_API_KEY": "key"}, func(handler http.Handler) {
		reqBody, _ := json.Marshal(map[string]any{
			"model":    "auto",
			"messages": []any{map[string]any{"role": "user", "content": "hi"}},
			"stream":   true,
		})
		w := testRequest(handler, "POST", "/v1/chat/completions", bytes.NewReader(reqBody))
		if w.Code != 200 {
			t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "hello") {
			t.Fatalf("stream body missing content: %s", w.Body.String())
		}
	})
}

func TestServer_NVIDIAAnthropicStream(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	_, _ = store.UpdateModelGroup("default", []string{"nvidia/meta/llama-4"})
	func() {
		c, err := store.ReadConfig()
		if err != nil {
			t.Fatal(err)
		}
		c.Models["nvidia/meta/llama-4"] = types.ModelDefinition{Provider: "nvidia", Name: "meta/llama-4"}
		if err := store.WriteConfig(c); err != nil {
			t.Fatal(err)
		}
	}()
	mock := utils.HTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		body := `data: {"id":"1","object":"chat.completion.chunk","choices":[{"delta":{"content":"hello"}}]}

data: [DONE]

`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader([]byte(body))),
			Header:     http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}},
		}, nil
	})
	withTestServerHandler(store, mock, utils.Environment{"NVIDIA_API_KEY": "nkey"}, func(handler http.Handler) {
		reqBody, _ := json.Marshal(map[string]any{
			"model":      "auto",
			"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
			"max_tokens": 100,
			"stream":     true,
		})
		w := testRequest(handler, "POST", "/anthropic/v1/messages", bytes.NewReader(reqBody))
		if w.Code != 200 {
			t.Fatalf("status: %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestServer_OpenRouterAnthropicFallback(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	_, _ = store.UpdateModelGroup("fast", []string{"openrouter/anthropic/claude-3-haiku:free"})
	func() {
		c, err := store.ReadConfig()
		if err != nil {
			t.Fatal(err)
		}
		c.Models["openrouter/anthropic/claude-3-haiku:free"] = types.ModelDefinition{Provider: "openrouter", Name: "anthropic/claude-3-haiku:free"}
		if err := store.WriteConfig(c); err != nil {
			t.Fatal(err)
		}
	}()
	callCount := 0
	mock := utils.HTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		callCount++
		u := req.URL.String()
		if strings.Contains(u, "/v1/messages") && !strings.Contains(u, "/chat/completions") {
			return mockResponse(404, map[string]any{"error": map[string]any{"message": "not found"}}), nil
		}
		return mockResponse(200, map[string]any{
			"id":      "chatcmpl_fb1",
			"model":   "anthropic/claude-3-haiku",
			"choices": []any{map[string]any{"message": map[string]any{"content": "fallback ok"}, "finish_reason": "stop"}},
			"content": []any{map[string]any{"type": "text", "text": "fallback ok"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		}), nil
	})
	withTestServerHandler(store, mock, utils.Environment{"OPENROUTER_API_KEY": "key"}, func(handler http.Handler) {
		reqBody, _ := json.Marshal(map[string]any{
			"model":      "fast",
			"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
			"max_tokens": 100,
		})
		w := testRequest(handler, "POST", "/anthropic/v1/messages", bytes.NewReader(reqBody))
		if w.Code != 200 {
			t.Fatalf("status: %d, body: %s (calls=%d)", w.Code, w.Body.String(), callCount)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 upstream calls (Messages→ChatCompletion), got %d", callCount)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("json: %v", err)
		}
		if _, ok := body["content"].([]any); !ok {
			t.Fatalf("expected anthropic content array, body: %s", w.Body.String())
		}
	})
}

func TestServer_AnthropicAllFailed502(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	_, _ = store.UpdateModelGroup("default", []string{"nvidia/model-a:free"})
	func() {
		c, err := store.ReadConfig()
		if err != nil {
			t.Fatal(err)
		}
		c.Models["nvidia/model-a:free"] = types.ModelDefinition{Provider: "nvidia", Name: "model-a:free"}
		if err := store.WriteConfig(c); err != nil {
			t.Fatal(err)
		}
	}()
	mock := utils.HTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		return mockResponse(500, map[string]any{"error": map[string]any{"message": "upstream error"}}), nil
	})
	withTestServerHandler(store, mock, utils.Environment{"NVIDIA_API_KEY": "key"}, func(handler http.Handler) {
		reqBody, _ := json.Marshal(map[string]any{
			"model":      "auto",
			"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
			"max_tokens": 100,
		})
		w := testRequest(handler, "POST", "/anthropic/v1/messages", bytes.NewReader(reqBody))
		if w.Code != 502 {
			t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("json: %v", err)
		}
		errObj, ok := body["error"].(map[string]any)
		if !ok {
			t.Fatalf("error field missing: %s", w.Body.String())
		}
		apiType, _ := errObj["type"].(string)
		if apiType != "api_error" {
			t.Fatalf("expected error.type=api_error for anthropic 502, got %q", apiType)
		}
	})
}

func TestServer_MissingKeySkipOnAnthropicRoute(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	_, _ = store.UpdateModelGroup("default", []string{"openrouter/model-a:free", "nvidia/model-b:free"})
	func() {
		c, err := store.ReadConfig()
		if err != nil {
			t.Fatal(err)
		}
		c.Models["openrouter/model-a:free"] = types.ModelDefinition{Provider: "openrouter", Name: "model-a:free"}
		c.Models["nvidia/model-b:free"] = types.ModelDefinition{Provider: "nvidia", Name: "model-b:free"}
		if err := store.WriteConfig(c); err != nil {
			t.Fatal(err)
		}
	}()
	callCount := 0
	env := utils.Environment{"NVIDIA_API_KEY": "nkey"}
	mock := utils.HTTPClientFunc(func(req *http.Request) (*http.Response, error) {
		callCount++
		return mockResponse(200, map[string]any{
			"id":      "chatcmpl_1",
			"model":   "model-b:free",
			"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}, "finish_reason": "stop"}},
			"content": []any{map[string]any{"type": "text", "text": "ok"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
		}), nil
	})
	withTestServerHandler(store, mock, env, func(handler http.Handler) {
		reqBody, _ := json.Marshal(map[string]any{
			"model":      "auto",
			"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
			"max_tokens": 100,
		})
		w := testRequest(handler, "POST", "/anthropic/v1/messages", bytes.NewReader(reqBody))
		if w.Code != 200 {
			t.Fatalf("expected 200 (skip openrouter, succeed nvidia), got %d: %s (calls=%d)", w.Code, w.Body.String(), callCount)
		}
		if callCount != 1 {
			t.Fatalf("expected 1 upstream call (nvidia only, openrouter skipped for missing key), got %d", callCount)
		}
	})
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

func TestServer_NonFreeModelRejected(t *testing.T) {
	root, err := os.MkdirTemp("", "sleepyrouter-paid-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	store := cfg.NewConfigStore(root)
	_, _ = store.UpdateModelGroup("default", []string{"paid/model"})
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
		if body["model"] != "model-a-free-upstream" {
			t.Fatalf("model: %v", body["model"])
		}
		if seenBody == nil || seenBody["model"] != "model-a-free-upstream" {
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
	nvidiaModel := types.SleepyRouterModel{ID: "nvidia/b/c", UpstreamID: "b/c", Provider: "nvidia", Source: types.SourceNVIDIA}
	if got := modelUpstreamID(nvidiaModel); got != "b/c" {
		t.Fatalf("nvidia modelUpstreamID: got %q, want b/c", got)
	}

	// OpenRouter: uses UpstreamID if present
	orModel := types.SleepyRouterModel{ID: "openrouter/b/c", UpstreamID: "b/c", Provider: "openrouter", Source: types.SourceOpenRouter}
	if got := modelUpstreamID(orModel); got != "b/c" {
		t.Fatalf("openrouter modelUpstreamID: got %q, want b/c", got)
	}

	// Copilot: "copilot/b/c" → upstream "b/c"
	copilotModel := types.SleepyRouterModel{ID: "copilot/b/c", UpstreamID: "b/c", Provider: "copilot", Source: types.SourceCopilot}
	if got := modelUpstreamID(copilotModel); got != "b/c" {
		t.Fatalf("copilot modelUpstreamID: got %q, want b/c", got)
	}
}

func TestServer_RoutesNVIDIAAnthropic(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	// Override group to include an nvidia model
	_, _ = store.UpdateModelGroup("default", []string{"nvidia/meta/llama-4"})
	func() {
		c, err := store.ReadConfig()
		if err != nil {
			t.Fatal(err)
		}
		c.Models["nvidia/meta/llama-4"] = types.ModelDefinition{Provider: "nvidia", Name: "meta/llama-4"}
		if err := store.WriteConfig(c); err != nil {
			t.Fatal(err)
		}
	}()
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
			if l.Model == "nvidia/meta/llama-4" && l.InputTokens == 5 && l.OutputTokens == 10 && l.Success {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("usage log not found for nvidia/meta/llama-4: %v", logs)
		}
	})
}

func TestServer_RejectsEmptyChoicesAndRetries(t *testing.T) {
	store, cleanup := tempServerStore(t)
	defer cleanup()
	// Three models: bad returns empty choices, good returns real response
	_, _ = store.UpdateModelGroup("default", []string{"model-empty:free", "model-good:free"})
	func() {
		c, err := store.ReadConfig()
		if err != nil {
			t.Fatal(err)
		}
		c.Models["model-empty:free"] = types.ModelDefinition{Provider: "openrouter", Name: "model-empty:free"}
		c.Models["model-good:free"] = types.ModelDefinition{Provider: "openrouter", Name: "model-good:free"}
		if err := store.WriteConfig(c); err != nil {
			t.Fatal(err)
		}
	}()
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
		_ = json.Unmarshal(w.Body.Bytes(), &body)
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

func TestSafeLogValue(t *testing.T) {
	if got := safeLogValue("hello"); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := safeLogValue(strings.Repeat("x", 300)); len(got) > 203 {
		t.Fatalf("too long: %d", len(got))
	}
}
