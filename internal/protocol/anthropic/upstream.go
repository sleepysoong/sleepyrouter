package anthropic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// ReasoningContinuation is private gateway state used to continue a provider's
// reasoning across stateless Anthropic Messages requests. Neither field is
// included in the Anthropic response.
type ReasoningContinuation struct {
	ChatCompletions string
	ResponsesItems  []json.RawMessage
}

// ToResponses converts an Anthropic request to a Responses API raw body.
// All tool IDs and JSON schemas are preserved byte-for-byte.
func ToResponses(p Parsed, upstreamModel string) ([]byte, error) {
	return ToResponsesWithContinuations(p, upstreamModel, nil, false)
}

// ToResponsesWithContinuations adds provider-private reasoning continuation
// data for prior assistant turns. Chat Completions continuations are encoded as
// a temporary internal marker and removed by the Chat Completions adapter;
// Responses continuations are inserted as their original reasoning items.
func ToResponsesWithContinuations(p Parsed, upstreamModel string, continuations map[string]ReasoningContinuation, chatCompletions bool) ([]byte, error) {
	instructions := systemToInstructions(p.Typed.System)
	inputItems, err := messagesToInput(p.Typed.Messages, continuations, chatCompletions)
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
	if effort := thinkingToResponsesEffort(p.Typed.Thinking); effort != "" {
		body["reasoning"] = map[string]any{"effort": effort}
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

// thinkingToResponsesEffort keeps Anthropic thinking-enabled requests on a
// reasoning-capable Responses path. Responses exposes effort levels, not
// Anthropic's separate thinking-token budget, so the budget mapping is an
// explicit approximation; output_config.effort, when present, overrides it.
func thinkingToResponsesEffort(raw json.RawMessage) string {
	if len(raw) == 0 || isJSONNull(raw) {
		return ""
	}
	var thinking struct {
		Type         string `json:"type"`
		BudgetTokens int    `json:"budget_tokens"`
	}
	if json.Unmarshal(raw, &thinking) != nil {
		return ""
	}
	switch thinking.Type {
	case "adaptive":
		// Responses has no adaptive mode; medium is the bridge's neutral,
		// reasoning-enabled default when the caller omitted output_config.effort.
		return "medium"
	case "enabled":
		switch {
		case thinking.BudgetTokens <= 4096:
			return "low"
		case thinking.BudgetTokens <= 8192:
			return "medium"
		case thinking.BudgetTokens <= 32768:
			return "high"
		default:
			return "xhigh"
		}
	default:
		return ""
	}
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

func messagesToInput(msgs []Message, continuations map[string]ReasoningContinuation, chatCompletions bool) ([]any, error) {
	var out []any
	for _, m := range msgs {
		var continuation ReasoningContinuation
		if m.Role == "assistant" {
			continuation = continuations[ContentFingerprint(m.Content)]
			if !chatCompletions {
				for _, item := range continuation.ResponsesItems {
					if json.Valid(item) {
						out = append(out, json.RawMessage(item))
					}
				}
			}
		}
		s := strings.TrimSpace(string(m.Content))
		if strings.HasPrefix(s, "\"") {
			var str string
			if err := json.Unmarshal(m.Content, &str); err != nil {
				return nil, err
			}
			if m.Role == "assistant" || m.Role == "system" {
				out = append(out, map[string]any{"type": "message", "role": m.Role, "content": str})
				appendChatReasoningMarker(&out, m.Role, continuation, chatCompletions)
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
		appendChatReasoningMarker(&out, m.Role, continuation, chatCompletions)
	}
	if out == nil {
		out = []any{}
	}
	return out, nil
}

func appendChatReasoningMarker(out *[]any, role string, continuation ReasoningContinuation, chatCompletions bool) {
	if role != "assistant" || !chatCompletions || continuation.ChatCompletions == "" {
		return
	}
	*out = append(*out, map[string]any{
		"type": "chat_reasoning", "content": continuation.ChatCompletions,
	})
}

// ContentFingerprint returns a stable digest for Anthropic assistant content.
// Both client history and gateway-generated response bodies are decoded into
// the same typed block representation before hashing, so omitted zero fields
// do not create different keys.
func ContentFingerprint(raw json.RawMessage) string {
	if len(raw) == 0 || isJSONNull(raw) {
		return ""
	}
	var canonical []byte
	var text string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &text) != nil {
			return ""
		}
		canonical, _ = json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "string", Text: text})
	} else {
		var blocks []ContentBlock
		if json.Unmarshal(raw, &blocks) != nil || blocks == nil {
			return ""
		}
		canonical, _ = json.Marshal(blocks)
	}
	if len(canonical) == 0 {
		return ""
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
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
	// Tool-result text blocks are plain text payloads. Keeping them as Responses
	// input_text parts causes the Chat Completions adapter to JSON-encode the
	// parts array into the tool message's content string, so downstream tools
	// receive JSON syntax instead of their actual result text.
	allText := true
	for _, nested := range blocks {
		if nested.Type != "text" {
			allText = false
			break
		}
	}
	if allText {
		var result strings.Builder
		result.WriteString(prefix)
		for _, nested := range blocks {
			result.WriteString(nested.Text)
		}
		return result.String(), nil
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
