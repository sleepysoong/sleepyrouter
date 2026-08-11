// Package providers builds GoAI-backed model clients per upstream source and
// adapts their results back into the *http.Response shape the sleepyrouter
// handler and its tests consume.
package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	goai "github.com/zendev-sh/goai"
	"github.com/zendev-sh/goai/provider"

	"github.com/sleepysoong/sleepyrouter/internal/adapt"
	"github.com/sleepysoong/sleepyrouter/internal/emit"
	"github.com/sleepysoong/sleepyrouter/internal/types"
)

// modelIDFrom extracts the wire model ID from a provider request body. The
// handler guarantees it is present (withUpstreamModel injects the resolved
// candidate ID before the provider runs).
func modelIDFrom(body map[string]any) string {
	modelID, _ := body["model"].(string)
	return modelID
}

// httpClientFor adapts the injected HTTP doer (possibly a test fake) to the
// *http.Client that GoAI models require.
func httpClientFor(doer types.HTTPDoer) *http.Client {
	if hc, ok := doer.(*http.Client); ok {
		return hc
	}
	if doer == nil {
		return http.DefaultClient
	}
	return &http.Client{Transport: roundTripperFunc(doer.Do)}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// goaiChatCompletion runs an OpenAI-format request through a GoAI model and
// converts the result back into the *http.Response the handler expects.
func goaiChatCompletion(ctx context.Context, model provider.LanguageModel, modelID string, body map[string]any, doer types.HTTPDoer) (*http.Response, error) {
	params, err := adapt.OpenAIRequest(ctx, body, modelID)
	if err != nil {
		return nil, err
	}
	isStream, _ := body["stream"].(bool)
	if isStream {
		stream, err := model.DoStream(ctx, params)
		if err != nil {
			return httpErrorResponse(err)
		}
		return sseResponse(func(w io.Writer) { emit.OpenAIStreamSSE(w, stream.Stream, modelID) }), nil
	}
	result, err := model.DoGenerate(ctx, params)
	if err != nil {
		return httpErrorResponse(err)
	}
	if result.Text == "" && result.Reasoning == "" && len(result.ToolCalls) == 0 && result.FinishReason == "" {
		// Legacy semantics: an upstream response with no choice content at all
		// is surfaced as "choices": [] so the handler retries the candidate.
		return okJSONResponse(`{"choices":[]}`), nil
	}
	data, err := emit.OpenAIResponse(*result, modelID)
	if err != nil {
		return nil, err
	}
	return okJSONResponse(string(data)), nil
}

// goaiAnthropicMessages runs an Anthropic-format request through a GoAI
// model against a native /v1/messages endpoint.
func goaiAnthropicMessages(ctx context.Context, model provider.LanguageModel, modelID string, body map[string]any, doer types.HTTPDoer) (*http.Response, error) {
	params, err := adapt.AnthropicRequest(ctx, body, modelID)
	if err != nil {
		return nil, err
	}
	isStream, _ := body["stream"].(bool)
	if isStream {
		stream, err := model.DoStream(ctx, params)
		if err != nil {
			return httpErrorResponse(err)
		}
		return sseResponse(func(w io.Writer) { emit.AnthropicStreamSSE(w, stream.Stream, modelID) }), nil
	}
	result, err := model.DoGenerate(ctx, params)
	if err != nil {
		return httpErrorResponse(err)
	}
	data, err := emit.AnthropicResponse(*result, modelID)
	if err != nil {
		return nil, err
	}
	return okJSONResponse(string(data)), nil
}

// httpErrorResponse converts a GoAI error (API status errors in particular)
// back into an *http.Response so the handler's existing failure/fallback
// logic keeps working. Non-API errors (request build, parse) are returned as
// Go errors, matching the legacy providers.
func httpErrorResponse(err error) (*http.Response, error) {
	var apiErr *goai.APIError
	if errors.As(err, &apiErr) {
		body := apiErr.ResponseBody
		if body == "" {
			body = fmt.Sprintf(`{"error":{"message":%q}}`, apiErr.Message)
		}
		return &http.Response{
			StatusCode: apiErr.StatusCode,
			Status:     fmt.Sprintf("%d %s", apiErr.StatusCode, http.StatusText(apiErr.StatusCode)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}
	return nil, err
}

// sseResponse wraps an SSE-writing emit function in an *http.Response with a
// streaming body, mirroring the legacy raw upstream stream responses.
func sseResponse(write func(io.Writer)) *http.Response {
	pr, pw := io.Pipe()
	go func() {
		write(pw)
		_ = pw.Close()
	}()
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       pr,
	}
}

func okJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
