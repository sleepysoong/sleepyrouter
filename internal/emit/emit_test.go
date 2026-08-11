// Package emit serializes GoAI generate results and stream chunks onto the
// OpenAI and Anthropic wire formats. These tests lock the event sequences and
// field shapes the legacy translators produced.
package emit

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/zendev-sh/goai/provider"
)

func openAIResult() provider.GenerateResult {
	return provider.GenerateResult{
		Text:      "it is sunny",
		Reasoning: "checked the forecast",
		ToolCalls: []provider.ToolCall{
			{
				ID:       "call_1",
				Name:     "get_weather",
				Input:    json.RawMessage(`{"city":"seoul"}`),
				Metadata: map[string]any{"thoughtSignature": "sig-A"},
			},
		},
		FinishReason: provider.FinishToolCalls,
		Response:     provider.ResponseMetadata{ID: "cmpl-test"},
		Usage: provider.Usage{
			InputTokens:      10,
			OutputTokens:     5,
			ReasoningTokens:  3,
			CacheReadTokens:  2,
			CacheWriteTokens: 1,
		},
	}
}

func TestOpenAIResponseWire(t *testing.T) {
	out, err := OpenAIResponse(openAIResult(), "m1")
	if err != nil {
		t.Fatalf("OpenAIResponse: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	if resp["object"] != "chat.completion" || resp["model"] != "m1" {
		t.Errorf("envelope wrong: %v", resp)
	}
	choices := resp["choices"].([]any)
	if len(choices) != 1 {
		t.Fatalf("choices len %d", len(choices))
	}
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "it is sunny" || msg["reasoning_content"] != "checked the forecast" {
		t.Errorf("message wrong: %v", msg)
	}
	tcs := msg["tool_calls"].([]any)
	tc := tcs[0].(map[string]any)
	if tc["id"] != "call_1" {
		t.Errorf("tool id wrong: %v", tc)
	}
	fn := tc["function"].(map[string]any)
	if fn["name"] != "get_weather" || fn["arguments"] != `{"city":"seoul"}` {
		t.Errorf("function wrong: %v", fn)
	}
	// 3-place signature: tool_call + function + extra_fields.
	if tc["thought_signature"] != "sig-A" || fn["thought_signature"] != "sig-A" {
		t.Errorf("3-place sig missing: tc=%v fn=%v", tc, fn)
	}
	ef := tc["extra_fields"].(map[string]any)
	if ef["thought_signature"] != "sig-A" {
		t.Errorf("extra_fields sig missing: %v", ef)
	}
	if choices[0].(map[string]any)["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason wrong: %v", choices[0])
	}

	usage := resp["usage"].(map[string]any)
	if usage["prompt_tokens"] != float64(10) || usage["completion_tokens"] != float64(5) ||
		usage["total_tokens"] != float64(15) {
		t.Errorf("usage wrong: %v", usage)
	}
	details := usage["completion_tokens_details"].(map[string]any)
	if details["reasoning_tokens"] != float64(3) {
		t.Errorf("reasoning details wrong: %v", details)
	}
}

func TestOpenAIResponseMessageSig(t *testing.T) {
	r := openAIResult()
	r.ProviderMetadata = map[string]map[string]any{
		"openai": {"thoughtSignature": "msg-sig"},
	}
	out, err := OpenAIResponse(r, "m1")
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	_ = json.Unmarshal(out, &resp)
	msg := resp["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["thought_signature"] != "msg-sig" {
		t.Errorf("message-level sig missing: %v", msg)
	}
}

func TestOpenAIResponseEmptyChoices(t *testing.T) {
	out, err := OpenAIResponse(provider.GenerateResult{}, "m1")
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	if len(resp["choices"].([]any)) != 0 {
		t.Errorf("expected empty choices, got %v", resp)
	}
}

