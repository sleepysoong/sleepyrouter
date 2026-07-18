package sseutil

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseToken(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  *int
	}{
		{"nil", nil, nil},
		{"float positive", float64(42), intPtr(42)},
		{"float zero", float64(0), intPtr(0)},
		{"float fractional", float64(3.14), nil},
		{"float negative", float64(-5), nil},
		{"int positive", 42, intPtr(42)},
		{"int zero", 0, intPtr(0)},
		{"int negative", -5, nil},
		{"string", "42", nil},
		{"bool", true, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseToken(c.value)
			if (c.want == nil) != (got == nil) {
				t.Fatalf("nil mismatch: want %v, got %v", c.want, got)
			}
			if c.want != nil && *got != *c.want {
				t.Errorf("value: want %d, got %d", *c.want, *got)
			}
		})
	}
}

func intPtr(n int) *int { return &n }

func TestSplitFrames(t *testing.T) {
	cases := []struct {
		name      string
		buffer    string
		wantFull  []string
		wantTrail string
	}{
		{"empty", "", []string{}, ""},
		{"single complete \\n\\n", "event: a\ndata: 1\n\n", []string{"event: a\ndata: 1"}, ""},
		{"two complete \\n\\n", "a: 1\n\nb: 2\n\n", []string{"a: 1", "b: 2"}, ""},
		{"trailing partial", "a: 1\n\ncruft", []string{"a: 1"}, "cruft"},
		{"\\r\\n\\r\\n boundary", "a: 1\r\n\r\nb: 2\r\n\r\n", []string{"a: 1", "b: 2"}, ""},
		{"no terminator", "just bytes", []string{}, "just bytes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotFrames, gotTrail := SplitFrames(c.buffer)
			if gotTrail != c.wantTrail {
				t.Errorf("trail: got %q, want %q", gotTrail, c.wantTrail)
			}
			if len(gotFrames) != len(c.wantFull) {
				t.Fatalf("frame count: got %d, want %d (%v)", len(gotFrames), len(c.wantFull), gotFrames)
			}
			for i, f := range c.wantFull {
				if gotFrames[i] != f {
					t.Errorf("frame %d: got %q, want %q", i, gotFrames[i], f)
				}
			}
		})
	}
}

func TestHeaders_SetsSSEHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	Headers(rec)
	h := rec.Result().Header
	if ct := h.Get("Content-Type"); ct != "text/event-stream; charset=utf-8" {
		t.Errorf("Content-Type: got %q, want text/event-stream; charset=utf-8", ct)
	}
	if cc := h.Get("Cache-Control"); cc != "no-cache, no-transform" {
		t.Errorf("Cache-Control: got %q, want no-cache, no-transform", cc)
	}
	if c := h.Get("Connection"); c != "keep-alive" {
		t.Errorf("Connection: got %q, want keep-alive", c)
	}
	if rec.Code != 200 {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
}

func TestWriteEvent_SerializesJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteEvent(rec, "message_start", map[string]any{"type": "message_start"})
	body := rec.Body.String()
	if !strings.HasPrefix(body, "event: message_start\n") {
		t.Errorf("missing event prefix: %q", body)
	}
	if !strings.Contains(body, `data: {"type":"message_start"}`) {
		t.Errorf("missing data line: %q", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("missing trailing double-newline: %q", body)
	}
}
