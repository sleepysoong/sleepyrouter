package protocol

import (
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

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
