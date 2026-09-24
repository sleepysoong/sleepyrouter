package upstream

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// ResponsesToChatCompletion converts the gateway's canonical Responses request
// into the official SDK's typed Chat Completions request. Features that depend
// on Responses server-side state or have no Chat Completions equivalent fail
// explicitly instead of being silently dropped.
func ResponsesToChatCompletion(raw []byte, model string) (openai.ChatCompletionNewParams, error) {
	params, _, err := ResponsesToChatCompletionRequest(raw, model)
	return params, err
}

// ResponsesToChatCompletionRequest converts the canonical Responses request
// and returns any provider-specific JSON overrides required by the target
// Chat Completions endpoint. The SDK owns the standard request shape; the
// override is only used for NVIDIA's documented numeric DeepSeek V4.1 effort.
func ResponsesToChatCompletionRequest(raw []byte, model string) (openai.ChatCompletionNewParams, []option.RequestOption, error) {
	var in struct {
		Input             json.RawMessage   `json:"input"`
		Instructions      string            `json:"instructions"`
		Tools             []json.RawMessage `json:"tools"`
		ToolChoice        json.RawMessage   `json:"tool_choice"`
		MaxOutputTokens   *int64            `json:"max_output_tokens"`
		Temperature       *float64          `json:"temperature"`
		TopP              *float64          `json:"top_p"`
		ParallelToolCalls *bool             `json:"parallel_tool_calls"`
		PreviousResponse  string            `json:"previous_response_id"`
		Reasoning         struct {
			Effort json.RawMessage `json:"effort"`
		} `json:"reasoning"`
		Text struct {
			Format json.RawMessage `json:"format"`
		} `json:"text"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return openai.ChatCompletionNewParams{}, nil, fmt.Errorf("decode Responses request: %w", err)
	}
	if in.PreviousResponse != "" {
		return openai.ChatCompletionNewParams{}, nil, errors.New("chat_completions provider does not support previous_response_id; send full input history")
	}

	messages := make([]any, 0, 1)
	if in.Instructions != "" {
		messages = append(messages, map[string]any{"role": "system", "content": in.Instructions})
	}
	input, err := responsesInputToChat(in.Input)
	if err != nil {
		return openai.ChatCompletionNewParams{}, nil, err
	}
	messages = append(messages, input...)
	if len(messages) == 0 {
		return openai.ChatCompletionNewParams{}, nil, errors.New("Responses request has no input or instructions")
	}

	body := map[string]any{"model": model, "messages": messages}
	var options []option.RequestOption
	for i, message := range messages {
		if assistant, ok := message.(map[string]any); ok && assistant["reasoning_content"] != nil {
			options = append(options, option.WithJSONSet(fmt.Sprintf("messages.%d.reasoning_content", i), assistant["reasoning_content"]))
		}
	}
	if len(in.Reasoning.Effort) > 0 && string(in.Reasoning.Effort) != "null" {
		effort, err := decodeReasoningEffort(in.Reasoning.Effort)
		if err != nil {
			return openai.ChatCompletionNewParams{}, nil, err
		}
		if isDeepSeekV41Flash(model) {
			numericEffort, err := deepSeekV41ReasoningEffort(effort)
			if err != nil {
				return openai.ChatCompletionNewParams{}, nil, err
			}
			options = append(options, option.WithJSONSet("reasoning_effort", numericEffort))
		} else {
			body["reasoning_effort"] = effort
		}
	}
	if in.MaxOutputTokens != nil {
		// NVIDIA's hosted DeepSeek endpoint documents max_tokens rather than
		// Responses' max_output_tokens.
		body["max_tokens"] = *in.MaxOutputTokens
	}
	if in.Temperature != nil {
		body["temperature"] = *in.Temperature
	}
	if in.TopP != nil {
		body["top_p"] = *in.TopP
	}
	if in.ParallelToolCalls != nil {
		body["parallel_tool_calls"] = *in.ParallelToolCalls
	}
	if len(in.Tools) > 0 {
		tools, err := responsesToolsToChat(in.Tools)
		if err != nil {
			return openai.ChatCompletionNewParams{}, nil, err
		}
		body["tools"] = tools
	}
	if len(in.ToolChoice) > 0 && string(in.ToolChoice) != "null" {
		choice, err := responsesToolChoiceToChat(in.ToolChoice)
		if err != nil {
			return openai.ChatCompletionNewParams{}, nil, err
		}
		body["tool_choice"] = choice
	}
	if len(in.Text.Format) > 0 && string(in.Text.Format) != "null" {
		format, err := responsesFormatToChat(in.Text.Format)
		if err != nil {
			return openai.ChatCompletionNewParams{}, nil, err
		}
		body["response_format"] = format
	}

	// These options have the same wire representation in both APIs. Copy only
	// this explicit allowlist; Responses-only controls must not leak upstream.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return openai.ChatCompletionNewParams{}, nil, fmt.Errorf("decode Responses fields: %w", err)
	}
	for _, key := range []string{"frequency_penalty", "presence_penalty", "seed", "stop", "user", "logit_bias"} {
		if value, ok := fields[key]; ok {
			var decoded any
			if err := json.Unmarshal(value, &decoded); err != nil {
				return openai.ChatCompletionNewParams{}, nil, fmt.Errorf("decode %s: %w", key, err)
			}
			body[key] = decoded
		}
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return openai.ChatCompletionNewParams{}, nil, fmt.Errorf("encode Chat Completions request: %w", err)
	}
	var params openai.ChatCompletionNewParams
	if err := json.Unmarshal(encoded, &params); err != nil {
		return openai.ChatCompletionNewParams{}, nil, fmt.Errorf("decode typed Chat Completions request: %w", err)
	}
	return params, options, nil
}

func decodeReasoningEffort(raw json.RawMessage) (string, error) {
	var effort string
	if err := json.Unmarshal(raw, &effort); err != nil {
		return "", fmt.Errorf("reasoning.effort must be a string: %w", err)
	}
	return strings.ToLower(strings.TrimSpace(effort)), nil
}

func isDeepSeekV41Flash(model string) bool {
	return strings.EqualFold(strings.TrimSpace(model), "deepseek-ai/deepseek-v4.1-flash")
}

// NVIDIA documents a continuous 1..100 scale for DeepSeek V4.1 Flash, not the
// OpenAI SDK's categorical ReasoningEffort enum. These are explicit bridge
// policy values, not claims that the providers' effort levels are identical.
func deepSeekV41ReasoningEffort(effort string) (int, error) {
	switch effort {
	case "minimal":
		return 1, nil
	case "low":
		return 25, nil
	case "medium":
		return 50, nil
	case "high":
		return 75, nil
	case "xhigh":
		return 90, nil
	case "max":
		return 100, nil
	default:
		value, err := strconv.Atoi(effort)
		if err == nil && value >= 1 && value <= 100 {
			return value, nil
		}
		return 0, fmt.Errorf("reasoning.effort %q cannot be mapped to NVIDIA DeepSeek V4.1 Flash's numeric 1..100 scale", effort)
	}
}

func responsesInputToChat(raw json.RawMessage) ([]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		return []any{map[string]any{"role": "user", "content": text}}, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("Responses input must be a string or item array: %w", err)
	}
	var out []any
	for i, itemRaw := range items {
		var item struct {
			Type      string          `json:"type"`
			Role      string          `json:"role"`
			Content   json.RawMessage `json:"content"`
			CallID    string          `json:"call_id"`
			Name      string          `json:"name"`
			Arguments string          `json:"arguments"`
			Output    json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal(itemRaw, &item); err != nil {
			return nil, fmt.Errorf("input[%d]: %w", i, err)
		}
		switch item.Type {
		case "message":
			if item.Role != "user" && item.Role != "assistant" && item.Role != "system" && item.Role != "developer" {
				return nil, fmt.Errorf("input[%d]: unsupported message role %q", i, item.Role)
			}
			content, err := responsesContentToChat(item.Content)
			if err != nil {
				return nil, fmt.Errorf("input[%d].content: %w", i, err)
			}
			out = append(out, map[string]any{"role": item.Role, "content": content})
		case "function_call":
			arguments := item.Arguments
			if arguments == "" {
				arguments = "{}"
			} else if !json.Valid([]byte(arguments)) {
				return nil, fmt.Errorf("input[%d].arguments is not valid JSON", i)
			}
			if item.CallID == "" || item.Name == "" {
				return nil, fmt.Errorf("input[%d]: function_call requires call_id and name", i)
			}
			toolCall := map[string]any{
				"id": item.CallID, "type": "function", "function": map[string]any{"name": item.Name, "arguments": arguments},
			}
			if len(out) > 0 {
				if previous, ok := out[len(out)-1].(map[string]any); ok && previous["role"] == "assistant" {
					calls, _ := previous["tool_calls"].([]any)
					previous["tool_calls"] = append(calls, toolCall)
					continue
				}
			}
			out = append(out, map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{toolCall}})
		case "function_call_output":
			if item.CallID == "" {
				return nil, fmt.Errorf("input[%d]: function_call_output requires call_id", i)
			}
			output, err := jsonValueToString(item.Output)
			if err != nil {
				return nil, fmt.Errorf("input[%d].output: %w", i, err)
			}
			out = append(out, map[string]any{"role": "tool", "tool_call_id": item.CallID, "content": output})
		case "chat_reasoning":
			var reasoning string
			if err := json.Unmarshal(item.Content, &reasoning); err != nil || reasoning == "" {
				return nil, fmt.Errorf("input[%d]: chat_reasoning requires content", i)
			}
			attached := false
			for j := len(out) - 1; j >= 0; j-- {
				previous, ok := out[j].(map[string]any)
				if !ok || previous["role"] != "assistant" {
					continue
				}
				previous["reasoning_content"] = reasoning
				attached = true
				break
			}
			if !attached {
				return nil, fmt.Errorf("input[%d]: chat_reasoning has no preceding assistant message", i)
			}
		default:
			return nil, fmt.Errorf("input[%d]: unsupported Responses item type %q for Chat Completions", i, item.Type)
		}
	}
	return out, nil
}

func responsesContentToChat(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		return text, nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("content must be a string or part array: %w", err)
	}
	out := make([]any, 0, len(parts))
	for i, partRaw := range parts {
		var part struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			ImageURL json.RawMessage `json:"image_url"`
			Detail   string          `json:"detail"`
		}
		if err := json.Unmarshal(partRaw, &part); err != nil {
			return nil, fmt.Errorf("part[%d]: %w", i, err)
		}
		switch part.Type {
		case "input_text", "output_text":
			out = append(out, map[string]any{"type": "text", "text": part.Text})
		case "input_image":
			var url string
			if err := json.Unmarshal(part.ImageURL, &url); err != nil || url == "" {
				return nil, fmt.Errorf("part[%d]: image requires an image_url string", i)
			}
			image := map[string]any{"url": url}
			if part.Detail != "" {
				image["detail"] = part.Detail
			}
			out = append(out, map[string]any{"type": "image_url", "image_url": image})
		default:
			return nil, fmt.Errorf("part[%d]: unsupported Responses content type %q for Chat Completions", i, part.Type)
		}
	}
	return out, nil
}

func responsesToolsToChat(rawTools []json.RawMessage) ([]any, error) {
	out := make([]any, 0, len(rawTools))
	for i, raw := range rawTools {
		var tool struct {
			Type        string          `json:"type"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
			Strict      *bool           `json:"strict"`
		}
		if err := json.Unmarshal(raw, &tool); err != nil {
			return nil, fmt.Errorf("tools[%d]: %w", i, err)
		}
		if tool.Type != "function" || tool.Name == "" {
			return nil, fmt.Errorf("tools[%d]: only named function tools are supported by Chat Completions", i)
		}
		fn := map[string]any{"name": tool.Name}
		if tool.Description != "" {
			fn["description"] = tool.Description
		}
		if len(tool.Parameters) > 0 && string(tool.Parameters) != "null" {
			var parameters any
			if err := json.Unmarshal(tool.Parameters, &parameters); err != nil {
				return nil, fmt.Errorf("tools[%d].parameters: %w", i, err)
			}
			fn["parameters"] = parameters
		}
		if tool.Strict != nil {
			fn["strict"] = *tool.Strict
		}
		out = append(out, map[string]any{"type": "function", "function": fn})
	}
	return out, nil
}

