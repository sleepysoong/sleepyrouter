package srv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/providers"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// handlerPreamble holds the shared state extracted from the preamble of
// both POST /v1/chat/completions and POST /anthropic/v1/messages handlers.
type handlerPreamble struct {
	apiKeys         types.ProviderAPIKeys
	body            map[string]any
	selected        *selectedModelsResult
	candidates      []string
	candidateReason routing.RouteReason
	routingModel    string
	logGroup        string
}

// handlerState carries mutable logging state across handler phases so that
// extracted handler functions and the deferred logResponse closure stay in sync.
type handlerState struct {
	requestedModel, routedModel, routeReason, lastError, logGroup string
	stream                                                         bool
	lastInputTokens, lastOutputTokens, logCandidateCount, logTriedCount *int
}

// readHandlerPreamble reads the request body, selects models, and computes
// route candidates. On error it writes the response to w and returns nil, false.
// Otherwise returns the preamble and true — the caller should proceed.
func readHandlerPreamble(ctx context.Context, store *cfg.ConfigStore, env utils.Environment, client types.HTTPDoer, w http.ResponseWriter, r *http.Request) (*handlerPreamble, bool) {
	apiKeys, err := cfg.RequireAnyProviderAPIKey(env, store.Paths.Root)
	if err != nil {
		writeJSONError(w, 500, err.Error())
		return nil, false
	}
	body, err := readBody(r)
	if err != nil {
		panic(err)
	}
	selected, err := selectedModelSelection(ctx, store, apiKeys, client)
	if err != nil {
		writeJSONError(w, 500, err.Error())
		return nil, false
	}
	if err := assertSelectedFree(selected.Models); err != nil {
		he := err.(*httpError)
		writeJSONError(w, he.StatusCode, he.Message)
		return nil, false
	}
	routingModel := requestedModelForRouting(selected.Models, body["model"])
	candidates, candidateReason := routing.OrderedCandidates(selected.ModelGroups, routingModel, selected.DefaultGroup, selected.GroupOrder...)
	logGroup := selected.DefaultGroup
	if candidateReason == routing.RouteModelGroup {
		logGroup = routing.NormalizeModelGroupName(routingModel)
	}
	return &handlerPreamble{
		apiKeys:         apiKeys,
		body:            body,
		selected:        selected,
		candidates:      candidates,
		candidateReason: candidateReason,
		routingModel:    routingModel,
		logGroup:        logGroup,
	}, true
}

// handleChatCompletion iterates model candidates for POST /v1/chat/completions.
// It updates st with the result of the attempt and returns; the calling
// handler's deferred logResponse will pick up the final values.
func handleChatCompletion(ctx context.Context, store *cfg.ConfigStore, pre *handlerPreamble, client types.HTTPDoer, w http.ResponseWriter, st *handlerState, requestLogger func(ServerLogEvent)) {
	apiKeys := pre.apiKeys
	body := pre.body
	selected := pre.selected
	Candidates := pre.candidates
	candidateReason := pre.candidateReason

	var upstreamError string
	triedAny := false
	triedCount := 0
	for _, modelID := range Candidates {
		model, ok := selected.ByID[modelID]
		if !ok {
			continue
		}
		apiKey := apiKeys.For(types.SourceOf(model))
		if apiKey == "" {
			upstreamError = missingKeyMessage(model)
			st.lastError = upstreamError
			continue
		}
		if requestLogger != nil {
			st.routedModel = modelID
			st.routeReason = string(candidateReason)
		}
		triedAny = true
		triedCount++
		upstreamBody := withUpstreamModel(body, model, st.stream)
		source := types.SourceOf(model)
		p := providers.GetProvider(source)
		if p == nil {
			upstreamError = fmt.Sprintf("unsupported provider: %s", source)
			st.lastError = upstreamError
			continue
		}
		upstream, upstreamErr := p.ChatCompletion(ctx, apiKey, upstreamBody, client)
		if upstreamErr != nil {
			upstreamError = upstreamErr.Error()
			st.lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
			continue
		}
		if utils.IsOK(upstream) {
			if st.stream {
				st.lastInputTokens, st.lastOutputTokens, st.logTriedCount = writeStreamResponse(w, upstream, store, model, triedCount)
				return
			}
			data, err := utils.ResponseJSON(upstream)
			if err != nil {
				upstreamError = err.Error()
				st.lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
				continue
			}
			choices, _ := data["choices"].([]any)
			if len(choices) == 0 {
				upstreamError = fmt.Sprintf("choices가 비어있어요 (%d)", upstream.StatusCode)
				st.lastError = fmt.Sprintf("[%s] choices가 비어있어요", modelID)
				usageID := model.UsageID
				if usageID == "" {
					usageID = model.ID
				}
				_ = store.AppendUsage(types.UsageLogEntry{TS: time.Now().UTC().Format(time.RFC3339), Model: usageID, InputTokens: 0, OutputTokens: 0, Success: false})
				continue
			}
			in, out, _ := usageFromResponse(data)
			st.lastInputTokens = in
			st.lastOutputTokens = out
			recordSuccessfulUsage(store, model, data)
			t := triedCount
			st.logTriedCount = &t
			writeJSON(w, upstream.StatusCode, data)
			return
		}
		upstreamError = recordUpstreamFailure(store, model, upstream)
		st.lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
	}
	if !triedAny {
		noUsableModelResponse(w, upstreamError)
		return
	}
	writeJSONError(w, 502, "선택된 모든 무료 모델이 실패했어요.", map[string]any{"details": upstreamError})
}

