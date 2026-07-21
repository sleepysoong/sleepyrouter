package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/protocol"
	"github.com/sleepysoong/sleepyrouter/internal/providers"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// ---------------------------------------------------------------------------
// HTTP helpers — shared by handler and srv layers
// ---------------------------------------------------------------------------

// HTTPError is a typed error with an HTTP status code.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string { return e.Message }

// WriteJSON serializes body as JSON and writes it with the given status code.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	data, _ := utils.MarshalJSONHelper(body)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// WriteJSONError writes the standard upstream-style error envelope:
// `{"error": {"message": message}}`, optionally extended with extra keys
// merged into the inner error object (e.g. "details", "type", "request").
func WriteJSONError(w http.ResponseWriter, status int, message string, extras ...map[string]any) {
	inner := map[string]any{"message": message}
	for _, e := range extras {
		for k, v := range e {
			inner[k] = v
		}
	}
	WriteJSON(w, status, map[string]any{"error": inner})
}

// ReadBody reads the request body and parses it as JSON.
func ReadBody(r *http.Request) (map[string]any, error) {
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
		return nil, &HTTPError{StatusCode: 400, Message: fmt.Sprintf("요청 본문을 파싱할 수 없어요. 유효한 JSON을 보내주세요. (%d바이트 수신)", len(text))}
	}
	return body, nil
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

// ---------------------------------------------------------------------------
// Exported pipeline types
// ---------------------------------------------------------------------------

// HandlerPreamble holds the shared state extracted from the preamble of
// both POST /v1/chat/completions and POST /anthropic/v1/messages handlers.
type HandlerPreamble struct {
	ApiKeys         types.ProviderAPIKeys
	Body            map[string]any
	Selected        *SelectedModelsResult
	Candidates      []string
	CandidateReason routing.RouteReason
	RoutingModel    string
	LogGroup        string
}

// HandlerState carries mutable logging state across handler phases so that
// extracted handler functions and the deferred logResponse closure stay in sync.
type HandlerState struct {
	RequestID                                                           int
	RequestMethod, RequestPath                                          string
	RequestedModel, RoutedModel, RouteReason, LastError, LogGroup       string
	Stream                                                              bool
	LastInputTokens, LastOutputTokens, LogCandidateCount, LogTriedCount *int
}

// ServerLogEvent is the structured kind that gets handed to the
// configured RequestLogger. ServerLogEvent.Type is either "request"
// (when the request enters the router) or "response" (when it leaves).
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

// ---------------------------------------------------------------------------
// Logging helpers
// ---------------------------------------------------------------------------

// controlCharPattern produces a sanitizer that replaces ASCII control
// characters with '?' so a misbehaving upstream can't push terminal
// escapes through the log line. Built once via sync.OnceValue.
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

func SafeLogValue(value string) string {
	sanitized := controlCharPattern()(value)
	if len(sanitized) > 200 {
		return sanitized[:197] + "..."
	}
	return sanitized
}

// LogUpstreamAttempt fires an "upstream" event for each API call to an
// upstream provider, giving the operator per-attempt visibility into the
// retry loop. The response body is re-wrapped so RecordUpstreamFailure can
// still consume it.
func LogUpstreamAttempt(logger func(ServerLogEvent), st *HandlerState, modelID string, resp *http.Response, err error, attemptStart time.Time) {
	if logger == nil {
		return
	}
	elapsed := int(time.Since(attemptStart).Milliseconds())
	errText := ""
	statusCode := 0
	if err != nil {
		errText = SafeLogValue(err.Error())
	} else if resp != nil {
		statusCode = resp.StatusCode
		if !utils.IsOK(resp) {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			errText = SafeLogValue(string(bodyBytes))
		}
	}
	logger(ServerLogEvent{
		Type:           "upstream",
		ID:             st.RequestID,
		Method:         st.RequestMethod,
		Path:           st.RequestPath,
		ModelID:        modelID,
		StatusCode:     statusCode,
		DurationMs:     elapsed,
		Error:          errText,
		RequestedModel: st.RequestedModel,
		Group:          st.LogGroup,
	})
}

// ---------------------------------------------------------------------------
// Pipeline handlers
// ---------------------------------------------------------------------------

