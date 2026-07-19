package srv

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
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
	requestLogger func(ServerLogEvent)
	startTime     time.Time
}

// stateKey is the context key used to attach per-request handlerState.
type stateKey struct{}

func withState(ctx context.Context, st *handlerState) context.Context {
	return context.WithValue(ctx, stateKey{}, st)
}

func stateFromContext(ctx context.Context) *handlerState {
	if v, ok := ctx.Value(stateKey{}).(*handlerState); ok {
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
	mux.HandleFunc("POST /v1/chat/completions", handleChat(deps))
	mux.HandleFunc("POST /anthropic/v1/messages", handleAnthropic(deps))
	mux.HandleFunc("POST /anthropic/messages", handleAnthropic(deps))
	mux.HandleFunc("/", handleNotFound())
}

func handleHealth(deps routeDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "service": "sleepyrouter", "version": types.Version, "uptime": int(time.Since(deps.startTime).Seconds())})
	}
}

func handleModels(deps routeDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		apiKeys, err := cfg.RequireAnyProviderAPIKey(deps.env, deps.store.Paths.Root)
		if err != nil {
			writeJSONError(w, 500, err.Error())
			return
		}
		selected, err := selectedModelSelection(r.Context(), deps.store, apiKeys, deps.client)
		if err != nil {
			writeJSONError(w, 500, err.Error())
			return
		}
		data := make([]map[string]any, 0, len(selected.Models))
		for _, model := range selected.Models {
			data = append(data, map[string]any{"id": model.ID, "object": "model", "created": 0, "owned_by": string(types.SourceOf(model)), "provider": model.Provider})
		}
		writeJSON(w, 200, map[string]any{"object": "list", "data": data})
	}
}

func handleCountTokens() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := stateFromContext(r.Context())
		body, err := readBody(r)
		if err != nil {
			status := 500
			msg := err.Error()
			if he, ok := err.(*httpError); ok {
				status = he.StatusCode
				msg = he.Message
			}
			writeJSONError(w, status, msg)
			return
		}
		if st != nil {
			st.requestedModel = utils.StringFromUnknown(body["model"])
		}
		writeJSON(w, 200, map[string]any{"input_tokens": estimateInputTokens(body)})
	}
}

func handleChat(deps routeDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		st := stateFromContext(ctx)
		pre, ok := readHandlerPreamble(ctx, deps.store, deps.env, deps.client, w, r)
		if !ok {
			return
		}
		if st != nil {
			st.requestedModel = utils.StringFromUnknown(pre.body["model"])
			st.stream = utils.BoolValue(pre.body["stream"])
			st.logGroup = pre.logGroup
			candCount := len(pre.candidates)
			st.logCandidateCount = &candCount
		}
		if deps.requestLogger != nil && st != nil {
			candCount := len(pre.candidates)
			deps.requestLogger(ServerLogEvent{
				Type:           "route",
				ID:             st.requestID,
				Method:         r.Method,
				Path:           r.URL.Path,
				RequestedModel: st.requestedModel,
				CandidateCount: &candCount,
				RouteReason:    string(pre.candidateReason),
				Group:          pre.logGroup,
			})
		}
		handleChatCompletion(ctx, deps.store, pre, deps.client, w, st, deps.requestLogger)
	}
}

func handleAnthropic(deps routeDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		st := stateFromContext(ctx)
		pre, ok := readHandlerPreamble(ctx, deps.store, deps.env, deps.client, w, r)
		if !ok {
			return
		}
		if st != nil {
			st.requestedModel = utils.StringFromUnknown(pre.body["model"])
			st.stream = utils.BoolValue(pre.body["stream"])
			st.logGroup = pre.logGroup
			candCount := len(pre.candidates)
			st.logCandidateCount = &candCount
		}
		if deps.requestLogger != nil && st != nil {
			candCount := len(pre.candidates)
			deps.requestLogger(ServerLogEvent{
				Type:           "route",
				ID:             st.requestID,
				Method:         r.Method,
				Path:           r.URL.Path,
				RequestedModel: st.requestedModel,
				CandidateCount: &candCount,
				RouteReason:    string(pre.candidateReason),
				Group:          pre.logGroup,
			})
		}
		handleAnthropicMessage(ctx, deps.store, pre, deps.client, w, st, deps.requestLogger)
	}
}

func handleNotFound() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSONError(w, 404, fmt.Sprintf("지원하지 않는 엔드포인트예요: %s %s. 사용 가능한 엔드포인트: GET /health, GET /v1/models, POST /v1/chat/completions, POST /anthropic/v1/messages", r.Method, r.URL.Path))
	}
}
