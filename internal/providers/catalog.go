package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

type ProviderCatalogResult struct {
	Models []types.SleepyRouterModel
	Errors []string
}

type LoadedModelCatalog struct {
	Models []types.SleepyRouterModel
	Source string // "fresh", "fetched", "stale"
	Errors []string
}

func catalogErrorMessage(source string, err error) string {
	if err == nil {
		return source + ": <nil>"
	}
	return source + ": " + err.Error()
}

func modelsForConfiguredProviders(models []types.SleepyRouterModel, apiKeys types.ProviderAPIKeys) []types.SleepyRouterModel {
	filtered := make([]types.SleepyRouterModel, 0, len(models))
	for _, model := range models {
		if apiKeys.For(types.SourceOf(model)) != "" {
			filtered = append(filtered, model)
		}
	}
	return uniqueModelsByID(filtered)
}

func compareByPopularity(a, b types.SleepyRouterModel) bool {
	aRank, bRank := int(^uint(0)>>1), int(^uint(0)>>1)
	if a.PopularityRank != nil {
		aRank = *a.PopularityRank
	}
	if b.PopularityRank != nil {
		bRank = *b.PopularityRank
	}
	if aRank != bRank {
		return aRank < bRank
	}
	aSource := a.Source
	if aSource == "" {
		aSource = "openrouter"
	}
	bSource := b.Source
	if bSource == "" {
		bSource = "openrouter"
	}
	if aSource != bSource {
		return aSource < bSource
	}
	if a.Provider != b.Provider {
		return a.Provider < b.Provider
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	return a.ID < b.ID
}

func uniqueModelsByID(models []types.SleepyRouterModel) []types.SleepyRouterModel {
	byID := make(map[string]types.SleepyRouterModel)
	order := make([]string, 0, len(models))
	for _, model := range models {
		if _, exists := byID[model.ID]; !exists {
			byID[model.ID] = model
			order = append(order, model.ID)
		}
	}
	result := make([]types.SleepyRouterModel, 0, len(order))
	for _, id := range order {
		result = append(result, byID[id])
	}
	return result
}

func ListAvailableFreeModels(ctx context.Context, apiKeys types.ProviderAPIKeys, client types.HTTPDoer) ProviderCatalogResult {
	type fetchResult struct {
		providerName string
		models       []types.SleepyRouterModel
		err          error
	}

	allProvs := AllProviders()
	results := make([]fetchResult, len(allProvs))
	var wg sync.WaitGroup

	for i, p := range allProvs {
		apiKey := apiKeys.For(p.Source())
		if apiKey == "" {
			continue
		}

		wg.Add(1)
		go func(index int, prov Provider, key string) {
			defer wg.Done()
			m, err := prov.ListFreeModels(ctx, key, client)
			results[index] = fetchResult{
				providerName: prov.Name(),
				models:       m,
				err:          err,
			}
		}(i, p, apiKey)
	}
	wg.Wait()

	var models []types.SleepyRouterModel
	var errors []string

	for _, res := range results {
		if res.providerName == "" {
			continue // Skip providers that were not fetched (no API key)
		}
		if res.err != nil {
			errors = append(errors, catalogErrorMessage(res.providerName, res.err))
		} else {
			models = append(models, res.models...)
		}
	}

	sort.SliceStable(models, func(i, j int) bool {
		return compareByPopularity(models[i], models[j])
	})

	return ProviderCatalogResult{
		Models: uniqueModelsByID(models),
		Errors: errors,
	}
}

func LoadModelCatalog(ctx context.Context, apiKeys types.ProviderAPIKeys, client types.HTTPDoer, store *cfg.ConfigStore) (*LoadedModelCatalog, error) {
	cache, _ := store.ReadModelCache()
	var cachedModels []types.SleepyRouterModel
	if cache != nil {
		cachedModels = modelsForConfiguredProviders(cache.Models, apiKeys)
	}
	if cache != nil && cfg.IsModelCacheFresh(*cache, time.Now()) && len(cachedModels) > 0 {
		return &LoadedModelCatalog{Models: cachedModels, Source: "fresh", Errors: nil}, nil
	}

	result := ListAvailableFreeModels(ctx, apiKeys, client)
	if len(result.Models) > 0 {
		_ = store.WriteModelCache(types.ModelCache{Models: result.Models, FetchedAt: time.Now().UTC().Format(time.RFC3339)})
		return &LoadedModelCatalog{Models: result.Models, Source: "fetched", Errors: result.Errors}, nil
	}
	if cache != nil && len(cachedModels) > 0 {
		return &LoadedModelCatalog{Models: cachedModels, Source: "stale", Errors: result.Errors}, nil
	}
	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("모든 프로바이더 모델 가져오기 실패: %s", JoinStrings(result.Errors, "; "))
	}
	return nil, fmt.Errorf("사용 가능한 프로바이더 모델이 없어요.")
}

func JoinStrings(items []string, sep string) string {
	if len(items) == 0 {
		return ""
	}
	result := items[0]
	for i := 1; i < len(items); i++ {
		result += sep + items[i]
	}
	return result
}

func IsCachedFreeModel(model types.SleepyRouterModel) bool {
	source := types.SourceOf(model)
	if source == types.SourceNVIDIA || source == types.SourceCopilot || source == types.SourceZen {
		return true
	}
	if strings.HasSuffix(model.ID, ":free") {
		return true
	}
	if len(model.Raw) > 0 {
		var raw map[string]any
		if json.Unmarshal(model.Raw, &raw) == nil {
			return IsFreeOpenRouterModelRaw(raw)
		}
	}
	return false
}

func IsFreeOpenRouterModelRaw(raw map[string]any) bool {
	modelID := utils.StringFromUnknown(raw["id"])
	if modelID == "" {
		return false
	}
	arch, _ := raw["architecture"].(map[string]any)
	outputs, _ := arch["output_modalities"].([]any)
	isTextOutput := len(outputs) == 0
	if !isTextOutput {
		for _, m := range outputs {
			if s, ok := m.(string); ok && s == "text" {
				isTextOutput = true
				break
			}
		}
	}
	if !isTextOutput {
		return false
	}
	if strings.HasSuffix(modelID, ":free") {
		return true
	}
	pricing, _ := raw["pricing"].(map[string]any)
	if pricing == nil {
		pricing = map[string]any{}
	}
	_, hasRequest := pricing["request"]
	requestVal := float64(0)
	if !hasRequest {
		requestVal = 0
	} else {
		requestVal = toFloat(pricing["request"])
	}
	return priceIsZeroRaw(pricing["prompt"]) && priceIsZeroRaw(pricing["completion"]) && priceIsZeroRaw(requestVal)
}

func toFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		f, _ := parseFloat(v)
		return f
	default:
		return 0
	}
}

func priceIsZeroRaw(value any) bool {
	if value == nil || value == "" {
		return false
	}
	switch v := value.(type) {
	case float64:
		return v == 0
	case int:
		return v == 0
	case string:
		f, err := parseFloat(v)
		return err == nil && f == 0
	default:
		return false
	}
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

