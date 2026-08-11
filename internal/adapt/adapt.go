// Package adapt converts sleepyrouter's upstream request bodies (both the
// OpenAI-compatible and Anthropic wire formats) into GoAI GenerateParams.
// The GoAI model serializes those params back to the upstream wire, so the
// adapter's only job is round-trip fidelity.
package adapt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zendev-sh/goai/provider"
)

// OpenAIRequest converts an OpenAI-format request body (as received from the
// handler) into GoAI generate params for an OpenAI-protocol model.
func OpenAIRequest(ctx context.Context, body map[string]any, modelID string) (provider.GenerateParams, error) {
	params := baseParams(body)

	if v, ok := body["temperature"].(float64); ok {
		params.Temperature = &v
	}
	if v, ok := body["top_p"].(float64); ok {
		params.TopP = &v
	}
	if v, ok := body["top_k"].(float64); ok {
		k := int(v)
		params.TopK = &k
	}
	if v, ok := body["frequency_penalty"].(float64); ok {
		params.FrequencyPenalty = &v
	}
	if v, ok := body["presence_penalty"].(float64); ok {
		params.PresencePenalty = &v
	}
	if v, ok := body["seed"].(float64); ok {
		seed := int(v)
		params.Seed = &seed
	}
	if v, ok := body["stop"].([]any); ok {
		for _, s := range v {
			if str, ok := s.(string); ok {
				params.StopSequences = append(params.StopSequences, str)
			}
		}
	}
	if v, ok := body["tools"].([]any); ok {
		params.Tools = openAITools(v)
	}
	if v, ok := body["tool_choice"]; ok {
		params.ToolChoice = toolChoiceString(v)
	}
	if v, ok := body["response_format"]; ok {
		params.ResponseFormat = openAIResponseFormat(v)
	}

	msgs, sys, err := openAIMessages(body["messages"])
	if err != nil {
		return params, err
	}
	params.Messages = msgs
	params.System = sys

	// Every remaining body key rides along in ProviderOptions, which the
	// openai-compat request builder forwards verbatim to the wire
	// (reasoning_effort, service_tier, user, unknown provider fields, ...).
	for k, v := range body {
		switch k {
		case "model", "stream", "stream_options", "messages", "system", "temperature",
			"top_p", "top_k", "frequency_penalty", "presence_penalty", "seed", "stop",
			"tools", "tool_choice", "response_format":
			continue
		}
		switch k {
		case "max_tokens", "max_completion_tokens", "max_output_tokens":
			if f, ok := v.(float64); ok && f > 0 {
				params.MaxOutputTokens = int(f)
			}
		default:
			if params.ProviderOptions == nil {
				params.ProviderOptions = map[string]any{}
			}
			params.ProviderOptions[k] = v
		}
	}
	return params, nil
}

// AnthropicRequest converts an Anthropic-format request body (Claude wire
// format as received by the handler) into generate params for a GoAI
// anthropic-protocol model.
func AnthropicRequest(ctx context.Context, body map[string]any, modelID string) (provider.GenerateParams, error) {
	params := baseParams(body)

	if v, ok := body["temperature"].(float64); ok {
		params.Temperature = &v
	}
	if v, ok := body["top_p"].(float64); ok {
		params.TopP = &v
	}
	if v, ok := body["top_k"].(float64); ok {
		k := int(v)
		params.TopK = &k
	}
	if v, ok := body["stop_sequences"].([]any); ok {
		for _, s := range v {
			if str, ok := s.(string); ok {
				params.StopSequences = append(params.StopSequences, str)
			}
		}
	}
	if v, ok := body["tools"].([]any); ok {
		params.Tools = anthropicTools(v)
	}
	if v, ok := body["tool_choice"]; ok {
		params.ToolChoice = toolChoiceString(v)
	}
	if v, ok := body["thinking"].(map[string]any); ok {
		params.ProviderOptions["thinking"] = v
	}

	switch sys := body["system"].(type) {
	case string:
		params.System = sys
	case []any:
		var b strings.Builder
		for _, c := range sys {
			if blk, ok := c.(map[string]any); ok {
				if text, ok := blk["text"].(string); ok {
					b.WriteString(text)
				}
			}
		}
		params.System = b.String()
	}

	msgs, err := anthropicMessages(body["messages"])
	if err != nil {
		return params, err
	}
	params.Messages = msgs

	for k, v := range body {
		switch k {
		case "model", "stream", "messages", "system", "temperature", "top_p", "top_k",
			"stop_sequences", "tools", "tool_choice", "thinking":
			continue
		case "max_tokens":
			if f, ok := v.(float64); ok && f > 0 {
				params.MaxOutputTokens = int(f)
			}
		default:
			if params.ProviderOptions == nil {
				params.ProviderOptions = map[string]any{}
			}
			params.ProviderOptions[k] = v
		}
	}
	return params, nil
}