func TestAnthropicResponseWire(t *testing.T) {
	out, err := AnthropicResponse(openAIResult(), "claude-1")
	if err != nil {
		t.Fatalf("AnthropicResponse: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, out)
	}
	if resp["type"] != "message" || resp["model"] != "claude-1" {
		t.Errorf("envelope wrong: %v", resp)
	}
	if resp["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason wrong: %v", resp["stop_reason"])
	}
	blocks := resp["content"].([]any)
	if len(blocks) != 3 {
		t.Fatalf("content blocks len %d: %v", len(blocks), blocks)
	}
	if blocks[0].(map[string]any)["type"] != "thinking" {
		t.Errorf("first block not thinking: %v", blocks[0])
	}
	tu := blocks[2].(map[string]any)
	if tu["type"] != "tool_use" || tu["id"] != "call_1" || tu["name"] != "get_weather" {
		t.Errorf("tool_use block wrong: %v", tu)
	}
	if tu["thought_signature"] != "sig-A" {
		t.Errorf("tool sig missing: %v", tu)
	}
	if tu["extra_fields"].(map[string]any)["thought_signature"] != "sig-A" {
		t.Errorf("extra_fields sig missing: %v", tu)
	}
	usage := resp["usage"].(map[string]any)
	if usage["input_tokens"] != float64(10) || usage["output_tokens"] != float64(5) {
		t.Errorf("usage wrong: %v", usage)
	}
}

func feedChunks(t *testing.T, chunks []provider.StreamChunk, fn func(w io.Writer, ch <-chan provider.StreamChunk, model string)) string {
	t.Helper()
	ch := make(chan provider.StreamChunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	var b strings.Builder
	fn(&b, ch, "m1")
	return b.String()
}

func sseEvents(t *testing.T, out string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			events = append(events, map[string]any{"done": true})
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			t.Fatalf("bad event JSON %q: %v", data, err)
			return nil
		}
		events = append(events, ev)
	}
	return events
}

func TestOpenAIStreamSSESequence(t *testing.T) {
	chunks := []provider.StreamChunk{
		{Type: provider.ChunkReasoning, Text: "think 1", Metadata: map[string]any{"thoughtSignature": "sig-R"}},
		{Type: provider.ChunkText, Text: "hello "},
		{Type: provider.ChunkText, Text: "world"},
		{Type: provider.ChunkToolCallStreamStart, ToolCallID: "call_1", ToolName: "get_weather", Metadata: map[string]any{"thoughtSignature": "sig-A"}},
		{Type: provider.ChunkToolCallDelta, ToolCallID: "call_1", ToolInput: `{"city"`},
		{Type: provider.ChunkToolCallDelta, ToolCallID: "call_1", ToolInput: `:"seoul"}`},
		{Type: provider.ChunkFinish, FinishReason: provider.FinishToolCalls, Usage: provider.Usage{InputTokens: 3, OutputTokens: 7}},
	}
	out := feedChunks(t, chunks, OpenAIStreamSSE)
	events := sseEvents(t, out)
	if len(events) == 0 {
		t.Fatalf("no events:\n%s", out)
	}
	var sawRole, sawReasoning, sawReasoningSig, sawText, sawToolStart, sawToolSig, sawFinish, sawUsage, sawDone bool
	for _, ev := range events {
		if ev["done"] == true {
			sawDone = true
			continue
		}
		choices, ok := ev["choices"].([]any)
		if !ok {
			continue
		}
		if len(choices) == 0 {
			// Trailing usage event (untyped, identified by empty choices).
			if ev["usage"].(map[string]any)["prompt_tokens"] == float64(3) {
				sawUsage = true
			}
			continue
		}
		ch := choices[0].(map[string]any)
		if fr, ok := ch["finish_reason"]; ok && fr == "tool_calls" {
			sawFinish = true
		}
		delta, ok := ch["delta"].(map[string]any)
		if !ok {
			continue
		}
		if role, ok := delta["role"]; ok && role == "assistant" {
			sawRole = true
		}
		if c, ok := delta["content"].(string); ok && c != "" {
			sawText = true
		}
		if r, ok := delta["reasoning_content"].(string); ok && r == "think 1" {
			sawReasoning = true
		}
		if sig, ok := delta["thought_signature"].(string); ok && sig == "sig-R" {
			sawReasoningSig = true
		}
		if tcs, ok := delta["tool_calls"].([]any); ok && len(tcs) > 0 {
			tc := tcs[0].(map[string]any)
			fn := tc["function"].(map[string]any)
			if tc["id"] == "call_1" && fn["name"] == "get_weather" {
				sawToolStart = true
			}
			// 3-place sig on the tool-call chunk.
			if tc["thought_signature"] == "sig-A" && fn["thought_signature"] == "sig-A" {
				sawToolSig = true
			}
			if ef, ok := tc["extra_fields"].(map[string]any); ok && ef["thought_signature"] == "sig-A" {
				sawToolSig = true
			}
		}
	}
	if !sawRole || !sawReasoning || !sawReasoningSig || !sawText || !sawToolStart || !sawToolSig || !sawFinish || !sawUsage || !sawDone {
		t.Errorf("sequence incomplete: role=%v reasoning=%v r-sig=%v text=%v tstart=%v t-sig=%v finish=%v usage=%v done=%v\n%s",
			sawRole, sawReasoning, sawReasoningSig, sawText, sawToolStart, sawToolSig, sawFinish, sawUsage, sawDone, out)
	}
}

