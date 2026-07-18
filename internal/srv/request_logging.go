package srv

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
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
// logUpstreamAttempt fires an "upstream" event for each API call to an
// upstream provider, giving the operator per-attempt visibility into the
// retry loop. The response body is re-wrapped so recordUpstreamFailure can
// still consume it.
func logUpstreamAttempt(logger func(ServerLogEvent), st *handlerState, modelID string, resp *http.Response, err error, attemptStart time.Time) {
	if logger == nil {
		return
	}
	elapsed := int(time.Since(attemptStart).Milliseconds())
	errText := ""
	statusCode := 0
	if err != nil {
		errText = safeLogValue(err.Error())
	} else if resp != nil {
		statusCode = resp.StatusCode
		if !utils.IsOK(resp) {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			errText = safeLogValue(string(bodyBytes))
		}
	}
	logger(ServerLogEvent{
		Type:           "upstream",
		ID:             st.requestID,
		Method:         st.requestMethod,
		Path:           st.requestPath,
		ModelID:        modelID,
		StatusCode:     statusCode,
		DurationMs:     elapsed,
		Error:          errText,
		RequestedModel: st.requestedModel,
		Group:          st.logGroup,
	})
}

func FormatServerLogEvent(event ServerLogEvent, colorEnabled bool) string {
	c := colorEnabled
	if event.Type == "request" {
		return fmt.Sprintf("#%d | %s [%s] %s", event.ID, ansiColor("request", 36, c), ansiColor(event.Method, 35, c), safeLogValue(event.Path))
	}
	if event.Type == "route" {
		cc := ""
		if event.CandidateCount != nil {
			cc = fmt.Sprintf("%d candidates ", *event.CandidateCount)
		}
		line := fmt.Sprintf("#%d | %s %s%s", event.ID, ansiColor("route", 33, c), cc, ansiColor("route="+event.RouteReason, 90, c))
		if event.RequestedModel != "" {
			line += " " + "requested=" + safeLogValue(event.RequestedModel)
		}
		if event.Group != "" {
			line += " " + "group=" + event.Group
		}
		return line
	}
	if event.Type == "upstream" {
		sc := statusColorCode(event.StatusCode)
		line := fmt.Sprintf("#%d | %s %s [%s] %dms", event.ID, ansiColor("upstream", 36, c), safeLogValue(event.ModelID), ansiColor(fmt.Sprintf("%d", event.StatusCode), sc, c), event.DurationMs)
		if event.RequestedModel != "" {
			line += " " + "requested=" + safeLogValue(event.RequestedModel)
		}
		if event.Group != "" {
			line += " " + "group=" + event.Group
		}
		if event.Error != "" {
			line += " " + "error=" + safeLogValue(event.Error)
		}
		return line
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
