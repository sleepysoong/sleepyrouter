package anthropic_test

import (
	"encoding/json"
	"strings"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
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

func TestStreamToolArgumentsUseItemIDAndKeepConcurrentBlocks(t *testing.T) {
	enc := anthropic.NewStreamEncoder("claude-sleepy")
	startEvents := enc.StartEvents()
	type input struct{ typ, body string }
	inputs := []input{
		{"response.output_item.added", `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_a","call_id":"call_a","name":"first","arguments":""}}`},
		{"response.output_item.added", `{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_b","call_id":"call_b","name":"second","arguments":""}}`},
		{"response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"fc_b","output_index":1,"delta":"{\"b\":"}`},
		{"response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"fc_a","output_index":0,"delta":"{\"a\":1}"}`},
		{"response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","item_id":"fc_b","output_index":1,"delta":"2}"}`},
		{"response.function_call_arguments.done", `{"type":"response.function_call_arguments.done","item_id":"fc_a","output_index":0,"arguments":"{\"a\":1}"}`},
		{"response.output_item.done", `{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_a","call_id":"call_a","name":"first","arguments":"{\"a\":1}"}}`},
		{"response.function_call_arguments.done", `{"type":"response.function_call_arguments.done","item_id":"fc_b","output_index":1,"arguments":"{\"b\":2}"}`},
		{"response.completed", `{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":4}}}`},
	}
	var events []anthropic.SSEEvent
	for _, in := range inputs {
		events = append(events, enc.HandleResponsesEvent(in.typ, in.body)...)
	}
	var accumulated anthropicsdk.Message
	for _, item := range append(startEvents, events...) {
		var sdkEvent anthropicsdk.MessageStreamEventUnion
		if err := sdkEvent.UnmarshalJSON([]byte(item.Data)); err != nil {
			t.Fatalf("sdk parse %s: %v", item.Event, err)
		}
		if err := accumulated.Accumulate(sdkEvent); err != nil {
			t.Fatalf("sdk accumulate %s: %v", item.Event, err)
		}
	}
	if len(accumulated.Content) != 2 || string(accumulated.Content[0].AsToolUse().Input) != `{"a":1}` || string(accumulated.Content[1].AsToolUse().Input) != `{"b":2}` {
		t.Fatalf("sdk accumulated: %+v", accumulated.Content)
	}
	var args = map[int]string{}
	var starts = map[int]string{}
	var stops = map[int]int{}
	for _, ev := range events {
		var v struct {
			Index        int `json:"index"`
			ContentBlock struct {
				ID string `json:"id"`
			} `json:"content_block"`
			Delta struct {
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(ev.Data), &v); err != nil {
			t.Fatal(err)
		}
		switch ev.Event {
		case "content_block_start":
			starts[v.Index] = v.ContentBlock.ID
		case "content_block_delta":
			args[v.Index] += v.Delta.PartialJSON
		case "content_block_stop":
			stops[v.Index]++
		}
	}
	if starts[0] != "call_a" || starts[1] != "call_b" || args[0] != `{"a":1}` || args[1] != `{"b":2}` || stops[0] != 1 || stops[1] != 1 {
		t.Fatalf("starts=%v args=%v stops=%v", starts, args, stops)
	}
}

func TestOfficialAnthropicSDKAccumulatesToolStream(t *testing.T) {
	enc := anthropic.NewStreamEncoder("claude-sleepy")
	events := enc.StartEvents()
	inputs := [][2]string{
		{"response.output_item.added", `{"output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":""}}`},
		{"response.function_call_arguments.delta", `{"item_id":"fc_1","output_index":0,"delta":"{\"path\":\"a\"}"}`},
		{"response.function_call_arguments.done", `{"item_id":"fc_1","output_index":0,"arguments":"{\"path\":\"a\"}"}`},
		{"response.completed", `{"response":{"usage":{"input_tokens":2,"output_tokens":4}}}`},
	}
	for _, in := range inputs {
		events = append(events, enc.HandleResponsesEvent(in[0], in[1])...)
	}
	var msg anthropicsdk.Message
	for _, item := range events {
		var ev anthropicsdk.MessageStreamEventUnion
		if err := ev.UnmarshalJSON([]byte(item.Data)); err != nil {
			t.Fatalf("parse %s: %v", item.Event, err)
		}
		if string(ev.Type) != item.Event {
			t.Fatalf("event=%s type=%s", item.Event, ev.Type)
		}
		if err := msg.Accumulate(ev); err != nil {
			t.Fatalf("accumulate %s: %v", item.Event, err)
		}
	}
	if len(msg.Content) != 1 || msg.Content[0].AsToolUse().ID != "call_1" || string(msg.Content[0].AsToolUse().Input) != `{"path":"a"}` || string(msg.StopReason) != "tool_use" {
		t.Fatalf("accumulated message = %+v", msg)
	}
	if msg.Usage.InputTokens != 2 || msg.Usage.OutputTokens != 4 {
		t.Fatalf("accumulated usage = %+v", msg.Usage)
	}
}

func TestStreamToolFinalArgumentsWithoutDeltas(t *testing.T) {
	enc := anthropic.NewStreamEncoder("virtual")
	enc.StartEvents()
	start := enc.HandleResponsesEvent("response.output_item.added", `{"output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":""}}`)
	done := enc.HandleResponsesEvent("response.function_call_arguments.done", `{"item_id":"fc_1","output_index":0,"arguments":"{\"path\":\"a\"}"}`)
	duplicate := enc.HandleResponsesEvent("response.output_item.done", `{"output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"a\"}"}}`)
	if len(start) != 1 || len(done) != 2 || done[0].Event != "content_block_delta" || !strings.Contains(done[0].Data, `\"path\"`) || done[1].Event != "content_block_stop" || len(duplicate) != 0 {
		t.Fatalf("start=%v done=%v duplicate=%v", start, done, duplicate)
	}
}

func TestInvalidToolArgumentsFailWithoutNormalStop(t *testing.T) {
	enc := anthropic.NewStreamEncoder("virtual")
	enc.StartEvents()
	enc.HandleResponsesEvent("response.output_item.added", `{"output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":""}}`)
	closed := enc.HandleResponsesEvent("response.function_call_arguments.done", `{"item_id":"fc_1","output_index":0,"arguments":"[1,2]"}`)
	if len(closed) != 2 || closed[0].Event != "content_block_delta" || closed[1].Event != "content_block_stop" || enc.Terminal {
		t.Fatalf("closed=%v terminal=%v", closed, enc.Terminal)
	}
	terminal := enc.HandleResponsesEvent("response.completed", `{"response":{"usage":{"input_tokens":1,"output_tokens":2}}}`)
	if len(terminal) != 1 || terminal[0].Event != "error" || !enc.Terminal || enc.Success {
		t.Fatalf("terminal=%v terminal-state=%v success=%v", terminal, enc.Terminal, enc.Success)
	}
	if more := enc.HandleResponsesEvent("response.completed", `{}`); len(more) != 0 {
		t.Fatalf("unexpected normal stop: %v", more)
	}
}

func TestMaxTokensPreservesTruncatedToolArguments(t *testing.T) {
	enc := anthropic.NewStreamEncoder("virtual")
	events := enc.StartEvents()
	events = append(events, enc.HandleResponsesEvent("response.output_item.added", `{"output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":""}}`)...)
	events = append(events, enc.HandleResponsesEvent("response.function_call_arguments.delta", `{"item_id":"fc_1","output_index":0,"delta":"{\"path\":"}`)...)
	events = append(events, enc.HandleResponsesEvent("response.function_call_arguments.done", `{"item_id":"fc_1","output_index":0,"arguments":"{\"path\":"}`)...)
	events = append(events, enc.HandleResponsesEvent("response.incomplete", `{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":3,"output_tokens":5}}}`)...)
	if enc.Success || !enc.Terminal || enc.StopReason != "max_tokens" {
		t.Fatalf("success=%v terminal=%v stop=%s", enc.Success, enc.Terminal, enc.StopReason)
	}
	var partialJSON string
	var message anthropicsdk.Message
	for _, item := range events {
		var ev anthropicsdk.MessageStreamEventUnion
		if err := ev.UnmarshalJSON([]byte(item.Data)); err != nil {
			t.Fatalf("parse %s: %v", item.Event, err)
		}
		if err := message.Accumulate(ev); err != nil {
			t.Fatalf("accumulate %s: %v", item.Event, err)
		}
		if item.Event == "content_block_delta" {
			var delta struct {
				Delta struct {
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(item.Data), &delta); err != nil {
				t.Fatal(err)
			}
			partialJSON += delta.Delta.PartialJSON
		}
	}
	if partialJSON != `{"path":` || string(message.StopReason) != "max_tokens" || message.Usage.InputTokens != 3 || message.Usage.OutputTokens != 5 {
		t.Fatalf("partial=%q stop=%q usage=%+v", partialJSON, message.StopReason, message.Usage)
	}
}

func TestToolDoneDisagreementFailsAfterBlockClosed(t *testing.T) {
	enc := anthropic.NewStreamEncoder("virtual")
	enc.StartEvents()
	enc.HandleResponsesEvent("response.output_item.added", `{"output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":""}}`)
	enc.HandleResponsesEvent("response.function_call_arguments.done", `{"item_id":"fc_1","output_index":0,"arguments":"{\"path\":\"a\"}"}`)
	events := enc.HandleResponsesEvent("response.output_item.done", `{"output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"b\"}"}}`)
	if len(events) != 1 || events[0].Event != "error" || enc.Success {
		t.Fatalf("events=%v success=%v", events, enc.Success)
	}
}

func TestNonStreamSkipsUnsignedThinkingAndRejectsFailures(t *testing.T) {
	out, err := anthropic.ToMessage([]byte(`{"status":"completed","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"private reasoning"}]},{"type":"message","content":[{"type":"output_text","text":"hello"}]}]}`), "virtual")
	if err != nil || strings.Contains(string(out), `"type":"thinking"`) || !strings.Contains(string(out), "hello") {
		t.Fatalf("out=%s err=%v", out, err)
	}
	for _, raw := range []string{
		`{"status":"failed","output":[]}`,
		`{"status":"incomplete","incomplete_details":{"reason":"content_filter"},"output":[]}`,
		`{"status":"in_progress","output":[]}`,
	} {
		if _, err := anthropic.ToMessage([]byte(raw), "virtual"); err == nil {
			t.Fatalf("accepted %s", raw)
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
