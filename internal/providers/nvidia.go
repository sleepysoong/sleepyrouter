package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

const NVIDIAChatCompletionsURL = "https://integrate.api.nvidia.com/v1/chat/completions"

type NVIDIAProvider struct {
	BaseProvider
}

func (p *NVIDIAProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return postChatCompletion(ctx, NVIDIAChatCompletionsURL, map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}, body, client)
}

func init() {
	RegisterProvider(types.SourceNVIDIA, &NVIDIAProvider{
		BaseProvider: BaseProvider{
			NameValue:   "NVIDIA",
			SourceValue: types.SourceNVIDIA,
			Protocol:    ProtocolOpenAI,
			MessagesErr: fmt.Errorf("Messages not supported natively by NVIDIA provider"),
		},
	})
}
