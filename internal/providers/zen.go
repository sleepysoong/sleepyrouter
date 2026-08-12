package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/zendev-sh/goai/provider/compat"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

const zenChatCompletionsURL = "https://opencode.ai/zen/v1/chat/completions"

type ZenProvider struct {
	BaseProvider
}

func (p *ZenProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	modelID := modelIDFrom(body)
	model := compat.Chat(
		modelID,
		compat.WithBaseURL(baseURLFrom("SLEEPYROUTER_ZEN_BASE_URL", "https://opencode.ai/zen/v1")),
		compat.WithAPIKey(apiKey),
		compat.WithHTTPClient(httpClientFor(client)),
	)
	return goaiChatCompletion(ctx, model, modelID, body, client)
}

func init() {
	RegisterProvider(types.SourceZen, &ZenProvider{
		BaseProvider: BaseProvider{
			NameValue:   "Zen",
			SourceValue: types.SourceZen,
			Protocol:    ProtocolOpenAI,
			MessagesErr: fmt.Errorf("Messages not supported natively by Zen provider"),
		},
	})
}
