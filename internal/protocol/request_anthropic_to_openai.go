package protocol

import (
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

func anthropicMessagesToOpenAI(messages any) []map[string]any {
	msgs, ok := messages.([]any)
	if !ok {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, raw := range msgs {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := utils.StringFromUnknown(msg["role"])
		if s, ok := msg["content"].(string); ok {
			out = append(out, map[string]any{"role": role, "content": s})
			continue
		}
		rawBlocks, _ := msg["content"].([]any)
		blocks := make([]map[string]any, 0, len(rawBlocks))
		for _, b := range rawBlocks {
			if m, ok := b.(map[string]any); ok {
				blocks = append(blocks, m)
			}
		}
		var toolUses []map[string]any
		var thinkingBlocks []map[string]any
		for _, b := range blocks {
			if b["type"] == "tool_use" {
				toolUses = append(toolUses, b)
			} else if b["type"] == "thinking" {
				thinkingBlocks = append(thinkingBlocks, b)
			}
		}
		if role == "assistant" && len(toolUses) > 0 {
			var nonToolBlocks []map[string]any
			for _, b := range blocks {
				if b["type"] != "tool_use" {
					nonToolBlocks = append(nonToolBlocks, b)
				}
			}
			inheritedSig := ""
			thinkingTexts := make([]string, 0, len(thinkingBlocks))
			for _, tb := range thinkingBlocks {
				if sig := utils.StringFromUnknown(tb["signature"]); sig != "" {
					inheritedSig = sig
				} else if sig := utils.StringFromUnknown(tb["thought_signature"]); sig != "" {
					inheritedSig = sig
				}
				if t := utils.StringFromUnknown(tb["thinking"]); t != "" {
					thinkingTexts = append(thinkingTexts, t)
				} else if t := utils.StringFromUnknown(tb["text"]); t != "" {
					thinkingTexts = append(thinkingTexts, t)
				}
			}

			content := openAIContentFromBlocks(nonToolBlocks)
			contentStr, _ := content.(string)
			var contentVal any
			if contentStr != "" {
				contentVal = contentStr
			} else {
				contentVal = nil
			}
			toolCalls := make([]map[string]any, 0, len(toolUses))
			for _, tu := range toolUses {
				toolCalls = append(toolCalls, toolUseToOpenAICall(tu, inheritedSig))
			}
			msgMap := map[string]any{
				"role":       "assistant",
				"content":    contentVal,
				"tool_calls": toolCalls,
			}
			if len(thinkingTexts) > 0 {
				msgMap["reasoning_content"] = strings.Join(thinkingTexts, "\n")
			}
			out = append(out, msgMap)
			continue
		}
		var pendingContentBlocks []map[string]any
		flushContent := func() {
			content := openAIContentFromBlocks(pendingContentBlocks)
			pendingContentBlocks = nil
			if content != nil {
				out = append(out, map[string]any{"role": role, "content": content})
			}
		}
		for _, block := range blocks {
			if block["type"] == "tool_result" {
				flushContent()
				out = append(out, map[string]any{
					"role":         "tool",
					"tool_call_id": sanitizeAnthropicID(block["tool_use_id"]),
					"content":      stringifyToolResult(block["content"]),
				})
				continue
			}
			if block["type"] == "text" || block["type"] == "image" {
				pendingContentBlocks = append(pendingContentBlocks, block)
			}
		}
		flushContent()
	}
	return out
}

func toolsToOpenAI(tools any) []map[string]any {
	toolList, ok := tools.([]any)
	if !ok || len(toolList) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(toolList))
	for _, raw := range toolList {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := utils.StringFromUnknown(tool["name"])
		if name == "" {
			continue
		}
		params := tool["input_schema"]
		if params == nil {
			params = map[string]any{"type": "object"}
		}
		result = append(result, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": tool["description"],
				"parameters":  params,
			},
		})
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func toolChoiceToOpenAI(toolChoice any) any {
	tc, ok := toolChoice.(map[string]any)
	if !ok {
		return nil
	}
	tcType := utils.StringFromUnknown(tc["type"])
	if tcType == "" {
		return nil
	}
	switch tcType {
	case "none":
		return "none"
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		name := utils.StringFromUnknown(tc["name"])
		if name != "" {
			return map[string]any{"type": "function", "function": map[string]any{"name": name}}
		}
	}
	return nil
}

func systemToText(system any) any {
	if system == nil {
		return nil
	}
	if s, ok := system.(string); ok {
		if s == "" {
			return nil
		}
		return s
	}
	blocks, ok := system.([]any)
	if !ok {
		return nil
	}
	parts := make([]string, 0, len(blocks))
	for _, raw := range blocks {
		if block, ok := raw.(map[string]any); ok {
			parts = append(parts, utils.StringFromUnknown(block["text"]))
		}
	}
	result := strings.Join(filterEmpty(parts), "\n")
	if result == "" {
		return nil
	}
	return result
}

// AnthropicToOpenAI converts an Anthropic messages request to OpenAI chat format.
func AnthropicToOpenAI(body map[string]any, modelID string) map[string]any {
	messages := []map[string]any{}
	system := systemToText(body["system"])
	if system != nil {
		messages = append(messages, map[string]any{"role": "system", "content": system})
	}
	messages = append(messages, anthropicMessagesToOpenAI(body["messages"])...)

	result := map[string]any{
		"model":    modelID,
		"messages": messages,
	}
	tools := toolsToOpenAI(body["tools"])
	if tools != nil {
		result["tools"] = tools
	}
	tc := toolChoiceToOpenAI(body["tool_choice"])
	if tc != nil {
		result["tool_choice"] = tc
	}
	if tcMap, ok := body["tool_choice"].(map[string]any); ok {
		if disableParallel, ok := tcMap["disable_parallel_tool_use"].(bool); ok && disableParallel {
			result["parallel_tool_calls"] = false
		}
	}
	if v, ok := body["max_tokens"]; ok {
		if n := utils.NumberValue(v); n != nil && *n == 0 {
			// ponytail: max_tokens:0 means prewarm prompt cache in Anthropic; omit for OpenAI upstreams that may reject 0
		} else {
			result["max_tokens"] = v
		}
	}
	for _, key := range []string{"temperature", "top_p"} {
		if v, ok := body[key]; ok {
			result[key] = v
		}
	}
	stop, hasStop := body["stop"]
	stopSeq, hasStopSeq := body["stop_sequences"]
	if hasStop {
		result["stop"] = stop
	} else if hasStopSeq {
		result["stop"] = stopSeq
	}
	if stream, ok := body["stream"]; ok {
		result["stream"] = stream
	}
	// thinking (Anthropic extended thinking) maps to OpenAI reasoning_effort.
	if thinking, ok := body["thinking"].(map[string]any); ok {
		switch thinking["type"] {
		case "disabled":
			result["reasoning_effort"] = "none"
		case "enabled", "adaptive":
			// ponytail: budget_tokens doesn't map cleanly to effort levels; medium is the reasonable default
			result["reasoning_effort"] = "medium"
		}
		// Forward as-is for providers that understand Anthropic fields (OpenRouter).
		result["thinking"] = thinking
	}
	// output_config (Anthropic structured outputs) — forward for OpenRouter passthrough.
	if outputConfig, ok := body["output_config"].(map[string]any); ok {
		result["output_config"] = outputConfig
		// output_config.effort is more specific than the thinking-derived default, so it wins.
		if effort, ok := outputConfig["effort"]; ok {
			result["reasoning_effort"] = effort
		}
	}
	if metadata, ok := body["metadata"].(map[string]any); ok {
		if userID, ok := metadata["user_id"]; ok {
			result["user"] = userID
		}
		result["metadata"] = metadata
	}
	if cacheControl, ok := body["cache_control"]; ok {
		result["cache_control"] = cacheControl
	}
	return result
}
