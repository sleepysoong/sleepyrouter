package protocol

import (
	"encoding/json"
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// parseToolArguments turns OpenAI tool call arguments (already a map, or a
// JSON-encoded string) into the map form Anthropic expects.
func parseToolArguments(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	s, ok := value.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return map[string]any{}
	}
	var parsed any
	if json.Unmarshal([]byte(s), &parsed) != nil {
		return map[string]any{}
	}
	if m, ok := parsed.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// extractSignatureFromToolCall finds the reasoning signature on a tool call.
// Providers nest it differently: OpenAI puts it top-level, OpenRouter in
// extra_fields, and Google under extra_content.google (ponytail: non-standard
// nesting, matched here so signature_delta fires for Google responses).
func extractSignatureFromToolCall(tc map[string]any) string {
	if sig := utils.StringFromUnknown(tc["thought_signature"]); sig != "" {
		return sig
	}
	if sig := utils.StringFromUnknown(tc["signature"]); sig != "" {
		return sig
	}
	if ef, ok := tc["extra_fields"].(map[string]any); ok {
		if sig := utils.StringFromUnknown(ef["thought_signature"]); sig != "" {
			return sig
		}
		if sig := utils.StringFromUnknown(ef["signature"]); sig != "" {
			return sig
		}
	}
	if fn, ok := tc["function"].(map[string]any); ok {
		if sig := utils.StringFromUnknown(fn["thought_signature"]); sig != "" {
			return sig
		}
		if sig := utils.StringFromUnknown(fn["signature"]); sig != "" {
			return sig
		}
	}
	if ec, ok := tc["extra_content"].(map[string]any); ok {
		if g, ok := ec["google"].(map[string]any); ok {
			if sig := utils.StringFromUnknown(g["thought_signature"]); sig != "" {
				return sig
			}
		}
	}
	return ""
}
