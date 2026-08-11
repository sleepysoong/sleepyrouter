// Package adapt converts the upstream wire bodies (OpenAI-compatible and
// Anthropic) into GoAI generate params. These tests lock the round-trip
// fidelity that the legacy pass-through provided.
package adapt

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/zendev-sh/goai/provider"
)

func TestAsMessageSliceBothShapes(t *testing.T) {
	// The handler's converters can hand us []map[string]any directly...
	msgs, err := asMessageSlice([]map[string]any{
		{"role": "user", "content": "hi"},
	})
	if err != nil || len(msgs) != 1 || msgs[0]["role"] != "user" {
		t.Fatalf("[]map[string]any input failed: %v %v", msgs, err)
	}
	// ...or the freshly-unmarshaled []any shape.
	msgs2, err := asMessageSlice([]any{
		map[string]any{"role": "user", "content": "hi"},
	})
	if err != nil || len(msgs2) != 1 {
		t.Fatalf("[]any input failed: %v %v", msgs2, err)
	}
	if _, err := asMessageSlice("nope"); err == nil {
		t.Fatal("expected error for non-array messages")
	}
}

func TestOpenAIRequestFullWireFidelity(t *testing.T) {
	// Values are float64 because the handler's bodies arrive from
	// json.Unmarshal; Go int literals would fail the type asserts.
	body := map[string]any{
		"model":             "some-model",
		"stream":            true,
		"stream_options":    map[string]any{"include_usage": true},
		"temperature":       float64(0.7),
		"top_p":             float64(0.9),
		"top_k":             float64(40),
		"frequency_penalty": float64(0.2),
		"presence_penalty":  float64(0.1),
		"seed":              float64(42),
		"max_tokens":        float64(512),
		"stop":              []any{"END", "STOP"},
		"reasoning_effort":  "high",
		"service_tier":      "scale",
		"user":              "u-1",
		"response_format":   map[string]any{"type": "json_object"},
		"tool_choice":       map[string]any{"type": "function", "name": "get_weather"},
		"messages": []map[string]any{
			{"role": "system", "content": "be terse"},
			{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "what's the weather"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://ex/1.png"}},
			}},
			{"role": "assistant", "content": "let me check",
				"reasoning_content": "I should call the tool",
				"tool_calls": []any{map[string]any{
					"id":   "call_1",
					"type": "function",
					"function": map[string]any{
						"name":              "get_weather",
						"arguments":         `{"city":"seoul"}`,
						"thought_signature": "sig-A",
					},
					"signature": "sig-A",
				}}},
			{"role": "tool", "tool_call_id": "call_1", "content": "sunny"},
		},
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{
				"name": "get_weather", "description": "weather check",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
			}},
			map[string]any{"type": "computer_20241022", "display_width": float64(1024)},
		},
	}

	p, err := OpenAIRequest(context.Background(), body, "some-model")
	if err != nil {
		t.Fatalf("OpenAIRequest: %v", err)
	}

	if p.MaxOutputTokens != 512 {
		t.Errorf("MaxOutputTokens = %d, want 512", p.MaxOutputTokens)
	}
	if p.Temperature == nil || *p.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", p.Temperature)
	}
	if p.TopP == nil || *p.TopP != 0.9 {
		t.Errorf("TopP = %v, want 0.9", p.TopP)
	}
	if p.TopK == nil || *p.TopK != 40 {
		t.Errorf("TopK = %v, want 40", p.TopK)
	}
	if p.Seed == nil || *p.Seed != 42 {
		t.Errorf("Seed = %v, want 42", p.Seed)
	}
	if p.FrequencyPenalty == nil || *p.FrequencyPenalty != 0.2 || p.PresencePenalty == nil || *p.PresencePenalty != 0.1 {
		t.Errorf("penalties wrong: %v %v", p.FrequencyPenalty, p.PresencePenalty)
	}
	if len(p.StopSequences) != 2 || p.StopSequences[0] != "END" || p.StopSequences[1] != "STOP" {
		t.Errorf("StopSequences = %v", p.StopSequences)
	}
	if p.ToolChoice != "get_weather" {
		t.Errorf("ToolChoice = %q, want get_weather", p.ToolChoice)
	}
	if p.ResponseFormat == nil || p.ResponseFormat.Name != "" {
		t.Errorf("ResponseFormat = %v, want json_object passthrough", p.ResponseFormat)
	}
	// Passthrough keys ride in ProviderOptions (stream_options is consumed
	// by goai, which always emits the trailing usage chunk itself).
	for _, k := range []string{"reasoning_effort", "service_tier", "user"} {
		if _, ok := p.ProviderOptions[k]; !ok {
			t.Errorf("ProviderOptions missing %q: %v", k, p.ProviderOptions)
		}
	}
	if _, ok := p.ProviderOptions["stream_options"]; ok {
		t.Errorf("stream_options should be consumed, got %v", p.ProviderOptions)
	}

	if len(p.Tools) != 2 {
		t.Fatalf("Tools len = %d, want 2", len(p.Tools))
	}
	if p.Tools[0].Name != "get_weather" || !bytes.Contains(p.Tools[0].InputSchema, []byte("object")) {
		t.Errorf("tool 0 wrong: %+v", p.Tools[0])
	}
	if p.Tools[1].ProviderDefinedType != "computer_20241022" {
		t.Errorf("provider-defined tool wrong: %+v", p.Tools[1])
	}
	if p.Tools[1].ProviderDefinedOptions["display_width"] != float64(1024) {
		t.Errorf("provider-defined opts wrong: %+v", p.Tools[1].ProviderDefinedOptions)
	}

	// System stays inside Messages (RoleSystem); the openai-compat
	// serializer emits it as the leading {"role":"system"} message.
	if p.System != "" {
		t.Errorf("System = %q, want extracted via RoleSystem message", p.System)
	}
	if len(p.Messages) != 4 {
		t.Fatalf("Messages len = %d, want 4", len(p.Messages))
	}
	if p.Messages[0].Role != provider.RoleSystem || p.Messages[0].Content[0].Text != "be terse" {
		t.Errorf("system message wrong: %+v", p.Messages[0])
	}

	user := p.Messages[1]
	if user.Role != provider.RoleUser || len(user.Content) != 2 {
		t.Fatalf("user msg wrong: %+v", user)
	}
	if user.Content[0].Type != provider.PartText || user.Content[1].Type != provider.PartImage || user.Content[1].URL != "https://ex/1.png" {
		t.Errorf("user parts wrong: %+v", user.Content)
	}

	assistant := p.Messages[2]
	if assistant.Role != provider.RoleAssistant {
		t.Fatalf("assistant role wrong: %+v", assistant)
	}
	foundReasoning, foundCall := false, false
	for _, part := range assistant.Content {
		switch part.Type {
		case provider.PartReasoning:
			foundReasoning = part.Text == "I should call the tool"
		case provider.PartToolCall:
			foundCall = part.ToolCallID == "call_1" && part.ToolName == "get_weather" &&
				string(part.ToolInput) == `{"city":"seoul"}`
			if part.ProviderOptions["thought_signature"] != "sig-A" {
				t.Errorf("tool call sig not preserved: %+v", part.ProviderOptions)
			}
			if part.ProviderOptions["signature"] != "sig-A" {
				t.Errorf("tool call signature not preserved: %+v", part.ProviderOptions)
			}
		}
	}
	if !foundReasoning || !foundCall {
		t.Errorf("assistant parts missing reasoning/call: %+v", assistant.Content)
	}

	tool := p.Messages[3]
	if tool.Role != provider.RoleTool || tool.Content[0].ToolCallID != "call_1" || tool.Content[0].ToolOutput != "sunny" {
		t.Errorf("tool msg wrong: %+v", tool)
	}
}

