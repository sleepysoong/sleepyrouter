package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/sseutil"
	"github.com/sleepysoong/sleepyrouter/internal/types"
)

// StreamUsage is what the streaming pipe reports back so the caller can
// append usage entries without re-scanning the body.
type StreamUsage struct {
	InputTokens  *int
	OutputTokens *int
	TotalTokens  *int
}

// PipeWebStreamToNode reads an upstream SSE stream, writes each line to the
// client, and harvests the final usage block for the caller to log.
func PipeWebStreamToNode(body io.ReadCloser, w http.ResponseWriter) StreamUsage {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	if body == nil {
		return StreamUsage{}
	}
	defer func() { _ = body.Close() }()

	usage := StreamUsage{}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintf(w, "%s\n", line)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[5:])
		if data == "" || data == "[DONE]" || !strings.HasPrefix(data, "{") {
			continue
		}
		var chunk struct {
			Usage *struct {
				PromptTokens     any `json:"prompt_tokens"`
				InputTokens      any `json:"input_tokens"`
				CompletionTokens any `json:"completion_tokens"`
				OutputTokens     any `json:"output_tokens"`
				TotalTokens      any `json:"total_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal([]byte(data), &chunk) == nil && chunk.Usage != nil {
			if v := sseutil.ParseToken(chunk.Usage.PromptTokens); v != nil {
				usage.InputTokens = v
			} else if v := sseutil.ParseToken(chunk.Usage.InputTokens); v != nil {
				usage.InputTokens = v
			}
			if v := sseutil.ParseToken(chunk.Usage.CompletionTokens); v != nil {
				usage.OutputTokens = v
			} else if v := sseutil.ParseToken(chunk.Usage.OutputTokens); v != nil {
				usage.OutputTokens = v
			}
			if v := sseutil.ParseToken(chunk.Usage.TotalTokens); v != nil {
				usage.TotalTokens = v
			}
		}
	}
	return usage
}

// WriteStreamResponse writes a successful streaming upstream response to the
// client wire, records usage, and returns the observed token counts and attempt count for logging.
func WriteStreamResponse(w http.ResponseWriter, upstream *http.Response, store *cfg.ConfigStore, model types.SleepyRouterModel, triedCount int) (inputTokens, outputTokens, tried *int) {
	contentType := upstream.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/event-stream; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(upstream.StatusCode)
	streamUsage := PipeWebStreamToNode(upstream.Body, w)
	usageID := model.UsageID
	if usageID == "" {
		usageID = model.ID
	}
	in := 0
	out := 0
	if streamUsage.InputTokens != nil {
		in = *streamUsage.InputTokens
	}
	if streamUsage.OutputTokens != nil {
		out = *streamUsage.OutputTokens
	}
	_ = store.AppendUsage(types.UsageLogEntry{TS: time.Now().UTC().Format(time.RFC3339), Model: usageID, InputTokens: in, OutputTokens: out, Success: true})
	t := triedCount
	return streamUsage.InputTokens, streamUsage.OutputTokens, &t
}
