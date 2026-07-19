package srv

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

func modelUpstreamID(model types.SleepyRouterModel) string {
	if model.UpstreamID != "" {
		return model.UpstreamID
	}
	return model.ID
}

type selectedModelsResult struct {
	Models            []types.SleepyRouterModel
	ByID              map[string]types.SleepyRouterModel
	IDs               []string
	ModelGroups       types.ModelGroups
	GroupOrder        []string
	DefaultModelGroup string
}

func selectedModelSelection(ctx context.Context, store *cfg.ConfigStore, apiKeys types.ProviderAPIKeys, client types.HTTPDoer) (*selectedModelsResult, error) {
	config, err := store.ReadConfig()
	if err != nil {
		return nil, err
	}
	allIDs := routing.AllGroupModelIDs(config.ModelGroups, config.GroupOrder...)
	models := make([]types.SleepyRouterModel, 0, len(allIDs))
	byID := make(map[string]types.SleepyRouterModel, len(allIDs))
	for _, id := range allIDs {
		def, ok := config.Models[id]
		if !ok {
			continue
		}
		source := types.ModelSource(def.Provider)
		m := types.SleepyRouterModel{
			ID:         id,
			UpstreamID: def.Name,
			Provider:   def.Provider,
			Source:     source,
			UsageID:    id,
		}
		models = append(models, m)
		byID[id] = m
	}
	return &selectedModelsResult{
		Models:            models,
		ByID:              byID,
		IDs:               modelIDs(models),
		ModelGroups:       config.ModelGroups,
		GroupOrder:        config.GroupOrder,
		DefaultModelGroup: config.DefaultModelGroup,
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
	case types.SourceZen:
		keyName = "OPENCODE_API_KEY"
	case types.SourceGoogle:
		keyName = "GOOGLE_API_KEY"
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
	writeJSONError(w, 400, "설정된 API 키로 사용 가능한 무료 모델이 없어요. API 키 설정과 모델 ID를 확인하세요.", map[string]any{"details": lastError})
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

func estimateInputTokens(body any) int {
	text, _ := utils.MarshalJSONHelper(body)
	if n := len(text); n > 0 {
		return max(1, (n+3)/4)
	}
	return 1
}
