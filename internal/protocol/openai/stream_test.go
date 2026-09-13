package openai_test

import (
	"errors"
	"testing"

	proto "github.com/sleepysoong/sleepyrouter/internal/protocol/openai"
	"github.com/sleepysoong/sleepyrouter/internal/upstream"
)

type fakeStream struct {
	events [][2]string
	idx    int
	err    error
	closed bool
}

func (f *fakeStream) Next() bool { return f.idx < len(f.events) }
func (f *fakeStream) Event() (string, []byte) {
	e := f.events[f.idx]
	f.idx++
	return e[0], []byte(e[1])
}
func (f *fakeStream) Err() error { return f.err }
func (f *fakeStream) Close() error {
	f.closed = true
	return nil
}

// Pre-commit failure must allow failover: a stream that errors before any
// meaningful event is not a commit.
func TestPrecommitFailureIsFailoverable(t *testing.T) {
	s := &fakeStream{events: [][2]string{{"response.created", `{}`}}, err: errors.New("boom")}
	n := 0
	for s.Next() {
		typ, p := s.Event()
		n++
		if upstream.IsMeaningfulEvent(typ, string(p)) {
			t.Fatal("created must not be meaningful")
		}
	}
	if n != 1 {
		t.Fatalf("n=%d", n)
	}
	if s.Err() == nil {
		t.Fatal("expected error")
	}
}

// Post-commit failure must NOT trigger another model: once a meaningful
// event exists, the stream is committed.
func TestPostCommitNoFailover(t *testing.T) {
	s := &fakeStream{events: [][2]string{
		{"response.created", `{}`},
		{"response.output_text.delta", `{"delta":"hello"}`},
	}, err: errors.New("mid-stream cut")}
	seenMeaningful := false
	for s.Next() {
		typ, p := s.Event()
		if upstream.IsMeaningfulEvent(typ, string(p)) {
			seenMeaningful = true
		}
	}
	if !seenMeaningful {
		t.Fatal("expected commit")
	}
	// Invariant: caller must not invoke next candidate after commit.
	// (Enforced in server.precommitAndStream*; this test pins the classifier.)
	var _ proto.EventStream = s
}
