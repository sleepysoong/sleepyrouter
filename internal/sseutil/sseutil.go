// Package sseutil contains low-level Server-Sent Events helpers used by
// sleepyrouter's streams package.
//
// The package deliberately keeps the surface tiny: SSE headers, single-event
// helper, buffer frame splitting, and a usage-token extractor. Higher-level
// orchestration (OpenAI→Anthropic translation, streaming fallbacks) lives in
// internal/srv/streams.go.
package sseutil

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// Headers writes the standard SSE response headers and flushes immediately
// so the client receives the response head as soon as possible.
//
// The Cache-Control/Connection pair keeps proxies from buffering and is the
// canonical SSE preamble per the WHATWG spec.
func Headers(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// WriteEvent serializes data as JSON and writes one SSE event to the wire,
// flushing afterwards so callers do not need to handle flushing themselves.
func WriteEvent(w http.ResponseWriter, event string, data any) {
	jsonData, _ := utils.MarshalJSONHelper(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(jsonData))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// SplitFrames returns the complete SSE frames in buffer plus any residual
// incomplete tail. Frames are detected as \n\n or \r\n\r\n boundaries. This
// is useful when reading a streaming body with buffered scanners and we want
// round-trip fidelity through the proxy.
//
// The function walks the buffer in a single pass to keep allocations small
// when used inside hot streaming paths.
func splitFrames(buffer string) ([]string, string) {
	frames := []string{}
	cursor := 0
	for {
		idx := strings.Index(buffer[cursor:], "\n\n")
		if idx < 0 {
			idx = strings.Index(buffer[cursor:], "\r\n\r\n")
			if idx < 0 {
				break
			}
			frames = append(frames, buffer[cursor:cursor+idx])
			cursor = cursor + idx + 4
			continue
		}
		frames = append(frames, buffer[cursor:cursor+idx])
		cursor = cursor + idx + 2
	}
	return frames, buffer[cursor:]
}

// ParseToken returns a non-negative integer pointer if value is a JSON number
// (either float64 with no fractional part or a plain int). nil otherwise.
// Because JSON numbers decode to float64 in Go, we guard against fractional
// or negative values that could otherwise be misread as token counts.
func ParseToken(value any) *int {
	if number, ok := value.(float64); ok && number >= 0 && number == float64(int(number)) {
		n := int(number)
		if n < 0 {
			return nil
		}
		return utils.IntPointer(n)
	}
	if number, ok := value.(int); ok && number >= 0 {
		return utils.IntPointer(number)
	}
	return nil
}