// ReadHandlerPreamble reads the request body, selects models, and computes
// route candidates. On error it writes the response to w and returns nil, false.
// Otherwise returns the preamble and true — the caller should proceed.
func ReadHandlerPreamble(ctx context.Context, store *cfg.ConfigStore, env utils.Environment, client types.HTTPDoer, w http.ResponseWriter, r *http.Request) (*HandlerPreamble, bool) {
	apiKeys, err := cfg.RequireAnyProviderAPIKey(env, store.Paths.Root)
	if err != nil {
		WriteJSONError(w, 500, err.Error())
		return nil, false
	}
	body, err := ReadBody(r)
	if err != nil {
		status := 500
		msg := err.Error()
		if he, ok := err.(*HTTPError); ok {
			status = he.StatusCode
			msg = he.Message
		}
		WriteJSONError(w, status, msg)
		return nil, false
	}
	selected, err := SelectedModelSelection(ctx, store, apiKeys, client)
	if err != nil {
		WriteJSONError(w, 500, err.Error())
		return nil, false
	}
	if err := AssertSelectedFree(selected.Models); err != nil {
		status := 500
		msg := err.Error()
		if he, ok := err.(*HTTPError); ok {
			status = he.StatusCode
			msg = he.Message
		}
		WriteJSONError(w, status, msg)
		return nil, false
	}
	routingModel := RequestedModelForRouting(selected.Models, body["model"])
	candidates, candidateReason := routing.OrderedCandidates(selected.ModelGroups, routingModel, selected.DefaultModelGroup, selected.GroupOrder...)
	logGroup := selected.DefaultModelGroup
	if candidateReason == routing.RouteModelGroup {
		logGroup = routing.NormalizeModelGroupName(routingModel)
	}
	return &HandlerPreamble{
		ApiKeys:         apiKeys,
		Body:            body,
		Selected:        selected,
		Candidates:      candidates,
		CandidateReason: candidateReason,
		RoutingModel:    routingModel,
		LogGroup:        logGroup,
	}, true
}

// HandleChatCompletion iterates model candidates for POST /v1/chat/completions.
func HandleChatCompletion(ctx context.Context, store *cfg.ConfigStore, pre *HandlerPreamble, client types.HTTPDoer, w http.ResponseWriter, st *HandlerState, requestLogger func(ServerLogEvent)) {
	body := pre.Body
	TryModelCandidates(ctx, pre, w, st, requestLogger, nil, store.Paths.Root, func(ctx context.Context, w http.ResponseWriter, model types.SleepyRouterModel, apiKey string, p providers.Provider, triedCount int) (bool, string) {
		modelID := model.ID
		upstreamBody := withUpstreamModel(body, model, st.Stream)
		attemptStart := time.Now()
		upstream, upstreamErr := p.ChatCompletion(ctx, apiKey, upstreamBody, client)
		LogUpstreamAttempt(requestLogger, st, modelID, upstream, upstreamErr, attemptStart)
		if upstreamErr != nil {
			return false, upstreamErr.Error()
		}
		if utils.IsOK(upstream) {
			if st.Stream {
				st.LastInputTokens, st.LastOutputTokens, st.LogTriedCount = WriteStreamResponse(w, upstream, store, model, triedCount)
				return true, ""
			}
		data, err := utils.ResponseJSON(upstream)
		if err != nil {
			return false, err.Error()
		}
		choices, _ := data["choices"].([]any)
		if len(choices) == 0 {
			return false, recordEmptyFailure(store, model, fmt.Sprintf("choices가 비어있어요 (%d)", upstream.StatusCode))
		}
		in, out, _ := UsageFromResponse(data)
			st.LastInputTokens = in
			st.LastOutputTokens = out
			recordSuccessfulUsage(store, model, data)
			t := triedCount
			st.LogTriedCount = &t
			WriteJSON(w, upstream.StatusCode, data)
			return true, ""
		}
		return false, RecordUpstreamFailure(store, model, upstream)
	})
}

