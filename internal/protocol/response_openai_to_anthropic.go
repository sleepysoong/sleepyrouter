package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// MapStopReason translates an OpenAI finish_reason into the matching
// Anthropic stop_reason. Unknown values collapse to "end_turn" so the caller
// receives a valid enum even when the upstream provider invents its own
// reasoning string.
func MapStopReason(reason any) string {
	s := utils.UnknownString(reason)
	switch s {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "refusal"
	// Anthropic-shaped stop reasons that some providers (e.g. OpenRouter)
	// pass through unchanged in an OpenAI-style response.
	case "pause_turn":
		return "pause_turn"
	case "model_context_window_exceeded":
		return "model_context_window_exceeded"
	default:
		return "end_turn"
	}
}

func contentFromOpenAI(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	parts, ok := content.([]any)
	if !ok {
		return ""
	}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if s, ok := part.(string); ok {
			result = append(result, s)
			continue
		}
		if m, ok := part.(map[string]any); ok {
			if m["type"] == "text" {
				result = append(result, utils.StringFromUnknown(m["text"]))
				continue
			}
			if t, ok := m["text"].(string); ok {
				result = append(result, t)
			}
		}
	}
	return strings.Join(filterEmpty(result), "\n")
}

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
	return ""
}

// OpenAIToAnthropic converts an OpenAI chat completion response to Anthropic
// message format. The fallbackModel is used when the upstream response
// omitted the model field.
func OpenAIToAnthropic(response map[string]any, fallbackModel string) map[string]any {
	choices, _ := response["choices"].([]any)
	var choice map[string]any
	if len(choices) > 0 {
		choice, _ = choices[0].(map[string]any)
	}
	if choice == nil {
		choice = map[string]any{}
	}
	message, _ := choice["message"].(map[string]any)
	if message == nil {
		message = map[string]any{}
	}

	contentVal := message["content"]
	if contentVal == nil {
		contentVal = choice["text"]
	}
	if contentVal == nil {
		contentVal = message["refusal"]
	}
	if contentVal == nil {
		contentVal = ""
	}
	content := contentFromOpenAI(contentVal)

	reasoningText := utils.StringFromUnknown(message["reasoning_content"])
	if reasoningText == "" {
		reasoningText = utils.StringFromUnknown(message["reasoning"])
	}
	if reasoningText == "" {
		reasoningText = utils.StringFromUnknown(message["thinking"])
	}
	if reasoningText == "" {
		reasoningText = utils.StringFromUnknown(message["thought"])
	}

	msgSig := utils.StringFromUnknown(message["thought_signature"])
	if msgSig == "" {
		msgSig = utils.StringFromUnknown(message["signature"])
	}
	if msgSig == "" {
		if ef, ok := message["extra_fields"].(map[string]any); ok {
			msgSig = utils.StringFromUnknown(ef["thought_signature"])
		}
	}

	toolCalls, _ := message["tool_calls"].([]any)
	allToolCalls := toolCalls
	if fc, ok := message["function_call"].(map[string]any); ok {
		allToolCalls = append(append([]any{}, toolCalls...), map[string]any{
			"id":       fmt.Sprintf("toolu_%d", time.Now().UnixMilli()),
			"type":     "function",
			"function": fc,
		})
	}

	if msgSig == "" {
		for _, raw := range allToolCalls {
			if tc, ok := raw.(map[string]any); ok {
				if sig := extractSignatureFromToolCall(tc); sig != "" {
					msgSig = sig
					break
				}
			}
		}
	}

	blocks := []map[string]any{}
	if reasoningText != "" || msgSig != "" {
		tb := map[string]any{
			"type":     "thinking",
			"thinking": reasoningText,
		}
		if msgSig != "" {
			tb["signature"] = msgSig
		}
		blocks = append(blocks, tb)
	}

	if content != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": content})
	}

	for _, raw := range allToolCalls {
		tc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		tcType := utils.StringFromUnknown(tc["type"])
		if tcType != "" && tcType != "function" {
			continue
		}
		fn, _ := tc["function"].(map[string]any)
		if fn == nil {
			fn = map[string]any{}
		}

		sig := extractSignatureFromToolCall(tc)
		if sig == "" {
			sig = msgSig
		}

		tu := map[string]any{
			"type":  "tool_use",
			"id":    sanitizeAnthropicID(tc["id"]),
			"name":  utils.StringFromUnknown(fn["name"]),
			"input": parseToolArguments(fn["arguments"]),
		}
		if sig != "" {
			tu["thought_signature"] = sig
			tu["signature"] = sig
			tu["extra_fields"] = map[string]any{"thought_signature": sig}
		} else if ef, ok := tc["extra_fields"].(map[string]any); ok && len(ef) > 0 {
			tu["extra_fields"] = ef
		}
		blocks = append(blocks, tu)
	}

	if len(blocks) == 0 {
		blocks = []map[string]any{{"type": "text", "text": ""}}
	}

	id := response["id"]
	idStr, ok := id.(string)
	if !ok {
		idStr = fmt.Sprintf("msg_%d", time.Now().UnixMilli())
	} else if strings.HasPrefix(idStr, "chatcmpl") {
		idStr = strings.Replace(idStr, "chatcmpl", "msg", 1)
	}

	model := response["model"]
	if model == nil {
		model = fallbackModel
	}

	usage, _ := response["usage"].(map[string]any)
	inputTokens := 0
	outputTokens := 0
	if v := utils.NumberValue(usage["prompt_tokens"]); v != nil {
		inputTokens = *v
	} else if v := utils.NumberValue(usage["input_tokens"]); v != nil {
		inputTokens = *v
	}
	if v := utils.NumberValue(usage["completion_tokens"]); v != nil {
		outputTokens = *v
	} else if v := utils.NumberValue(usage["output_tokens"]); v != nil {
		outputTokens = *v
	}

	usageMap := map[string]any{
		"input_tokens":  inputTokens,
		"output_tokens": outputTokens,
	}
	if ctd, ok := usage["completion_tokens_details"].(map[string]any); ok {
		if rt := utils.NumberValue(ctd["reasoning_tokens"]); rt != nil {
			usageMap["output_tokens_details"] = map[string]any{"thinking_tokens": *rt}
		}
	}

	stopReason := MapStopReason(choice["finish_reason"])

	// Some providers (e.g. OpenRouter) pass Anthropic-shaped fields through
	// in an OpenAI-style response; forward stop_details when present.
	stopDetails, _ := response["stop_details"].(map[string]any)
	if stopDetails == nil {
		stopDetails, _ = choice["stop_details"].(map[string]any)
	}
	if stopReason == "refusal" && stopDetails == nil {
		if refusalText := utils.StringFromUnknown(message["refusal"]); refusalText != "" {
			stopDetails = map[string]any{"type": "refusal", "refusal": refusalText}
		}
	}

	result := map[string]any{
		"id":            idStr,
		"type":          "message",
		"role":          "assistant",
		"content":       blocks,
		"model":         model,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage":         usageMap,
	}
	if stopDetails != nil {
		result["stop_details"] = stopDetails
	}
	return result
}
