package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/protocol"
	"github.com/sleepysoong/sleepyrouter/internal/sseutil"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
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

type openAIToolStreamState struct {
	blockIndex        int
	id                string
	name              string
	started           bool
	bufferedArguments string
}

// anthropicStreamState carries the mutable scan state shared by the closures
// that PipeOpenAIStreamAsAnthropic previously defined inline.
type anthropicStreamState struct {
	w              http.ResponseWriter
	nextBlockIndex int
	textBlockIndex int
	textBlockOpen  bool
	usedTool       bool
	toolBlocks     map[int]*openAIToolStreamState
	toolOrder      []int
	mu             sync.Mutex
}

func (s *anthropicStreamState) ensureTextBlock() int {
	if !s.textBlockOpen {
		s.textBlockIndex = s.nextBlockIndex
		s.nextBlockIndex++
		sseutil.WriteEvent(s.w, "content_block_start", map[string]any{"type": "content_block_start", "index": s.textBlockIndex, "content_block": map[string]any{"type": "text", "text": ""}})
		s.textBlockOpen = true
	}
	return s.textBlockIndex
}

func (s *anthropicStreamState) stopTextBlock() {
	if s.textBlockOpen && s.textBlockIndex >= 0 {
		sseutil.WriteEvent(s.w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": s.textBlockIndex})
		s.textBlockOpen = false
		s.textBlockIndex = -1
	}
}

func (s *anthropicStreamState) ensureToolBlock(toolIndex int, delta map[string]any) *openAIToolStreamState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, exists := s.toolBlocks[toolIndex]
	if !exists {
		state = &openAIToolStreamState{
			blockIndex: s.nextBlockIndex,
			id:         fmt.Sprintf("toolu_%d_%d", time.Now().UnixMilli(), toolIndex),
			name:       utils.StringFromUnknown(delta["name"]),
		}
		s.nextBlockIndex++
		s.toolBlocks[toolIndex] = state
		s.toolOrder = append(s.toolOrder, toolIndex)
	}
	if id, ok := delta["id"].(string); ok && id != "" {
		state.id = id
	}
	if name, ok := delta["name"].(string); ok && name != "" {
		state.name = name
	}
	if !state.started && state.name != "" {
		s.stopTextBlock()
		sseutil.WriteEvent(s.w, "content_block_start", map[string]any{"type": "content_block_start", "index": state.blockIndex, "content_block": map[string]any{"type": "tool_use", "id": state.id, "name": state.name, "input": map[string]any{}}})
		state.started = true
		s.usedTool = true
		if state.bufferedArguments != "" {
			sseutil.WriteEvent(s.w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": state.blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": state.bufferedArguments}})
			state.bufferedArguments = ""
		}
	}
	return state
}

type openAIStreamToolCall struct {
	Index    *int    `json:"index"`
	ID       *string `json:"id"`
	Function *struct {
		Name      *string `json:"name"`
		Arguments *string `json:"arguments"`
	} `json:"function"`
}

type openAIStreamChoice struct {
	FinishReason any `json:"finish_reason"`
	Delta        *struct {
		Content      *string `json:"content"`
		FunctionCall *struct {
			Name      *string `json:"name"`
			Arguments *string `json:"arguments"`
		} `json:"function_call"`
		ToolCalls []openAIStreamToolCall `json:"tool_calls"`
	} `json:"delta"`
}

