package srv

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// ServerOptions configures a SleepyRouter http.Server. Each field has a
// sensible default so callers only set what they need.
type ServerOptions struct {
	Store         *cfg.ConfigStore
	FetchImpl     types.HTTPDoer
	Env           utils.Environment
	RequestLogger func(ServerLogEvent)
	StartTime     time.Time
}

// ServerLogEvent is the structured kind that gets handed to the
// configured RequestLogger. ServerLogEvent.Type is either "request"
// (when the request enters the router) or "response" (when it leaves).
type ServerLogEvent struct {
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

func safeLogValue(value string) string {
	sanitized := controlCharPattern()(value)
	if len(sanitized) > 200 {
		return sanitized[:197] + "..."
	}
	return sanitized
}

// ansiColor wraps value in an ANSI escape sequence only when enabled, so
// callers don't need to know whether the output target is a TTY.
func ansiColor(value string, code int, enabled bool) string {
	if enabled {
		return fmt.Sprintf("\x1b[%dm%s\x1b[0m", code, value)
	}
	return value
}

// statusColorCode maps HTTP status codes to ANSI colour codes:
// 31 (red) for 5xx, 33 (yellow) for 4xx, 32 (green) otherwise.
func statusColorCode(statusCode int) int {
	if statusCode >= 500 {
		return 31
	}
	if statusCode >= 400 {
		return 33
	}
	return 32
}

// FormatServerLogEvent renders a single log line. Request events are
// concise; response events include routing, status, duration, usage,
// and error context.
func FormatServerLogEvent(event ServerLogEvent, colorEnabled bool) string {
	c := colorEnabled
	if event.Type == "request" {
		return fmt.Sprintf("#%d | %s [%s] %s", event.ID, ansiColor("request", 36, c), ansiColor(event.Method, 35, c), safeLogValue(event.Path))
	}
	sc := statusColorCode(event.StatusCode)
	details := []string{
		fmt.Sprintf("#%d | %s [%s] %s [%s] %s", event.ID, ansiColor("response", sc, c), ansiColor(fmt.Sprintf("%d", event.StatusCode), sc, c), ansiColor(fmt.Sprintf("%dms", event.DurationMs), 90, c), ansiColor(event.Method, 35, c), safeLogValue(event.Path)),
	}
	if event.RequestedModel != "" {
		details = append(details, "requested="+safeLogValue(event.RequestedModel))
	}
	if event.ModelID != "" {
		details = append(details, "model="+safeLogValue(event.ModelID))
	}
	if event.RouteReason != "" {
		details = append(details, "route="+event.RouteReason)
	}
	if event.Group != "" {
		details = append(details, "group="+event.Group)
	}
	if event.CandidateCount != nil {
		details = append(details, fmt.Sprintf("candidates=%d", *event.CandidateCount))
	}
	if event.TriedCount != nil {
		details = append(details, fmt.Sprintf("tried=%d", *event.TriedCount))
	}
	if event.InputTokens != nil {
		details = append(details, fmt.Sprintf("in=%d", *event.InputTokens))
	}
	if event.OutputTokens != nil {
		details = append(details, fmt.Sprintf("out=%d", *event.OutputTokens))
	}
	if event.Stream {
		details = append(details, "stream=true")
	}
	if event.Error != "" {
		details = append(details, "error="+safeLogValue(event.Error))
	}
	return strings.Join(details, " ")
}
