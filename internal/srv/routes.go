package srv

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/handler"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// routeDeps bundles the per-server dependencies every route handler reads.
// One routeDeps is shared by all requests; per-request state lives in
// r.Context() via withState.
type routeDeps struct {
	store         *cfg.ConfigStore
	env           utils.Environment
	client        types.HTTPDoer
	requestLogger func(handler.ServerLogEvent)
	startTime     time.Time
}

// stateKey is the context key used to attach per-request HandlerState.
type stateKey struct{}

func withState(ctx context.Context, st *handler.HandlerState) context.Context {
	return context.WithValue(ctx, stateKey{}, st)
}

func stateFromContext(ctx context.Context) *handler.HandlerState {
	if v, ok := ctx.Value(stateKey{}).(*handler.HandlerState); ok {
		return v
	}
	return nil
}

// registerRoutes wires each endpoint onto mux. Adding a new endpoint is a
// single mux.HandleFunc line; the per-request observation wrappers
// (logging, recover, recorder) live in CreateSleepyRouterServer and apply
// to every route automatically.
func registerRoutes(mux *http.ServeMux, deps routeDeps) {
	mux.HandleFunc("GET /health", handleHealth(deps))
	mux.HandleFunc("GET /v1/models", handleModels(deps))
	mux.HandleFunc("POST /anthropic/v1/messages/count_tokens", handleCountTokens())
	mux.HandleFunc("POST /anthropic/messages/count_tokens", handleCountTokens())
	mux.HandleFunc("POST /v1/chat/completions", handleChatEndpoint(deps, ApiOpenAI))
	mux.HandleFunc("POST /anthropic/v1/messages", handleChatEndpoint(deps, ApiAnthropic))
	mux.HandleFunc("POST /anthropic/messages", handleChatEndpoint(deps, ApiAnthropic))
	mux.HandleFunc("/", handleNotFound())
}

func handleHealth(deps routeDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler.WriteJSON(w, 200, map[string]any{"ok": true, "service": "sleepyrouter", "version": types.Version, "uptime": int(time.Since(deps.startTime).Seconds())})
	}
}

func handleModels(deps routeDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKeys, err := cfg.RequireAnyProviderAPIKey(deps.env, deps.store.Paths.Root)
		if err != nil {
			handler.WriteJSONError(w, 500, err.Error())
			return
		}
		selected, err := handler.SelectedModelSelection(r.Context(), deps.store, apiKeys, deps.client)
		if err != nil {
			handler.WriteJSONError(w, 500, err.Error())
			return
		}
		data := make([]map[string]any, 0, len(selected.Models))
		for _, model := range selected.Models {
			data = append(data, map[string]any{"id": model.ID, "object": "model", "created": 0, "owned_by": string(types.SourceOf(model)), "provider": model.Provider})
		}
		handler.WriteJSON(w, 200, map[string]any{"object": "list", "data": data})
	}
}

func handleCountTokens() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := stateFromContext(r.Context())
		body, err := handler.ReadBody(r)
		if err != nil {
			status := 500
			msg := err.Error()
			if he, ok := err.(*handler.HTTPError); ok {
				status = he.StatusCode
				msg = he.Message
			}
			handler.WriteJSONError(w, status, msg)
			return
		}
		if st != nil {
			st.RequestedModel = utils.StringFromUnknown(body["model"])
		}
		handler.WriteJSON(w, 200, map[string]any{"input_tokens": handler.EstimateInputTokens(body)})
	}
}

func handleChatEndpoint(deps routeDeps, apiType ApiType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		st := stateFromContext(ctx)
		pre, ok := handler.ReadHandlerPreamble(ctx, deps.store, deps.env, deps.client, w, r)
		if !ok {
			return
		}
		if st != nil {
			st.RequestedModel = utils.StringFromUnknown(pre.Body["model"])
			st.Stream = utils.BoolValue(pre.Body["stream"])
			st.LogGroup = pre.LogGroup
			candCount := len(pre.Candidates)
			st.LogCandidateCount = &candCount
		}
		if deps.requestLogger != nil && st != nil {
			candCount := len(pre.Candidates)
			deps.requestLogger(handler.ServerLogEvent{
				Type:           "route",
				ID:             st.RequestID,
				Method:         r.Method,
				Path:           r.URL.Path,
				RequestedModel: st.RequestedModel,
				CandidateCount: &candCount,
				RouteReason:    string(pre.CandidateReason),
				Group:          pre.LogGroup,
			})
		}
		if apiType == ApiAnthropic {
			handler.HandleAnthropicMessage(ctx, deps.store, pre, deps.client, w, st, deps.requestLogger)
		} else {
			handler.HandleChatCompletion(ctx, deps.store, pre, deps.client, w, st, deps.requestLogger)
		}
	}
}

// ApiType selects which protocol the routed handler uses.
type ApiType int

const (
	ApiOpenAI ApiType = iota
	ApiAnthropic
)

func handleNotFound() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handler.WriteJSONError(w, 404, fmt.Sprintf("지원하지 않는 엔드포인트예요: %s %s. 사용 가능한 엔드포인트: GET /health, GET /v1/models, POST /v1/chat/completions, POST /anthropic/v1/messages", r.Method, r.URL.Path))
	}
}
