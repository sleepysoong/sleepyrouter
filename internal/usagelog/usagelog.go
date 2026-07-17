// Package usagelog extracts token usage from upstream responses and records
// it to disk via the configuration store. It is the single source-of-truth
// for "did this request cost tokens, and how many" inside sleepyrouter.
//
// The package is intentionally small: two pure extractors and two writers.
// All callers inside internal/srv hand the resulting ints and the
// HumanReadableFailure reason to the request log emitter.
package usagelog

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// Extract reads prompt/completion/total token counts from a typical OpenAI
// or Claude response body. It returns nil pointers for absent fields so a
// caller can distinguish "no usage reported" from "zero usage reported".
//
// Both prompt_tokens/input_tokens and completion_tokens/output_tokens are
// accepted because different upstreams use different conventions.
func Extract(data map[string]any) (inputTokens, outputTokens, totalTokens *int) {
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

// ExtractFromResponse is a convenience wrapper around Extract that decodes
// the response body first. Returns nil tokens (without error) for nil
// responses, so callers can use the result without nil-checks.
func ExtractFromResponse(response *http.Response) (inputTokens, outputTokens, totalTokens *int, err error) {
	if response == nil {
		return nil, nil, nil, nil
	}
	data, err := utils.ResponseJSON(response)
	if err != nil {
		return nil, nil, nil, err
	}
	in, out, total := Extract(data)
	return in, out, total, nil
}

// Success appends a single line to the persistent usage log for the given
// model. Failure to persist is intentionally swallowed because usage logs
// are best-effort observability, not a critical path.
func Success(store *cfg.ConfigStore, model types.SleepyRouterModel, data map[string]any) {
	in, out, _ := Extract(data)
	record(store, model, in, out, true)
}

// Failure appends a zero-token failure record by reading the response body
// and returning it as a human-readable string for the caller to log.
func Failure(store *cfg.ConfigStore, model types.SleepyRouterModel, response *http.Response) string {
	text, _ := io.ReadAll(response.Body)
	record(store, model, nil, nil, false)
	return fmt.Sprintf("[%d] %s", response.StatusCode, string(text))
}

// Truncate clamps s to at most max bytes. It is provided here because usage
// reporting shares its truncation semantics with route error formatting.
func Truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func record(store *cfg.ConfigStore, model types.SleepyRouterModel, in, out *int, success bool) {
	usageID := model.UsageID
	if usageID == "" {
		usageID = model.ID
	}
	input := 0
	output := 0
	if in != nil {
		input = *in
	}
	if out != nil {
		output = *out
	}
	_ = store.AppendUsage(types.UsageLogEntry{
		TS:           time.Now().UTC().Format(time.RFC3339),
		Model:        usageID,
		InputTokens:  input,
		OutputTokens: output,
		Success:      success,
	})
}

// EstimateInput produces a coarse-grained input token estimate for an
// arbitrary body. It uses the byte length heuristic (1 token ~ 4 bytes) so
// requests can be counted even when an upstream provider does not return
// usage for Anthropic count_tokens endpoints.
func EstimateInput(body any) int {
	text, _ := utils.MarshalJSONHelper(body)
	n := (len(text) + 3) / 4
	if n < 1 {
		return 1
	}
	return n
}
