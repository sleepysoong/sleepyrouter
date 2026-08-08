package srv

import (
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/protocol"
)

func TestOpenAIToAnthropic_TextMessage(t *testing.T) {
	out := protocol.OpenAIToAnthropic(map[string]any{
		"id":    "chatcmpl_1",
		"model": "m",
		"choices": []any{map[string]any{
			"message":       map[string]any{"content": "hello"},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": float64(2), "completion_tokens": float64(3)},
	}, "m")
	if out["type"] != "message" || out["role"] != "assistant" {
		t.Fatalf("type/role: %v/%v", out["type"], out["role"])
	}
	content := out["content"].([]map[string]any)
	if len(content) != 1 || content[0]["text"] != "hello" {
		t.Fatalf("content: %v", content)
	}
	usage := out["usage"].(map[string]any)
	if usage["input_tokens"].(int) != 2 || usage["output_tokens"].(int) != 3 {
		t.Fatalf("usage: %v", usage)
	}
}

func TestOpenAIToAnthropic_ToolCalls(t *testing.T) {
	out := protocol.OpenAIToAnthropic(map[string]any{
		"id":    "chatcmpl_1",
		"model": "m",
		"choices": []any{map[string]any{
			"message": map[string]any{
				"tool_calls": []any{map[string]any{
					"id":   "call_1",
					"type": "function",
					"function": map[string]any{
						"name":      "Bash",
						"arguments": `{"command":"ls"}`,
					},
				}},
			},
			"finish_reason": "tool_calls",
		}},
	}, "m")
	if out["stop_reason"] != "tool_use" {
		t.Fatalf("stop_reason: %v", out["stop_reason"])
	}
	content := out["content"].([]map[string]any)
	if len(content) != 1 || content[0]["type"] != "tool_use" || content[0]["name"] != "Bash" {
		t.Fatalf("content: %v", content)
	}
	input := content[0]["input"].(map[string]any)
	if input["command"] != "ls" {
		t.Fatalf("input: %v", input)
	}
}

func TestOpenAIToAnthropic_LegacyFunctionCall(t *testing.T) {
	out := protocol.OpenAIToAnthropic(map[string]any{
		"choices": []any{map[string]any{
			"message": map[string]any{
				"function_call": map[string]any{
					"name":      "Bash",
					"arguments": `{"command":"pwd"}`,
				},
			},
			"finish_reason": "function_call",
		}},
	}, "m")
	content := out["content"].([]map[string]any)
	if len(content) != 1 || content[0]["type"] != "tool_use" || content[0]["name"] != "Bash" {
		t.Fatalf("content: %v", content)
	}
	input := content[0]["input"].(map[string]any)
	if input["command"] != "pwd" {
		t.Fatalf("input: %v", input)
	}
}

func TestOpenAIToAnthropic_ChatCMPLPrefix(t *testing.T) {
	out := protocol.OpenAIToAnthropic(map[string]any{
		"id":      "chatcmpl_xyz_456",
		"model":   "m",
		"choices": []any{map[string]any{"message": map[string]any{"content": ""}, "finish_reason": "stop"}},
	}, "m")
	id, ok := out["id"].(string)
	if !ok {
		t.Fatal("id is not a string")
	}
	if len(id) < 4 || id[:4] != "msg_" {
		t.Fatalf("expected msg_ prefix, got %s", id)
	}
}

func TestMapStopReason(t *testing.T) {
	tests := []struct{ input, expected string }{
		{"length", "max_tokens"},
		{"tool_calls", "tool_use"},
		{"function_call", "tool_use"},
		{"content_filter", "refusal"},
		{"stop", "end_turn"},
		{"unknown", "end_turn"},
	}
	for _, tc := range tests {
		if got := protocol.MapStopReason(tc.input); got != tc.expected {
			t.Errorf("protocol.MapStopReason(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
func TestOpenAIToAnthropic_ThoughtSignatureAndReasoning(t *testing.T) {
	out := protocol.OpenAIToAnthropic(map[string]any{
		"id":    "chatcmpl_gemini_123",
		"model": "google/gemini-3.6-flash",
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"role":              "assistant",
					"reasoning_content": "Let me search the files",
					"thought_signature": "sig_gemini_resp_999",
					"tool_calls": []any{
						map[string]any{
							"id":                "call_gemini_1",
							"type":              "function",
							"thought_signature": "sig_gemini_resp_999",
							"function": map[string]any{
								"name":      "Bash",
								"arguments": `{"command":"pwd"}`,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
	}, "google/gemini-3.6-flash")

	content, ok := out["content"].([]map[string]any)
	if !ok || len(content) != 2 {
		t.Fatalf("expected 2 blocks (thinking + tool_use), got %d (%v)", len(content), out["content"])
	}

	thinkingBlock := content[0]
	if thinkingBlock["type"] != "thinking" || thinkingBlock["thinking"] != "Let me search the files" || thinkingBlock["signature"] != "sig_gemini_resp_999" {
		t.Fatalf("unexpected thinking block: %v", thinkingBlock)
	}

	toolBlock := content[1]
	if toolBlock["type"] != "tool_use" || toolBlock["thought_signature"] != "sig_gemini_resp_999" {
		t.Fatalf("unexpected tool block: %v", toolBlock)
	}
}
