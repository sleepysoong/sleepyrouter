package srv

import (
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/protocol"
)

func TestAnthropicToOpenAI_TextSystem(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{
		"system":     "sys",
		"max_tokens": float64(10),
		"messages": []any{
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "hi"}}},
		},
	}, "m", "OpenRouter")
	if out["model"] != "m" {
		t.Fatalf("model: %v", out["model"])
	}
	messages := out["messages"].([]map[string]any)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0]["role"] != "system" || messages[0]["content"] != "sys" {
		t.Fatalf("system msg: %v", messages[0])
	}
	if messages[1]["role"] != "user" || messages[1]["content"] != "hi" {
		t.Fatalf("user msg: %v", messages[1])
	}
	if out["max_tokens"].(float64) != 10 {
		t.Fatalf("max_tokens: %v", out["max_tokens"])
	}
}

func TestAnthropicToOpenAI_EmptyMessages(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{"max_tokens": float64(5)}, "m", "OpenRouter")
	if out["model"] != "m" {
		t.Fatalf("model: %v", out["model"])
	}
	messages := out["messages"].([]map[string]any)
	if len(messages) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(messages))
	}
}

func TestAnthropicToOpenAI_SystemArrayBlocks(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{
		"system": []any{
			map[string]any{"type": "text", "text": "rule1"},
			map[string]any{"type": "text", "text": "rule2"},
		},
		"messages": []any{},
	}, "m", "OpenRouter")
	messages := out["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("expected 1 msg, got %d", len(messages))
	}
	if messages[0]["role"] != "system" || messages[0]["content"] != "rule1\nrule2" {
		t.Fatalf("system: %v", messages[0])
	}
}

func TestAnthropicToOpenAI_ImageBlocks(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "look"},
				map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "abc"}},
			}},
		},
	}, "m", "OpenRouter")
	content := out["messages"].([]map[string]any)[0]["content"]
	parts, ok := content.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map, got %T", content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0]["type"] != "text" || parts[0]["text"] != "look" {
		t.Fatalf("part[0]: %v", parts[0])
	}
	if parts[1]["type"] != "image_url" {
		t.Fatalf("part[1].type: %v", parts[1]["type"])
	}
	iu := parts[1]["image_url"].(map[string]any)
	if iu["url"] != "data:image/png;base64,abc" {
		t.Fatalf("url: %v", iu["url"])
	}
}

func TestAnthropicToOpenAI_PassStop(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{"stop": []any{"\n"}}, "m", "OpenRouter")
	if out["stop"] == nil {
		t.Fatal("expected stop field")
	}
}

func TestAnthropicToOpenAI_ThoughtSignaturePreservation(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{
		"messages": []any{
			map[string]any{
				"role": "assistant",
				"content": []any{
					map[string]any{
						"type":      "thinking",
						"thinking":  "I should run bash to see files",
						"signature": "sig_gemini_thought_12345",
					},
					map[string]any{
						"type":              "tool_use",
						"id":                "toolu_test_1",
						"name":              "Bash",
						"input":             map[string]any{"command": "ls"},
						"thought_signature": "sig_gemini_thought_12345",
					},
				},
			},
		},
	}, "google/gemini-3.6-flash", "Google")

	messages := out["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	msg := messages[0]
	if msg["role"] != "assistant" {
		t.Fatalf("expected role assistant, got %v", msg["role"])
	}

	if msg["reasoning_content"] != "I should run bash to see files" {
		t.Fatalf("expected reasoning_content, got %v", msg["reasoning_content"])
	}

	toolCalls, ok := msg["tool_calls"].([]map[string]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %v", msg["tool_calls"])
	}

	tc := toolCalls[0]
	if tc["thought_signature"] != "sig_gemini_thought_12345" {
		t.Fatalf("expected tool_call.thought_signature 'sig_gemini_thought_12345', got %v", tc["thought_signature"])
	}

	fn := tc["function"].(map[string]any)
	if fn["thought_signature"] != "sig_gemini_thought_12345" {
		t.Fatalf("expected function.thought_signature 'sig_gemini_thought_12345', got %v", fn["thought_signature"])
	}

	extraFields, ok := tc["extra_fields"].(map[string]any)
	if !ok || extraFields["thought_signature"] != "sig_gemini_thought_12345" {
		t.Fatalf("expected extra_fields.thought_signature 'sig_gemini_thought_12345', got %v", extraFields)
	}
}
