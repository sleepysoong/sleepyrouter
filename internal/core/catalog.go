package core

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/types"
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
	var wg sync.WaitGroup
	var openrouterModels, nvidiaModels, copilotModels []types.SleepyRouterModel
	var openrouterErr, nvidiaErr, copilotErr error
	labels := []string{}
	models := []types.SleepyRouterModel{}
	errors := []string{}

	if apiKeys.OpenRouter != "" {
		labels = append(labels, "OpenRouter")
		wg.Add(1)
		go func() {
			defer wg.Done()
			openrouterModels, openrouterErr = ListOpenRouterFreeModels(ctx, apiKeys.OpenRouter, client)
		}()
	}
	if apiKeys.NVIDIA != "" {
		labels = append(labels, "NVIDIA")
		wg.Add(1)
		go func() {
			defer wg.Done()
			nvidiaModels, nvidiaErr = ListNVIDIAFreeModels(ctx, apiKeys.NVIDIA, client)
		}()
	}
	if apiKeys.Copilot != "" {
		labels = append(labels, "Copilot")
		wg.Add(1)
		go func() {
			defer wg.Done()
			copilotModels, copilotErr = ListCopilotFreeModels(ctx, apiKeys.Copilot, client)
		}()
	}
	wg.Wait()

	// Collect results matching the original enrollment order
	for _, label := range labels {
		var m []types.SleepyRouterModel
		var e error
		switch label {
		case "OpenRouter":
			m, e = openrouterModels, openrouterErr
		case "NVIDIA":
			m, e = nvidiaModels, nvidiaErr
		case "Copilot":
			m, e = copilotModels, copilotErr
		}
		if e != nil {
			errors = append(errors, catalogErrorMessage(label, e))
		} else {
			models = append(models, m...)
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

func LoadModelCatalog(ctx context.Context, apiKeys types.ProviderAPIKeys, client types.HTTPDoer, store *ConfigStore) (*LoadedModelCatalog, error) {
	cache, _ := store.ReadModelCache()
	var cachedModels []types.SleepyRouterModel
	if cache != nil {
		cachedModels = modelsForConfiguredProviders(cache.Models, apiKeys)
	}
	if cache != nil && IsModelCacheFresh(*cache, time.Now()) && len(cachedModels) > 0 {
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
