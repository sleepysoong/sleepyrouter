package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

const ZenChatCompletionsURL = "https://opencode.ai/zen/v1/chat/completions"

type ZenProvider struct {
	BaseProvider
}

func (p *ZenProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return postChatCompletion(ctx, ZenChatCompletionsURL, map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}, body, client)
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