func baseParams(body map[string]any) provider.GenerateParams {
	params := provider.GenerateParams{
		MaxOutputTokens: 0,
		ProviderOptions: map[string]any{},
	}
	if v, ok := body["max_tokens"].(float64); ok && v > 0 {
		params.MaxOutputTokens = int(v)
	}
	return params
}

// asMessageSlice normalizes the "messages" field of a request body, which
// may arrive as []any (freshly JSON-unmarshaled) or []map[string]any (built
// by the handler's protocol converters, e.g. AnthropicToOpenAI).
func asMessageSlice(raw any) ([]map[string]any, error) {
	switch v := raw.(type) {
	case []any:
		items := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				items = append(items, m)
			}
		}
		return items, nil
	case []map[string]any:
		return v, nil
	}
	return nil, fmt.Errorf("요청에 messages 배열이 없어요")
}

// openAIMessages converts the OpenAI "messages" array into GoAI messages,
// honoring the extended wire fields the handler guarantees: assistant
// reasoning_content, tool_calls, tool role messages, and image content.
func openAIMessages(raw any) ([]provider.Message, string, error) {
	items, err := asMessageSlice(raw)
	if err != nil || len(items) == 0 {
		return nil, "", fmt.Errorf("요청에 messages 배열이 없어요")
	}
	var out []provider.Message
	for _, m := range items {
		role, _ := m["role"].(string)
		switch role {
		case "system":
			text := messageText(m["content"])
			if text == "" {
				continue
			}
			out = append(out, provider.Message{
				Role:    provider.RoleSystem,
				Content: []provider.Part{{Type: provider.PartText, Text: text}},
			})
		case "user":
			content := messageUserContent(m["content"])
			if len(content) == 0 {
				continue
			}
			out = append(out, provider.Message{Role: provider.RoleUser, Content: content})
		case "assistant":
			content := messageText(m["content"])
			var parts []provider.Part
			if content != "" {
				parts = append(parts, provider.Part{Type: provider.PartText, Text: content})
			}
			if rc, ok := m["reasoning_content"].(string); ok && rc != "" {
				parts = append(parts, provider.Part{Type: provider.PartReasoning, Text: rc})
			}
			if tcs, ok := m["tool_calls"].([]any); ok {
				for _, tc := range tcs {
					call, ok := tc.(map[string]any)
					if !ok {
						continue
					}
					if t := openAIToolCall(call); t != nil {
						parts = append(parts, *t)
					}
				}
			}
			if len(parts) == 0 {
				continue
			}
			msg := provider.Message{Role: provider.RoleAssistant, Content: parts}
			// Preserve any remaining per-message wire keys (e.g. provider
			// -specific extras) through the request round trip.
			var po map[string]any
			for k, v := range m {
				switch k {
				case "role", "content", "tool_calls", "reasoning_content", "name":
				default:
					if po == nil {
						po = map[string]any{}
					}
					po[k] = v
				}
			}
			if len(po) > 0 {
				msg.ProviderOptions = po
			}
			out = append(out, msg)
		case "tool":
			tcID, _ := m["tool_call_id"].(string)
			text := messageText(m["content"])
			if tcID == "" || text == "" {
				continue
			}
			out = append(out, provider.Message{
				Role:    provider.RoleTool,
				Content: []provider.Part{{Type: provider.PartToolResult, ToolCallID: tcID, ToolOutput: text}},
			})
		default:
			// Unknown roles (e.g. legacy "function") ride along as text.
			text := messageText(m["content"])
			if text != "" {
				out = append(out, provider.Message{
					Role:    provider.RoleUser,
					Content: []provider.Part{{Type: provider.PartText, Text: text}},
				})
			}
		}
	}
	if len(out) == 0 {
		return nil, "", fmt.Errorf("요청에 변환 가능한 메시지가 없어요")
	}
	return out, "", nil
}

