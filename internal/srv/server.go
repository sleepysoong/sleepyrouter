package srv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/protocol"
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
	requestID                                                           int
	requestMethod, requestPath                                          string
	requestedModel, routedModel, routeReason, lastError, logGroup       string
	stream                                                              bool
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
		status := 500
		msg := err.Error()
		if he, ok := err.(*httpError); ok {
			status = he.StatusCode
			msg = he.Message
		}
		writeJSONError(w, status, msg)
		return nil, false
	}
	selected, err := selectedModelSelection(ctx, store, apiKeys, client)
	if err != nil {
		writeJSONError(w, 500, err.Error())
		return nil, false
	}
	if err := assertSelectedFree(selected.Models); err != nil {
		status := 500
		msg := err.Error()
		if he, ok := err.(*httpError); ok {
			status = he.StatusCode
			msg = he.Message
		}
		writeJSONError(w, status, msg)
		return nil, false
	}
	routingModel := requestedModelForRouting(selected.Models, body["model"])
	candidates, candidateReason := routing.OrderedCandidates(selected.ModelGroups, routingModel, selected.DefaultModelGroup, selected.GroupOrder...)
	logGroup := selected.DefaultModelGroup
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
	body := pre.body
	tryModelCandidates(ctx, pre, w, st, requestLogger, nil, func(ctx context.Context, w http.ResponseWriter, model types.SleepyRouterModel, apiKey string, p providers.Provider, triedCount int) (bool, string) {
		modelID := model.ID
		upstreamBody := withUpstreamModel(body, model, st.stream)
		attemptStart := time.Now()
		upstream, upstreamErr := p.ChatCompletion(ctx, apiKey, upstreamBody, client)
		logUpstreamAttempt(requestLogger, st, modelID, upstream, upstreamErr, attemptStart)
		if upstreamErr != nil {
			return false, upstreamErr.Error()
		}
		if utils.IsOK(upstream) {
			if st.stream {
				st.lastInputTokens, st.lastOutputTokens, st.logTriedCount = writeStreamResponse(w, upstream, store, model, triedCount)
				return true, ""
			}
			data, err := utils.ResponseJSON(upstream)
			if err != nil {
				return false, err.Error()
			}
			choices, _ := data["choices"].([]any)
			if len(choices) == 0 {
				usageID := model.UsageID
				if usageID == "" {
					usageID = model.ID
				}
				_ = store.AppendUsage(types.UsageLogEntry{TS: time.Now().UTC().Format(time.RFC3339), Model: usageID, InputTokens: 0, OutputTokens: 0, Success: false})
				return false, fmt.Sprintf("choices가 비어있어요 (%d)", upstream.StatusCode)
			}
			in, out, _ := usageFromResponse(data)
			st.lastInputTokens = in
			st.lastOutputTokens = out
			recordSuccessfulUsage(store, model, data)
			t := triedCount
			st.logTriedCount = &t
			writeJSON(w, upstream.StatusCode, data)
			return true, ""
		}
		return false, recordUpstreamFailure(store, model, upstream)
	})
}

