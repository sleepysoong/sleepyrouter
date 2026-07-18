package protocol

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

var unsafeCharPattern = regexp.MustCompile("[\x00-\x1f\x7f]")

// ExtractTextContent extracts the textual content from an Anthropic
// "content" field, which may be either a plain string or an array of typed
// blocks.
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
