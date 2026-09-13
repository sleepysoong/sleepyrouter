package anthropic_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/protocol/anthropic"
)

func TestParseStringAndBlocks(t *testing.T) {
	raw := []byte(`{"model":"coding","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`)
	p, err := anthropic.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Requirements.Tools {
		t.Fatal("no tools expected")
	}
	raw2 := []byte(`{"model":"coding","max_tokens":16,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	if _, err := anthropic.Parse(raw2); err != nil {
		t.Fatalf("blocks: %v", err)
	}
}

func TestUnknownBlockRejected(t *testing.T) {
	raw := []byte(`{"model":"m","max_tokens":8,"messages":[{"role":"user","content":[{"type":"weird"}]}]}`)
	if _, err := anthropic.Parse(raw); err == nil {
		t.Fatal("expected unknown block error")
	}
}

func TestToolMappingPreservesID(t *testing.T) {
	raw := []byte(`{
		"model":"coding","max_tokens":64,
		"system":"you are helpful",
		"tools":[{"name":"read_file","description":"read","input_schema":{"type":"object","properties":{"p":{"type":"number"}}}}],
		"tool_choice":{"type":"tool","name":"read_file"},
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_123","name":"read_file","input":{"p":1.5}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_123","content":"ok"}]}
		]}`)
	p, err := anthropic.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !p.Requirements.Tools {
		t.Fatal("tools required")
	}
	body, err := anthropic.ToResponses(p, "vendor/model")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "toolu_123") || !strings.Contains(s, "read_file") {
		t.Fatalf("ID relation broken: %s", s)
	}
	// Number precision: 1.5 must survive (not 1.5000001 float noise is ok, but key must exist).
	if !strings.Contains(s, "1.5") {
		t.Fatalf("number precision lost: %s", s)
	}
}

func TestResponseMapping(t *testing.T) {
	responsesRaw := []byte(`{
		"id":"resp_1","model":"vendor/m",
		"output":[
			{"type":"message","content":[{"type":"output_text","text":"hello"}]},
			{"type":"function_call","call_id":"call_1","name":"read_file","arguments":"{\"p\":1}"}
		],
		"usage":{"input_tokens":10,"output_tokens":5}
	}`)
	out, err := anthropic.ToMessage(responsesRaw, "claude-sleepy")
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	var v struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"content"`
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.HasPrefix(v.ID, "msg_sr_") {
		t.Fatalf("id = %q", v.ID)
	}
	if v.Model != "claude-sleepy" {
		t.Fatalf("model = %q", v.Model)
	}
	if v.StopReason != "tool_use" {
		t.Fatalf("stop = %q", v.StopReason)
	}
	if v.Usage.InputTokens != 10 || v.Usage.OutputTokens != 5 {
		t.Fatalf("usage = %+v", v.Usage)
	}
}

func TestStreamOrdering(t *testing.T) {
	enc := anthropic.NewStreamEncoder("claude-sleepy")
	var order []string
	for _, ev := range enc.StartEvents() {
		order = append(order, ev.Event)
	}
	for _, ev := range enc.HandleResponsesEvent("response.output_text.delta", `{"delta":"hi"}`) {
		order = append(order, ev.Event)
	}
	for _, ev := range enc.HandleResponsesEvent("response.completed", `{"usage":{"input_tokens":1,"output_tokens":2}}`) {
		order = append(order, ev.Event)
	}
	want := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if len(order) != len(want) {
		t.Fatalf("order = %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestCountTokensOvercounts(t *testing.T) {
	c := anthropic.NewCounter()
	n, err := c.CountRequest([]byte(`{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"hello world"}]}`))
	if err != nil || n < 3 {
		t.Fatalf("count = %d %v", n, err)
	}
}
