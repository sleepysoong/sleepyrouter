package protocol

import "testing"

func TestAnthropicToOpenAI_ThinkingEnabled(t *testing.T) {
	body := map[string]any{
		"messages": []any{},
		"thinking": map[string]any{"type": "enabled", "budget_tokens": 1024},
	}
	result := AnthropicToOpenAI(body, "model-x")
	if got := result["reasoning_effort"]; got != "medium" {
		t.Errorf("reasoning_effort = %v, want medium", got)
	}
	thinking, ok := result["thinking"].(map[string]any)
	if !ok || thinking["type"] != "enabled" {
		t.Errorf("thinking not forwarded: %#v", result["thinking"])
	}
}

func TestAnthropicToOpenAI_ThinkingDisabled(t *testing.T) {
	body := map[string]any{
		"messages": []any{},
		"thinking": map[string]any{"type": "disabled"},
	}
	result := AnthropicToOpenAI(body, "model-x")
	if got := result["reasoning_effort"]; got != "none" {
		t.Errorf("reasoning_effort = %v, want none", got)
	}
}

func TestAnthropicToOpenAI_ThinkingAbsent(t *testing.T) {
	body := map[string]any{
		"messages": []any{},
	}
	result := AnthropicToOpenAI(body, "model-x")
	if _, ok := result["reasoning_effort"]; ok {
		t.Errorf("reasoning_effort set to %v when thinking absent, want unset", result["reasoning_effort"])
	}
}

func TestAnthropicToOpenAI_OutputConfig(t *testing.T) {
	body := map[string]any{
		"messages":      []any{},
		"output_config": map[string]any{"effort": "high", "format": "json_schema"},
	}
	result := AnthropicToOpenAI(body, "model-x")
	oc, ok := result["output_config"].(map[string]any)
	if !ok || oc["format"] != "json_schema" {
		t.Errorf("output_config not forwarded: %#v", result["output_config"])
	}
	if got := result["reasoning_effort"]; got != "high" {
		t.Errorf("reasoning_effort = %v, want high (from output_config.effort)", got)
	}
}

func TestAnthropicToOpenAI_Metadata(t *testing.T) {
	body := map[string]any{
		"messages": []any{},
		"metadata": map[string]any{"user_id": "user-42"},
	}
	result := AnthropicToOpenAI(body, "model-x")
	if got := result["user"]; got != "user-42" {
		t.Errorf("user = %v, want user-42", got)
	}
	md, ok := result["metadata"].(map[string]any)
	if !ok || md["user_id"] != "user-42" {
		t.Errorf("metadata not forwarded: %#v", result["metadata"])
	}
}

func TestAnthropicToOpenAI_CacheControl(t *testing.T) {
	body := map[string]any{
		"messages":      []any{},
		"cache_control": map[string]any{"type": "ephemeral"},
	}
	result := AnthropicToOpenAI(body, "model-x")
	cc, ok := result["cache_control"].(map[string]any)
	if !ok || cc["type"] != "ephemeral" {
		t.Errorf("cache_control not forwarded: %#v", result["cache_control"])
	}

	// Per-block: cache_control on a content block survives into the OpenAI part.
	blocks := []map[string]any{
		{"type": "text", "text": "hello", "cache_control": map[string]any{"type": "ephemeral"}},
		{"type": "image", "source": map[string]any{"type": "url", "url": "https://example.com/a.png"}, "cache_control": map[string]any{"type": "ephemeral"}},
	}
	content, ok := openAIContentFromBlocks(blocks).([]map[string]any)
	if !ok {
		t.Fatalf("mixed content should be a parts list, got %#v", content)
	}
	for _, part := range content {
		if part["type"] == "text" {
			if _, ok := part["cache_control"]; !ok {
				t.Errorf("text part missing cache_control: %#v", part)
			}
		}
		if part["type"] == "image_url" {
			if _, ok := part["cache_control"]; !ok {
				t.Errorf("image part missing cache_control: %#v", part)
			}
		}
	}
}

func TestAnthropicToOpenAI_MaxTokensZero(t *testing.T) {
	body := map[string]any{
		"messages":   []any{},
		"max_tokens": 0,
	}
	result := AnthropicToOpenAI(body, "model-x")
	if _, ok := result["max_tokens"]; ok {
		t.Errorf("max_tokens = %v, want omitted when 0", result["max_tokens"])
	}
}

func TestAnthropicToOpenAI_MaxTokensPositive(t *testing.T) {
	body := map[string]any{
		"messages":   []any{},
		"max_tokens": 512,
	}
	result := AnthropicToOpenAI(body, "model-x")
	if got := result["max_tokens"]; got != 512 {
		t.Errorf("max_tokens = %v, want 512", got)
	}
}
