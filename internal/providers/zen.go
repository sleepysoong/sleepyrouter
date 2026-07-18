package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

const ZenChatCompletionsURL = "https://opencode.ai/zen/v1/chat/completions"

var knownZenModels = []struct {
	ID            string
	Name          string
	ContextLength int
}{
	{"deepseek-v4-flash-free", "DeepSeek V4 Flash Free", 128000},
}

func normalizeZenModel(def struct {
	ID            string
	Name          string
	ContextLength int
}) types.SleepyRouterModel {
	return types.SleepyRouterModel{
		ID:            "zen/" + def.ID,
		UpstreamID:    def.ID,
		Name:          def.Name,
		Provider:      "zen",
		Source:        types.SourceZen,
		UsageID:       "zen/" + def.ID,
		ContextLength: utils.IntPointer(def.ContextLength),
	}
}

func ListZenFreeModels(ctx context.Context, apiKey string, client types.HTTPDoer) ([]types.SleepyRouterModel, error) {
	models := make([]types.SleepyRouterModel, 0, len(knownZenModels))
	for _, m := range knownZenModels {
		models = append(models, normalizeZenModel(m))
	}
	return models, nil
}

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

type ZenProvider struct{}

func (p *ZenProvider) Name() string {
	return "Zen"
}

func (p *ZenProvider) Source() types.ModelSource {
	return types.SourceZen
}

func (p *ZenProvider) ListFreeModels(ctx context.Context, apiKey string, client types.HTTPDoer) ([]types.SleepyRouterModel, error) {
	return ListZenFreeModels(ctx, apiKey, client)
}

func (p *ZenProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return PostZenChatCompletion(ctx, apiKey, body, client)
}

func (p *ZenProvider) Messages(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return nil, fmt.Errorf("Messages not supported natively by Zen provider")
}

func (p *ZenProvider) MessageProtocol() MessageProtocol {
	return ProtocolOpenAI
}

func init() {
	RegisterProvider(types.SourceZen, &ZenProvider{})
}
