package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

const NVIDIAChatCompletionsURL = "https://integrate.api.nvidia.com/v1/chat/completions"

func PostNVIDIAChatCompletion(ctx context.Context, apiKey string, body any, client types.HTTPDoer) (*http.Response, error) {
	req, err := utils.JSONRequest(ctx, http.MethodPost, NVIDIAChatCompletionsURL, map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}, body)
	if err != nil {
		return nil, err
	}
	return utils.HTTPClient(client).Do(req)
}

type NVIDIAProvider struct {
	BaseProvider
}

func (p *NVIDIAProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return PostNVIDIAChatCompletion(ctx, apiKey, body, client)
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
