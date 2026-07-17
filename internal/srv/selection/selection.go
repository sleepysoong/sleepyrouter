// Package selection owns the model-selection logic that turns a
// ConfigStore + provider credentials into the ordered list of usable
// upstream models for a given request.
//
// It is intentionally free of HTTP machinery: callers hand it a
// context, store, and provider keys, and receive a *Result describing
// the free models that should be tried, in order, for routing.
package selection

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/providers"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// UpstreamID returns the canonical model identifier an upstream provider
// will recognise. If the model carries an explicit UpstreamID it wins;
// otherwise provider-specific prefixes ("nvidia/", "copilot/") are
// stripped so the bare upstream name is sent.
func UpstreamID(model types.SleepyRouterModel) string {
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

// Result bundles everything the router needs to make a decision after
// selection has finished.
type Result struct {
	Models       []types.SleepyRouterModel
	ByID         map[string]types.SleepyRouterModel
	IDs          []string
	ModelGroups  types.ModelGroups
	GroupOrder   []string
	DefaultGroup string
}

// Load reads the persisted config and the provider model catalog, then
// returns the subset of configured models that are either free upstream
// models or stubs the cache could not resolve. Failures to read the cache
// are swallowed because stubs are an acceptable fallback.
func Load(ctx context.Context, store *cfg.ConfigStore, apiKeys types.ProviderAPIKeys, client types.HTTPDoer) (*Result, error) {
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
	return &Result{
		Models:       models,
		ByID:         byID,
		IDs:          IDs(models),
		ModelGroups:  config.ModelGroups,
		GroupOrder:   config.GroupOrder,
		DefaultGroup: config.DefaultGroup,
	}, nil
}

// IDs returns the ordered list of model IDs for a slice of models. It
// is provided alongside Load because the router needs IDs in the same
// order as the models for log surfaces and fallback iteration.
func IDs(models []types.SleepyRouterModel) []string {
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	return ids
}

// MissingKeyMessage renders the Korean-language error string shown when
// a model's provider API key is absent. Centralising it here keeps the
// router's loop focused on dispatch.
func MissingKeyMessage(model types.SleepyRouterModel) string {
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

// RequestedModelForRouting normalises the requested model string against
// the selected model catalog. If the request asked for an upstream ID,
// we translate it back to the canonical in-config ID so routing groups
// resolve correctly. An unmatched string is returned verbatim so the
// router can decide what to do with it.
func RequestedModelForRouting(models []types.SleepyRouterModel, requestedModel any) string {
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
		if UpstreamID(m) == s {
			return m.ID
		}
	}
	return s
}

// WithUpstreamModel clones the request body, replaces "model" with the
// upstream identifier, and (for streamed requests) asks the upstream to
// include usage in the final chunk. It never mutates the input.
func WithUpstreamModel(body map[string]any, model types.SleepyRouterModel, stream bool) map[string]any {
	result := utils.CloneObject(body)
	result["model"] = UpstreamID(model)
	if stream {
		result["stream_options"] = map[string]any{"include_usage": true}
	}
	return result
}

// WriteNoUsableModelResponse writes the 400 response used when no
// configured model had a matching API key. The last upstream error is
// included so the client can see why every candidate was skipped.
func WriteNoUsableModelResponse(w http.ResponseWriter, lastError string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	data, _ := json.Marshal(map[string]any{"error": map[string]any{"message": "설정된 API 키로 사용 가능한 무료 모델이 없어요. API 키 설정과 모델 ID를 확인하세요.", "details": lastError}})
	_, _ = w.Write(data)
}

// AssertNonEmpty returns the Korean error for "no free models selected"
// when the chosen route produced an empty list. Returning an error keeps
// the caller-side handlers free of model-list checks.
func AssertNonEmpty(models []types.SleepyRouterModel) error {
	if len(models) == 0 {
		return fmt.Errorf("선택된 무료 모델이 없어요. config.json의 modelGroups에 사용할 모델을 하나 이상 추가하세요. (예: \"nvidia/z-ai/glm-5.1\")")
	}
	return nil
}
