package providers

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
	ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error)
	Messages(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error)
	MessageProtocol() MessageProtocol
}

var providers = make(map[types.ModelSource]Provider)

func RegisterProvider(source types.ModelSource, p Provider) {
	providers[source] = p
}

func GetProvider(source types.ModelSource) Provider {
	return providers[source]
}

// BaseProvider carries the metadata shared by every provider so concrete
// providers only implement the parts that actually differ: ListFreeModels and
// ChatCompletion. Providers that do not natively support the Messages endpoint
// precompute the error in messagesErr and inherit Messages from BaseProvider;
// providers that do support it (OpenRouter) override Messages.
type BaseProvider struct {
	NameValue   string
	SourceValue types.ModelSource
	Protocol    MessageProtocol
	MessagesErr error
}

func (b *BaseProvider) Name() string                  { return b.NameValue }
func (b *BaseProvider) Source() types.ModelSource     { return b.SourceValue }
func (b *BaseProvider) MessageProtocol() MessageProtocol { return b.Protocol }
func (b *BaseProvider) Messages(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return nil, b.MessagesErr
}

func AllProviders() []Provider {
	order := types.AllModelSources
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