// handleAnthropicMessage iterates model candidates for POST /anthropic/v1/messages.
func handleAnthropicMessage(ctx context.Context, store *cfg.ConfigStore, pre *handlerPreamble, client types.HTTPDoer, w http.ResponseWriter, st *handlerState, requestLogger func(ServerLogEvent)) {
	apiKeys := pre.apiKeys
	body := pre.body
	selected := pre.selected
	Candidates := pre.candidates
	candidateReason := pre.candidateReason

	var upstreamError string
	triedAny := false
	triedCount := 0
	for _, modelID := range Candidates {
		model, ok := selected.ByID[modelID]
		if !ok {
			continue
		}
		apiKey := apiKeys.For(types.SourceOf(model))
		if apiKey == "" {
			upstreamError = missingKeyMessage(model)
			st.lastError = upstreamError
			continue
		}
		if requestLogger != nil {
			st.routedModel = modelID
			st.routeReason = string(candidateReason)
		}
		triedAny = true
		triedCount++
		source := types.SourceOf(model)
		p := providers.GetProvider(source)
		if p == nil {
			upstreamError = fmt.Sprintf("unsupported provider: %s", source)
			st.lastError = upstreamError
			continue
		}

		var upstream *http.Response
		var upstreamErr error

		if p.MessageProtocol() == providers.ProtocolAnthropic {
			upstreamBody := withUpstreamModel(body, model, st.stream)
			upstream, upstreamErr = p.Messages(ctx, apiKey, upstreamBody, client)
			if upstreamErr == nil && !utils.IsOK(upstream) && (upstream.StatusCode == 404 || upstream.StatusCode == 405) {
				fallbackBody := AnthropicToOpenAI(body, modelUpstreamID(model))
				if st.stream {
					fallbackBody["stream_options"] = map[string]any{"include_usage": true}
				}
				upstream, upstreamErr = p.ChatCompletion(ctx, apiKey, fallbackBody, client)
				if upstreamErr == nil && utils.IsOK(upstream) {
					t := triedCount
					st.logTriedCount = &t
					if st.stream {
						recordSuccessfulUsage(store, model, nil)
						PipeOpenAIStreamAsAnthropic(upstream.Body, w, modelID)
					} else {
						data, err := utils.ResponseJSON(upstream)
						if err != nil {
							upstreamError = err.Error()
							st.lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
							continue
						}
						in, out, _ := usageFromResponse(data)
						st.lastInputTokens = in
						st.lastOutputTokens = out
						recordSuccessfulUsage(store, model, data)
						writeJSON(w, upstream.StatusCode, OpenAIToAnthropic(data, modelID))
					}
					return
				}
			}
			if upstreamErr != nil {
				upstreamError = upstreamErr.Error()
				st.lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
				continue
			}
			if utils.IsOK(upstream) {
				if st.stream {
					st.lastInputTokens, st.lastOutputTokens, st.logTriedCount = writeStreamResponse(w, upstream, store, model, triedCount)
					return
				}
				data, err := utils.ResponseJSON(upstream)
				if err != nil {
					upstreamError = err.Error()
					st.lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
					continue
				}
				_, hasChoicesArr := data["choices"].([]any)
				_, hasContentArr := data["content"].([]any)
				empty := !hasChoicesArr && !hasContentArr
				if empty {
					upstreamError = fmt.Sprintf("choices와 content가 모두 비어있어요 (%d)", upstream.StatusCode)
					st.lastError = fmt.Sprintf("[%s] choices와 content가 모두 비어있어요", modelID)
					usageID := model.UsageID
					if usageID == "" {
						usageID = model.ID
					}
					_ = store.AppendUsage(types.UsageLogEntry{TS: time.Now().UTC().Format(time.RFC3339), Model: usageID, InputTokens: 0, OutputTokens: 0, Success: false})
					continue
				}
				in, out, _ := usageFromResponse(data)
				st.lastInputTokens = in
				st.lastOutputTokens = out
				recordSuccessfulUsage(store, model, data)
				t := triedCount
				st.logTriedCount = &t
				writeJSON(w, upstream.StatusCode, data)
				return
			}
		} else { // providers.ProtocolOpenAI
			fallbackBody := AnthropicToOpenAI(body, modelUpstreamID(model))
			upstream, upstreamErr = p.ChatCompletion(ctx, apiKey, fallbackBody, client)
			if upstreamErr != nil {
				upstreamError = upstreamErr.Error()
				st.lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
				continue
			}
			if utils.IsOK(upstream) {
				t := triedCount
				st.logTriedCount = &t
				if st.stream {
					recordSuccessfulUsage(store, model, nil)
					PipeOpenAIStreamAsAnthropic(upstream.Body, w, modelID)
				} else {
					data, err := utils.ResponseJSON(upstream)
					if err != nil {
						upstreamError = err.Error()
						st.lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
						continue
					}
					in, out, _ := usageFromResponse(data)
					st.lastInputTokens = in
					st.lastOutputTokens = out
					recordSuccessfulUsage(store, model, data)
					writeJSON(w, upstream.StatusCode, OpenAIToAnthropic(data, modelID))
				}
				return
			}
		}
		upstreamError = recordUpstreamFailure(store, model, upstream)
		st.lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
	}
	if !triedAny {
		noUsableModelResponse(w, upstreamError)
		return
	}
	writeJSONError(w, 502, "선택된 모든 무료 모델이 실패했어요.", map[string]any{"type": "api_error", "details": upstreamError})
}

