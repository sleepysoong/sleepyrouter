package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

type ServerOptions struct {
	Store         *ConfigStore
	FetchImpl     types.HTTPDoer
	Env           utils.Environment
	RequestLogger func(ServerLogEvent)
	StartTime     time.Time
}

type ServerLogEvent struct {
	Type           string
	ID             int
	Method         string
	Path           string
	StatusCode     int
	DurationMs     int
	RequestedModel string
	ModelID        string
	RouteReason    string
	Stream         bool
	InputTokens    *int
	OutputTokens   *int
	Error          string
	Group          string
	CandidateCount *int
	TriedCount     *int
}

var controlCharPattern = sync.OnceValue(func() func(string) string {
	return func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if r < 0x20 || r == 0x7f {
				b.WriteByte('?')
			} else {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
})

func safeLogValue(value string) string {
	sanitized := controlCharPattern()(value)
	if len(sanitized) > 200 {
		return sanitized[:197] + "..."
	}
	return sanitized
}

func ansiColor(value string, code int, enabled bool) string {
	if enabled {
		return fmt.Sprintf("\x1b[%dm%s\x1b[0m", code, value)
	}
	return value
}

func statusColorCode(statusCode int) int {
	if statusCode >= 500 {
		return 31
	}
	if statusCode >= 400 {
		return 33
	}
	return 32
}

func FormatServerLogEvent(event ServerLogEvent, colorEnabled bool) string {
	c := colorEnabled
	if event.Type == "request" {
		return fmt.Sprintf("#%d | %s [%s] %s", event.ID, ansiColor("request", 36, c), ansiColor(event.Method, 35, c), safeLogValue(event.Path))
	}
	sc := statusColorCode(event.StatusCode)
	details := []string{
		fmt.Sprintf("#%d | %s [%s] %s [%s] %s", event.ID, ansiColor("response", sc, c), ansiColor(fmt.Sprintf("%d", event.StatusCode), sc, c), ansiColor(fmt.Sprintf("%dms", event.DurationMs), 90, c), ansiColor(event.Method, 35, c), safeLogValue(event.Path)),
	}
	if event.RequestedModel != "" {
		details = append(details, "requested="+safeLogValue(event.RequestedModel))
	}
	if event.ModelID != "" {
		details = append(details, "model="+safeLogValue(event.ModelID))
	}
	if event.RouteReason != "" {
		details = append(details, "route="+event.RouteReason)
	}
	if event.Group != "" {
		details = append(details, "group="+event.Group)
	}
	if event.CandidateCount != nil {
		details = append(details, fmt.Sprintf("candidates=%d", *event.CandidateCount))
	}
	if event.TriedCount != nil {
		details = append(details, fmt.Sprintf("tried=%d", *event.TriedCount))
	}
	if event.InputTokens != nil {
		details = append(details, fmt.Sprintf("in=%d", *event.InputTokens))
	}
	if event.OutputTokens != nil {
		details = append(details, fmt.Sprintf("out=%d", *event.OutputTokens))
	}
	if event.Stream {
		details = append(details, "stream=true")
	}
	if event.Error != "" {
		details = append(details, "error="+safeLogValue(event.Error))
	}
	return strings.Join(details, " ")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	data, _ := utils.MarshalJSONHelper(body)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(data)
}

type httpError struct {
	StatusCode int
	Message    string
}

func (e *httpError) Error() string { return e.Message }

func readBody(r *http.Request) (map[string]any, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	text := string(data)
	if text == "" {
		return map[string]any{}, nil
	}
	var body map[string]any
	if json.Unmarshal(data, &body) != nil {
		return nil, &httpError{StatusCode: 400, Message: fmt.Sprintf("요청 본문을 파싱할 수 없어요. 유효한 JSON을 보내주세요. (%d바이트 수신)", len(text))}
	}
	return body, nil
}

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

func isCachedFreeModel(model types.SleepyRouterModel) bool {
	source := types.SourceOf(model)
	if source == types.SourceNVIDIA || source == types.SourceCopilot {
		return true
	}
	if strings.HasSuffix(model.ID, ":free") {
		return true
	}
	if len(model.Raw) > 0 {
		var raw map[string]any
		if json.Unmarshal(model.Raw, &raw) == nil {
			return isFreeOpenRouterModelRaw(raw)
		}
	}
	return false
}

func isFreeOpenRouterModelRaw(raw map[string]any) bool {
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

type selectedModelsResult struct {
	Models       []types.SleepyRouterModel
	ByID         map[string]types.SleepyRouterModel
	IDs          []string
	ModelGroups  types.ModelGroups
	GroupOrder   []string
	DefaultGroup string
}

func selectedModelSelection(ctx context.Context, store *ConfigStore, apiKeys types.ProviderAPIKeys, client types.HTTPDoer) (*selectedModelsResult, error) {
	config, err := store.ReadConfig()
	if err != nil {
		return nil, err
	}
	catalog, err := LoadModelCatalog(ctx, apiKeys, client, store)
	if err != nil {
		return nil, err
	}
	var freeModels []types.SleepyRouterModel
	for _, m := range catalog.Models {
		if isCachedFreeModel(m) {
			freeModels = append(freeModels, m)
		}
	}
	allIDs := AllGroupModelIDs(config.ModelGroups, config.GroupOrder...)
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

func recordSuccessfulUsage(store *ConfigStore, model types.SleepyRouterModel, data map[string]any) {
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

func recordUpstreamFailure(store *ConfigStore, model types.SleepyRouterModel, response *http.Response) string {
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
	return maxInt(1, (len(text)+3)/4)
}

func CreateSleepyRouterServer(options ServerOptions) *http.Server {
	store := options.Store
	if store == nil {
		store = NewConfigStore("")
	}
	env := options.Env
	if env == nil {
		env = utils.CurrentEnvironment()
	}
	client := options.FetchImpl
	requestLogger := options.RequestLogger
	startTime := options.StartTime
	if startTime.IsZero() {
		startTime = time.Now()
	}
	nextID := new(int64)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := int(atomic.AddInt64(nextID, 1))
		startedAt := time.Now()

		var requestedModel, routedModel, routeReason, lastError, logGroup string
		var stream bool
		var lastInputTokens, lastOutputTokens *int
		var logCandidateCount, logTriedCount *int

		logRequest := func() {
			if requestLogger == nil {
				return
			}
			requestLogger(ServerLogEvent{
				Type:   "request",
				ID:     id,
				Method: r.Method,
				Path:   r.URL.Path,
			})
		}
		logResponse := func() {
			if requestLogger == nil {
				return
			}
			statusCode := 500
			if writer, ok := w.(*responseRecorder); ok {
				statusCode = writer.statusCode
			}
			requestLogger(ServerLogEvent{
				Type:           "response",
				ID:             id,
				Method:         r.Method,
				Path:           r.URL.Path,
				StatusCode:     statusCode,
				DurationMs:     int(time.Since(startedAt).Milliseconds()),
				RequestedModel: requestedModel,
				ModelID:        routedModel,
				RouteReason:    routeReason,
				Stream:         stream,
				InputTokens:    lastInputTokens,
				OutputTokens:   lastOutputTokens,
				Error:          lastError,
				Group:          logGroup,
				CandidateCount: logCandidateCount,
				TriedCount:     logTriedCount,
			})
		}

		logRequest()
		if requestLogger != nil {
			if _, ok := w.(*responseRecorder); !ok {
				w = newResponseRecorder(w)
			}
		}

		defer logResponse()

		method := r.Method
		path := r.URL.Path

		defer func() {
			if err := recover(); err != nil {
				statusCode := 500
				if he, ok := err.(*httpError); ok {
					statusCode = he.StatusCode
				}
				msg := errorString(err)
				writeJSON(w, statusCode, map[string]any{"error": map[string]any{"message": msg, "request": method + " " + r.URL.String()}})
			}
		}()

		// GET /health
		if method == "GET" && path == "/health" {
			writeJSON(w, 200, map[string]any{"ok": true, "service": "sleepyrouter", "version": types.Version, "uptime": int(time.Since(startTime).Seconds())})
			return
		}

		// GET /v1/models
		if method == "GET" && path == "/v1/models" {
			apiKeys, err := RequireAnyProviderAPIKey(env, store.Paths.Root)
			if err != nil {
				writeJSON(w, 500, map[string]any{"error": map[string]any{"message": err.Error()}})
				return
			}
			selected, err := selectedModelSelection(ctx, store, apiKeys, client)
			if err != nil {
				writeJSON(w, 500, map[string]any{"error": map[string]any{"message": err.Error()}})
				return
			}
			data := make([]map[string]any, 0, len(selected.Models))
			for _, model := range selected.Models {
				data = append(data, map[string]any{"id": model.ID, "object": "model", "created": 0, "owned_by": string(types.SourceOf(model)), "provider": model.Provider})
			}
			writeJSON(w, 200, map[string]any{"object": "list", "data": data})
			return
		}

		// POST /anthropic/v1/messages/count_tokens or /anthropic/messages/count_tokens
		if method == "POST" && (path == "/anthropic/v1/messages/count_tokens" || path == "/anthropic/messages/count_tokens") {
			body, err := readBody(r)
			if err != nil {
				panic(err)
			}
			requestedModel = utils.StringFromUnknown(body["model"])
			writeJSON(w, 200, map[string]any{"input_tokens": estimateInputTokens(body)})
			return
		}

		// POST /v1/chat/completions
		if method == "POST" && path == "/v1/chat/completions" {
			apiKeys, err := RequireAnyProviderAPIKey(env, store.Paths.Root)
			if err != nil {
				writeJSON(w, 500, map[string]any{"error": map[string]any{"message": err.Error()}})
				return
			}
			body, err := readBody(r)
			if err != nil {
				panic(err)
			}
			requestedModel = utils.StringFromUnknown(body["model"])
			stream = utils.BoolValue(body["stream"])
			selected, err := selectedModelSelection(ctx, store, apiKeys, client)
			if err != nil {
				writeJSON(w, 500, map[string]any{"error": map[string]any{"message": err.Error()}})
				return
			}
			if err := assertSelectedFree(selected.Models); err != nil {
				he := err.(*httpError)
				writeJSON(w, he.StatusCode, map[string]any{"error": map[string]any{"message": he.Message}})
				return
			}
			routingModel := requestedModelForRouting(selected.Models, body["model"])
			CandidateIDs := OrderedCandidates(selected.ModelGroups, routingModel, selected.DefaultGroup, selected.GroupOrder...)
			normalized := NormalizeModelGroupName(routingModel)
			logGroup = selected.DefaultGroup
			if normalized != "" {
				if _, ok := selected.ModelGroups[normalized]; ok {
					logGroup = normalized
				}
			}
			candCount := len(CandidateIDs)
			logCandidateCount = &candCount

			var upstreamError string
			triedAny := false
			triedCount := 0
			for _, modelID := range CandidateIDs {
				model, ok := selected.ByID[modelID]
				if !ok {
					continue
				}
				apiKey := apiKeys.For(types.SourceOf(model))
				if apiKey == "" {
					upstreamError = missingKeyMessage(model)
					lastError = upstreamError
					continue
				}
				if requestLogger != nil {
					routedModel = modelID
					routeReason = "fallback-order"
				}
				triedAny = true
				triedCount++
				upstreamBody := withUpstreamModel(body, model, stream)
				source := types.SourceOf(model)
				p := GetProvider(source)
				if p == nil {
					upstreamError = fmt.Sprintf("unsupported provider: %s", source)
					lastError = upstreamError
					continue
				}
				upstream, upstreamErr := p.ChatCompletion(ctx, apiKey, upstreamBody, stream, client)
				if upstreamErr != nil {
					upstreamError = upstreamErr.Error()
					lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
					continue
				}
				if utils.IsOK(upstream) {
					if stream {
						contentType := upstream.Header.Get("Content-Type")
						if contentType == "" {
							contentType = "text/event-stream; charset=utf-8"
						}
						w.Header().Set("Content-Type", contentType)
						w.WriteHeader(upstream.StatusCode)
						streamUsage := PipeWebStreamToNode(upstream.Body, w)
						lastInputTokens = streamUsage.InputTokens
						lastOutputTokens = streamUsage.OutputTokens
						usageID := model.UsageID
						if usageID == "" {
							usageID = model.ID
						}
						in := 0
						out := 0
						if streamUsage.InputTokens != nil {
							in = *streamUsage.InputTokens
						}
						if streamUsage.OutputTokens != nil {
							out = *streamUsage.OutputTokens
						}
						_ = store.AppendUsage(types.UsageLogEntry{TS: time.Now().UTC().Format(time.RFC3339), Model: usageID, InputTokens: in, OutputTokens: out, Success: true})
						t := triedCount
						logTriedCount = &t
						return
					}
					data, err := utils.ResponseJSON(upstream)
					if err != nil {
						upstreamError = err.Error()
						lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
						continue
					}
					choices, _ := data["choices"].([]any)
					if len(choices) == 0 {
						upstreamError = fmt.Sprintf("choices가 비어있어요 (%d)", upstream.StatusCode)
						lastError = fmt.Sprintf("[%s] choices가 비어있어요", modelID)
						usageID := model.UsageID
						if usageID == "" {
							usageID = model.ID
						}
						_ = store.AppendUsage(types.UsageLogEntry{TS: time.Now().UTC().Format(time.RFC3339), Model: usageID, InputTokens: 0, OutputTokens: 0, Success: false})
						continue
					}
					in, out, _ := usageFromResponse(data)
					lastInputTokens = in
					lastOutputTokens = out
					recordSuccessfulUsage(store, model, data)
					t := triedCount
					logTriedCount = &t
					writeJSON(w, upstream.StatusCode, data)
					return
				}
				upstreamError = recordUpstreamFailure(store, model, upstream)
				lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
			}
			if !triedAny {
				noUsableModelResponse(w, upstreamError)
				return
			}
			writeJSON(w, 502, map[string]any{"error": map[string]any{"message": "선택된 모든 무료 모델이 실패했어요.", "details": upstreamError}})
			return
		}

		// POST /anthropic/v1/messages or /anthropic/messages
		if method == "POST" && (path == "/anthropic/v1/messages" || path == "/anthropic/messages") {
			apiKeys, err := RequireAnyProviderAPIKey(env, store.Paths.Root)
			if err != nil {
				writeJSON(w, 500, map[string]any{"error": map[string]any{"message": err.Error()}})
				return
			}
			body, err := readBody(r)
			if err != nil {
				panic(err)
			}
			requestedModel = utils.StringFromUnknown(body["model"])
			stream = utils.BoolValue(body["stream"])
			selected, err := selectedModelSelection(ctx, store, apiKeys, client)
			if err != nil {
				writeJSON(w, 500, map[string]any{"error": map[string]any{"message": err.Error()}})
				return
			}
			if err := assertSelectedFree(selected.Models); err != nil {
				he := err.(*httpError)
				writeJSON(w, he.StatusCode, map[string]any{"error": map[string]any{"message": he.Message}})
				return
			}
			routingModel := requestedModelForRouting(selected.Models, body["model"])
			CandidateIDs := OrderedCandidates(selected.ModelGroups, routingModel, selected.DefaultGroup, selected.GroupOrder...)
			normalized := NormalizeModelGroupName(routingModel)
			logGroup = selected.DefaultGroup
			if normalized != "" {
				if _, ok := selected.ModelGroups[normalized]; ok {
					logGroup = normalized
				}
			}
			candCount := len(CandidateIDs)
			logCandidateCount = &candCount

			var upstreamError string
			triedAny := false
			triedCount := 0
			for _, modelID := range CandidateIDs {
				model, ok := selected.ByID[modelID]
				if !ok {
					continue
				}
				apiKey := apiKeys.For(types.SourceOf(model))
				if apiKey == "" {
					upstreamError = missingKeyMessage(model)
					lastError = upstreamError
					continue
				}
				if requestLogger != nil {
					routedModel = modelID
					routeReason = "fallback-order"
				}
				triedAny = true
				triedCount++
				source := types.SourceOf(model)
				p := GetProvider(source)
				if p == nil {
					upstreamError = fmt.Sprintf("unsupported provider: %s", source)
					lastError = upstreamError
					continue
				}

				var upstream *http.Response
				var upstreamErr error

				if p.MessageProtocol() == ProtocolAnthropic {
					upstreamBody := withUpstreamModel(body, model, stream)
					upstream, upstreamErr = p.Messages(ctx, apiKey, upstreamBody, stream, client)
					if upstreamErr == nil && !utils.IsOK(upstream) && (upstream.StatusCode == 404 || upstream.StatusCode == 405) {
						fallbackBody := AnthropicToOpenAI(body, modelUpstreamID(model))
						if stream {
							fallbackBody["stream_options"] = map[string]any{"include_usage": true}
						}
						upstream, upstreamErr = p.ChatCompletion(ctx, apiKey, fallbackBody, stream, client)
						if upstreamErr == nil && utils.IsOK(upstream) {
							t := triedCount
							logTriedCount = &t
							if stream {
								recordSuccessfulUsage(store, model, nil)
								PipeOpenAIStreamAsAnthropic(upstream.Body, w, modelID)
							} else {
								data, err := utils.ResponseJSON(upstream)
								if err != nil {
									upstreamError = err.Error()
									lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
									continue
								}
								in, out, _ := usageFromResponse(data)
								lastInputTokens = in
								lastOutputTokens = out
								recordSuccessfulUsage(store, model, data)
								writeJSON(w, upstream.StatusCode, OpenAIToAnthropic(data, modelID))
							}
							return
						}
					}
					if upstreamErr != nil {
						upstreamError = upstreamErr.Error()
						lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
						continue
					}
					if utils.IsOK(upstream) {
						if stream {
							contentType := upstream.Header.Get("Content-Type")
							if contentType == "" {
								contentType = "text/event-stream; charset=utf-8"
							}
							w.Header().Set("Content-Type", contentType)
							w.WriteHeader(upstream.StatusCode)
							streamUsage := PipeWebStreamToNode(upstream.Body, w)
							lastInputTokens = streamUsage.InputTokens
							lastOutputTokens = streamUsage.OutputTokens
							usageID := model.UsageID
							if usageID == "" {
								usageID = model.ID
							}
							in := 0
							out := 0
							if streamUsage.InputTokens != nil {
								in = *streamUsage.InputTokens
							}
							if streamUsage.OutputTokens != nil {
								out = *streamUsage.OutputTokens
							}
							_ = store.AppendUsage(types.UsageLogEntry{TS: time.Now().UTC().Format(time.RFC3339), Model: usageID, InputTokens: in, OutputTokens: out, Success: true})
							t := triedCount
							logTriedCount = &t
							return
						}
						data, err := utils.ResponseJSON(upstream)
						if err != nil {
							upstreamError = err.Error()
							lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
							continue
						}
						// empty choices/content check for Anthropic route
						_, hasChoicesArr := data["choices"].([]any)
						_, hasContentArr := data["content"].([]any)
						empty := !hasChoicesArr && !hasContentArr
						if empty {
							upstreamError = fmt.Sprintf("choices와 content가 모두 비어있어요 (%d)", upstream.StatusCode)
							lastError = fmt.Sprintf("[%s] choices와 content가 모두 비어있어요", modelID)
							usageID := model.UsageID
							if usageID == "" {
								usageID = model.ID
							}
							_ = store.AppendUsage(types.UsageLogEntry{TS: time.Now().UTC().Format(time.RFC3339), Model: usageID, InputTokens: 0, OutputTokens: 0, Success: false})
							continue
						}
						in, out, _ := usageFromResponse(data)
						lastInputTokens = in
						lastOutputTokens = out
						recordSuccessfulUsage(store, model, data)
						t := triedCount
						logTriedCount = &t
						writeJSON(w, upstream.StatusCode, data)
						return
					}
				} else { // ProtocolOpenAI
					fallbackBody := AnthropicToOpenAI(body, modelUpstreamID(model))
					upstream, upstreamErr = p.ChatCompletion(ctx, apiKey, fallbackBody, stream, client)
					if upstreamErr != nil {
						upstreamError = upstreamErr.Error()
						lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
						continue
					}
					if utils.IsOK(upstream) {
						t := triedCount
						logTriedCount = &t
						if stream {
							recordSuccessfulUsage(store, model, nil)
							PipeOpenAIStreamAsAnthropic(upstream.Body, w, modelID)
						} else {
							data, err := utils.ResponseJSON(upstream)
							if err != nil {
								upstreamError = err.Error()
								lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
								continue
							}
							in, out, _ := usageFromResponse(data)
							lastInputTokens = in
							lastOutputTokens = out
							recordSuccessfulUsage(store, model, data)
							writeJSON(w, upstream.StatusCode, OpenAIToAnthropic(data, modelID))
						}
						return
					}
				}
				upstreamError = recordUpstreamFailure(store, model, upstream)
				lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
			}
			if !triedAny {
				noUsableModelResponse(w, upstreamError)
				return
			}
			writeJSON(w, 502, map[string]any{"error": map[string]any{"type": "api_error", "message": "선택된 모든 무료 모델이 실패했어요.", "details": upstreamError}})
			return
		}

		writeJSON(w, 404, map[string]any{"error": map[string]any{"message": fmt.Sprintf("지원하지 않는 엔드포인트예요: %s %s. 사용 가능한 엔드포인트: GET /health, GET /v1/models, POST /v1/chat/completions, POST /anthropic/v1/messages", method, path)}})
	})

	return &http.Server{Handler: handler}
}

func Listen(server *http.Server, port int) (int, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, err
	}
	go server.Serve(ln)
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func errorString(err any) string {
	switch e := err.(type) {
	case error:
		return e.Error()
	case string:
		return e
	default:
		return fmt.Sprint(e)
	}
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	wrote      bool
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, statusCode: 200}
}

func (r *responseRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.statusCode = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