// handleAnthropicMessage iterates model candidates for POST /anthropic/v1/messages.
func handleAnthropicMessage(ctx context.Context, store *cfg.ConfigStore, pre *handlerPreamble, client types.HTTPDoer, w http.ResponseWriter, st *handlerState, requestLogger func(ServerLogEvent)) {
	body := pre.body
	tryModelCandidates(ctx, pre, w, st, requestLogger, map[string]any{"type": "api_error"}, func(ctx context.Context, w http.ResponseWriter, model types.SleepyRouterModel, apiKey string, p providers.Provider, triedCount int) (bool, string) {
		modelID := model.ID
		var upstream *http.Response
		var upstreamErr error
		if p.MessageProtocol() == providers.ProtocolAnthropic {
			upstreamBody := withUpstreamModel(body, model, st.stream)
			attemptStart := time.Now()
			upstream, upstreamErr = p.Messages(ctx, apiKey, upstreamBody, client)
			logUpstreamAttempt(requestLogger, st, modelID, upstream, upstreamErr, attemptStart)
			if upstreamErr == nil && !utils.IsOK(upstream) && (upstream.StatusCode == 404 || upstream.StatusCode == 405) {
				fallbackBody := protocol.AnthropicToOpenAI(body, modelUpstreamID(model))
				if st.stream {
					fallbackBody["stream_options"] = map[string]any{"include_usage": true}
				}
				attemptStart := time.Now()
				upstream, upstreamErr = p.ChatCompletion(ctx, apiKey, fallbackBody, client)
				logUpstreamAttempt(requestLogger, st, modelID, upstream, upstreamErr, attemptStart)
				if upstreamErr == nil && utils.IsOK(upstream) {
					t := triedCount
					st.logTriedCount = &t
					if st.stream {
						recordSuccessfulUsage(store, model, nil)
						PipeOpenAIStreamAsAnthropic(upstream.Body, w, modelID)
					} else {
						data, err := utils.ResponseJSON(upstream)
						if err != nil {
							return false, err.Error()
						}
						in, out, _ := usageFromResponse(data)
						st.lastInputTokens = in
						st.lastOutputTokens = out
						recordSuccessfulUsage(store, model, data)
						writeJSON(w, upstream.StatusCode, protocol.OpenAIToAnthropic(data, modelID))
					}
					return true, ""
				}
			}
			if upstreamErr != nil {
				return false, upstreamErr.Error()
			}
			if utils.IsOK(upstream) {
				if st.stream {
					st.lastInputTokens, st.lastOutputTokens, st.logTriedCount = writeStreamResponse(w, upstream, store, model, triedCount)
					return true, ""
				}
				data, err := utils.ResponseJSON(upstream)
				if err != nil {
					return false, err.Error()
				}
				_, hasChoicesArr := data["choices"].([]any)
				_, hasContentArr := data["content"].([]any)
				empty := !hasChoicesArr && !hasContentArr
				if empty {
					usageID := model.UsageID
					if usageID == "" {
						usageID = model.ID
					}
					_ = store.AppendUsage(types.UsageLogEntry{TS: time.Now().UTC().Format(time.RFC3339), Model: usageID, InputTokens: 0, OutputTokens: 0, Success: false})
					return false, fmt.Sprintf("choices와 content가 모두 비어있어요 (%d)", upstream.StatusCode)
				}
				in, out, _ := usageFromResponse(data)
				st.lastInputTokens = in
				st.lastOutputTokens = out
				recordSuccessfulUsage(store, model, data)
				t := triedCount
				st.logTriedCount = &t
				writeJSON(w, upstream.StatusCode, data)
				return true, ""
			}
		} else { // providers.ProtocolOpenAI
			fallbackBody := protocol.AnthropicToOpenAI(body, modelUpstreamID(model))
			attemptStart := time.Now()
			upstream, upstreamErr = p.ChatCompletion(ctx, apiKey, fallbackBody, client)
			logUpstreamAttempt(requestLogger, st, modelID, upstream, upstreamErr, attemptStart)
			if upstreamErr != nil {
				return false, upstreamErr.Error()
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
						return false, err.Error()
					}
					in, out, _ := usageFromResponse(data)
					st.lastInputTokens = in
					st.lastOutputTokens = out
					recordSuccessfulUsage(store, model, data)
					writeJSON(w, upstream.StatusCode, protocol.OpenAIToAnthropic(data, modelID))
				}
				return true, ""
			}
		}
		return false, recordUpstreamFailure(store, model, upstream)
	})
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

	mux := http.NewServeMux()
	registerRoutes(mux, routeDeps{store: store, env: env, client: client, requestLogger: requestLogger, startTime: startTime})

	nextID := new(int64)
	handler := withObservation(mux, nextID, requestLogger, startTime)
	return &http.Server{
		Handler:     handler,
		ReadTimeout: 60 * time.Second,
		IdleTimeout: 120 * time.Second,
	}
}

// withObservation wraps mux so that every request gets a fresh handlerState,
// a logRequest/logResponse pair, a responseRecorder (when logging is on),
// and a deferred panic recover. Route handlers retrieve the state through
// r.Context().
func withObservation(mux *http.ServeMux, nextID *int64, requestLogger func(ServerLogEvent), startTime time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := int(atomic.AddInt64(nextID, 1))
		startedAt := time.Now()

		st := &handlerState{
			requestID:     id,
			requestMethod: r.Method,
			requestPath:   r.URL.Path,
		}

		if requestLogger != nil {
			requestLogger(ServerLogEvent{
				Type:   "request",
				ID:     id,
				Method: r.Method,
				Path:   r.URL.Path,
			})
			if _, ok := w.(*responseRecorder); !ok {
				w = newResponseRecorder(w)
			}
		}
		defer func() {
			if requestLogger != nil {
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
		}()
		defer func() {
			if err := recover(); err != nil {
				statusCode := 500
				if he, ok := err.(*httpError); ok {
					statusCode = he.StatusCode
				}
				msg := errorString(err)
				writeJSONError(w, statusCode, msg, map[string]any{"request": r.Method + " " + r.URL.String()})
			}
		}()

		mux.ServeHTTP(w, r.WithContext(withState(r.Context(), st)))
	})
}

func Listen(server *http.Server, port int) (int, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, err
	}
	go func() { _ = server.Serve(ln) }()
	if tcpAddr, ok := ln.Addr().(*net.TCPAddr); ok {
		// ponytail: constants defined in RFCs
		return tcpAddr.Port, nil
	}
	return 0, fmt.Errorf("listen address is not a TCP address: %v", ln.Addr())
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
