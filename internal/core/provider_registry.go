package core

import (
	"context"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

type MessageProtocol string

const (
	ProtocolOpenAI    MessageProtocol = "openai"
	ProtocolAnthropic MessageProtocol = "anthropic"
)

type Provider interface {
	Name() string
	Source() types.ModelSource
	ListFreeModels(ctx context.Context, apiKey string, client types.HTTPDoer) ([]types.SleepyRouterModel, error)
	ChatCompletion(ctx context.Context, apiKey string, body map[string]any, stream bool, client types.HTTPDoer) (*http.Response, error)
	Messages(ctx context.Context, apiKey string, body map[string]any, stream bool, client types.HTTPDoer) (*http.Response, error)
	MessageProtocol() MessageProtocol
}

var providers = make(map[types.ModelSource]Provider)

func RegisterProvider(source types.ModelSource, p Provider) {
	providers[source] = p
}

func GetProvider(source types.ModelSource) Provider {
	return providers[source]
}

func AllProviders() []Provider {
	// Stable order: OpenRouter first, then NVIDIA, then Copilot, then any others
	order := []types.ModelSource{types.SourceOpenRouter, types.SourceNVIDIA, types.SourceCopilot}
	list := make([]Provider, 0, len(providers))
	for _, source := range order {
		if p, ok := providers[source]; ok {
			list = append(list, p)
		}
	}
	for source, p := range providers {
		found := false
		for _, o := range order {
			if o == source {
				found = true
				break
			}
		}
		if !found {
			list = append(list, p)
		}
	}
	return list
}