func finishChatCompletionAsAnthropic(w http.ResponseWriter, store *cfg.ConfigStore, model types.SleepyRouterModel, upstream *http.Response, modelID string, st *HandlerState, triedCount int) (bool, string) {
	t := triedCount
	st.LogTriedCount = &t
	if st.Stream {
		recordSuccessfulUsage(store, model, nil)
		PipeOpenAIStreamAsAnthropic(upstream.Body, w, modelID)
	} else {
		data, err := utils.ResponseJSON(upstream)
		if err != nil {
			return false, err.Error()
		}
		in, out, _ := UsageFromResponse(data)
		st.LastInputTokens = in
		st.LastOutputTokens = out
		recordSuccessfulUsage(store, model, data)
		WriteJSON(w, upstream.StatusCode, protocol.OpenAIToAnthropic(data, modelID))
	}
	return true, ""
}

// HandleAnthropicMessage iterates model candidates for POST /anthropic/v1/messages.
func HandleAnthropicMessage(ctx context.Context, store *cfg.ConfigStore, pre *HandlerPreamble, client types.HTTPDoer, w http.ResponseWriter, st *HandlerState, requestLogger func(ServerLogEvent)) {
	body := pre.Body
	TryModelCandidates(ctx, pre, w, st, requestLogger, map[string]any{"type": "api_error"}, store.Paths.Root, func(ctx context.Context, w http.ResponseWriter, model types.SleepyRouterModel, apiKey string, p providers.Provider, triedCount int) (bool, string) {
		modelID := model.ID
		var upstream *http.Response
		var upstreamErr error
		if p.MessageProtocol() == providers.ProtocolAnthropic {
			upstreamBody := withUpstreamModel(body, model, st.Stream)
			attemptStart := time.Now()
			upstream, upstreamErr = p.Messages(ctx, apiKey, upstreamBody, client)
			LogUpstreamAttempt(requestLogger, st, modelID, upstream, upstreamErr, attemptStart)
			if upstreamErr == nil && !utils.IsOK(upstream) && (upstream.StatusCode == 404 || upstream.StatusCode == 405) {
				fallbackBody := protocol.AnthropicToOpenAI(body, modelUpstreamID(model))
				if st.Stream {
					fallbackBody["stream_options"] = map[string]any{"include_usage": true}
				}
				attemptStart := time.Now()
				upstream, upstreamErr = p.ChatCompletion(ctx, apiKey, fallbackBody, client)
				LogUpstreamAttempt(requestLogger, st, modelID, upstream, upstreamErr, attemptStart)
				if upstreamErr == nil && utils.IsOK(upstream) {
					return finishChatCompletionAsAnthropic(w, store, model, upstream, modelID, st, triedCount)
				}
			}
			if upstreamErr != nil {
				return false, upstreamErr.Error()
			}
			if utils.IsOK(upstream) {
				if st.Stream {
					st.LastInputTokens, st.LastOutputTokens, st.LogTriedCount = WriteStreamResponse(w, upstream, store, model, triedCount)
					return true, ""
				}
				data, err := utils.ResponseJSON(upstream)
				if err != nil {
					return false, err.Error()
				}
			_, hasChoicesArr := data["choices"].([]any)
			_, hasContentArr := data["content"].([]any)
			if !hasChoicesArr && !hasContentArr {
				return false, recordEmptyFailure(store, model, fmt.Sprintf("choices와 content가 모두 비어있어요 (%d)", upstream.StatusCode))
			}
				in, out, _ := UsageFromResponse(data)
				st.LastInputTokens = in
				st.LastOutputTokens = out
				recordSuccessfulUsage(store, model, data)
				t := triedCount
				st.LogTriedCount = &t
				WriteJSON(w, upstream.StatusCode, data)
				return true, ""
			}
		} else { // providers.ProtocolOpenAI
			fallbackBody := protocol.AnthropicToOpenAI(body, modelUpstreamID(model))
			attemptStart := time.Now()
			upstream, upstreamErr = p.ChatCompletion(ctx, apiKey, fallbackBody, client)
			LogUpstreamAttempt(requestLogger, st, modelID, upstream, upstreamErr, attemptStart)
			if upstreamErr != nil {
				return false, upstreamErr.Error()
			}
			if utils.IsOK(upstream) {
				return finishChatCompletionAsAnthropic(w, store, model, upstream, modelID, st, triedCount)
			}
		}
		return false, RecordUpstreamFailure(store, model, upstream)
	})
}
