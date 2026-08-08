package srv

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

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