func CreateSleepyRouterServer(options ServerOptions) *http.Server {
	store := options.Store
	if store == nil {
		store = cfg.NewConfigStore("")
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

		st := &handlerState{}

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
				RequestedModel: st.requestedModel,
				ModelID:        st.routedModel,
				RouteReason:    st.routeReason,
				Stream:         st.stream,
				InputTokens:    st.lastInputTokens,
				OutputTokens:   st.lastOutputTokens,
				Error:          st.lastError,
				Group:          st.logGroup,
				CandidateCount: st.logCandidateCount,
				TriedCount:     st.logTriedCount,
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
			writeJSONError(w, statusCode, msg, map[string]any{"request": method + " " + r.URL.String()})
			}
		}()

		// GET /health
		if method == "GET" && path == "/health" {
			writeJSON(w, 200, map[string]any{"ok": true, "service": "sleepyrouter", "version": types.Version, "uptime": int(time.Since(startTime).Seconds())})
			return
		}

		// GET /v1/models
		if method == "GET" && path == "/v1/models" {
			apiKeys, err := cfg.RequireAnyProviderAPIKey(env, store.Paths.Root)
			if err != nil {
				writeJSONError(w, 500, err.Error())
				return
			}
			selected, err := selectedModelSelection(ctx, store, apiKeys, client)
			if err != nil {
				writeJSONError(w, 500, err.Error())
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
			st.requestedModel = utils.StringFromUnknown(body["model"])
			writeJSON(w, 200, map[string]any{"input_tokens": estimateInputTokens(body)})
			return
		}

		// POST /v1/chat/completions
		if method == "POST" && path == "/v1/chat/completions" {
			pre, ok := readHandlerPreamble(ctx, store, env, client, w, r)
			if !ok {
				return
			}
			st.requestedModel = utils.StringFromUnknown(pre.body["model"])
			st.stream = utils.BoolValue(pre.body["stream"])
			st.logGroup = pre.logGroup
			candCount := len(pre.candidates)
			st.logCandidateCount = &candCount
			handleChatCompletion(ctx, store, pre, client, w, st, requestLogger)
			return
		}

		// POST /anthropic/v1/messages or /anthropic/messages
		if method == "POST" && (path == "/anthropic/v1/messages" || path == "/anthropic/messages") {
			pre, ok := readHandlerPreamble(ctx, store, env, client, w, r)
			if !ok {
				return
			}
			st.requestedModel = utils.StringFromUnknown(pre.body["model"])
			st.stream = utils.BoolValue(pre.body["stream"])
			st.logGroup = pre.logGroup
			candCount := len(pre.candidates)
			st.logCandidateCount = &candCount
			handleAnthropicMessage(ctx, store, pre, client, w, st, requestLogger)
			return
		}

		writeJSONError(w, 404, fmt.Sprintf("지원하지 않는 엔드포인트예요: %s %s. 사용 가능한 엔드포인트: GET /health, GET /v1/models, POST /v1/chat/completions, POST /anthropic/v1/messages", method, path))
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