// anthropicMessages converts the Anthropic "messages" array into GoAI
// messages: text, thinking (+signature), redacted_thinking, tool_use,
// tool_result, and image blocks.
func anthropicMessages(raw any) ([]provider.Message, error) {
	items, err := asMessageSlice(raw)
	if err != nil || len(items) == 0 {
		return nil, fmt.Errorf("요청에 messages 배열이 없어요")
	}
	var out []provider.Message
	for _, m := range items {
		role, _ := m["role"].(string)
		if role != "user" && role != "assistant" {
			continue
		}
		var parts []provider.Part
		switch content := m["content"].(type) {
		case string:
			if content != "" {
				parts = append(parts, provider.Part{Type: provider.PartText, Text: content})
			}
		case []any:
			for _, c := range content {
				blk, ok := c.(map[string]any)
				if !ok {
					continue
				}
				if p := anthropicBlock(blk); p != nil {
					parts = append(parts, *p)
				}
			}
		}
		if len(parts) == 0 {
			continue
		}
		r := provider.RoleUser
		if role == "assistant" {
			r = provider.RoleAssistant
		}
		out = append(out, provider.Message{Role: r, Content: parts})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("요청에 변환 가능한 메시지가 없어요")
	}
	return out, nil
}

// anthropicBlock converts one Anthropic content block into a GoAI part.
func anthropicBlock(blk map[string]any) *provider.Part {
	typ, _ := blk["type"].(string)
	switch typ {
	case "text":
		text, _ := blk["text"].(string)
		if text == "" {
			return nil
		}
		return &provider.Part{Type: provider.PartText, Text: text}
	case "thinking":
		text, _ := blk["thinking"].(string)
		p := provider.Part{Type: provider.PartReasoning, Text: text,
			ProviderOptions: map[string]any{}}
		if sig, ok := blk["signature"].(string); ok && sig != "" {
			p.ProviderOptions["signature"] = sig
		}
		return &p
	case "redacted_thinking":
		p := provider.Part{Type: provider.PartReasoning,
			ProviderOptions: map[string]any{}}
		if data, ok := blk["data"].(string); ok && data != "" {
			p.ProviderOptions["redactedData"] = data
		}
		return &p
	case "tool_use":
		id, _ := blk["id"].(string)
		name, _ := blk["name"].(string)
		if id == "" || name == "" {
			return nil
		}
		var input json.RawMessage
		if raw, err := json.Marshal(blk["input"]); err == nil {
			input = raw
		}
		return &provider.Part{Type: provider.PartToolCall, ToolCallID: id, ToolName: name, ToolInput: input}
	case "tool_result":
		id, _ := blk["tool_use_id"].(string)
		if id == "" {
			return nil
		}
		return &provider.Part{
			Type:       provider.PartToolResult,
			ToolCallID: id,
			ToolOutput: toolResultText(blk["content"]),
		}
	case "image":
		src, ok := blk["source"].(map[string]any)
		if !ok {
			return nil
		}
		mediaType, _ := src["media_type"].(string)
		if data, ok := src["data"].(string); ok && data != "" && mediaType != "" {
			return &provider.Part{Type: provider.PartImage, URL: "data:" + mediaType + ";base64," + data}
		}
		if url, ok := src["url"].(string); ok && url != "" {
			return &provider.Part{Type: provider.PartImage, URL: url}
		}
		return nil
	default:
		// Unknown blocks (e.g. cache_control-only markers) are dropped.
		return nil
	}
}

// openAITools converts the OpenAI "tools" array into GoAI tool definitions,
// preserving provider-defined tools ({"type": ...} without a function body).
func openAITools(raw []any) []provider.ToolDefinition {
	var tools []provider.ToolDefinition
	for _, item := range raw {
		t, ok := item.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := t["type"].(string)
		if fn, ok := t["function"].(map[string]any); ok {
			def := provider.ToolDefinition{}
			def.Name, _ = fn["name"].(string)
			def.Description, _ = fn["description"].(string)
			if schema, err := json.Marshal(fn["parameters"]); err == nil {
				def.InputSchema = schema
			}
			tools = append(tools, def)
			continue
		}
		if typ != "" && typ != "function" {
			def := provider.ToolDefinition{ProviderDefinedType: typ, ProviderDefinedOptions: map[string]any{}}
			for k, v := range t {
				if k != "type" {
					def.ProviderDefinedOptions[k] = v
				}
			}
			tools = append(tools, def)
		}
	}
	return tools
}