func responsesToolChoiceToChat(raw json.RawMessage) (any, error) {
	var choice string
	if err := json.Unmarshal(raw, &choice); err == nil {
		switch choice {
		case "auto", "none", "required":
			return choice, nil
		default:
			return nil, fmt.Errorf("unsupported Responses tool_choice %q", choice)
		}
	}
	var named struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &named); err != nil {
		return nil, fmt.Errorf("decode tool_choice: %w", err)
	}
	if named.Type != "function" || named.Name == "" {
		return nil, fmt.Errorf("unsupported Responses tool_choice type %q", named.Type)
	}
	return map[string]any{"type": "function", "function": map[string]any{"name": named.Name}}, nil
}

func responsesFormatToChat(raw json.RawMessage) (any, error) {
	var format struct {
		Type   string          `json:"type"`
		Name   string          `json:"name"`
		Schema json.RawMessage `json:"schema"`
		Strict *bool           `json:"strict"`
	}
	if err := json.Unmarshal(raw, &format); err != nil {
		return nil, fmt.Errorf("decode text.format: %w", err)
	}
	switch format.Type {
	case "json_object":
		return map[string]any{"type": "json_object"}, nil
	case "json_schema":
		if format.Name == "" || len(format.Schema) == 0 {
			return nil, errors.New("text.format json_schema requires name and schema")
		}
		var schema any
		if err := json.Unmarshal(format.Schema, &schema); err != nil {
			return nil, fmt.Errorf("decode text.format schema: %w", err)
		}
		jsonSchema := map[string]any{"name": format.Name, "schema": schema}
		if format.Strict != nil {
			jsonSchema["strict"] = *format.Strict
		}
		return map[string]any{"type": "json_schema", "json_schema": jsonSchema}, nil
	default:
		return nil, fmt.Errorf("unsupported Responses text.format type %q", format.Type)
	}
}

