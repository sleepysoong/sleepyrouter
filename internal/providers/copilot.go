package providers

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
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

func exchangeCopilotToken(ctx context.Context, apiKey string, client types.HTTPDoer) (*copilotToken, error) {
	req, err := utils.GetRequest(ctx, CopilotTokenURL, map[string]string{
		"Authorization": "token " + apiKey,
		"User-Agent":    "sleepyrouter/" + types.Version,
	})
	if err != nil {
		return nil, err
	}
	response, err := utils.HTTPClient(client).Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if !utils.IsOK(response) {
		return nil, fmt.Errorf("Copilot 토큰 교환 실패: %d %s (GET copilot_internal/v2/token)", response.StatusCode, utils.StatusText(response))
	}
	body, err := utils.ResponseJSON(response)
	if err != nil {
		return nil, err
	}
	token := utils.StringFromUnknown(body["token"])
	expiresAt, ok := body["expires_at"].(float64)
	if token == "" || !ok || expiresAt == 0 {
		return nil, fmt.Errorf("Copilot 토큰 응답에 token 또는 expires_at 필드가 없어요.")
	}
	return &copilotToken{Token: token, ExpiresAt: time.Unix(int64(expiresAt), 0)}, nil
}

func copilotSessionToken(ctx context.Context, apiKey string, client types.HTTPDoer) (string, error) {
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

func PostCopilotChatCompletion(ctx context.Context, apiKey string, body any, client types.HTTPDoer) (*http.Response, error) {
	sessionToken, err := copilotSessionToken(ctx, apiKey, client)
	if err != nil {
		return nil, err
	}
	req, err := utils.JSONRequest(ctx, http.MethodPost, CopilotChatCompletionsURL, map[string]string{
		"Authorization":          "Bearer " + sessionToken,
		"Content-Type":           "application/json",
		"Copilot-Integration-Id": "vscode-chat",
		"Editor-types.Version":         "vscode/1.99.0",
		"Editor-Plugin-types.Version":  "copilot-chat/0.26.7",
		"x-github-api-version":   "2025-04-01",
	}, body)
	if err != nil {
		return nil, err
	}
	return utils.HTTPClient(client).Do(req)
}

func ResetCopilotTokenCache() {
	copilotTokenCache.Lock()
	copilotTokenCache.token = nil
	copilotTokenCache.Unlock()
}

type CopilotProvider struct {
	BaseProvider
}

func (p *CopilotProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return PostCopilotChatCompletion(ctx, apiKey, body, client)
}

func init() {
	RegisterProvider(types.SourceCopilot, &CopilotProvider{
		BaseProvider: BaseProvider{
			NameValue:   "Copilot",
			SourceValue: types.SourceCopilot,
			Protocol:    ProtocolOpenAI,
			MessagesErr: fmt.Errorf("Messages not supported natively by Copilot provider"),
		},
	})
}
