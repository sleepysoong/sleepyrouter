package openai

import (
	"context"
	"encoding/json"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
)

// sdkEventStream adapts the official SDK stream to EventStream.
type sdkEventStream struct {
	inner eventStreamShim
}

// eventStreamShim is a minimal interface over ssestream.Stream to avoid
// generic coupling in this file's signature.
type eventStreamShim interface {
	Next() bool
	Current() responses.ResponseStreamEventUnion
	Err() error
	Close() error
}

func (s *sdkEventStream) Next() bool   { return s.inner.Next() }
func (s *sdkEventStream) Err() error   { return s.inner.Err() }
func (s *sdkEventStream) Close() error { return s.inner.Close() }
func (s *sdkEventStream) Event() (string, []byte) {
	ev := s.inner.Current()
	if raw := ev.RawJSON(); raw != "" {
		return ev.Type, []byte(raw)
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return "", nil
	}
	return ev.Type, b
}

func openStream(ctx context.Context, cl openai.Client, c routing.Candidate, rawBody []byte) (EventStream, error) {
	var params responses.ResponseNewParams
	if err := json.Unmarshal(rawBody, &params); err != nil {
		return nil, err
	}
	params.Model = c.UpstreamModel
	st := cl.Responses.NewStreaming(ctx, params)
	return &sdkEventStream{inner: st}, nil
}

var _ routing.Candidate