func jsonValueToString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	var valueAny any
	if err := json.Unmarshal(raw, &valueAny); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(valueAny)
	return string(encoded), err
}

// ChatCompletionResponseBody maps one SDK-typed Chat Completion response to
// the Responses API envelope used by the rest of the gateway.
func ChatCompletionResponseBody(resp *openai.ChatCompletion) ([]byte, error) {
	if resp == nil || len(resp.Choices) == 0 {
		return nil, errors.New("Chat Completions returned no choices")
	}
	choice := resp.Choices[0]
	responseID := ChatCompletionResponseID(resp)
	var output []any
	if choice.Message.Content != "" {
		output = append(output, map[string]any{
			"id": "msg_" + strings.TrimPrefix(responseID, "resp_"), "type": "message", "status": "completed", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": choice.Message.Content, "annotations": []any{}}},
		})
	}
	if choice.Message.Refusal != "" {
		output = append(output, map[string]any{
			"id": "msg_" + strings.TrimPrefix(responseID, "resp_") + "_refusal", "type": "message", "status": "completed", "role": "assistant",
			"content": []any{map[string]any{"type": "refusal", "refusal": choice.Message.Refusal}},
		})
	}
	for i, call := range choice.Message.ToolCalls {
		if call.Type != "function" {
			return nil, fmt.Errorf("unsupported Chat Completions tool-call type %q", call.Type)
		}
		fn := call.AsFunction()
		itemID := "fc_" + safeID(call.ID)
		if itemID == "fc_" {
			itemID = fmt.Sprintf("fc_%s_%d", strings.TrimPrefix(responseID, "resp_"), i)
		}
		output = append(output, map[string]any{
			"id": itemID, "type": "function_call", "status": "completed", "call_id": call.ID,
			"name": fn.Function.Name, "arguments": fn.Function.Arguments,
		})
	}
	if output == nil {
		output = []any{}
	}
	status := "completed"
	var incomplete any
	if choice.FinishReason == "length" {
		status = "incomplete"
		incomplete = map[string]any{"reason": "max_output_tokens"}
	}
	usage := map[string]any{
		"input_tokens":  resp.Usage.PromptTokens,
		"output_tokens": resp.Usage.CompletionTokens,
		"total_tokens":  resp.Usage.TotalTokens,
		"input_tokens_details": map[string]any{
			"cached_tokens":      resp.Usage.PromptTokensDetails.CachedTokens,
			"cache_write_tokens": resp.Usage.PromptTokensDetails.CacheWriteTokens,
		},
		"output_tokens_details": map[string]any{"reasoning_tokens": resp.Usage.CompletionTokensDetails.ReasoningTokens},
	}
	body := map[string]any{
		"id": responseID, "object": "response", "created_at": resp.Created, "status": status,
		"model": resp.Model, "output": output, "usage": usage,
		"incomplete_details": incomplete, "error": nil,
	}
	return json.Marshal(body)
}

// ChatCompletionResponseID gives the bridged response a Responses-shaped ID so
// client state/affinity does not leak the upstream Chat Completion namespace.
func ChatCompletionResponseID(resp *openai.ChatCompletion) string {
	if resp == nil || resp.ID == "" {
		return "resp_" + safeID(uuid.NewString())
	}
	id := safeID(resp.ID)
	if strings.HasPrefix(id, "resp_") {
		return id
	}
	return "resp_" + id
}

func safeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
