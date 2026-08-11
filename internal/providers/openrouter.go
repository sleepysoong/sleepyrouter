package providers

import (
	"context"
	"net/http"

	"github.com/zendev-sh/goai/provider/anthropic"
	"github.com/zendev-sh/goai/provider/openrouter"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

const (
	openRouterChatCompletionsURL   = "https://openrouter.ai/api/v1/chat/completions"
	openRouterAnthropicMessagesURL = "https://openrouter.ai/api/v1/messages"
)

// openRouterHeaders are attached to every OpenRouter request, matching the
// legacy provider wire behavior.
func openRouterHeaders() map[string]string {
	return map[string]string{
		"HTTP-Referer":       "https://github.com/sleepysoong/sleepyrouter",
		"X-OpenRouter-Title": "sleepyrouter",
	}
}

type OpenRouterProvider struct {
	BaseProvider
}

func (p *OpenRouterProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	modelID := modelIDFrom(body)
	model := openrouter.Chat(
		modelID,
		openrouter.WithAPIKey(apiKey),
		openrouter.WithHeaders(openRouterHeaders()),
		openrouter.WithHTTPClient(httpClientFor(client)),
	)
	return goaiChatCompletion(ctx, model, modelID, body, client)
}

func (p *OpenRouterProvider) Messages(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	modelID := modelIDFrom(body)
	model := anthropic.Chat(
		modelID,
		anthropic.WithBaseURL("https://openrouter.ai"),
		anthropic.WithURLBuilder(func(baseURL, modelID string, streaming bool) string {
			return openRouterAnthropicMessagesURL
		}),
		anthropic.WithAPIKey(apiKey),
		anthropic.WithAuthMode(anthropic.AuthBearer),
		anthropic.WithBetaFeatures(""),
		anthropic.WithHeaders(map[string]string{
			"HTTP-Referer":       "https://github.com/sleepysoong/sleepyrouter",
			"X-OpenRouter-Title": "sleepyrouter",
			"anthropic-version":  "2023-06-01",
		}),
		anthropic.WithHTTPClient(httpClientFor(client)),
	)
	return goaiAnthropicMessages(ctx, model, modelID, body, client)
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
