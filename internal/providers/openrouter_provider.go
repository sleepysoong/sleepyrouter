package providers

import (
	"context"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

const openRouterChatCompletionsURL = "https://openrouter.ai/api/v1/chat/completions"
const openRouterAnthropicMessagesURL = "https://openrouter.ai/api/v1/messages"

type OpenRouterProvider struct {
	BaseProvider
}

func (p *OpenRouterProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return postChatCompletion(ctx, openRouterChatCompletionsURL, map[string]string{
		"Authorization":      "Bearer " + apiKey,
		"Content-Type":       "application/json",
		"HTTP-Referer":       "https://github.com/sleepysoong/sleepyrouter",
		"X-OpenRouter-Title": "sleepyrouter",
	}, body, client)
}

func (p *OpenRouterProvider) Messages(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return postChatCompletion(ctx, openRouterAnthropicMessagesURL, map[string]string{
		"Authorization":      "Bearer " + apiKey,
		"Content-Type":       "application/json",
		"anthropic-version":  "2023-06-01",
		"HTTP-Referer":       "https://github.com/sleepysoong/sleepyrouter",
		"X-OpenRouter-Title": "sleepyrouter",
	}, body, client)
}

func init() {
	RegisterProvider(types.SourceOpenRouter, &OpenRouterProvider{
		BaseProvider: BaseProvider{
			NameValue:   "OpenRouter",
			SourceValue: types.SourceOpenRouter,
			Protocol:    ProtocolAnthropic,
		},
	})
}
