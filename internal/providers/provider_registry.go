package providers

import (
	"context"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

type MessageProtocol string

const (
	ProtocolOpenAI    MessageProtocol = "openai"
	ProtocolAnthropic MessageProtocol = "anthropic"
)

type Provider interface {
	Name() string
	Source() types.ModelSource
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
// providers only implement the parts that actually differ: ChatCompletion.
// Providers that do not natively support the Messages endpoint
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

// postChatCompletion is the shared HTTP POST used by every provider's ChatCompletion.
func postChatCompletion(ctx context.Context, url string, headers map[string]string, body any, client types.HTTPDoer) (*http.Response, error) {
	req, err := utils.JSONRequest(ctx, http.MethodPost, url, headers, body)
	if err != nil {
		return nil, err
	}
	return utils.HTTPClient(client).Do(req)
}


