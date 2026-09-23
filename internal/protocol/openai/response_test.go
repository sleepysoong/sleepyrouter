package openai_test

import (
	"encoding/json"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/protocol/openai"
)

func TestRewriteStreamEventModelOnlyNestedResponse(t *testing.T) {
	in := `{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","model":"upstream","metadata":{"other":"keep"}}}`
	out := openai.RewriteStreamEventModel(in, "virtual")
	var v struct {
		Model    string `json:"model"`
		Response struct {
			Model    string            `json:"model"`
			Metadata map[string]string `json:"metadata"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	if v.Model != "" || v.Response.Model != "virtual" || v.Response.Metadata["other"] != "keep" {
		t.Fatalf("rewritten=%s", out)
	}
	delta := `{"type":"response.output_text.delta","delta":"model"}`
	if got := openai.RewriteStreamEventModel(delta, "virtual"); got != delta {
		t.Fatalf("delta changed: %s", got)
	}
}

func TestStreamErrorEventMatchesResponsesShape(t *testing.T) {
	var v struct {
		Type           string  `json:"type"`
		Code           string  `json:"code"`
		Message        string  `json:"message"`
		Param          *string `json:"param"`
		SequenceNumber int64   `json:"sequence_number"`
	}
	if err := json.Unmarshal(openai.StreamErrorEvent("cut", 4), &v); err != nil {
		t.Fatal(err)
	}
	if v.Type != "error" || v.Code == "" || v.Message != "cut" || v.Param != nil || v.SequenceNumber != 4 {
		t.Fatalf("event=%+v", v)
	}
}
