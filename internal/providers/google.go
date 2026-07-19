package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

const googleChatCompletionsURL = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"

type GoogleProvider struct {
	BaseProvider
}

func (p *GoogleProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return postChatCompletion(ctx, googleChatCompletionsURL, map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}, body, client)
}

func init() {
	RegisterProvider(types.SourceGoogle, &GoogleProvider{
		BaseProvider: BaseProvider{
			NameValue:   "Google",
			SourceValue: types.SourceGoogle,
			Protocol:    ProtocolOpenAI,
			MessagesErr: fmt.Errorf("Messages not supported natively by Google provider"),
		},
	})
}
