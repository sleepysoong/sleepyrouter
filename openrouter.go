package sleepyrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const openRouterModelsURL = "https://openrouter.ai/api/v1/models"
const openRouterChatCompletionsURL = "https://openrouter.ai/api/v1/chat/completions"
const openRouterAnthropicMessagesURL = "https://openrouter.ai/api/v1/messages"

type OpenRouterArchitecture struct {
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Modality         string   `json:"modality"`
}

type OpenRouterModel struct {
	ID                  string                 `json:"id"`
	Name                string                 `json:"name"`
	CanonicalSlug       string                 `json:"canonical_slug"`
	Created             any                    `json:"created"`
	ContextLength       any                    `json:"context_length"`
	Pricing             map[string]any         `json:"pricing"`
	Architecture        OpenRouterArchitecture `json:"architecture"`
	SupportedParameters []string               `json:"supported_parameters"`
	Raw                 json.RawMessage        `json:"-"`
}

func InferProvider(modelID string) string {
	if before, _, found := strings.Cut(modelID, "/"); found && before != "" {
		return before
	}
	return "openrouter"
}

func priceIsZero(value any) bool {
	if value == nil || value == "" {
		return false
	}
	switch value := value.(type) {
	case float64:
		return value == 0
	case int:
		return value == 0
	case string:
		number, err := strconv.ParseFloat(value, 64)
		return err == nil && number == 0
	default:
		return false
	}
}

func isTextOutput(model OpenRouterModel) bool {
	return len(model.Architecture.OutputModalities) == 0 || containsString(model.Architecture.OutputModalities, "text")
}

func IsFreeOpenRouterModel(model OpenRouterModel) bool {
	if model.ID == "" || !isTextOutput(model) {
		return false
	}
	if strings.HasSuffix(model.ID, ":free") {
		return true
	}
	pricing := model.Pricing
	if pricing == nil {
		pricing = map[string]any{}
	}
	requestPrice, exists := pricing["request"]
	if !exists {
		requestPrice = float64(0)
	}
	return priceIsZero(pricing["prompt"]) && priceIsZero(pricing["completion"]) && priceIsZero(requestPrice)
}

func openRouterUsageID(id string) string {
	parts := strings.Split(id, "/")
	modelName := id
	if len(parts) >= 2 {
		modelName = strings.Join(parts[1:], "/")
	}
	return "openrouter/" + strings.TrimSuffix(modelName, ":free")
}

func NormalizeOpenRouterModel(model OpenRouterModel, popularityRank *int, catalog ProviderMetadataCatalog) OmfmModel {
	rawID := model.ID
	if rawID == "" {
		rawID = model.CanonicalSlug
	}
	if rawID == "" {
		rawID = "unknown"
	}
	metadata, _ := ModelMetadata(SourceOpenRouter, rawID, catalog)
	contextLength := ParseTokenCount(model.ContextLength)
	if contextLength == nil {
		contextLength = metadata.ContextLength
	}
	raw := model.Raw
	if len(raw) == 0 {
		raw, _ = marshalJSON(model)
	}
	name := model.Name
	if name == "" {
		name = rawID
	}
	return OmfmModel{
		ID:                  "openrouter/" + rawID,
		UpstreamID:          rawID,
		Name:                name,
		Provider:            InferProvider(rawID),
		Source:              SourceOpenRouter,
		UsageID:             openRouterUsageID(rawID),
		ContextLength:       contextLength,
		PopularityRank:      popularityRank,
		SupportedParameters: append([]string(nil), model.SupportedParameters...),
		Raw:                 raw,
	}
}

