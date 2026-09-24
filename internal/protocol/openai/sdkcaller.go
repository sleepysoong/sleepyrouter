package openai

import (
	"context"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/sleepysoong/sleepyrouter/internal/provider"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/upstream"
)

// SDKCaller is the production UpstreamCaller using the official SDK.
type SDKCaller struct {
	Pool     *upstream.Pool
	Registry *provider.Registry
}

func (s *SDKCaller) clientFor(c routing.Candidate) (openai.Client, provider.CompatibilityHook, bool) {
	prof := s.Registry.Get(c.ProviderID)
	cl, ok := s.Pool.Get(c.ProviderID, c.Provider)
	return cl, prof.Hook, ok
}

type sdkAttemptError struct {
	ae routing.AttemptError
}

func (e *sdkAttemptError) Error() string                      { return e.ae.SafeMessage }
func (e *sdkAttemptError) AttemptError() routing.AttemptError { return e.ae }

// DoNonStream implements UpstreamCaller.
func (s *SDKCaller) DoNonStream(ctx context.Context, c routing.Candidate, rawBody []byte) (upstream.Result, error) {
	cl, hook, ok := s.clientFor(c)
	if !ok {
		return upstream.Result{}, &sdkAttemptError{ae: routing.AttemptError{Class: routing.ErrorUnknown, SafeMessage: "API key missing for provider " + c.ProviderID, Skipped: true, SkipReason: "missing_api_key"}}
	}
	rewritten, err := upstream.RewriteModel(rawBody, c.UpstreamModel)
	if err != nil {
		return upstream.Result{}, err
	}
	rewritten, err = upstream.ApplyProviderDefaults(rewritten, c)
	if err != nil {
		return upstream.Result{}, err
	}
	if hook != nil {
		if err := hook.PrepareRequest(ctx, c, &provider.UpstreamRequest{RawBody: rewritten}); err != nil {
			return upstream.Result{}, err
		}
	}
	start := time.Now()
	res, err := upstream.ExecuteNonStream(ctx, cl, c, rewritten, hookOrNoop(hook))
	if err != nil {
		ae := upstream.NormalizeError(c, err, time.Since(start), hookOrNoop(hook))
		return upstream.Result{}, &sdkAttemptError{ae: ae}
	}
	return res, nil
}

// DoStream implements UpstreamCaller.
func (s *SDKCaller) DoStream(ctx context.Context, c routing.Candidate, rawBody []byte) (EventStream, error) {
	cl, hook, ok := s.clientFor(c)
	if !ok {
		return nil, &sdkAttemptError{ae: routing.AttemptError{Class: routing.ErrorUnknown, SafeMessage: "API key missing for provider " + c.ProviderID, Skipped: true, SkipReason: "missing_api_key"}}
	}
	rewritten, err := upstream.RewriteModel(rawBody, c.UpstreamModel)
	if err != nil {
		return nil, err
	}
	rewritten, err = upstream.ApplyProviderDefaults(rewritten, c)
	if err != nil {
		return nil, err
	}
	var st EventStream
	if c.Provider != nil && c.Provider.WireAPI == "chat_completions" {
		st, err = openChatCompletionStream(ctx, cl, c, rewritten)
	} else {
		st, err = openStream(ctx, cl, c, rewritten)
	}
	if err != nil {
		ae := upstream.NormalizeError(c, err, 0, hookOrNoop(hook))
		return nil, &sdkAttemptError{ae: ae}
	}
	return st, nil
}

func hookOrNoop(h provider.CompatibilityHook) provider.CompatibilityHook {
	if h != nil {
		return h
	}
	return noopHook{}
}

type noopHook struct{}

func (noopHook) PrepareRequest(_ context.Context, _ routing.Candidate, _ *provider.UpstreamRequest) error {
	return nil
}
func (noopHook) ClassifyError(status int, t, code, msg string) routing.ErrorClass {
	return routing.ClassifyBody(status, t, code, msg)
}

var _ = responses.ResponseNewParams{}
