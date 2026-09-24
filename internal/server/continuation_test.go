package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/protocol/anthropic"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
)

func TestReasoningContinuationsAreScopedAndDefensivelyCopied(t *testing.T) {
	cache := newReasoningContinuations()
	candidate := routing.Candidate{ProviderID: "provider-a", UpstreamModel: "model-a"}
	value := anthropic.ReasoningContinuation{
		ChatCompletions: "opaque-chat-reasoning",
		ResponsesItems:  []json.RawMessage{json.RawMessage(`{"type":"reasoning","encrypted_content":"opaque"}`)},
	}
	cache.Store("session-a", candidate, "fingerprint-a", value)
	value.ResponsesItems[0][0] = 'x'

	loaded, ok := cache.Load("session-a", candidate, "fingerprint-a")
	if !ok {
		t.Fatal("continuation was not found")
	}
	if loaded.ChatCompletions != "opaque-chat-reasoning" || string(loaded.ResponsesItems[0]) != `{"type":"reasoning","encrypted_content":"opaque"}` {
		t.Fatalf("stored continuation changed with caller-owned input: %+v", loaded)
	}
	loaded.ResponsesItems[0][0] = 'x'
	loadedAgain, ok := cache.Load("session-a", candidate, "fingerprint-a")
	if !ok || string(loadedAgain.ResponsesItems[0]) != `{"type":"reasoning","encrypted_content":"opaque"}` {
		t.Fatalf("loaded continuation was not defensively copied: %+v, found=%v", loadedAgain, ok)
	}

	otherCandidate := candidate
	otherCandidate.ProviderID = "provider-b"
	otherModel := candidate
	otherModel.UpstreamModel = "model-b"
	for _, scope := range []struct {
		session     string
		candidate   routing.Candidate
		fingerprint string
	}{
		{session: "session-b", candidate: candidate, fingerprint: "fingerprint-a"},
		{session: "session-a", candidate: otherCandidate, fingerprint: "fingerprint-a"},
		{session: "session-a", candidate: otherModel, fingerprint: "fingerprint-a"},
		{session: "session-a", candidate: candidate, fingerprint: "fingerprint-b"},
	} {
		if _, ok := cache.Load(scope.session, scope.candidate, scope.fingerprint); ok {
			t.Fatalf("continuation crossed scope boundary: %+v", scope)
		}
	}
}

func TestReasoningContinuationsExpireAndReleaseCapacity(t *testing.T) {
	cache := newReasoningContinuations()
	candidate := routing.Candidate{ProviderID: "provider-a", UpstreamModel: "model-a"}
	cache.Store("session-a", candidate, "fingerprint-a", anthropic.ReasoningContinuation{ChatCompletions: "opaque"})
	key := continuationKey{session: "session-a", provider: "provider-a", model: "model-a", fingerprint: "fingerprint-a"}
	cache.mu.Lock()
	entry := cache.entries[key]
	entry.expires = time.Now().Add(-time.Second)
	cache.entries[key] = entry
	cache.mu.Unlock()

	if _, ok := cache.Load("session-a", candidate, "fingerprint-a"); ok {
		t.Fatal("expired continuation was returned")
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.bytes != 0 || len(cache.entries) != 0 {
		t.Fatalf("expired continuation still occupies cache: bytes=%d entries=%d", cache.bytes, len(cache.entries))
	}
}
