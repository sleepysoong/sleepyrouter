package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

type sseEvent struct {
	event string
	data  map[string]any
}

func parseSSE(t *testing.T, body string) []sseEvent {
	t.Helper()
	var events []sseEvent
	for _, frame := range strings.Split(body, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" {
			continue
		}
		var name string
		var data string
		for _, line := range strings.Split(frame, "\n") {
			if strings.HasPrefix(line, "event: ") {
				name = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				data = strings.TrimPrefix(line, "data: ")
			}
		}
		if name == "" {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			t.Fatalf("failed to parse SSE data %q: %v", data, err)
		}
		events = append(events, sseEvent{event: name, data: parsed})
	}
	return events
}

func streamAnthropic(t *testing.T, input string) []sseEvent {
	t.Helper()
	rec := httptest.NewRecorder()
	PipeOpenAIStreamAsAnthropic(io.NopCloser(strings.NewReader(input)), rec, "test-model")
	return parseSSE(t, rec.Body.String())
}

func openAIChunk(usageJSON, deltaJSON string) string {
	return "data: " + `{"choices":[{"delta":{` + deltaJSON + `}}],"usage":{` + usageJSON + `}}` + "\n\n"
}

func TestPipeOpenAIStreamAsAnthropic_ThinkingDelta(t *testing.T) {
	input := openAIChunk("", `"reasoning_content":"Let me think"`)
	input += openAIChunk("", `"reasoning_content":" step by step"`)
	input += openAIChunk("", `"thought_signature":"sig-abc123"`)
	input += openAIChunk("", `"content":"Final answer"`)
	input += "data: [DONE]\n\n"

	events := streamAnthropic(t, input)

	// content_block_start with a thinking block appears.
	var thinkingStart, signatureDelta, thinkingStop int = -1, -1, -1
	for i, ev := range events {
		if ev.event != "content_block_start" {
			continue
		}
		cb, _ := ev.data["content_block"].(map[string]any)
		if cb["type"] == "thinking" {
			thinkingStart = i
		}
	}
	if thinkingStart == -1 {
		t.Fatalf("expected a content_block_start with type thinking, got events: %+v", events)
	}

	var thinkingDeltas []string
	for i, ev := range events {
		if ev.event != "content_block_delta" {
			continue
		}
		delta, _ := ev.data["delta"].(map[string]any)
		switch delta["type"] {
		case "thinking_delta":
			thinkingDeltas = append(thinkingDeltas, delta["thinking"].(string))
		case "signature_delta":
			signatureDelta = i
			if delta["signature"] != "sig-abc123" {
				t.Fatalf("expected signature sig-abc123, got %v", delta["signature"])
			}
		}
	}
	if len(thinkingDeltas) != 2 || strings.Join(thinkingDeltas, "") != "Let me think step by step" {
		t.Fatalf("expected two thinking deltas forming the full reasoning, got %v", thinkingDeltas)
	}

	// signature_delta appears before content_block_stop for the thinking block.
	thinkingIdx := int(events[thinkingStart].data["index"].(float64))
	for i := signatureDelta + 1; i < len(events); i++ {
		if events[i].event == "content_block_stop" {
			if int(events[i].data["index"].(float64)) == thinkingIdx {
				thinkingStop = i
				break
			}
		}
	}
	if thinkingStop == -1 {
		t.Fatalf("expected content_block_stop for thinking block index %d after signature_delta", thinkingIdx)
	}
	if signatureDelta == -1 {
		t.Fatal("expected a signature_delta event")
	}
	if signatureDelta > thinkingStop {
		t.Fatalf("signature_delta at %d must come before content_block_stop at %d", signatureDelta, thinkingStop)
	}
}

func TestPipeOpenAIStreamAsAnthropic_TextAndThinking(t *testing.T) {
	input := openAIChunk("", `"reasoning_content":"thinking first"`)
	input += openAIChunk("", `"content":"text second"`)
	input += "data: [DONE]\n\n"

	events := streamAnthropic(t, input)

	var starts []string
	for _, ev := range events {
		if ev.event != "content_block_start" {
			continue
		}
		cb, _ := ev.data["content_block"].(map[string]any)
		starts = append(starts, cb["type"].(string))
	}
	if len(starts) != 2 || starts[0] != "thinking" || starts[1] != "text" {
		t.Fatalf("expected thinking block before text block, got starts: %v", starts)
	}
}

func TestPipeOpenAIStreamAsAnthropic_ThinkingTokens(t *testing.T) {
	input := openAIChunk(`"completion_tokens":50,"completion_tokens_details":{"reasoning_tokens":12}`, `"reasoning_content":"think"`)
	input += openAIChunk("", `"content":"answer"`)
	input += "data: [DONE]\n\n"

	events := streamAnthropic(t, input)

	for _, ev := range events {
		if ev.event != "message_delta" {
			continue
		}
		usage, _ := ev.data["usage"].(map[string]any)
		if usage["output_tokens"] != float64(50) {
			t.Fatalf("expected output_tokens 50, got %v", usage["output_tokens"])
		}
		details, _ := usage["output_tokens_details"].(map[string]any)
		if details["thinking_tokens"] != float64(12) {
			t.Fatalf("expected thinking_tokens 12, got %v", details["thinking_tokens"])
		}
		return
	}
	t.Fatal("no message_delta event found")
}

func TestPipeOpenAIStreamAsAnthropic_NoThinkingTokensOmitsDetails(t *testing.T) {
	input := openAIChunk(`"completion_tokens":10`, `"content":"answer"`)
	input += "data: [DONE]\n\n"

	events := streamAnthropic(t, input)

	for _, ev := range events {
		if ev.event != "message_delta" {
			continue
		}
		usage, _ := ev.data["usage"].(map[string]any)
		if _, ok := usage["output_tokens_details"]; ok {
			t.Fatal("output_tokens_details should be omitted when there are no thinking tokens")
		}
		return
	}
	t.Fatal("no message_delta event found")
}
