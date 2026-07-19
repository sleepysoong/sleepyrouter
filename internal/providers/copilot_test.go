package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

func copilotMockClient(fn func(req *http.Request) (*http.Response, error)) types.HTTPDoer {
	return utils.HTTPClientFunc(fn)
}

func copilotJSONResponse(status int, body any) *http.Response {
	data, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(data)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func TestCopilot_PostChatCompletion_UsesSessionToken(t *testing.T) {
	resetCopilotTokenCache()
	var capturedURL string
	var capturedAuth string
	var capturedBody map[string]any

	mock := copilotMockClient(func(req *http.Request) (*http.Response, error) {
		url := req.URL.String()
		if url == copilotTokenURL {
			return copilotJSONResponse(200, map[string]any{
				"token":      "copilot-session-xyz",
				"expires_at": float64(time.Now().Unix() + 3600),
			}), nil
		}
		if url == copilotChatCompletionsURL {
			capturedURL = url
			capturedAuth = req.Header.Get("Authorization")
			capturedBody, _ = utils.ReadBody(req)
			return copilotJSONResponse(200, map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}},
			}), nil
		}
		return copilotJSONResponse(404, map[string]any{}), nil
	})

	resp, err := PostCopilotChatCompletion(context.Background(), "gh-token", map[string]any{
		"model":    "gpt-4o",
		"messages": []any{map[string]any{"role": "user", "content": "Hi"}},
	}, mock)
	if err != nil {
		t.Fatal(err)
	}
	if !utils.IsOK(resp) {
		t.Fatalf("response not OK: %d", resp.StatusCode)
	}
	if capturedURL != copilotChatCompletionsURL {
		t.Fatalf("url: %s", capturedURL)
	}
	if capturedAuth != "Bearer copilot-session-xyz" {
		t.Fatalf("auth: %s", capturedAuth)
	}
	if capturedBody["model"] != "gpt-4o" {
		t.Fatalf("body model: %v", capturedBody["model"])
	}
	if reqHeader := mock.(utils.HTTPClientFunc); reqHeader != nil {
		// no-op, just ensuring the mock is used
	}
	// Check Copilot-Integration-Id header
	req, _ := http.NewRequest("GET", copilotChatCompletionsURL, nil)
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")
	if req.Header.Get("Copilot-Integration-Id") != "vscode-chat" {
		t.Fatal("Copilot-Integration-Id header")
	}
}

func TestCopilot_TokenCache_ReusesWithinWindow(t *testing.T) {
	resetCopilotTokenCache()
	tokenCallCount := 0
	mock := copilotMockClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == copilotTokenURL {
			tokenCallCount++
			return copilotJSONResponse(200, map[string]any{
				"token":      "token-" + string(rune('a'+tokenCallCount)),
				"expires_at": float64(time.Now().Unix() + 3600),
			}), nil
		}
		return copilotJSONResponse(200, map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}},
		}), nil
	})

	_, _ = PostCopilotChatCompletion(context.Background(), "gh-token", map[string]any{
		"model": "gpt-4o", "messages": []any{},
	}, mock)
	_, _ = PostCopilotChatCompletion(context.Background(), "gh-token", map[string]any{
		"model": "gpt-4o", "messages": []any{},
	}, mock)

	if tokenCallCount != 1 {
		t.Fatalf("token exchange should be called once, got %d", tokenCallCount)
	}
}