// anthropicTools converts the Anthropic "tools" array ({"name", "description",
// "input_schema"}) into GoAI tool definitions.
func anthropicTools(raw []any) []provider.ToolDefinition {
	var tools []provider.ToolDefinition
	for _, item := range raw {
		t, ok := item.(map[string]any)
		if !ok {
			continue
		}
		def := provider.ToolDefinition{}
		def.Name, _ = t["name"].(string)
		def.Description, _ = t["description"].(string)
		if schema, err := json.Marshal(t["input_schema"]); err == nil {
			def.InputSchema = schema
		}
		tools = append(tools, def)
	}
	return tools
}

// openAIToolCall converts one OpenAI assistant tool_call into a GoAI part,
// preserving the Gemini-style continuation fields (thought_signature,
// signature, extra_fields, index) in the part's ProviderOptions so the
// request serializer can re-emit them.
func openAIToolCall(call map[string]any) *provider.Part {
	id, _ := call["id"].(string)
	fn, ok := call["function"].(map[string]any)
	if !ok {
		return nil
	}
	name, _ := fn["name"].(string)
	if id == "" || name == "" {
		return nil
	}
	p := provider.Part{Type: provider.PartToolCall, ToolCallID: id, ToolName: name}
	if args, ok := fn["arguments"].(string); ok && args != "" {
		p.ToolInput = json.RawMessage(args)
	}
	po := map[string]any{}
	if sig, _ := fn["thought_signature"].(string); sig != "" {
		po["thought_signature"] = sig
	}
	if sig, _ := fn["signature"].(string); sig != "" {
		po["signature"] = sig
	}
	if sig, _ := call["thought_signature"].(string); sig != "" {
		po["thought_signature"] = sig
	}
	if sig, _ := call["signature"].(string); sig != "" {
		po["signature"] = sig
	}
	if ef, ok := call["extra_fields"]; ok {
		po["extra_fields"] = ef
	}
	if idx, ok := call["index"]; ok {
		po["index"] = idx
	}
	if len(po) > 0 {
		p.ProviderOptions = po
	}
	return &p
}

// openAIResponseFormat maps the OpenAI response_format into GoAI's
// ResponseFormat. Everything non-standard rides in ProviderOptions so the
// wire body survives round-trip.
func openAIResponseFormat(v any) *provider.ResponseFormat {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	rf := &provider.ResponseFormat{}
	if js, ok := m["json_schema"].(map[string]any); ok {
		rf.Name, _ = js["name"].(string)
		if schema, err := json.Marshal(js["schema"]); err == nil {
			rf.Schema = schema
		}
		return rf
	}
	// json_object / raw text formats have no schema; the provider options
	// passthrough carries the full original body key instead.
	return rf
}

func toolChoiceString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		if name, ok := t["name"].(string); ok {
			return name
		}
		if typ, ok := t["type"].(string); ok {
			return typ
		}
	}
	return ""
}

// messageText flattens a message content field (string or string array) into
// plain text.
func messageText(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, p := range c {
			if s, ok := p.(string); ok {
				b.WriteString(s)
			}
		}
		return b.String()
	}
	return ""
}

// messageUserContent converts a user message content field into parts,
// supporting text + image_url blocks.
func messageUserContent(v any) []provider.Part {
	switch c := v.(type) {
	case string:
		if c == "" {
			return nil
		}
		return []provider.Part{{Type: provider.PartText, Text: c}}
	case []any:
		var parts []provider.Part
		for _, p := range c {
			blk, ok := p.(map[string]any)
			if !ok {
				continue
			}
			switch blk["type"] {
			case "text":
				if text, ok := blk["text"].(string); ok && text != "" {
					parts = append(parts, provider.Part{Type: provider.PartText, Text: text})
				}
			case "image_url":
				if url, ok := blk["image_url"].(map[string]any); ok {
					if u, ok := url["url"].(string); ok && u != "" {
						parts = append(parts, provider.Part{Type: provider.PartImage, URL: u})
					}
				}
			}
		}
		return parts
	}
	return nil
}

// toolResultText flattens an Anthropic tool_result content (string or block
// array) into plain text.
func toolResultText(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, p := range c {
			if blk, ok := p.(map[string]any); ok {
				if text, ok := blk["text"].(string); ok {
					b.WriteString(text)
				}
			}
		}
		return b.String()
	}
	return ""
}
