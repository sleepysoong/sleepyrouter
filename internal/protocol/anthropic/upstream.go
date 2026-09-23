package anthropic

import (
	"encoding/json"
	"fmt"
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
	if disableParallelToolCalls(p.Typed.ToolChoice) {
		body["parallel_tool_calls"] = false
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
	outputConfig, err := outputConfigToResponses(p.Typed.OutputConfig)
	if err != nil {
		return nil, err
	}
	for key, value := range outputConfig {
		body[key] = value
	}
	if len(p.Typed.Metadata) > 0 && !isJSONNull(p.Typed.Metadata) {
		var metadata struct {
			UserID *string `json:"user_id"`
		}
		if err := json.Unmarshal(p.Typed.Metadata, &metadata); err == nil && metadata.UserID != nil {
			body["metadata"] = map[string]string{"user_id": *metadata.UserID}
		}
	}
	body["stream"] = false // upstream streaming is controlled by caller transport
	return json.Marshal(body)
}

func systemToInstructions(raw json.RawMessage) string {
	if len(raw) == 0 || isJSONNull(raw) {
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
			if m.Role == "assistant" || m.Role == "system" {
				out = append(out, map[string]any{"type": "message", "role": m.Role, "content": str})
				continue
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
		var content []any
		var text strings.Builder
		flushMessage := func() {
			if m.Role == "assistant" || m.Role == "system" {
				if text.Len() > 0 {
					out = append(out, map[string]any{"type": "message", "role": m.Role, "content": text.String()})
					text.Reset()
				}
				return
			}
			if len(content) > 0 {
				out = append(out, map[string]any{"type": "message", "role": m.Role, "content": content})
				content = nil
			}
		}
		for _, b := range blocks {
			switch b.Type {
			case "text":
				if m.Role == "assistant" || m.Role == "system" {
					text.WriteString(b.Text)
				} else {
					content = append(content, map[string]any{"type": "input_text", "text": b.Text})
				}
			case "image":
				converted, err := imageToResponsesContent(b.Source)
				if err != nil {
					return nil, fmt.Errorf("image block: %w", err)
				}
				content = append(content, converted)
			case "document":
				converted, err := documentToResponsesContent(b.Source, b.Title)
				if err != nil {
					return nil, fmt.Errorf("document block: %w", err)
				}
				content = append(content, converted)
			case "tool_use":
				flushMessage()
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
				flushMessage()
				output, err := toolResultToResponsesOutput(b)
				if err != nil {
					return nil, fmt.Errorf("tool_result %q: %w", b.ToolUseID, err)
				}
				out = append(out, map[string]any{
					"type": "function_call_output", "call_id": b.ToolUseID, "output": output,
				})
			case "thinking", "redacted_thinking":
				// No direct upstream equivalent; preserve order via ignored marker is wrong,
				// so skip (documented best-effort). Do not fail the request.
			}
		}
		flushMessage()
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
		converted := map[string]any{
			"type": "function", "name": t.Name,
			"description": t.Description, "parameters": params,
		}
		if t.Strict != nil {
			converted["strict"] = *t.Strict
		}
		out = append(out, converted)
	}
	return out, nil
}

func toolChoiceToResponses(raw json.RawMessage) any {
	if len(raw) == 0 || isJSONNull(raw) {
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

func disableParallelToolCalls(raw json.RawMessage) bool {
	var v struct {
		DisableParallelToolUse bool `json:"disable_parallel_tool_use"`
	}
	return json.Unmarshal(raw, &v) == nil && v.DisableParallelToolUse
}

func outputConfigToResponses(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	var config struct {
		Effort *string `json:"effort"`
		Format *struct {
			Type   string          `json:"type"`
			Schema json.RawMessage `json:"schema"`
		} `json:"format"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("output_config: %w", err)
	}
	out := map[string]any{}
	if config.Effort != nil {
		out["reasoning"] = map[string]any{"effort": *config.Effort}
	}
	if config.Format != nil {
		out["text"] = map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "anthropic_output",
			"schema": config.Format.Schema, "strict": true,
		}}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func imageToResponsesContent(raw json.RawMessage) (map[string]any, error) {
	var src struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
		URL       string `json:"url"`
	}
	if err := json.Unmarshal(raw, &src); err != nil {
		return nil, fmt.Errorf("invalid source: %w", err)
	}
	switch src.Type {
	case "base64":
		if src.MediaType == "" || src.Data == "" {
			return nil, fmt.Errorf("base64 source requires media_type and data")
		}
		return map[string]any{"type": "input_image", "image_url": "data:" + src.MediaType + ";base64," + src.Data}, nil
	case "url":
		if src.URL == "" {
			return nil, fmt.Errorf("url source requires url")
		}
		return map[string]any{"type": "input_image", "image_url": src.URL}, nil
	default:
		return nil, fmt.Errorf("unsupported image source type %q", src.Type)
	}
}

func documentToResponsesContent(raw json.RawMessage, title string) (map[string]any, error) {
	var src struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
		URL       string `json:"url"`
	}
	if err := json.Unmarshal(raw, &src); err != nil {
		return nil, fmt.Errorf("invalid source: %w", err)
	}
	switch src.Type {
	case "text":
		if src.Data == "" {
			return nil, fmt.Errorf("text source requires data")
		}
		return map[string]any{"type": "input_text", "text": src.Data}, nil
	case "url":
		if src.URL == "" {
			return nil, fmt.Errorf("url source requires url")
		}
		return map[string]any{"type": "input_file", "file_url": src.URL}, nil
	case "base64":
		if src.MediaType == "" || src.Data == "" {
			return nil, fmt.Errorf("base64 source requires media_type and data")
		}
		if title == "" {
			title = "document"
			switch src.MediaType {
			case "application/pdf":
				title += ".pdf"
			case "text/plain":
				title += ".txt"
			}
		}
		return map[string]any{"type": "input_file", "filename": title, "file_data": "data:" + src.MediaType + ";base64," + src.Data}, nil
	default:
		return nil, fmt.Errorf("unsupported document source type %q", src.Type)
	}
}

func toolResultToResponsesOutput(block ContentBlock) (any, error) {
	content := strings.TrimSpace(string(block.Content))
	prefix := ""
	if block.IsError {
		prefix = "[Tool execution failed] "
	}
	if content == "" || content == "null" {
		return prefix, nil
	}
	if strings.HasPrefix(content, "\"") {
		var text string
		if err := json.Unmarshal(block.Content, &text); err != nil {
			return nil, err
		}
		return prefix + text, nil
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(block.Content, &blocks); err != nil {
		return prefix + content, nil
	}
	out := make([]any, 0, len(blocks)+1)
	if prefix != "" {
		out = append(out, map[string]any{"type": "input_text", "text": strings.TrimSpace(prefix)})
	}
	for _, nested := range blocks {
		switch nested.Type {
		case "text":
			out = append(out, map[string]any{"type": "input_text", "text": nested.Text})
		case "image":
			converted, err := imageToResponsesContent(nested.Source)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		case "document":
			converted, err := documentToResponsesContent(nested.Source, nested.Title)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		default:
			return nil, fmt.Errorf("unsupported nested content type %q", nested.Type)
		}
	}
	if len(out) == 0 {
		return "", nil
	}
	return out, nil
}
