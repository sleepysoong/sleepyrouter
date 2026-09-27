package upstream

import (
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func TestNormalizeInputItems(t *testing.T) {
	in := `{"model":"m","input":[{"role":"user","content":"hi"},{"type":"message","role":"assistant","content":"a"},{"type":"function_call_output","call_id":"c1","output":"x"},{"role":"system","content":[{"type":"input_text","text":"s"}]}]}`
	out, err := RewriteModel([]byte(in), "up")
	if err != nil {
		t.Fatal(err)
	}
	types := gjson.GetBytes(out, "input.#.type").String()
	if types != `["message","message","function_call_output","message"]` {
		t.Fatalf("types = %s", types)
	}
	if gjson.GetBytes(out, "model").String() != "up" || gjson.GetBytes(out, "input.2.call_id").String() != "c1" {
		t.Fatalf("other fields changed: %s", out)
	}
	// String input and missing input stay as they are.
	for _, raw := range []string{`{"model":"m","input":"hi"}`, `{"model":"m"}`} {
		o, err := NormalizeInputItems([]byte(raw))
		if err != nil || string(o) != raw {
			t.Fatalf("%s -> %s (%v)", raw, o, err)
		}
	}
	// Chat Completions bridge accepts the conversation once items are typed
	// (user, assistant, tool result, system → 4 messages).
	params, _, err := ResponsesToChatCompletionRequest(out, "up")
	if err != nil || len(params.Messages) != 4 {
		t.Fatalf("chat bridge: %d messages, err=%v", len(params.Messages), err)
	}
	if !strings.Contains(string(out), `"content":"hi"`) {
		t.Fatalf("content changed: %s", out)
	}
}
