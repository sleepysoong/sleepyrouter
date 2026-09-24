package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/sleepysoong/sleepyrouter/internal/provider"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/tidwall/sjson"
)

// Result is a successful non-streaming upstream call.
type Result struct {
	RawBody               []byte
	ReasoningContent      string
	ReasoningItems        []json.RawMessage
	ResponseID            string
	InputTokens           int64
	CachedInputTokens     int64
	CacheWriteInputTokens int64
	OutputTokens          int64
	Model                 string
	Status                responses.ResponseStatus
}

// RewriteModel replaces top-level "model" without touching other fields.
// Uses sjson so unknown fields keep byte-level fidelity.
func RewriteModel(raw []byte, upstreamModel string) ([]byte, error) {
	return sjson.SetBytes(raw, "model", upstreamModel)
}

// NormalizeError maps SDK/transport errors to AttemptError (no secrets).
func NormalizeError(c routing.Candidate, err error, dur time.Duration, hook provider.CompatibilityHook) routing.AttemptError {
	ae := routing.AttemptError{Candidate: c.LocalModelID, Provider: c.ProviderID, Duration: dur}
	if err == nil {
		return ae
	}
	if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "context canceled") {
		ae.Class = routing.ErrorClient
		ae.SafeMessage = "request cancelled by client"
		return ae
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") || strings.Contains(strings.ToLower(err.Error()), "timeout") {
		ae.Class = routing.ErrorTimeout
		ae.SafeMessage = truncate(err.Error(), 300)
		return ae
	}
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		ae.StatusCode = apiErr.StatusCode
		class := hook.ClassifyError(apiErr.StatusCode, apiErr.Type, apiErr.Code, apiErr.Message)
		ae.Class = class
		ae.SafeMessage = truncate(apiErr.Type+": "+apiErr.Code+": "+apiErr.Message, 300)
		if ae.SafeMessage == "" {
			ae.SafeMessage = truncate(err.Error(), 300)
		}
		return ae
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "connection refused"), strings.Contains(lower, "no such host"), strings.Contains(lower, "dns"), strings.Contains(lower, "connection reset"), strings.Contains(lower, "tls"):
		ae.Class = routing.ErrorNetwork
	default:
		ae.Class = routing.ErrorUnknown
	}
	ae.SafeMessage = truncate(err.Error(), 300)
	return ae
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ExecuteNonStream performs one candidate attempt (non-streaming).
func ExecuteNonStream(ctx context.Context, client openai.Client, c routing.Candidate, rawBody []byte, hook provider.CompatibilityHook) (Result, error) {
	if c.Provider != nil && c.Provider.WireAPI == "chat_completions" {
		params, requestOptions, err := ResponsesToChatCompletionRequest(rawBody, c.UpstreamModel)
		if err != nil {
			return Result{}, err
		}
		resp, err := client.Chat.Completions.New(ctx, params, requestOptions...)
		if err != nil {
			return Result{}, err
		}
		out, err := ChatCompletionResponseBody(resp)
		if err != nil {
			return Result{}, err
		}
		status := responses.ResponseStatusCompleted
		if len(resp.Choices) > 0 && resp.Choices[0].FinishReason == "length" {
			status = responses.ResponseStatusIncomplete
		}
		var reasoningContent string
		if len(resp.Choices) > 0 {
			var vendorFields struct {
				ReasoningContent string `json:"reasoning_content"`
			}
			_ = json.Unmarshal([]byte(resp.Choices[0].Message.RawJSON()), &vendorFields)
			reasoningContent = vendorFields.ReasoningContent
		}
		return Result{
			RawBody: out, ReasoningContent: reasoningContent, ResponseID: ChatCompletionResponseID(resp),
			InputTokens:           resp.Usage.PromptTokens,
			CachedInputTokens:     resp.Usage.PromptTokensDetails.CachedTokens,
			CacheWriteInputTokens: resp.Usage.PromptTokensDetails.CacheWriteTokens,
			OutputTokens:          resp.Usage.CompletionTokens, Model: resp.Model, Status: status,
		}, nil
	}
	var params responses.ResponseNewParams
	if err := json.Unmarshal(rawBody, &params); err != nil {
		return Result{}, err
	}
	params.Model = c.UpstreamModel
	resp, err := client.Responses.New(ctx, params)
	if err != nil {
		return Result{}, err
	}
	raw := resp.RawJSON()
	var out []byte
	if raw != "" {
		out = []byte(raw)
	} else {
		b, mErr := json.Marshal(resp)
		if mErr != nil {
			return Result{}, mErr
		}
		out = b
	}
	var usageIn, usageOut int64
	var cachedInput, cacheWriteInput int64
	var respID, model string
	if resp.Usage.InputTokens != 0 || resp.Usage.OutputTokens != 0 {
		usageIn = resp.Usage.InputTokens
		usageOut = resp.Usage.OutputTokens
		cachedInput = resp.Usage.InputTokensDetails.CachedTokens
		cacheWriteInput = resp.Usage.InputTokensDetails.CacheWriteTokens
	}
	respID = resp.ID
	model = string(resp.Model)
	if (resp.Status == responses.ResponseStatusCompleted || resp.Status == "") && isEmptyResponse(resp) {
		return Result{}, &emptyResponseError{status: http.StatusOK}
	}
	return Result{
		RawBody: out, ReasoningItems: ResponsesReasoningItems(out), ResponseID: respID, InputTokens: usageIn,
		CachedInputTokens: cachedInput, CacheWriteInputTokens: cacheWriteInput,
		OutputTokens: usageOut, Model: model, Status: resp.Status,
	}, nil
}

// ResponsesReasoningItems extracts complete reasoning output items for a
// later stateless tool turn. The raw JSON is retained so encrypted provider
// continuation fields are not lost through an SDK type round-trip.
func ResponsesReasoningItems(raw []byte) []json.RawMessage {
	var response struct {
		Output []json.RawMessage `json:"output"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return nil
	}
	var items []json.RawMessage
	for _, item := range response.Output {
		var header struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(item, &header) == nil && header.Type == "reasoning" {
			items = append(items, append(json.RawMessage(nil), item...))
		}
	}
	return items
}

type emptyResponseError struct{ status int }

func (e *emptyResponseError) Error() string { return "upstream returned empty response" }

func isEmptyResponse(resp *responses.Response) bool {
	if resp == nil {
		return true
	}
	return len(resp.Output) == 0
}
