// Package httperr contains the small HTTP error and JSON response helpers
// shared across sleepyrouter's HTTP surface.
//
// HTTPError is the typed error handler routes raise when the request
// fails validation; WriteJSON and WriteJSONError serialize responses in
// the upstream-style error envelope. SafeLogValue sanitizes log strings
// so a misbehaving upstream cannot push terminal escape sequences into
// the router's log lines.
package httperr

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// HTTPError is a typed error with an HTTP status code.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string { return e.Message }

// WriteJSON serializes body as JSON and writes it with the given status code.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	data, _ := utils.MarshalJSONHelper(body)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// WriteJSONError writes the standard upstream-style error envelope:
// `{"error": {"message": message}}`, optionally extended with extra keys
// merged into the inner error object (e.g. "details", "type", "request").
func WriteJSONError(w http.ResponseWriter, status int, message string, extras ...map[string]any) {
	inner := map[string]any{"message": message}
	for _, e := range extras {
		for k, v := range e {
			inner[k] = v
		}
	}
	WriteJSON(w, status, map[string]any{"error": inner})
}

// ReadBody reads the request body and parses it as JSON. An empty body
// yields an empty map; an unparseable body yields an HTTPError so the
// caller can forward the 400 status code.
func ReadBody(r *http.Request) (map[string]any, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	text := string(data)
	if text == "" {
		return map[string]any{}, nil
	}
	var body map[string]any
	if json.Unmarshal(data, &body) != nil {
		return nil, &HTTPError{StatusCode: 400, Message: fmt.Sprintf("요청 본문을 파싱할 수 없어요. 유효한 JSON을 보내주세요. (%d바이트 수신)", len(text))}
	}
	return body, nil
}

// Truncate clamps s to at most max bytes, returning s unchanged when shorter.
func Truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

// controlCharPattern produces a sanitizer that replaces ASCII control
// characters with '?' so a misbehaving upstream can't push terminal
// escapes through the log line. Built once via sync.OnceValue.
var controlCharPattern = sync.OnceValue(func() func(string) string {
	return func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if r < 0x20 || r == 0x7f {
				b.WriteByte('?')
			} else {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
})

// SafeLogValue sanitizes value for inclusion in a log line: it strips
// ASCII control characters and truncates to 200 bytes with an ellipsis.
func SafeLogValue(value string) string {
	sanitized := controlCharPattern()(value)
	if len(sanitized) > 200 {
		return sanitized[:197] + "..."
	}
	return sanitized
}
