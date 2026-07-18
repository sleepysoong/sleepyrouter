package providers

import (
	"context"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

type OpenRouterProvider struct{}

func (p *OpenRouterProvider) Name() string {
	return "OpenRouter"
}

func (p *OpenRouterProvider) Source() types.ModelSource {
	return types.SourceOpenRouter
}

func (p *OpenRouterProvider) ListFreeModels(ctx context.Context, apiKey string, client types.HTTPDoer) ([]types.SleepyRouterModel, error) {
	return ListOpenRouterFreeModels(ctx, apiKey, client)
}

func (p *OpenRouterProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, stream bool, client types.HTTPDoer) (*http.Response, error) {
	return PostOpenRouterChatCompletion(ctx, apiKey, body, client)
}

func (p *OpenRouterProvider) Messages(ctx context.Context, apiKey string, body map[string]any, stream bool, client types.HTTPDoer) (*http.Response, error) {
	return PostOpenRouterAnthropicMessage(ctx, apiKey, body, client)
}

func (p *OpenRouterProvider) MessageProtocol() MessageProtocol {
	return ProtocolAnthropic
}

func init() {
	RegisterProvider(types.SourceOpenRouter, &OpenRouterProvider{})
}
