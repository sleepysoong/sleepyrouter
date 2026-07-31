package protocol

import "testing"

func TestMapStopReason(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"length_to_max_tokens", "length", "max_tokens"},
		{"tool_calls_to_tool_use", "tool_calls", "tool_use"},
		{"function_call_to_tool_use", "function_call", "tool_use"},
		{"content_filter_to_refusal", "content_filter", "refusal"},
		{"stop_to_end_turn", "stop", "end_turn"},
		{"empty_to_end_turn", "", "end_turn"},
		{"nil_to_end_turn", nil, "end_turn"},
		{"unknown_to_end_turn", "made_up_reason", "end_turn"},
		{"pause_turn_passthrough", "pause_turn", "pause_turn"},
		{"model_context_window_exceeded_passthrough", "model_context_window_exceeded", "model_context_window_exceeded"},
	}
	for _, c := range cases {
		if got := MapStopReason(c.in); got != c.want {
			t.Errorf("%s: MapStopReason(%v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestOpenAIToAnthropic_Usage(t *testing.T) {
	out := OpenAIToAnthropic(map[string]any{
		"id":    "chatcmpl_1",
		"model": "m",
		"choices": []any{map[string]any{
			"message":       map[string]any{"content": "hi"},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":             float64(10),
			"completion_tokens":         float64(20),
			"completion_tokens_details": map[string]any{"reasoning_tokens": float64(8)},
		},
	}, "m")

	usage, ok := out["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage missing: %v", out)
	}
	if usage["input_tokens"] != 10 || usage["output_tokens"] != 20 {
		t.Fatalf("usage tokens: %v", usage)
	}
	details, ok := usage["output_tokens_details"].(map[string]any)
	if !ok {
		t.Fatalf("output_tokens_details missing: %v", usage)
	}
	if details["thinking_tokens"] != 8 {
		t.Fatalf("thinking_tokens = %v, want 8", details["thinking_tokens"])
	}
}

func TestOpenAIToAnthropic_Usage_NoReasoningTokens(t *testing.T) {
	out := OpenAIToAnthropic(map[string]any{
		"id":    "chatcmpl_1",
		"model": "m",
		"choices": []any{map[string]any{
			"message":       map[string]any{"content": "hi"},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     float64(1),
			"completion_tokens": float64(2),
		},
	}, "m")

	usage := out["usage"].(map[string]any)
	if _, present := usage["output_tokens_details"]; present {
		t.Fatalf("output_tokens_details should be absent: %v", usage)
	}
}

func TestOpenAIToAnthropic_StopDetails(t *testing.T) {
	out := OpenAIToAnthropic(map[string]any{
		"id":    "chatcmpl_1",
		"model": "m",
		"choices": []any{map[string]any{
			"message":       map[string]any{"content": "hi"},
			"finish_reason": "content_filter",
		}},
		"stop_details": map[string]any{"type": "refusal", "refusal": "policy violation"},
	}, "m")

	if out["stop_reason"] != "refusal" {
		t.Fatalf("stop_reason = %v, want refusal", out["stop_reason"])
	}
	details, ok := out["stop_details"].(map[string]any)
	if !ok {
		t.Fatalf("stop_details missing: %v", out)
	}
	if details["refusal"] != "policy violation" {
		t.Fatalf("stop_details.refusal = %v, want 'policy violation'", details["refusal"])
	}
}

func TestOpenAIToAnthropic_StopDetails_FromMessageRefusal(t *testing.T) {
	out := OpenAIToAnthropic(map[string]any{
		"id":    "chatcmpl_1",
		"model": "m",
		"choices": []any{map[string]any{
			"message":       map[string]any{"content": "", "refusal": "I can't help with that"},
			"finish_reason": "content_filter",
		}},
	}, "m")

	details, ok := out["stop_details"].(map[string]any)
	if !ok {
		t.Fatalf("stop_details missing: %v", out)
	}
	if details["type"] != "refusal" || details["refusal"] != "I can't help with that" {
		t.Fatalf("stop_details = %v", details)
	}
}

func TestOpenAIToAnthropic_StopDetails_Absent(t *testing.T) {
	out := OpenAIToAnthropic(map[string]any{
		"id":    "chatcmpl_1",
		"model": "m",
		"choices": []any{map[string]any{
			"message":       map[string]any{"content": "hi"},
			"finish_reason": "stop",
		}},
	}, "m")

	if _, present := out["stop_details"]; present {
		t.Fatalf("stop_details should be absent: %v", out)
	}
}

func TestExtractTextContent_NoPanic(t *testing.T) {
	_, err := ExtractTextContent([]any{map[string]any{"type": "image", "source": map[string]any{}}})
	if err == nil {
		t.Fatal("expected error for unsupported block, got nil")
	}
}

func TestExtractTextContent_NilAndString(t *testing.T) {
	if got, err := ExtractTextContent(nil); err != nil || got != "" {
		t.Fatalf("nil: got %q, err %v", got, err)
	}
	if got, err := ExtractTextContent("hello"); err != nil || got != "hello" {
		t.Fatalf("string: got %q, err %v", got, err)
	}
}