func fetchOpenRouterModels(ctx context.Context, apiKey, category string, client HTTPDoer) ([]OpenRouterModel, error) {
	endpoint, err := url.Parse(openRouterModelsURL)
	if err != nil {
		return nil, err
	}
	if category != "" {
		query := endpoint.Query()
		query.Set("category", category)
		endpoint.RawQuery = query.Encode()
	}
	req, err := getRequest(ctx, endpoint.String(), map[string]string{
		"Authorization": "Bearer " + apiKey,
		"User-Agent":    "sleepyrouter/" + Version,
	})
	if err != nil {
		return nil, err
	}
	response, err := httpClient(client).Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if !isOK(response) {
		suffix := ""
		if category != "" {
			suffix = "?category=" + category
		}
		return nil, fmt.Errorf("OpenRouter 모델 목록 요청 실패: %d %s (GET /api/v1/models%s)", response.StatusCode, statusText(response), suffix)
	}
	var body struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, err
	}
	models := make([]OpenRouterModel, 0, len(body.Data))
	for _, raw := range body.Data {
		var model OpenRouterModel
		if err := json.Unmarshal(raw, &model); err != nil {
			continue
		}
		model.Raw = append(json.RawMessage(nil), raw...)
		models = append(models, model)
	}
	return models, nil
}

func ListOpenRouterFreeModels(ctx context.Context, apiKey string, client HTTPDoer) ([]OmfmModel, error) {
	var allModels []OpenRouterModel
	var programmingModels []OpenRouterModel
	var allError error
	var metadata ProviderMetadataCatalog
	var wait sync.WaitGroup
	wait.Add(3)
	go func() {
		defer wait.Done()
		allModels, allError = fetchOpenRouterModels(ctx, apiKey, "", client)
	}()
	go func() {
		defer wait.Done()
		programmingModels, _ = fetchOpenRouterModels(ctx, apiKey, "programming", client)
	}()
	go func() {
		defer wait.Done()
		metadata = LoadModelMetadataCatalog(ctx, client)
	}()
	wait.Wait()
	if allError != nil {
		return nil, allError
	}
	popularityByID := make(map[string]int, len(programmingModels))
	for index, model := range programmingModels {
		if model.ID != "" {
			popularityByID[model.ID] = index
		}
	}
	models := make([]OmfmModel, 0)
	for index, model := range allModels {
		if !IsFreeOpenRouterModel(model) {
			continue
		}
		rank, found := popularityByID[model.ID]
		if !found {
			rank = len(programmingModels) + index
		}
		models = append(models, NormalizeOpenRouterModel(model, intPointer(rank), metadata))
	}
	sort.Slice(models, func(i, j int) bool {
		left, right := models[i], models[j]
		leftRank, rightRank := int(^uint(0)>>1), int(^uint(0)>>1)
		if left.PopularityRank != nil {
			leftRank = *left.PopularityRank
		}
		if right.PopularityRank != nil {
			rightRank = *right.PopularityRank
		}
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		return left.ID < right.ID
	})
	return models, nil
}

func PostOpenRouterChatCompletion(ctx context.Context, apiKey string, body any, client HTTPDoer) (*http.Response, error) {
	req, err := jsonRequest(ctx, http.MethodPost, openRouterChatCompletionsURL, map[string]string{
		"Authorization":      "Bearer " + apiKey,
		"Content-Type":       "application/json",
		"HTTP-Referer":       "https://github.com/hakilee/sleepyrouter",
		"X-OpenRouter-Title": "sleepyrouter",
	}, body)
	if err != nil {
		return nil, err
	}
	return httpClient(client).Do(req)
}

func PostOpenRouterAnthropicMessage(ctx context.Context, apiKey string, body any, client HTTPDoer) (*http.Response, error) {
	req, err := jsonRequest(ctx, http.MethodPost, openRouterAnthropicMessagesURL, map[string]string{
		"Authorization":      "Bearer " + apiKey,
		"Content-Type":       "application/json",
		"anthropic-version":  "2023-06-01",
		"HTTP-Referer":       "https://github.com/hakilee/sleepyrouter",
		"X-OpenRouter-Title": "sleepyrouter",
	}, body)
	if err != nil {
		return nil, err
	}
	return httpClient(client).Do(req)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
