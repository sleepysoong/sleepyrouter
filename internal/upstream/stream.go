package upstream

import (
	"encoding/json"
	"strings"
)

// Precommit decides stream commit points.
// First meaningful event commits; pure metadata stays buffered.
type PrecommitConfig struct {
	MaxEvents int
	MaxBytes  int
}

// DefaultPrecommit follows spec defaults: 32 events / 64 KiB.
func DefaultPrecommit() PrecommitConfig { return PrecommitConfig{MaxEvents: 32, MaxBytes: 64 * 1024} }

// IsMeaningfulEvent reports whether a Responses SSE event justifies commit.
// Conservative: any output text/function delta, content start, or completion.
func IsMeaningfulEvent(eventType, payload string) bool {
	t := strings.TrimSpace(eventType)
	p := payload
	switch t {
	case "response.output_text.delta",
		"response.refusal.delta",
		"response.refusal.done",
		"response.function_call_arguments.delta",
		"response.output_item.added",
		"response.content_part.added",
		"response.output_text.done",
		"response.function_call_arguments.done",
		"response.output_item.done",
		"response.completed",
		"response.failed",
		"response.incomplete":
		return true
	}
	// Heuristic fallback on payload content.
	if strings.Contains(p, "\"delta\"") && (strings.Contains(p, "\"text\"") || strings.Contains(p, "\"arguments\"") || strings.Contains(p, "\"output_text\"")) {
		return true
	}
	if strings.Contains(p, "\"output\"") && strings.Contains(p, "\"type\":\"function_call\"") {
		return true
	}
	return false
}

// IsEmptyCompletion reports whether a response.completed event carries no
// user-visible output (no items, or reasoning items only). Upstreams such as
// reasoning models on Chat Completions bridges occasionally finish with only
// reasoning_content; seen before commit, this must fail over like the
// non-streaming empty-response path instead of committing an empty success.
func IsEmptyCompletion(eventType, payload string) bool {
	if strings.TrimSpace(eventType) != "response.completed" {
		return false
	}
	var ev struct {
		Response *struct {
			Output []struct {
				Type string `json:"type"`
			} `json:"output"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(payload), &ev) != nil || ev.Response == nil {
		return false
	}
	for _, item := range ev.Response.Output {
		if item.Type != "reasoning" {
			return false
		}
	}
	return true
}
