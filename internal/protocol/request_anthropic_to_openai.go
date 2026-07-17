package protocol

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// ExtractTextContent extracts the textual content from an Anthropic
// "content" field, which may be either a plain string or an array of typed
// blocks. Non-text typed blocks cause a panic because the request is then
// outside the supported surface (images have a separate upstream-friendly
// conversion path; see openAIContentFromBlocks).
func ExtractTextContent(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	blocks, ok := content.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if s, ok := block.(string); ok {
			parts = append(parts, s)
			continue
		}
		if m, ok := block.(map[string]any); ok {
			if m["type"] == "text" {
				parts = append(parts, utils.StringFromUnknown(m["text"]))
				continue
			}
			typeName := utils.UnknownString(m["type"])
			if typeName == "" {
				typeName = "unknown"
			}
			panic(fmt.Errorf("지원하지 않는 Anthropic 콘텐츠 블록이에요: %s", typeName))
		}
		panic(fmt.Errorf("지원하지 않는 Anthropic 콘텐츠 블록이에요: %s", "unknown"))
	}
	return strings.Join(filterEmpty(parts), "\n")
}

func filterEmpty(items []string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

var unsafeCharPattern = regexp.MustCompile("[\x00-\x1f\x7f]")

func sanitizeAnthropicID(value any) string {
	fallback := fmt.Sprintf("toolu_%d", time.Now().UnixMilli())
	raw := fallback
	if s, ok := value.(string); ok && s != "" {
		raw = s
	}
	sanitized := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, raw)
	if sanitized == "" {
		return fallback
	}
	return sanitized
}

func imageUrlFromAnthropic(block map[string]any) string {
	source, ok := block["source"].(map[string]any)
	if !ok {
		return ""
	}
	if source["type"] == "url" {
		return utils.StringFromUnknown(source["url"])
	}
	if source["type"] == "base64" {
		mediaType := utils.StringFromUnknown(source["media_type"])
		data := utils.StringFromUnknown(source["data"])
		if mediaType != "" && data != "" {
			return "data:" + mediaType + ";base64," + data
		}
	}
	return ""
}

func openAIContentFromBlocks(blocks []map[string]any) any {
	parts := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		if block["type"] == "text" {
			text := utils.StringFromUnknown(block["text"])
			if text != "" {
				parts = append(parts, map[string]any{"type": "text", "text": text})
			}
			continue
		}
		if block["type"] == "image" {
			url := imageUrlFromAnthropic(block)
			if url != "" {
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
			}
		}
	}
	if len(parts) == 0 {
		return nil
	}
	allText := true
	for _, part := range parts {
		if part["type"] != "text" {
			allText = false
			break
		}
	}
	if allText {
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			texts = append(texts, utils.StringFromUnknown(part["text"]))
		}
		return strings.Join(texts, "\n")
	}
	return parts
}

func stringifyToolResult(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	blocks, ok := content.([]any)
	if !ok {
		return utils.UnknownString(content)
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if s, ok := block.(string); ok {
			parts = append(parts, s)
			continue
		}
		if m, ok := block.(map[string]any); ok && m["type"] == "text" {
			parts = append(parts, utils.StringFromUnknown(m["text"]))
			continue
		}
		data, _ := json.Marshal(block)
		parts = append(parts, string(data))
	}
	return strings.Join(parts, "\n")
}

func toolUseToOpenAICall(block map[string]any) map[string]any {
	input := block["input"]
	if input == nil {
		input = map[string]any{}
	}
	data, _ := json.Marshal(input)
	return map[string]any{
		"id":   sanitizeAnthropicID(block["id"]),
		"type": "function",
		"function": map[string]any{
			"name":      utils.StringFromUnknown(block["name"]),
			"arguments": string(data),
		},
	}
}

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
		for _, b := range blocks {
			if b["type"] == "tool_use" {
				toolUses = append(toolUses, b)
			}
		}
		if role == "assistant" && len(toolUses) > 0 {
			var nonToolBlocks []map[string]any
			for _, b := range blocks {
				if b["type"] != "tool_use" {
					nonToolBlocks = append(nonToolBlocks, b)
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
				toolCalls = append(toolCalls, toolUseToOpenAICall(tu))
			}
			out = append(out, map[string]any{
				"role":       "assistant",
				"content":    contentVal,
				"tool_calls": toolCalls,
			})
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
	for _, key := range []string{"max_tokens", "temperature", "top_p"} {
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
	return result
}
