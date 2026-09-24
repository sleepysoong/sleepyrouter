package server

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/protocol/anthropic"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
)

const (
	continuationTTL        = 20 * time.Minute
	continuationMaxEntries = 512
	continuationMaxBytes   = 8 << 20
	continuationMaxEntry   = 2 << 20
)

type continuationKey struct {
	session     string
	provider    string
	model       string
	fingerprint string
}

type continuationEntry struct {
	value   anthropic.ReasoningContinuation
	bytes   int
	expires time.Time
	touched time.Time
}

// reasoningContinuations is a bounded, process-local cache because Anthropic
// Messages clients send full history but omit opaque provider reasoning state.
// Entries are scoped to the Claude Code session, provider, model and assistant
// content digest, and are never written to logs or returned to the client.
type reasoningContinuations struct {
	mu      sync.Mutex
	entries map[continuationKey]continuationEntry
	bytes   int
}

func newReasoningContinuations() *reasoningContinuations {
	return &reasoningContinuations{entries: make(map[continuationKey]continuationEntry)}
}

func continuationSize(value anthropic.ReasoningContinuation) int {
	n := len(value.ChatCompletions)
	for _, item := range value.ResponsesItems {
		n += len(item)
	}
	return n
}

func cloneContinuation(value anthropic.ReasoningContinuation) anthropic.ReasoningContinuation {
	cloned := anthropic.ReasoningContinuation{ChatCompletions: value.ChatCompletions}
	if len(value.ResponsesItems) > 0 {
		cloned.ResponsesItems = make([]json.RawMessage, len(value.ResponsesItems))
		for i, item := range value.ResponsesItems {
			cloned.ResponsesItems[i] = append(json.RawMessage(nil), item...)
		}
	}
	return cloned
}

func (c *reasoningContinuations) Store(session string, candidate routing.Candidate, fingerprint string, value anthropic.ReasoningContinuation) {
	if c == nil || session == "" || candidate.ProviderID == "" || candidate.UpstreamModel == "" || fingerprint == "" {
		return
	}
	size := continuationSize(value)
	if size == 0 || size > continuationMaxEntry {
		return
	}
	key := continuationKey{session: session, provider: candidate.ProviderID, model: candidate.UpstreamModel, fingerprint: fingerprint}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeExpired(now)
	if old, ok := c.entries[key]; ok {
		c.bytes -= old.bytes
		delete(c.entries, key)
	}
	for len(c.entries) >= continuationMaxEntries || c.bytes+size > continuationMaxBytes {
		if !c.evictOldest() {
			return
		}
	}
	c.entries[key] = continuationEntry{value: cloneContinuation(value), bytes: size, expires: now.Add(continuationTTL), touched: now}
	c.bytes += size
}

func (c *reasoningContinuations) Load(session string, candidate routing.Candidate, fingerprint string) (anthropic.ReasoningContinuation, bool) {
	if c == nil || session == "" || candidate.ProviderID == "" || candidate.UpstreamModel == "" || fingerprint == "" {
		return anthropic.ReasoningContinuation{}, false
	}
	key := continuationKey{session: session, provider: candidate.ProviderID, model: candidate.UpstreamModel, fingerprint: fingerprint}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeExpired(now)
	entry, ok := c.entries[key]
	if !ok {
		return anthropic.ReasoningContinuation{}, false
	}
	entry.touched = now
	c.entries[key] = entry
	return cloneContinuation(entry.value), true
}

func (c *reasoningContinuations) purgeExpired(now time.Time) {
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			c.bytes -= entry.bytes
			delete(c.entries, key)
		}
	}
}

func (c *reasoningContinuations) evictOldest() bool {
	var oldestKey continuationKey
	var oldest time.Time
	found := false
	for key, entry := range c.entries {
		if !found || entry.touched.Before(oldest) {
			oldestKey, oldest, found = key, entry.touched, true
		}
	}
	if !found {
		return false
	}
	entry := c.entries[oldestKey]
	c.bytes -= entry.bytes
	delete(c.entries, oldestKey)
	return true
}

func (s *Server) reasoningForRequest(parsed anthropic.Parsed, candidate routing.Candidate) map[string]anthropic.ReasoningContinuation {
	if s.continuations == nil || parsed.SessionID == "" {
		return nil
	}
	var continuations map[string]anthropic.ReasoningContinuation
	for _, message := range parsed.Typed.Messages {
		if message.Role != "assistant" {
			continue
		}
		fingerprint := anthropic.ContentFingerprint(message.Content)
		if value, ok := s.continuations.Load(parsed.SessionID, candidate, fingerprint); ok {
			if continuations == nil {
				continuations = make(map[string]anthropic.ReasoningContinuation)
			}
			continuations[fingerprint] = value
		}
	}
	return continuations
}

func (s *Server) rememberReasoning(parsed anthropic.Parsed, candidate routing.Candidate, content json.RawMessage, value anthropic.ReasoningContinuation) {
	if s.continuations == nil {
		return
	}
	s.continuations.Store(parsed.SessionID, candidate, anthropic.ContentFingerprint(content), value)
}

func anthropicMessageContent(message []byte) json.RawMessage {
	var response struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(message, &response) != nil {
		return nil
	}
	return response.Content
}
