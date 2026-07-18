package providers

import (
	"context"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

const openRouterChatCompletionsURL = "https://openrouter.ai/api/v1/chat/completions"
const openRouterAnthropicMessagesURL = "https://openrouter.ai/api/v1/messages"

func PostOpenRouterChatCompletion(ctx context.Context, apiKey string, body any, client types.HTTPDoer) (*http.Response, error) {
	req, err := utils.JSONRequest(ctx, http.MethodPost, openRouterChatCompletionsURL, map[string]string{
		"Authorization":      "Bearer " + apiKey,
		"Content-Type":       "application/json",
		"HTTP-Referer":       "https://github.com/hakilee/sleepyrouter",
		"X-OpenRouter-Title": "sleepyrouter",
	}, body)
	if err != nil {
		return nil, err
	}
	return utils.HTTPClient(client).Do(req)
}

func PostOpenRouterAnthropicMessage(ctx context.Context, apiKey string, body any, client types.HTTPDoer) (*http.Response, error) {
	req, err := utils.JSONRequest(ctx, http.MethodPost, openRouterAnthropicMessagesURL, map[string]string{
		"Authorization":      "Bearer " + apiKey,
		"Content-Type":       "application/json",
		"anthropic-version":  "2023-06-01",
		"HTTP-Referer":       "https://github.com/hakilee/sleepyrouter",
		"X-OpenRouter-Title": "sleepyrouter",
	}, body)
	if err != nil {
		return nil, err
	}
	return utils.HTTPClient(client).Do(req)
}
