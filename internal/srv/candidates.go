package srv

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/providers"
	"github.com/sleepysoong/sleepyrouter/internal/types"
)

// modelAttemptFunc is invoked once per candidate model with the resolved
// provider and API key. It owns the upstream call(s) and response writing:
// when handled is true the attempt wrote the response and the loop stops;
// otherwise upstreamError (if non-empty) becomes the running error surfaced
// in the final 502 envelope.
type modelAttemptFunc func(ctx context.Context, w http.ResponseWriter, model types.SleepyRouterModel, apiKey string, p providers.Provider, triedCount int) (handled bool, upstreamError string)

// tryModelCandidates drives the candidate iteration shared by POST
// /v1/chat/completions and POST /anthropic/v1/messages. It resolves each
// candidate to its SleepyRouterModel, checks the matched API key, looks up
// the provider, and invokes attempt for the handler-specific upstream call
// and response. On success the attempt writes the response and the loop
// stops; on failure the accumulated error becomes the 502 envelope body.
// failureExtras are merged into the 502 envelope (e.g. {"type":"api_error"}
// for Anthropic-shaped errors).
func tryModelCandidates(ctx context.Context, pre *handlerPreamble, w http.ResponseWriter, st *handlerState, requestLogger func(ServerLogEvent), failureExtras map[string]any, attempt modelAttemptFunc) {
	apiKeys := pre.apiKeys
	selected := pre.selected
	candidates := pre.candidates
	candidateReason := pre.candidateReason

	var upstreamError string
	triedAny := false
	triedCount := 0
	for _, modelID := range candidates {
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
		handled, attemptErr := attempt(ctx, w, model, apiKey, p, triedCount)
		if handled {
			return
		}
		if attemptErr != "" {
			upstreamError = attemptErr
			st.lastError = fmt.Sprintf("[%s] %s", modelID, truncate(upstreamError, 300))
		}
	}
	if !triedAny {
		noUsableModelResponse(w, upstreamError)
		return
	}
	extras := map[string]any{"details": upstreamError}
	for k, v := range failureExtras {
		extras[k] = v
	}
	writeJSONError(w, 502, "선택된 모든 무료 모델이 실패했어요.", extras)
}
