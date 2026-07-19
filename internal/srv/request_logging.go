package srv

import (
	"bytes"
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
