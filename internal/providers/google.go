package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/zendev-sh/goai/provider/compat"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

const googleChatCompletionsURL = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"

type GoogleProvider struct {
	BaseProvider
}

func (p *GoogleProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	modelID := modelIDFrom(body)
	model := compat.Chat(
		modelID,
		compat.WithBaseURL(baseURLFrom("SLEEPYROUTER_GOOGLE_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai")),
		compat.WithAPIKey(apiKey),
		compat.WithIncludeReasoningContent(true),
		compat.WithHTTPClient(httpClientFor(client)),
	)
	return goaiChatCompletion(ctx, model, modelID, body, client)
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
