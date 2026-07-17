package srv

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/providers"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

func modelUpstreamID(model types.SleepyRouterModel) string {
	source := types.SourceOf(model)
	if model.UpstreamID != "" {
		return model.UpstreamID
	}
	switch source {
	case types.SourceNVIDIA:
		return strings.TrimPrefix(model.ID, "nvidia/")
	case types.SourceCopilot:
		return strings.TrimPrefix(model.ID, "copilot/")
	default:
		return model.ID
	}
}

type selectedModelsResult struct {
	Models       []types.SleepyRouterModel
	ByID         map[string]types.SleepyRouterModel
	IDs          []string
	ModelGroups  types.ModelGroups
	GroupOrder   []string
	DefaultGroup string
}

func selectedModelSelection(ctx context.Context, store *cfg.ConfigStore, apiKeys types.ProviderAPIKeys, client types.HTTPDoer) (*selectedModelsResult, error) {
	config, err := store.ReadConfig()
	if err != nil {
		return nil, err
	}
	catalog, err := providers.LoadModelCatalog(ctx, apiKeys, client, store)
	if err != nil {
		return nil, err
	}
	var freeModels []types.SleepyRouterModel
	for _, m := range catalog.Models {
		if providers.IsCachedFreeModel(m) {
			freeModels = append(freeModels, m)
		}
	}
	allIDs := routing.AllGroupModelIDs(config.ModelGroups, config.GroupOrder...)
	freeByID := make(map[string]types.SleepyRouterModel, len(freeModels))
	for _, m := range freeModels {
		freeByID[m.ID] = m
	}
	cache, _ := store.ReadModelCache()
	cacheIDs := make(map[string]bool)
	if cache != nil {
		for _, m := range cache.Models {
			cacheIDs[m.ID] = true
		}
	}
	models := make([]types.SleepyRouterModel, 0, len(allIDs))
	byID := make(map[string]types.SleepyRouterModel, len(allIDs))
	for _, id := range allIDs {
		if free, ok := freeByID[id]; ok {
			models = append(models, free)
			byID[id] = free
		} else if !cacheIDs[id] {
			source := types.SourceOpenRouter
			if strings.HasPrefix(id, "nvidia/") {
				source = types.SourceNVIDIA
			} else if strings.HasPrefix(id, "copilot/") {
				source = types.SourceCopilot
			}
			stub := types.SleepyRouterModel{ID: id, Name: id, Provider: string(source), Source: source}
			models = append(models, stub)
			byID[id] = stub
		}
	}
	return &selectedModelsResult{
		Models:       models,
		ByID:         byID,
		IDs:          modelIDs(models),
		ModelGroups:  config.ModelGroups,
		GroupOrder:   config.GroupOrder,
		DefaultGroup: config.DefaultGroup,
	}, nil
}

func modelIDs(models []types.SleepyRouterModel) []string {
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	return ids
}

func assertSelectedFree(models []types.SleepyRouterModel) error {
	if len(models) == 0 {
		return &httpError{StatusCode: 400, Message: "선택된 무료 모델이 없어요. config.json의 modelGroups에 사용할 모델을 하나 이상 추가하세요. (예: \"nvidia/z-ai/glm-5.1\")"}
	}
	return nil
}

func missingKeyMessage(model types.SleepyRouterModel) string {
	source := types.SourceOf(model)
	keyName := "OPENROUTER_API_KEY"
	switch source {
	case types.SourceNVIDIA:
		keyName = "NVIDIA_API_KEY"
	case types.SourceCopilot:
		keyName = "GITHUB_COPILOT_TOKEN"
	}
	return fmt.Sprintf("%s가 없어서 %s을(를) 사용할 수 없어요. 환경변수 또는 .env 파일에 키를 추가하세요.", keyName, model.ID)
}

func withUpstreamModel(body map[string]any, model types.SleepyRouterModel, stream bool) map[string]any {
	result := utils.CloneObject(body)
	result["model"] = modelUpstreamID(model)
	if stream {
		result["stream_options"] = map[string]any{"include_usage": true}
	}
	return result
}

func requestedModelForRouting(models []types.SleepyRouterModel, requestedModel any) string {
	s, ok := requestedModel.(string)
	if !ok || s == "" {
		return ""
	}
	for _, m := range models {
		if m.ID == s {
			return s
		}
	}
	for _, m := range models {
		if modelUpstreamID(m) == s {
			return m.ID
		}
	}
	return s
}

func noUsableModelResponse(w http.ResponseWriter, lastError string) {
	writeJSON(w, 400, map[string]any{"error": map[string]any{"message": "설정된 API 키로 사용 가능한 무료 모델이 없어요. API 키 설정과 모델 ID를 확인하세요.", "details": lastError}})
}

func usageFromResponse(data map[string]any) (inputTokens, outputTokens, totalTokens *int) {
	usage, ok := data["usage"].(map[string]any)
	if !ok {
		return
	}
	inputTokens = utils.NumberValue(usage["prompt_tokens"])
	if inputTokens == nil {
		inputTokens = utils.NumberValue(usage["input_tokens"])
	}
	outputTokens = utils.NumberValue(usage["completion_tokens"])
	if outputTokens == nil {
		outputTokens = utils.NumberValue(usage["output_tokens"])
	}
	totalTokens = utils.NumberValue(usage["total_tokens"])
	if totalTokens == nil && (inputTokens != nil || outputTokens != nil) {
		in := 0
		out := 0
		if inputTokens != nil {
			in = *inputTokens
		}
		if outputTokens != nil {
			out = *outputTokens
		}
		totalTokens = utils.IntPointer(in + out)
	}
	return
}

func recordSuccessfulUsage(store *cfg.ConfigStore, model types.SleepyRouterModel, data map[string]any) {
	inputTokens, outputTokens, _ := usageFromResponse(data)
	in := 0
	out := 0
	if inputTokens != nil {
		in = *inputTokens
	}
	if outputTokens != nil {
		out = *outputTokens
	}
	usageID := model.UsageID
	if usageID == "" {
		usageID = model.ID
	}
	_ = store.AppendUsage(types.UsageLogEntry{TS: time.Now().UTC().Format(time.RFC3339), Model: usageID, InputTokens: in, OutputTokens: out, Success: true})
}

func recordUpstreamFailure(store *cfg.ConfigStore, model types.SleepyRouterModel, response *http.Response) string {
	text, _ := io.ReadAll(response.Body)
	usageID := model.UsageID
	if usageID == "" {
		usageID = model.ID
	}
	_ = store.AppendUsage(types.UsageLogEntry{TS: time.Now().UTC().Format(time.RFC3339), Model: usageID, InputTokens: 0, OutputTokens: 0, Success: false})
	return fmt.Sprintf("[%d] %s", response.StatusCode, string(text))
}

func writeOpenAIAsAnthropic(ctx context.Context, upstream *http.Response, w http.ResponseWriter, body map[string]any, modelID string) {
	if streamVal, ok := body["stream"].(bool); ok && streamVal {
		PipeOpenAIStreamAsAnthropic(upstream.Body, w, modelID)
		return
	}
	data, err := utils.ResponseJSON(upstream)
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": map[string]any{"message": "업스트림 응답을 읽을 수 없어요.", "details": err.Error()}})
		return
	}
	writeJSON(w, upstream.StatusCode, OpenAIToAnthropic(data, modelID))
}

func estimateInputTokens(body any) int {
	text, _ := utils.MarshalJSONHelper(body)
	if n := len(text); n > 0 {
		return max(1, (n+3)/4)
	}
	return 1
}