func TestAnthropicStreamSSESequence(t *testing.T) {
	chunks := []provider.StreamChunk{
		{Type: provider.ChunkReasoning, Text: "think hard", Metadata: map[string]any{"thoughtSignature": "sig-R"}},
		{Type: provider.ChunkText, Text: "hi"},
		{Type: provider.ChunkToolCallStreamStart, ToolCallID: "toolu_1", ToolName: "lookup", Metadata: map[string]any{"thoughtSignature": "sig-T"}},
		{Type: provider.ChunkToolCallDelta, ToolCallID: "toolu_1", ToolInput: `{"q"`},
		{Type: provider.ChunkToolCallDelta, ToolCallID: "toolu_1", ToolInput: `:"x"}`},
		{Type: provider.ChunkFinish, FinishReason: provider.FinishToolCalls,
			Metadata: map[string]any{"iterations": float64(2), "contextManagement": "lax"},
			Response: provider.ResponseMetadata{ID: "m-1"},
			Usage:    provider.Usage{InputTokens: 4, OutputTokens: 6, ReasoningTokens: 2}},
	}
	out := feedChunks(t, chunks, AnthropicStreamSSE)
	events := sseEvents(t, out)
	if len(events) == 0 {
		t.Fatalf("no events:\n%s", out)
	}
	var sawStart, sawThinking, sawSigDelta, sawBlockStop, sawText, sawToolSig, sawInputDelta, sawDelta, sawStop bool
	for _, ev := range events {
		switch ev["type"] {
		case "message_start":
			sawStart = true
		case "content_block_start":
			if ev["content_block"].(map[string]any)["type"] == "thinking" {
				sawThinking = true
			} else if ev["content_block"].(map[string]any)["type"] == "tool_use" {
				cb := ev["content_block"].(map[string]any)
				if cb["thought_signature"] == "sig-T" && cb["extra_fields"].(map[string]any)["thought_signature"] == "sig-T" {
					sawToolSig = true
				}
			}
		case "content_block_delta":
			delta := ev["delta"].(map[string]any)
			switch delta["type"] {
			case "signature_delta":
				if delta["signature"] == "sig-R" {
					sawSigDelta = true
				}
			case "text_delta":
				if delta["text"] == "hi" {
					sawText = true
				}
			case "input_json_delta":
				if strings.Contains(delta["partial_json"].(string), "x") {
					sawInputDelta = true
				}
			}
		case "content_block_stop":
			sawBlockStop = true
		case "message_delta":
			sawDelta = true
		case "message_stop":
			sawStop = true
		}
	}
	if ev := findEvent(events, "message_delta"); ev != nil {
		if du := ev["usage"].(map[string]any); du["output_tokens"] == float64(6) && du["input_tokens"] == float64(4) {
			sawInputDelta = true
		} else {
			t.Errorf("message_delta usage wrong: %v", du)
		}
	}
	if !sawStart || !sawThinking || !sawSigDelta || !sawBlockStop || !sawText || !sawToolSig || !sawInputDelta || !sawStop || !sawDelta {
		t.Errorf("anthropic sequence incomplete: start=%v thinking=%v sigdelta=%v blockstop=%v text=%v tsig=%v idelta=%v delta=%v stop=%v\n%s",
			sawStart, sawThinking, sawSigDelta, sawBlockStop, sawText, sawToolSig, sawInputDelta, sawDelta, sawStop, out)
	}
}

func findEvent(events []map[string]any, typ string) map[string]any {
	for _, ev := range events {
		if ev["type"] == typ {
			return ev
		}
	}
	return nil
}