// PipeOpenAIStreamAsAnthropic reads an OpenAI streaming response and
// converts each `data:` chunk to the equivalent Anthropic SSE event sequence
// (message_start, content_block_*, message_delta, message_stop).
func PipeOpenAIStreamAsAnthropic(body io.ReadCloser, w http.ResponseWriter, model string) {
	sseutil.Headers(w)
	sseutil.WriteEvent(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            fmt.Sprintf("msg_%d", time.Now().UnixMilli()),
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})

	if body == nil {
		sseutil.WriteEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
		sseutil.WriteEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		sseutil.WriteEvent(w, "message_stop", map[string]any{"type": "message_stop"})
		return
	}
	defer func() { _ = body.Close() }()

	st := &anthropicStreamState{
		w:              w,
		textBlockIndex: -1,
		toolBlocks:     make(map[int]*openAIToolStreamState),
	}

	var (
		finishReason any
		outputTokens int
	)

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[5:])
		if data == "" || data == "[DONE]" || !strings.HasPrefix(data, "{") {
			continue
		}

		var chunk struct {
			Usage *struct {
				CompletionTokens any `json:"completion_tokens"`
				OutputTokens     any `json:"output_tokens"`
			} `json:"usage"`
			Choices []openAIStreamChoice `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		var choice *openAIStreamChoice
		if len(chunk.Choices) > 0 {
			choice = &openAIStreamChoice{
				FinishReason: chunk.Choices[0].FinishReason,
				Delta:        chunk.Choices[0].Delta,
			}
		}
		if choice != nil && choice.FinishReason != nil {
			finishReason = choice.FinishReason
		}
		if chunk.Usage != nil {
			if v := sseutil.ParseToken(chunk.Usage.CompletionTokens); v != nil {
				outputTokens = *v
			}
			if v := sseutil.ParseToken(chunk.Usage.OutputTokens); v != nil {
				outputTokens = *v
			}
		}
		if choice != nil && choice.Delta != nil {
			if choice.Delta.Content != nil && *choice.Delta.Content != "" {
				sseutil.WriteEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": st.ensureTextBlock(), "delta": map[string]any{"type": "text_delta", "text": *choice.Delta.Content}})
			}
			for _, tc := range choice.Delta.ToolCalls {
				toolIndex := 0
				if tc.Index != nil {
					toolIndex = *tc.Index
				}
				delta := map[string]any{}
				if tc.ID != nil {
					delta["id"] = *tc.ID
				}
				if tc.Function != nil && tc.Function.Name != nil {
					delta["name"] = *tc.Function.Name
				}
				state := st.ensureToolBlock(toolIndex, delta)
				if tc.Function != nil && tc.Function.Arguments != nil {
					partialJson := *tc.Function.Arguments
					if state.started {
						sseutil.WriteEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": state.blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": partialJson}})
					} else {
						state.bufferedArguments += partialJson
					}
				}
			}
			if choice.Delta.FunctionCall != nil {
				delta := map[string]any{}
				if choice.Delta.FunctionCall.Name != nil {
					delta["name"] = *choice.Delta.FunctionCall.Name
				}
				state := st.ensureToolBlock(0, delta)
				if choice.Delta.FunctionCall.Arguments != nil {
					partialJson := *choice.Delta.FunctionCall.Arguments
					if state.started {
						sseutil.WriteEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": state.blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": partialJson}})
					} else {
						state.bufferedArguments += partialJson
					}
				}
			}
		}
	}

	if !st.textBlockOpen && len(st.toolBlocks) == 0 {
		st.ensureTextBlock()
	}
	st.stopTextBlock()
	for _, idx := range st.toolOrder {
		state := st.toolBlocks[idx]
		if !state.started {
			sseutil.WriteEvent(w, "content_block_start", map[string]any{
				"type":          "content_block_start",
				"index":         state.blockIndex,
				"content_block": map[string]any{"type": "tool_use", "id": state.id, "name": valueOr(state.name, "tool"), "input": map[string]any{}},
			})
			if state.bufferedArguments != "" {
				sseutil.WriteEvent(w, "content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": state.blockIndex,
					"delta": map[string]any{"type": "input_json_delta", "partial_json": state.bufferedArguments},
				})
			}
			state.started = true
			st.usedTool = true
		}
		if state.started {
			sseutil.WriteEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": state.blockIndex})
		}
	}

	stopReason := protocol.MapStopReason(finishReason)
	if st.usedTool {
		stopReason = "tool_use"
	}
	sseutil.WriteEvent(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": outputTokens}})
	sseutil.WriteEvent(w, "message_stop", map[string]any{"type": "message_stop"})
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
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
