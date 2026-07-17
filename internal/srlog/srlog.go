// Package srlog formats HTTP request/response log lines for sleepyrouter.
//
// The package is intentionally a stand-alone formatter: it owns the log line
// shape and ANSI colour mapping but does not perform any I/O itself. Callers
// pass formatted lines to their own logger. This design keeps the package free
// of side effects and trivially testable in isolation.
package srlog

import (
	"fmt"
	"strings"
	"sync"
)

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

// safeLogValue strips control characters and clamps length to a sane upper
// bound so that a malicious or malformed value cannot blow up log output.
func safeLogValue(value string) string {
	sanitized := controlCharPattern()(value)
	if len(sanitized) > 200 {
		return sanitized[:197] + "..."
	}
	return sanitized
}

func ansiColor(value string, code int, enabled bool) string {
	if enabled {
		return fmt.Sprintf("\x1b[%dm%s\x1b[0m", code, value)
	}
	return value
}

func statusColorCode(statusCode int) int {
	if statusCode >= 500 {
		return 31
	}
	if statusCode >= 400 {
		return 33
	}
	return 32
}

// Event represents one observable request/response pair emitted by the
// router. Fields use *int / pointer types so an absent metric is reported
// exactly as such rather than as a misleading zero.
type Event struct {
	Type           string
	ID             int
	Method         string
	Path           string
	StatusCode     int
	DurationMs     int
	RequestedModel string
	ModelID        string
	RouteReason    string
	Stream         bool
	InputTokens    *int
	OutputTokens   *int
	Error          string
	Group          string
	CandidateCount *int
	TriedCount     *int
}

// Format renders a single line suitable for either stdout (with Colour=true)
// or an arbitrary log sink (Colour=false). The Type field alternates between
// "request" and "response".
func Format(event Event, color bool) string {
	c := color
	if event.Type == "request" {
		return fmt.Sprintf("#%d | %s [%s] %s", event.ID, ansiColor("request", 36, c), ansiColor(event.Method, 35, c), safeLogValue(event.Path))
	}
	sc := statusColorCode(event.StatusCode)
	parts := []string{
		fmt.Sprintf("#%d | %s [%s] %s [%s] %s",
			event.ID,
			ansiColor("response", sc, c),
			ansiColor(fmt.Sprintf("%d", event.StatusCode), sc, c),
			ansiColor(fmt.Sprintf("%dms", event.DurationMs), 90, c),
			ansiColor(event.Method, 35, c),
			safeLogValue(event.Path),
		),
	}
	if event.RequestedModel != "" {
		parts = append(parts, "requested="+safeLogValue(event.RequestedModel))
	}
	if event.ModelID != "" {
		parts = append(parts, "model="+safeLogValue(event.ModelID))
	}
	if event.RouteReason != "" {
		parts = append(parts, "route="+event.RouteReason)
	}
	if event.Group != "" {
		parts = append(parts, "group="+event.Group)
	}
	if event.CandidateCount != nil {
		parts = append(parts, fmt.Sprintf("candidates=%d", *event.CandidateCount))
	}
	if event.TriedCount != nil {
		parts = append(parts, fmt.Sprintf("tried=%d", *event.TriedCount))
	}
	if event.InputTokens != nil {
		parts = append(parts, fmt.Sprintf("in=%d", *event.InputTokens))
	}
	if event.OutputTokens != nil {
		parts = append(parts, fmt.Sprintf("out=%d", *event.OutputTokens))
	}
	if event.Stream {
		parts = append(parts, "stream=true")
	}
	if event.Error != "" {
		parts = append(parts, "error="+safeLogValue(event.Error))
	}
	return strings.Join(parts, " ")
}
