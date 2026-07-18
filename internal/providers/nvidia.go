package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
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
	id := utils.StringFromUnknown(model["id"])
	name := utils.StringFromUnknown(model["name"])
	typeName := utils.StringFromUnknown(model["type"])
	task := utils.StringFromUnknown(model["task"])
	tags := strings.Join(utils.StringSliceFromUnknown(model["tags"]), " ")
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

func NormalizeNVIDIAModel(model map[string]any, catalog ProviderMetadataCatalog) types.SleepyRouterModel {
	upstreamID := utils.StringFromUnknown(model["id"])
	if upstreamID == "" {
		upstreamID = "unknown"
	}
	metadata, _ := ModelMetadata(types.SourceNVIDIA, upstreamID, catalog)
	name := utils.StringFromUnknown(model["name"])
	if name == "" {
		name = metadata.Name
	}
	if name == "" {
		name = titleFromID(upstreamID)
	}
	raw, _ := utils.MarshalJSONHelper(model)
	contextLength := utils.ExtractContextLengthFromRecord(model)
	if contextLength == nil {
		contextLength = metadata.ContextLength
	}
	return types.SleepyRouterModel{
		ID:            "nvidia/" + upstreamID,
		UpstreamID:    upstreamID,
		Name:          name,
		Provider:      "nvidia",
		Source:        types.SourceNVIDIA,
		UsageID:       nvidiaUsageID(upstreamID),
		ContextLength: contextLength,
		Raw:           raw,
	}
}

func ListNVIDIAFreeModels(ctx context.Context, apiKey string, client types.HTTPDoer) ([]types.SleepyRouterModel, error) {
	metadataChannel := make(chan ProviderMetadataCatalog, 1)
	go func() { metadataChannel <- LoadModelMetadataCatalog(ctx, client) }()
	req, err := utils.GetRequest(ctx, nvidiaModelsURL, map[string]string{
		"Authorization": "Bearer " + apiKey,
		"User-Agent":    "sleepyrouter/" + types.Version,
	})
	if err != nil {
		return nil, err
	}
	response, err := utils.HTTPClient(client).Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if !utils.IsOK(response) {
		return nil, fmt.Errorf("NVIDIA 모델 목록 요청 실패: %d %s (GET /v1/models)", response.StatusCode, utils.StatusText(response))
	}
	var raw any
	if err := json.NewDecoder(response.Body).Decode(&raw); err != nil {
		return nil, err
	}
	rows := []any{}
	if values, ok := utils.ArrayValue(raw); ok {
		rows = values
	} else if body, ok := utils.ObjectValue(raw); ok {
		rows, _ = utils.ArrayValue(body["data"])
	}
	metadata := <-metadataChannel
	models := make([]types.SleepyRouterModel, 0, len(rows))
	for _, row := range rows {
		model, ok := utils.ObjectValue(row)
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

func PostNVIDIAChatCompletion(ctx context.Context, apiKey string, body any, client types.HTTPDoer) (*http.Response, error) {
	req, err := utils.JSONRequest(ctx, http.MethodPost, NVIDIAChatCompletionsURL, map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}, body)
	if err != nil {
		return nil, err
	}
	return utils.HTTPClient(client).Do(req)
}

type NVIDIAProvider struct{}

func (p *NVIDIAProvider) Name() string {
	return "NVIDIA"
}

func (p *NVIDIAProvider) Source() types.ModelSource {
	return types.SourceNVIDIA
}

func (p *NVIDIAProvider) ListFreeModels(ctx context.Context, apiKey string, client types.HTTPDoer) ([]types.SleepyRouterModel, error) {
	return ListNVIDIAFreeModels(ctx, apiKey, client)
}

func (p *NVIDIAProvider) ChatCompletion(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return PostNVIDIAChatCompletion(ctx, apiKey, body, client)
}

func (p *NVIDIAProvider) Messages(ctx context.Context, apiKey string, body map[string]any, client types.HTTPDoer) (*http.Response, error) {
	return nil, fmt.Errorf("Messages not supported natively by NVIDIA provider")
}

func (p *NVIDIAProvider) MessageProtocol() MessageProtocol {
	return ProtocolOpenAI
}

func init() {
	RegisterProvider(types.SourceNVIDIA, &NVIDIAProvider{})
}
