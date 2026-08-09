package utils

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestParseDotEnv(t *testing.T) {
	content := "KEY=value\r\n  SPACED = padded  \n# comment\n\nQUOTED=\"two words\"\nSINGLE='raw'\nNO_EQUALS\n"
	env := ParseDotEnv(content)
	cases := map[string]string{
		"KEY":    "value",
		"SPACED": "padded",
		"QUOTED": "two words",
		"SINGLE": "raw",
	}
	for key, want := range cases {
		if got := env[key]; got != want {
			t.Errorf("ParseDotEnv[%q] = %q, want %q", key, got, want)
		}
	}
	for _, key := range []string{"comment", "NO_EQUALS"} {
		if _, ok := env[key]; ok {
			t.Errorf("ParseDotEnv should not contain %q, got %q", key, env[key])
		}
	}
}

func TestGetConfigRoot(t *testing.T) {
	if got := GetConfigRoot(Environment{"SLEEPYROUTER_HOME": "/srv/home"}); got != "/srv/home" {
		t.Errorf("GetConfigRoot with SLEEPYROUTER_HOME = %q", got)
	}
	if got := GetConfigPath("/root/dir"); got != "/root/dir/config.json" {
		t.Errorf("GetConfigPath = %q", got)
	}
	if got := GetEnvPath("/root/dir"); got != "/root/dir/.env" {
		t.Errorf("GetEnvPath = %q", got)
	}
	if got := GetUsagePath("/root/dir"); got != "/root/dir/usage.jsonl" {
		t.Errorf("GetUsagePath = %q", got)
	}
}

func TestIsTerminalDevNull(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Error("/dev/null should not report as a terminal")
	}
}

func TestMarshalJSONHelper(t *testing.T) {
	data, err := MarshalJSONHelper(map[string]any{"html": "<b>&"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"<b>&"`)) {
		t.Errorf("MarshalJSONHelper should not escape HTML, got %s", data)
	}
	if data[len(data)-1] == '\n' {
		t.Error("MarshalJSONHelper should not end with a newline")
	}
}

func TestIsOKAndStatusText(t *testing.T) {
	if !IsOK(&http.Response{StatusCode: 200}) {
		t.Error("200 should be OK")
	}
	if IsOK(&http.Response{StatusCode: 404}) {
		t.Error("404 should not be OK")
	}
	if IsOK(nil) {
		t.Error("nil response should not be OK")
	}
	if got := StatusText(&http.Response{Status: "200 OK"}); got != "OK" {
		t.Errorf("StatusText = %q, want OK", got)
	}
	if got := StatusText(&http.Response{StatusCode: http.StatusBadGateway}); got != "Bad Gateway" {
		t.Errorf("StatusText = %q, want Bad Gateway", got)
	}
	if got := StatusText(nil); got != "" {
		t.Errorf("StatusText(nil) = %q, want empty", got)
	}
}

func TestResponseJSON(t *testing.T) {
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"a":1}`))}
	body, err := ResponseJSON(resp)
	if err != nil {
		t.Fatal(err)
	}
	if body["a"] != float64(1) {
		t.Errorf("body[a] = %v, want 1", body["a"])
	}
	if _, err := ResponseJSON(&http.Response{}); err == nil {
		t.Error("ResponseJSON with nil body should error")
	}
}

func TestCloneObject(t *testing.T) {
	orig := map[string]any{"k": 1}
	clone := CloneObject(orig)
	clone["k"] = 2
	clone["extra"] = true
	if orig["k"] != 1 {
		t.Error("CloneObject must not share the underlying map")
	}
	if _, ok := orig["extra"]; ok {
		t.Error("CloneObject must not mutate the original map")
	}
}

func TestBoolValue(t *testing.T) {
	cases := []struct {
		in   any
		want bool
	}{
		{nil, false},
		{true, true},
		{false, false},
		{float64(0), false},
		{float64(1), true},
		{"", false},
		{"x", true},
		{struct{}{}, true},
	}
	for _, c := range cases {
		if got := BoolValue(c.in); got != c.want {
			t.Errorf("BoolValue(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestNumberValue(t *testing.T) {
	if got := NumberValue(float64(5)); got == nil || *got != 5 {
		t.Errorf("NumberValue(5.0) = %v, want 5", got)
	}
	if got := NumberValue(int(7)); got == nil || *got != 7 {
		t.Errorf("NumberValue(7) = %v, want 7", got)
	}
	if got := NumberValue(float64(0.5)); got != nil {
		t.Errorf("NumberValue(0.5) = %v, want nil", got)
	}
	if got := NumberValue(-1.0); got != nil {
		t.Errorf("NumberValue(-1.0) = %v, want nil", got)
	}
	if got := NumberValue("x"); got != nil {
		t.Errorf("NumberValue(\"x\") = %v, want nil", got)
	}
}

func TestStringHelpers(t *testing.T) {
	if got := StringFromUnknown("s"); got != "s" {
		t.Errorf("StringFromUnknown(\"s\") = %q", got)
	}
	if got := StringFromUnknown(42); got != "" {
		t.Errorf("StringFromUnknown(42) = %q, want empty", got)
	}
	if got := UnknownString("s"); got != "s" {
		t.Errorf("UnknownString(\"s\") = %q", got)
	}
	if got := UnknownString(42); got != "42" {
		t.Errorf("UnknownString(42) = %q, want 42", got)
	}
	if got := IntPointer(3); got == nil || *got != 3 {
		t.Errorf("IntPointer(3) = %v, want 3", got)
	}
}

func TestHTTPClient(t *testing.T) {
	if HTTPClient(nil) != http.DefaultClient {
		t.Error("HTTPClient(nil) should return http.DefaultClient")
	}
	mock := HTTPClientFunc(func(*http.Request) (*http.Response, error) { return nil, nil })
	if _, ok := HTTPClient(mock).(HTTPClientFunc); !ok {
		t.Error("HTTPClient should pass through a non-nil client")
	}
}

func TestGetRequest(t *testing.T) {
	req, err := GetRequest(t.Context(), "https://example.com/resource", map[string]string{"X-A": "b"})
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodGet {
		t.Errorf("method = %q, want GET", req.Method)
	}
	if got := req.Header.Get("X-A"); got != "b" {
		t.Errorf("header X-A = %q, want b", got)
	}
}

func TestJSONRequest(t *testing.T) {
	req, err := JSONRequest(t.Context(), http.MethodPost, "https://example.com/resource", map[string]string{"X-A": "b"}, map[string]any{"n": 1})
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodPost {
		t.Errorf("method = %q, want POST", req.Method)
	}
	body, _ := io.ReadAll(req.Body)
	if string(body) != `{"n":1}` {
		t.Errorf("body = %s, want {\"n\":1}", body)
	}
}