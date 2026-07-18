package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

const ZenChatCompletionsURL = "https://opencode.ai/zen/v1/chat/completions"

func PostZenChatCompletion(ctx context.Context, apiKey string, body any, client types.HTTPDoer) (*http.Response, error) {
	req, err := utils.JSONRequest(ctx, http.MethodPost, ZenChatCompletionsURL, map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}, body)
	if err != nil {
		return nil, err
	}
	return utils.HTTPClient(client).Do(req)
}

type ZenProvider struct {
	BaseProvider
}

func (p *ZenProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return PostZenChatCompletion(ctx, apiKey, body, client)
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
