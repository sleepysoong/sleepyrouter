package anthropic

import (
	"encoding/json"
	"strings"
)

// ToResponses converts an Anthropic request to a Responses API raw body.
// All tool IDs and JSON schemas are preserved byte-for-byte.
func ToResponses(p Parsed, upstreamModel string) ([]byte, error) {
	instructions := systemToInstructions(p.Typed.System)
	inputItems, err := messagesToInput(p.Typed.Messages)
	if err != nil {
		return nil, err
	}
	tools, err := toolsToResponses(p.Typed.Tools)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"model": upstreamModel,
		"input": inputItems,
	}
	if instructions != "" {
		body["instructions"] = instructions
	}
	if len(tools) > 0 {
		body["tools"] = tools
		if tc := toolChoiceToResponses(p.Typed.ToolChoice); tc != nil {
			body["tool_choice"] = tc
		}
	}
	if p.Typed.MaxTokens > 0 {
		body["max_output_tokens"] = p.Typed.MaxTokens
	}
	if p.Typed.Temperature != nil {
		body["temperature"] = *p.Typed.Temperature
	}
	if p.Typed.TopP != nil {
		body["top_p"] = *p.Typed.TopP
	}
	if len(p.Typed.Metadata) > 0 && string(p.Typed.Metadata) != "null" {
		var md map[string]any
		if err := json.Unmarshal(p.Typed.Metadata, &md); err == nil {
			body["metadata"] = md
		}
	}
	body["stream"] = false // upstream streaming is controlled by caller transport
	return json.Marshal(body)
}

func systemToInstructions(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "\"") {
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			return str
		}
		return ""
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func messagesToInput(msgs []Message) ([]any, error) {
	var out []any
	for _, m := range msgs {
		s := strings.TrimSpace(string(m.Content))
		if strings.HasPrefix(s, "\"") {
			var str string
			if err := json.Unmarshal(m.Content, &str); err != nil {
				return nil, err
			}
			out = append(out, map[string]any{
				"type": "message", "role": m.Role,
				"content": []any{map[string]any{"type": "input_text", "text": str}},
			})
			continue
		}
		var blocks []ContentBlock
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			return nil, err
		}
		for _, b := range blocks {
			switch b.Type {
			case "text":
				out = append(out, map[string]any{
					"type": "message", "role": m.Role,
					"content": []any{map[string]any{"type": "input_text", "text": b.Text}},
				})
			case "image":
				// Preserve source as image input; vision requirement already set.
				var imgURL string
				var detail string
				if len(b.Source) > 0 {
					var src struct {
						Type      string `json:"type"`
						MediaType string `json:"media_type"`
						Data      string `json:"data"`
						URL       string `json:"url"`
					}
					if err := json.Unmarshal(b.Source, &src); err == nil {
						if src.Type == "base64" && src.Data != "" {
							mt := src.MediaType
							if mt == "" {
								mt = "image/png"
							}
							imgURL = "data:" + mt + ";base64," + src.Data
						} else if src.URL != "" {
							imgURL = src.URL
						}
					}
					detail = string(b.Source)
				}
				_ = detail
				content := map[string]any{"type": "input_image"}
				if imgURL != "" {
					content["image_url"] = imgURL
				} else if len(b.Source) > 0 {
					content["source"] = json.RawMessage(b.Source)
				}
				out = append(out, map[string]any{
					"type": "message", "role": m.Role, "content": []any{content},
				})
			case "tool_use":
				var args string
				if len(b.Input) > 0 && string(b.Input) != "null" {
					args = string(b.Input)
				} else {
					args = "{}"
				}
				out = append(out, map[string]any{
					"type": "function_call", "call_id": b.ID, "name": b.Name, "arguments": args,
				})
			case "tool_result":
				var output string
				cs := strings.TrimSpace(string(b.Content))
				if cs == "" || cs == "null" {
					output = ""
				} else if strings.HasPrefix(cs, "\"") {
					var str string
					if err := json.Unmarshal(b.Content, &str); err == nil {
						output = str
					} else {
						output = cs
					}
				} else {
					// Block list or object: preserve as JSON string without re-encoding numbers.
					output = cs
				}
				out = append(out, map[string]any{
					"type": "function_call_output", "call_id": b.ToolUseID, "output": output,
				})
			case "thinking", "redacted_thinking":
				// No direct upstream equivalent; preserve order via ignored marker is wrong,
				// so skip (documented best-effort). Do not fail the request.
			}
		}
	}
	if out == nil {
		out = []any{}
	}
	return out, nil
}

func toolsToResponses(tools []Tool) ([]any, error) {
	var out []any
	for _, t := range tools {
		params := json.RawMessage(`{"type":"object"}`)
		if len(t.InputSchema) > 0 && string(t.InputSchema) != "null" {
			params = t.InputSchema // preserved byte-for-byte
		}
		out = append(out, map[string]any{
			"type": "function", "name": t.Name,
			"description": t.Description, "parameters": params,
		})
	}
	return out, nil
}

func toolChoiceToResponses(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "\"") {
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			switch str {
			case "auto", "none":
				return str
			}
		}
		return nil
	}
	var v struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	switch v.Type {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "none":
		return "none"
	case "tool":
		if v.Name != "" {
			return map[string]any{"type": "function", "name": v.Name}
		}
		return "auto"
	default:
		return nil
	}
}
