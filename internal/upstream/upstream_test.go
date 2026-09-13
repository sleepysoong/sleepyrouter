package upstream_test

import (
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/upstream"
)

func TestRewriteModelPreservesUnknown(t *testing.T) {
	raw := []byte(`{"model":"coding","input":"hi","future_feature":{"x":1},"stream":false}`)
	out, err := upstream.RewriteModel(raw, "vendor/model-b")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	s := string(out)
	if !contains(s, `"model":"vendor/model-b"`) {
		t.Fatalf("model not rewritten: %s", s)
	}
	if !contains(s, "future_feature") {
		t.Fatalf("unknown field dropped: %s", s)
	}
}

func TestMeaningfulEvents(t *testing.T) {
	if upstream.IsMeaningfulEvent("response.created", `{}`) {
		t.Fatal("created should not commit")
	}
	if !upstream.IsMeaningfulEvent("response.output_text.delta", `{"delta":"hi"}`) {
		t.Fatal("text delta should commit")
	}
	if !upstream.IsMeaningfulEvent("response.completed", `{}`) {
		t.Fatal("completed should commit")
	}
}

func TestDeriveRequirements(t *testing.T) {
	r := upstream.DeriveRequirements([]byte(`{"model":"m","tools":[{"type":"function"}],"input":"hi"}`))
	if !r.Tools {
		t.Fatal("tools")
	}
	r2 := upstream.DeriveRequirements([]byte(`{"model":"m","reasoning":{"effort":"high"},"input":"hi"}`))
	if !r2.Reasoning {
		t.Fatal("reasoning")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
