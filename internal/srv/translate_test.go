package srv

import (
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/protocol"
)

func TestExtractTextContent_String(t *testing.T) {
	if got := protocol.ExtractTextContent("hello"); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestExtractTextContent_StringArray(t *testing.T) {
	if got := protocol.ExtractTextContent([]any{"a", "b"}); got != "a\nb" {
		t.Fatalf("expected 'a\\nb', got %q", got)
	}
}

func TestExtractTextContent_Blocks(t *testing.T) {
	if got := protocol.ExtractTextContent([]any{
		map[string]any{"type": "text", "text": "hello"},
		map[string]any{"type": "text", "text": "world"},
	}); got != "hello\nworld" {
		t.Fatalf("expected 'hello\\nworld', got %q", got)
	}
}

func TestExtractTextContent_RejectsImage(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unsupported block")
		}
	}()
	protocol.ExtractTextContent([]any{map[string]any{"type": "image", "source": map[string]any{}}})
}

func TestAnthropicToOpenAI_TextSystem(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{
		"system":     "sys",
		"max_tokens": float64(10),
		"messages": []any{
			map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "hi"}}},
		},
	}, "m")
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

func TestAnthropicToOpenAI_ToolsHistory(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{
		"tools": []any{
			map[string]any{"name": "Bash", "description": "Run shell", "input_schema": map[string]any{
				"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}},
			}},
		},
		"tool_choice": map[string]any{"type": "auto"},
		"messages": []any{
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "text", "text": "checking"},
				map[string]any{"type": "tool_use", "id": "toolu_1", "name": "Bash", "input": map[string]any{"command": "ls"}},
			}},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": []any{map[string]any{"type": "text", "text": "README.md"}}},
			}},
		},
	}, "m")

	// Check tools
	tools, ok := out["tools"].([]map[string]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools: %v", tools)
	}
	if tools[0]["type"] != "function" {
		t.Fatalf("tools[0].type: %v", tools[0]["type"])
	}
	fn := tools[0]["function"].(map[string]any)
	if fn["name"] != "Bash" {
		t.Fatalf("tools[0].function.name: %v", fn["name"])
	}

	// Check tool_choice
	if out["tool_choice"] != "auto" {
		t.Fatalf("tool_choice: %v", out["tool_choice"])
	}

	// Check messages
	messages := out["messages"].([]map[string]any)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0]["role"] != "assistant" || messages[0]["content"] != "checking" {
		t.Fatalf("msg[0] role/content: %v/%v", messages[0]["role"], messages[0]["content"])
	}
	tc, _ := messages[0]["tool_calls"].([]map[string]any)
	if len(tc) != 1 {
		t.Fatalf("msg[0] tool_calls: %v", tc)
	}
	if tc[0]["id"] != "toolu_1" {
		t.Fatalf("tool_call id: %v", tc[0]["id"])
	}
	if tc[0]["function"].(map[string]any)["name"] != "Bash" {
		t.Fatalf("tool_call name: %v", tc[0]["function"].(map[string]any)["name"])
	}

	if messages[1]["role"] != "tool" {
		t.Fatalf("msg[1] role: %v", messages[1]["role"])
	}
	if messages[1]["tool_call_id"] != "toolu_1" {
		t.Fatalf("msg[1] tool_call_id: %v", messages[1]["tool_call_id"])
	}
	if messages[1]["content"] != "README.md" {
		t.Fatalf("msg[1] content: %v", messages[1]["content"])
	}
}

func TestAnthropicToOpenAI_ToolUseHistory(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "hi"},
			}},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "tool_use", "id": "call_1", "name": "Bash", "input": map[string]any{"command": "ls"}},
			}},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "call_1", "content": "done"},
			}},
			map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "tool_use", "id": "call_2", "name": "Bash", "input": map[string]any{"command": "pwd"}},
			}},
		},
	}, "m")
	messages := out["messages"].([]map[string]any)
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d: %v", len(messages), messages)
	}
	// msg0: user "hi"
	if messages[0]["role"] != "user" || messages[0]["content"] != "hi" {
		t.Fatalf("msg[0]: %v", messages[0])
	}
	// msg1: assistant with tool_calls
	if messages[1]["role"] != "assistant" {
		t.Fatalf("msg[1] role: %v", messages[1]["role"])
	}
	// msg2: tool result
	if messages[2]["role"] != "tool" || messages[2]["content"] != "done" {
		t.Fatalf("msg[2]: %v", messages[2])
	}
	// msg3: assistant with tool_calls
	if messages[3]["role"] != "assistant" {
		t.Fatalf("msg[3] role: %v", messages[3]["role"])
	}
}

func TestAnthropicToOpenAI_EmptyMessages(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{"max_tokens": float64(5)}, "m")
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
	}, "m")
	messages := out["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("expected 1 msg, got %d", len(messages))
	}
	if messages[0]["role"] != "system" || messages[0]["content"] != "rule1\nrule2" {
		t.Fatalf("system: %v", messages[0])
	}
}

func TestAnthropicToOpenAI_PreservesToolResultOrder(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "call:1", "content": "done"},
				map[string]any{"type": "text", "text": "continue"},
			}},
		},
	}, "m")
	messages := out["messages"].([]map[string]any)
	if len(messages) != 2 {
		t.Fatalf("expected 2, got %d", len(messages))
	}
	if messages[0]["role"] != "tool" || messages[0]["content"] != "done" {
		t.Fatalf("msg[0]: %v", messages[0])
	}
	if messages[1]["role"] != "user" || messages[1]["content"] != "continue" {
		t.Fatalf("msg[1]: %v", messages[1])
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
	}, "m")
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

func TestAnthropicToOpenAI_ToolChoiceNone(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{
		"tool_choice": map[string]any{"type": "none", "disable_parallel_tool_use": true},
	}, "m")
	if out["tool_choice"] != "none" {
		t.Fatalf("tool_choice: %v", out["tool_choice"])
	}
	if out["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls: %v", out["parallel_tool_calls"])
	}
}

func TestExtractTextContent_RejectsNonTextBlocks(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	protocol.ExtractTextContent([]any{map[string]any{"type": "image", "source": map[string]any{}}})
}

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

func TestAnthropicToOpenAI_PassStop(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{"stop": []any{"\n"}}, "m")
	if out["stop"] == nil {
		t.Fatal("expected stop field")
	}
}
