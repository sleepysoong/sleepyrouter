package sleepyrouter

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const CopilotChatCompletionsURL = "https://api.githubcopilot.com/chat/completions"
const CopilotTokenURL = "https://api.github.com/copilot_internal/v2/token"

type copilotToken struct {
	Token     string
	ExpiresAt time.Time
}

var copilotTokenCache struct {
	sync.Mutex
	token *copilotToken
}

var knownCopilotModels = []struct {
	ID            string
	Name          string
	ContextLength int
}{
	{"gpt-4o", "GPT-4o", 128000},
	{"gpt-4o-mini", "GPT-4o Mini", 128000},
	{"o3-mini", "o3-mini", 200000},
	{"claude-sonnet-4", "Claude Sonnet 4", 200000},
	{"gemini-2.5-pro", "Gemini 2.5 Pro", 1000000},
}

func normalizeCopilotModel(def struct {
	ID            string
	Name          string
	ContextLength int
}) OmfmModel {
	return OmfmModel{
		ID:            "copilot/" + def.ID,
		UpstreamID:    def.ID,
		Name:          def.Name,
		Provider:      "copilot",
		Source:        SourceCopilot,
		UsageID:       "copilot/" + def.ID,
		ContextLength: intPointer(def.ContextLength),
	}
}

func exchangeCopilotToken(ctx context.Context, apiKey string, client HTTPDoer) (*copilotToken, error) {
	req, err := getRequest(ctx, CopilotTokenURL, map[string]string{
		"Authorization": "token " + apiKey,
		"User-Agent":    "sleepyrouter/" + Version,
	})
	if err != nil {
		return nil, err
	}
	response, err := httpClient(client).Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if !isOK(response) {
		return nil, fmt.Errorf("Copilot 토큰 교환 실패: %d %s (GET copilot_internal/v2/token)", response.StatusCode, statusText(response))
	}
	body, err := responseJSON(response)
	if err != nil {
		return nil, err
	}
	token := stringFromUnknown(body["token"])
	expiresAt, ok := body["expires_at"].(float64)
	if token == "" || !ok || expiresAt == 0 {
		return nil, fmt.Errorf("Copilot 토큰 응답에 token 또는 expires_at 필드가 없어요.")
	}
	return &copilotToken{Token: token, ExpiresAt: time.Unix(int64(expiresAt), 0)}, nil
}

func copilotSessionToken(ctx context.Context, apiKey string, client HTTPDoer) (string, error) {
	copilotTokenCache.Lock()
	cached := copilotTokenCache.token
	if cached != nil && time.Now().Before(cached.ExpiresAt.Add(-5*time.Minute)) {
		token := cached.Token
		copilotTokenCache.Unlock()
		return token, nil
	}
	copilotTokenCache.Unlock()
	token, err := exchangeCopilotToken(ctx, apiKey, client)
	if err != nil {
		return "", err
	}
	copilotTokenCache.Lock()
	copilotTokenCache.token = token
	copilotTokenCache.Unlock()
	return token.Token, nil
}

func ListCopilotFreeModels(ctx context.Context, apiKey string, client HTTPDoer) ([]OmfmModel, error) {
	if _, err := exchangeCopilotToken(ctx, apiKey, client); err != nil {
		return nil, err
	}
	models := make([]OmfmModel, 0, len(knownCopilotModels))
	for _, model := range knownCopilotModels {
		models = append(models, normalizeCopilotModel(model))
	}
	return models, nil
}

func PostCopilotChatCompletion(ctx context.Context, apiKey string, body any, client HTTPDoer) (*http.Response, error) {
	sessionToken, err := copilotSessionToken(ctx, apiKey, client)
	if err != nil {
		return nil, err
	}
	req, err := jsonRequest(ctx, http.MethodPost, CopilotChatCompletionsURL, map[string]string{
		"Authorization":          "Bearer " + sessionToken,
		"Content-Type":           "application/json",
		"Copilot-Integration-Id": "vscode-chat",
		"Editor-Version":         "vscode/1.99.0",
		"Editor-Plugin-Version":  "copilot-chat/0.26.7",
		"x-github-api-version":   "2025-04-01",
	}, body)
	if err != nil {
		return nil, err
	}
	return httpClient(client).Do(req)
}

func ResetCopilotTokenCache() {
	copilotTokenCache.Lock()
	copilotTokenCache.token = nil
	copilotTokenCache.Unlock()
}
