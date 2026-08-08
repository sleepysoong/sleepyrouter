package srv

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

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
