// Package httpx provides small HTTP helpers shared across handlers in sleepyrouter.
//
// The package owns three concerns:
//
//   - JSON response writing with the correct Content-Type header
//   - A lightweight status-code recorder that wraps a ResponseWriter for logging
//   - An idiomatic httpError type that carries a status code alongside its message,
//     so handlers can panic with structured errors and a recover defer turns them
//     into proper HTTP responses
//
// Nothing in here depends on the upstream provider layer or protocol translation,
// so it can be reused by any HTTP surface in the project.
package httpx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// Error represents an HTTP error with a status code. It implements the error
// interface and is meant to be used with recover() so handlers can bubble
// structured failures up to the top-level dispatcher.
type Error struct {
	StatusCode int
	Message    string
}

// Error implements the error interface.
func (e *Error) Error() string { return e.Message }

// As returns the embedded Error when err is one, otherwise nil.
func As(err error) *Error {
	if err == nil {
		return nil
	}
	if he, ok := err.(*Error); ok {
		return he
	}
	return nil
}

// New constructs an *Error with the given status and message.
func New(statusCode int, message string) *Error {
	return &Error{StatusCode: statusCode, Message: message}
}

// Newf constructs an *Error with a formatted message.
func Newf(statusCode int, format string, args ...any) *Error {
	return &Error{StatusCode: statusCode, Message: fmt.Sprintf(format, args...)}
}

// WriteJSON serializes body as JSON and writes it with the given status and
// Content-Type. Encoding errors are intentionally swallowed because the response
// has already been partially committed by the time we get here.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	data, _ := utils.MarshalJSONHelper(body)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// ReadJSON reads the entire request body and unmarshals it into a generic map.
// Empty bodies return an empty map. Malformed bodies return an *Error with
// status 400 so handlers can panic-and-recover from a single point.
func ReadJSON(r *http.Request) (map[string]any, error) {
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
		return nil, &Error{
			StatusCode: http.StatusBadRequest,
			Message:    fmt.Sprintf("요청 본문을 파싱할 수 없어요. 유효한 JSON을 보내주세요. (%d바이트 수신)", len(text)),
		}
	}
	return body, nil
}

// String is a no-allocation-safe accessor for the embedded message.
func (e *Error) String() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// responseRecorder wraps an http.ResponseWriter to capture the status code and
// the first write time, primarily so that access logs can read what actually
// went on the wire.
//
// responseRecorder is intentionally a thin shim: it forwards every call to
// the underlying writer except WriteHeader (and Flush when the wrapped writer
// supports it). It is safe to use as a drop-in http.ResponseWriter.
type ResponseRecorder struct {
	http.ResponseWriter
	statusCode int
	wrote      bool
}

// NewResponseRecorder returns a recorder that defaults to 200 (the typical
// http.ResponseWriter behaviour when WriteHeader is never called explicitly).
func NewResponseRecorder(w http.ResponseWriter) *ResponseRecorder {
	return &ResponseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
}

// StatusCode returns the captured status (200 if WriteHeader was never called).
func (r *ResponseRecorder) StatusCode() int {
	return r.statusCode
}

// Wrote reports whether WriteHeader has been called.
func (r *ResponseRecorder) Wrote() bool {
	return r.wrote
}

// WriteHeader captures the status code on the first call and forwards.
func (r *ResponseRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.statusCode = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Flush forwards to the underlying writer when it supports http.Flusher. This
// is required for SSE handlers that rely on the ResponseWriter being a Flusher.
func (r *ResponseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