func TestAnthropicRequestFidelity(t *testing.T) {
	body := map[string]any{
		"model":          "claude-x",
		"max_tokens":     float64(1024),
		"temperature":    float64(0.3),
		"top_p":          float64(0.8),
		"top_k":          float64(5),
		"stop_sequences": []any{"END"},
		"thinking":       map[string]any{"type": "enabled", "budget_tokens": float64(512)},
		"system":         []any{map[string]any{"type": "text", "text": "sys-one"}, map[string]any{"type": "text", "text": "sys-two"}},
		"tools":          []any{map[string]any{"name": "lookup", "description": "find", "input_schema": map[string]any{"type": "object"}}},
		"tool_choice":    map[string]any{"type": "tool", "name": "lookup"},
		"messages": []map[string]any{
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": []any{
				map[string]any{"type": "thinking", "thinking": "think hard", "signature": "sig-Th"},
				map[string]any{"type": "redacted_thinking", "data": "red-1"},
				map[string]any{"type": "tool_use", "id": "toolu_1", "name": "lookup", "input": map[string]any{"q": "x"}},
			}},
			{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": []any{
					map[string]any{"type": "text", "text": "no results"},
				}},
				map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "QUJD"}},
			}},
		},
	}

	p, err := AnthropicRequest(context.Background(), body, "claude-x")
	if err != nil {
		t.Fatalf("AnthropicRequest: %v", err)
	}

	if p.MaxOutputTokens != 1024 {
		t.Errorf("MaxOutputTokens = %d", p.MaxOutputTokens)
	}
	if p.System != "sys-onesys-two" {
		t.Errorf("System = %q", p.System)
	}
	if p.TopK == nil || *p.TopK != 5 {
		t.Errorf("TopK = %v", p.TopK)
	}
	if p.ToolChoice != "lookup" {
		t.Errorf("ToolChoice = %q", p.ToolChoice)
	}
	if p.ProviderOptions["thinking"] == nil {
		t.Errorf("thinking ProviderOptions missing: %v", p.ProviderOptions)
	}
	if len(p.Tools) != 1 || p.Tools[0].Name != "lookup" {
		t.Errorf("tools wrong: %+v", p.Tools)
	}

	if len(p.Messages) != 3 {
		t.Fatalf("Messages len = %d, want 3", len(p.Messages))
	}
	asst := p.Messages[1]
	if asst.Role != provider.RoleAssistant || len(asst.Content) != 3 {
		t.Fatalf("assistant msg wrong: %+v", asst)
	}
	if asst.Content[0].Type != provider.PartReasoning || asst.Content[0].Text != "think hard" ||
		asst.Content[0].ProviderOptions["signature"] != "sig-Th" {
		t.Errorf("thinking block wrong: %+v", asst.Content[0])
	}
	if asst.Content[1].Type != provider.PartReasoning || asst.Content[1].ProviderOptions["redactedData"] != "red-1" {
		t.Errorf("redacted block wrong: %+v", asst.Content[1])
	}
	if asst.Content[2].Type != provider.PartToolCall || asst.Content[2].ToolCallID != "toolu_1" ||
		string(asst.Content[2].ToolInput) != `{"q":"x"}` {
		t.Errorf("tool_use block wrong: %+v", asst.Content[2])
	}

	user := p.Messages[2]
	if user.Content[0].Type != provider.PartToolResult || user.Content[0].ToolCallID != "toolu_1" || user.Content[0].ToolOutput != "no results" {
		t.Errorf("tool_result wrong: %+v", user.Content[0])
	}
	if user.Content[1].Type != provider.PartImage || user.Content[1].URL != "data:image/png;base64,QUJD" {
		t.Errorf("image data-URI wrong: %+v", user.Content[1])
	}
}

func TestOpenAIRequestEmptyMessagesError(t *testing.T) {
	if _, err := OpenAIRequest(context.Background(), map[string]any{"model": "m", "messages": []any{}}, "m"); err == nil {
		t.Fatal("expected error for empty messages")
	}
}

func TestJSONRoundTripMessagesShape(t *testing.T) {
	// The handler passes JSON-unmarshaled bodies; verify the []any shape
	// produced by a full body unmarshal works end to end.
	var full map[string]any
	if err := json.Unmarshal([]byte(`{"model":"m","messages":[{"role":"user","content":"hello"}]}`), &full); err != nil {
		t.Fatal(err)
	}
	p, err := OpenAIRequest(context.Background(), full, "m")
	if err != nil {
		t.Fatalf("OpenAIRequest: %v", err)
	}
	if len(p.Messages) != 1 || p.Messages[0].Role != provider.RoleUser || p.Messages[0].Content[0].Text != "hello" {
		t.Errorf("messages wrong: %+v", p.Messages)
	}
}
