package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// ExtractTextContent extracts the textual content from an Anthropic
// "content" field, which may be either a plain string or an array of typed
// blocks. Unsupported block types produce an error rather than a panic.
func ExtractTextContent(content any) (string, error) {
	if content == nil {
		return "", nil
	}
	if text, ok := content.(string); ok {
		return text, nil
	}
	blocks, ok := content.([]any)
	if !ok {
		return "", nil
	}
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if s, ok := block.(string); ok {
			parts = append(parts, s)
			continue
		}
		m, ok := block.(map[string]any)
		if !ok {
			return "", fmt.Errorf("unsupported Anthropic content block: %s", "unknown")
		}
		switch m["type"] {
		case "text":
			parts = append(parts, utils.StringFromUnknown(m["text"]))
		case "thinking":
			t := utils.StringFromUnknown(m["thinking"])
			if t == "" {
				t = utils.StringFromUnknown(m["text"])
			}
			if t != "" {
				parts = append(parts, t)
			}
		case "redacted_thinking":
			// Skip redacted thinking blocks; there is no usable text.
		default:
			typeName := utils.UnknownString(m["type"])
			if typeName == "" {
				typeName = "unknown"
			}
			return "", fmt.Errorf("unsupported Anthropic content block: %s", typeName)
		}
	}
	return strings.Join(filterEmpty(parts), "\n"), nil
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
				part := map[string]any{"type": "text", "text": text}
				if cc, ok := block["cache_control"]; ok {
					part["cache_control"] = cc
				}
				parts = append(parts, part)
			}
			continue
		}
		if block["type"] == "image" {
			url := imageUrlFromAnthropic(block)
			if url != "" {
				part := map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}}
				if cc, ok := block["cache_control"]; ok {
					part["cache_control"] = cc
				}
				parts = append(parts, part)
			}
			continue
		}
		if block["type"] == "thinking" || block["type"] == "redacted_thinking" {
			continue
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

func toolUseToOpenAICall(block map[string]any, inheritedSig string) map[string]any {
	input := block["input"]
	if input == nil {
		input = map[string]any{}
	}
	data, _ := json.Marshal(input)

	sig := utils.StringFromUnknown(block["thought_signature"])
	if sig == "" {
		sig = utils.StringFromUnknown(block["signature"])
	}
	if sig == "" {
		if ef, ok := block["extra_fields"].(map[string]any); ok {
			sig = utils.StringFromUnknown(ef["thought_signature"])
			if sig == "" {
				sig = utils.StringFromUnknown(ef["signature"])
			}
		}
	}
	if sig == "" {
		sig = inheritedSig
	}

	fnMap := map[string]any{
		"name":      utils.StringFromUnknown(block["name"]),
		"arguments": string(data),
	}
	if sig != "" {
		fnMap["thought_signature"] = sig
	}

	tc := map[string]any{
		"id":       sanitizeAnthropicID(block["id"]),
		"type":     "function",
		"function": fnMap,
	}
	if sig != "" {
		tc["thought_signature"] = sig
		tc["extra_fields"] = map[string]any{"thought_signature": sig}
	} else if ef, ok := block["extra_fields"].(map[string]any); ok && len(ef) > 0 {
		tc["extra_fields"] = ef
	}
	return tc
}
