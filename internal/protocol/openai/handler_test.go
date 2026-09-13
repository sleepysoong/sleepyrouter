package openai_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	proto "github.com/sleepysoong/sleepyrouter/internal/protocol/openai"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/upstream"
)

type fakeCaller struct {
	results map[string]upstream.Result
	errs    map[string]error
	calls   []string
}

func (f *fakeCaller) DoNonStream(_ context.Context, c routing.Candidate, _ []byte) (upstream.Result, error) {
	f.calls = append(f.calls, c.LocalModelID)
	if err, ok := f.errs[c.LocalModelID]; ok {
		return upstream.Result{}, err
	}
	return f.results[c.LocalModelID], nil
}

func (f *fakeCaller) DoStream(_ context.Context, _ routing.Candidate, _ []byte) (proto.EventStream, error) {
	return nil, errors.New("not implemented")
}

type carrier struct{ ae routing.AttemptError }

func (c carrier) Error() string                      { return c.ae.SafeMessage }
func (c carrier) AttemptError() routing.AttemptError { return c.ae }

func testCandidates() []routing.Candidate {
	mk := func(id, prov string) routing.Candidate {
		return routing.Candidate{
			LocalModelID: id, ProviderID: prov, UpstreamModel: "up-" + id,
			Model:    config.RuntimeModel{LocalID: id, ProviderID: prov, UpstreamModel: "up-" + id},
			Provider: &config.RuntimeProvider{ID: prov, APIKey: "k"},
		}
	}
	return []routing.Candidate{mk("a", "zen"), mk("b", "nvidia"), mk("c", "openrouter"), mk("d", "gemini")}
}

func TestFailoverABC(t *testing.T) {
	f := &fakeCaller{
		errs: map[string]error{
			"a": carrier{routing.AttemptError{StatusCode: 429, Class: routing.ErrorRateLimit, SafeMessage: "rate limited"}},
			"b": carrier{routing.AttemptError{StatusCode: 500, Class: routing.ErrorUpstream, SafeMessage: "boom"}},
		},
		results: map[string]upstream.Result{"c": {RawBody: []byte(`{"id":"resp_1"}`)}},
	}
	out, lastErr := proto.ExecuteNonStream(context.Background(), f, testCandidates(), []byte(`{}`), nil)
	if lastErr != nil {
		t.Fatalf("expected success, got %v", lastErr)
	}
	if out.Result == nil || out.Candidate.LocalModelID != "c" {
		t.Fatalf("winner = %+v", out.Candidate)
	}
	want := []string{"a", "b", "c"}
	if len(f.calls) != 3 || f.calls[0] != want[0] || f.calls[1] != want[1] || f.calls[2] != want[2] {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
	for _, d := range f.calls {
		if d == "d" {
			t.Fatal("d must not be called after success")
		}
	}
}

func TestClientErrorStops(t *testing.T) {
	f := &fakeCaller{errs: map[string]error{
		"a": carrier{routing.AttemptError{StatusCode: 400, Class: routing.ErrorClient, SafeMessage: "bad request"}},
	}}
	_, lastErr := proto.ExecuteNonStream(context.Background(), f, testCandidates(), []byte(`{}`), nil)
	if lastErr == nil || lastErr.Class != routing.ErrorClient {
		t.Fatalf("expected client stop, got %+v", lastErr)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestKeyMissingSkipped(t *testing.T) {
	cands := testCandidates()
	cands[0].Provider = &config.RuntimeProvider{ID: "zen"}
	f := &fakeCaller{results: map[string]upstream.Result{"b": {RawBody: []byte(`{}`)}}}
	out, err := proto.ExecuteNonStream(context.Background(), f, cands, []byte(`{}`), nil)
	if err != nil || out.Candidate.LocalModelID != "b" {
		t.Fatalf("got %+v %v", out.Candidate, err)
	}
}

func TestParseRequiresModel(t *testing.T) {
	if _, err := proto.Parse([]byte(`{"input":"hi"}`)); err == nil {
		t.Fatal("expected missing model error")
	}
	if _, err := proto.Parse([]byte(`{bad`)); err == nil {
		t.Fatal("expected malformed error")
	}
	pr, err := proto.Parse([]byte(`{"model":"coding","stream":true}`))
	if err != nil || !pr.Stream || pr.RequestedModel != "coding" {
		t.Fatalf("got %+v %v", pr, err)
	}
}
