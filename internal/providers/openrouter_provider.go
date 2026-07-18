package providers

import (
	"context"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

type OpenRouterProvider struct {
	BaseProvider
}

func (p *OpenRouterProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return PostOpenRouterChatCompletion(ctx, apiKey, body, client)
}

func (p *OpenRouterProvider) Messages(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return PostOpenRouterAnthropicMessage(ctx, apiKey, body, client)
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
