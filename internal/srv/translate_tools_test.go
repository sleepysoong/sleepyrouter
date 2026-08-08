package srv

import (
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/protocol"
)

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
	}, "m", "OpenRouter")

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

	if out["tool_choice"] != "auto" {
		t.Fatalf("tool_choice: %v", out["tool_choice"])
	}

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
	}, "m", "OpenRouter")
	messages := out["messages"].([]map[string]any)
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d: %v", len(messages), messages)
	}
	if messages[0]["role"] != "user" || messages[0]["content"] != "hi" {
		t.Fatalf("msg[0]: %v", messages[0])
	}
	if messages[1]["role"] != "assistant" {
		t.Fatalf("msg[1] role: %v", messages[1]["role"])
	}
	if messages[2]["role"] != "tool" || messages[2]["content"] != "done" {
		t.Fatalf("msg[2]: %v", messages[2])
	}
	if messages[3]["role"] != "assistant" {
		t.Fatalf("msg[3] role: %v", messages[3]["role"])
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
	}, "m", "OpenRouter")
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

func TestAnthropicToOpenAI_ToolChoiceNone(t *testing.T) {
	out := protocol.AnthropicToOpenAI(map[string]any{
		"tool_choice": map[string]any{"type": "none", "disable_parallel_tool_use": true},
	}, "m", "OpenRouter")
	if out["tool_choice"] != "none" {
		t.Fatalf("tool_choice: %v", out["tool_choice"])
	}
	if out["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls: %v", out["parallel_tool_calls"])
	}
}
