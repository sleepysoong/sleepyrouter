package srv

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/httperr"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

func TestServer_NonFreeModelRejected(t *testing.T) {
	root, err := os.MkdirTemp("", "sleepyrouter-paid-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(root) }()
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
		body, _ := httperr.ReadBody(req)
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
		body, _ := httperr.ReadBody(req)
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
		body, _ := httperr.ReadBody(req)
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
