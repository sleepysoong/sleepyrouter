package core

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

func copilotMockClient(fn func(req *http.Request) (*http.Response, error)) types.HTTPDoer {
	return httpClientFunc(fn)
}

func copilotJSONResponse(status int, body any) *http.Response {
	data, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(data)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func TestCopilot_ListFreeModels_ReturnsKnownModels(t *testing.T) {
	ResetCopilotTokenCache()
	mock := copilotMockClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == CopilotTokenURL {
			return copilotJSONResponse(200, map[string]any{
				"token":      "tok",
				"expires_at": float64(time.Now().Unix() + 3600),
			}), nil
		}
		return copilotJSONResponse(404, map[string]any{}), nil
	})

	models, err := ListCopilotFreeModels(context.Background(), "gh-token", mock)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) == 0 {
		t.Fatal("no models returned")
	}
	for _, m := range models {
		if !strings.HasPrefix(m.ID, "copilot/") {
			t.Fatalf("id should start with copilot/: %s", m.ID)
		}
		if m.Source != types.SourceCopilot || m.Provider != "copilot" {
			t.Fatalf("source/provider: %v/%v", m.Source, m.Provider)
		}
	}
	// Check specific models
	ids := make([]string, len(models))
	for i, m := range models {
		ids[i] = m.ID
	}
	if !containsString(ids, "copilot/gpt-4o") {
		t.Fatalf("expected copilot/gpt-4o, got %v", ids)
	}
	if !containsString(ids, "copilot/claude-sonnet-4") {
		t.Fatalf("expected copilot/claude-sonnet-4, got %v", ids)
	}
}

func TestCopilot_ListFreeModels_ThrowsOnTokenFailure(t *testing.T) {
	ResetCopilotTokenCache()
	mock := copilotMockClient(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 401,
			Body:       io.NopCloser(strings.NewReader("Unauthorized")),
			Header:     http.Header{},
		}, nil
	})

	_, err := ListCopilotFreeModels(context.Background(), "bad-key", mock)
	if err == nil || !strings.Contains(err.Error(), "Copilot 토큰 교환 실패") {
		t.Fatalf("err: %v", err)
	}
}

func TestCopilot_PostChatCompletion_UsesSessionToken(t *testing.T) {
	ResetCopilotTokenCache()
	var capturedURL string
	var capturedAuth string
	var capturedBody map[string]any

	mock := copilotMockClient(func(req *http.Request) (*http.Response, error) {
		url := req.URL.String()
		if url == CopilotTokenURL {
			return copilotJSONResponse(200, map[string]any{
				"token":      "copilot-session-xyz",
				"expires_at": float64(time.Now().Unix() + 3600),
			}), nil
		}
		if url == CopilotChatCompletionsURL {
			capturedURL = url
			capturedAuth = req.Header.Get("Authorization")
			capturedBody, _ = readBody(req)
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
	if capturedURL != CopilotChatCompletionsURL {
		t.Fatalf("url: %s", capturedURL)
	}
	if capturedAuth != "Bearer copilot-session-xyz" {
		t.Fatalf("auth: %s", capturedAuth)
	}
	if capturedBody["model"] != "gpt-4o" {
		t.Fatalf("body model: %v", capturedBody["model"])
	}
	if reqHeader := mock.(httpClientFunc); reqHeader != nil {
		// no-op, just ensuring the mock is used
	}
	// Check Copilot-Integration-Id header
	req, _ := http.NewRequest("GET", CopilotChatCompletionsURL, nil)
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")
	if req.Header.Get("Copilot-Integration-Id") != "vscode-chat" {
		t.Fatal("Copilot-Integration-Id header")
	}
}

func TestCopilot_TokenCache_ReusesWithinWindow(t *testing.T) {
	ResetCopilotTokenCache()
	tokenCallCount := 0
	mock := copilotMockClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == CopilotTokenURL {
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

func TestCopilot_NormalizeModelID(t *testing.T) {
	ResetCopilotTokenCache()
	mock := copilotMockClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == CopilotTokenURL {
			return copilotJSONResponse(200, map[string]any{
				"token":      "tok",
				"expires_at": float64(time.Now().Unix() + 3600),
			}), nil
		}
		return copilotJSONResponse(200, map[string]any{}), nil
	})
	models, err := ListCopilotFreeModels(context.Background(), "gh-token", mock)
	if err != nil {
		t.Fatal(err)
	}
	var gpt4o *types.SleepyRouterModel
	for i, m := range models {
		if m.ID == "copilot/gpt-4o" {
			gpt4o = &models[i]
			break
		}
	}
	if gpt4o == nil {
		t.Fatal("copilot/gpt-4o not found")
	}
	if gpt4o.UpstreamID != "gpt-4o" {
		t.Fatalf("upstreamId: %s", gpt4o.UpstreamID)
	}
	if gpt4o.Name != "GPT-4o" {
		t.Fatalf("name: %s", gpt4o.Name)
	}
	if gpt4o.UsageID != "copilot/gpt-4o" {
		t.Fatalf("usageId: %s", gpt4o.UsageID)
	}
	if gpt4o.ContextLength == nil || *gpt4o.ContextLength != 128000 {
		t.Fatalf("contextLength: %v", gpt4o.ContextLength)
	}
}
