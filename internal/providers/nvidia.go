package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/zendev-sh/goai/provider/nvidia"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

const nvidiaChatCompletionsURL = "https://integrate.api.nvidia.com/v1/chat/completions"

type NVIDIAProvider struct {
	BaseProvider
}

func (p *NVIDIAProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	modelID := modelIDFrom(body)
	model := nvidia.Chat(
		modelID,
		nvidia.WithAPIKey(apiKey),
		nvidia.WithBaseURL(baseURLFrom("SLEEPYROUTER_NVIDIA_BASE_URL", "https://integrate.api.nvidia.com/v1")),
		nvidia.WithHTTPClient(httpClientFor(client)),
	)
	return goaiChatCompletion(ctx, model, modelID, body, client)
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
