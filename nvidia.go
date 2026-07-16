package sleepyrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

const NVIDIAChatCompletionsURL = "https://integrate.api.nvidia.com/v1/chat/completions"
const nvidiaModelsURL = "https://integrate.api.nvidia.com/v1/models"

var nvidiaNonChatPattern = regexp.MustCompile(`(?i)(?:^|[/_-])bge|embed|embedding|rerank|rank|reward|ocr|video|audio|speech|voice|speaker|detector|detection|translate|translation|guard|safety|retriever`)
var nvidiaAllowedTaskPattern = regexp.MustCompile(`(?i)chat|generate|completion|instruct`)

func titleFromID(id string) string {
	parts := strings.Split(id, "/")
	name := parts[len(parts)-1]
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	for index, word := range words {
		if word == "" {
			continue
		}
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func IsChatLikeNVIDIAModel(model map[string]any) bool {
	id := stringFromUnknown(model["id"])
	name := stringFromUnknown(model["name"])
	typeName := stringFromUnknown(model["type"])
	task := stringFromUnknown(model["task"])
	tags := strings.Join(stringSliceFromUnknown(model["tags"]), " ")
	haystack := strings.Join([]string{id, name, typeName, task, tags}, " ")
	if id == "" || nvidiaNonChatPattern.MatchString(haystack) {
		return false
	}
	return task == "" || nvidiaAllowedTaskPattern.MatchString(task)
}

func nvidiaUsageID(upstreamID string) string {
	parts := strings.Split(upstreamID, "/")
	modelName := upstreamID
	if len(parts) >= 2 {
		modelName = strings.Join(parts[1:], "/")
	}
	pattern := regexp.MustCompile(`-(\d{3,}b(?:-\w+)?)$`)
	return "nvidia/" + pattern.ReplaceAllString(modelName, "")
}

func NormalizeNVIDIAModel(model map[string]any, catalog ProviderMetadataCatalog) OmfmModel {
	upstreamID := stringFromUnknown(model["id"])
	if upstreamID == "" {
		upstreamID = "unknown"
	}
	metadata, _ := ModelMetadata(SourceNVIDIA, upstreamID, catalog)
	name := stringFromUnknown(model["name"])
	if name == "" {
		name = metadata.Name
	}
	if name == "" {
		name = titleFromID(upstreamID)
	}
	raw, _ := marshalJSON(model)
	contextLength := ExtractContextLengthFromRecord(model)
	if contextLength == nil {
		contextLength = metadata.ContextLength
	}
	return OmfmModel{
		ID:            "nvidia/" + upstreamID,
		UpstreamID:    upstreamID,
		Name:          name,
		Provider:      "nvidia",
		Source:        SourceNVIDIA,
		UsageID:       nvidiaUsageID(upstreamID),
		ContextLength: contextLength,
		Raw:           raw,
	}
}

func ListNVIDIAFreeModels(ctx context.Context, apiKey string, client HTTPDoer) ([]OmfmModel, error) {
	metadataChannel := make(chan ProviderMetadataCatalog, 1)
	go func() { metadataChannel <- LoadModelMetadataCatalog(ctx, client) }()
	req, err := getRequest(ctx, nvidiaModelsURL, map[string]string{
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
		return nil, fmt.Errorf("NVIDIA 모델 목록 요청 실패: %d %s (GET /v1/models)", response.StatusCode, statusText(response))
	}
	var raw any
	if err := json.NewDecoder(response.Body).Decode(&raw); err != nil {
		return nil, err
	}
	rows := []any{}
	if values, ok := arrayValue(raw); ok {
		rows = values
	} else if body, ok := objectValue(raw); ok {
		rows, _ = arrayValue(body["data"])
	}
	metadata := <-metadataChannel
	models := make([]OmfmModel, 0, len(rows))
	for _, row := range rows {
		model, ok := objectValue(row)
		if !ok || !IsChatLikeNVIDIAModel(model) {
			continue
		}
		models = append(models, NormalizeNVIDIAModel(model, metadata))
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Name != models[j].Name {
			return models[i].Name < models[j].Name
		}
		return models[i].ID < models[j].ID
	})
	return models, nil
}

func PostNVIDIAChatCompletion(ctx context.Context, apiKey string, body any, client HTTPDoer) (*http.Response, error) {
	req, err := jsonRequest(ctx, http.MethodPost, NVIDIAChatCompletionsURL, map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}, body)
	if err != nil {
		return nil, err
	}
	return httpClient(client).Do(req)
}
