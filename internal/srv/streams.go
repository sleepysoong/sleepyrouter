package srv

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/protocol"
	"github.com/sleepysoong/sleepyrouter/internal/sseutil"
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
	defer body.Close()

	usage := StreamUsage{}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(w, "%s\n", line)
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
	defer body.Close()

	var (
		usedTool       bool
		nextBlockIndex int
		textBlockIndex = -1
		textBlockOpen  bool
		finishReason   any
		outputTokens   int
		toolBlocks     = make(map[int]*openAIToolStreamState)
		toolOrder      []int
		mu             sync.Mutex
	)

	ensureTextBlock := func() int {
		if !textBlockOpen {
			textBlockIndex = nextBlockIndex
			nextBlockIndex++
			sseutil.WriteEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": textBlockIndex, "content_block": map[string]any{"type": "text", "text": ""}})
			textBlockOpen = true
		}
		return textBlockIndex
	}
	stopTextBlock := func() {
		if textBlockOpen && textBlockIndex >= 0 {
			sseutil.WriteEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": textBlockIndex})
			textBlockOpen = false
			textBlockIndex = -1
		}
	}
	ensureToolBlock := func(toolIndex int, delta map[string]any) *openAIToolStreamState {
		mu.Lock()
		defer mu.Unlock()
		state, exists := toolBlocks[toolIndex]
		if !exists {
			state = &openAIToolStreamState{
				blockIndex: nextBlockIndex,
				id:         fmt.Sprintf("toolu_%d_%d", time.Now().UnixMilli(), toolIndex),
				name:       utils.StringFromUnknown(delta["name"]),
			}
			nextBlockIndex++
			toolBlocks[toolIndex] = state
			toolOrder = append(toolOrder, toolIndex)
		}
		if id, ok := delta["id"].(string); ok && id != "" {
			state.id = id
		}
		if name, ok := delta["name"].(string); ok && name != "" {
			state.name = name
		}
		if !state.started && state.name != "" {
			stopTextBlock()
			sseutil.WriteEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": state.blockIndex, "content_block": map[string]any{"type": "tool_use", "id": state.id, "name": state.name, "input": map[string]any{}}})
			state.started = true
			usedTool = true
			if state.bufferedArguments != "" {
				sseutil.WriteEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": state.blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": state.bufferedArguments}})
				state.bufferedArguments = ""
			}
		}
		return state
	}

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
			Choices []struct {
				FinishReason any `json:"finish_reason"`
				Delta        *struct {
					Content      *string `json:"content"`
					FunctionCall *struct {
						Name      *string `json:"name"`
						Arguments *string `json:"arguments"`
					} `json:"function_call"`
					ToolCalls []struct {
						Index    *int    `json:"index"`
						ID       *string `json:"id"`
						Function *struct {
							Name      *string `json:"name"`
							Arguments *string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		var choice *struct {
			FinishReason any `json:"finish_reason"`
			Delta        *struct {
				Content      *string `json:"content"`
				FunctionCall *struct {
					Name      *string `json:"name"`
					Arguments *string `json:"arguments"`
				} `json:"function_call"`
				ToolCalls []struct {
					Index    *int    `json:"index"`
					ID       *string `json:"id"`
					Function *struct {
						Name      *string `json:"name"`
						Arguments *string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		}
		if len(chunk.Choices) > 0 {
			choice = &struct {
				FinishReason any `json:"finish_reason"`
				Delta        *struct {
					Content      *string `json:"content"`
					FunctionCall *struct {
						Name      *string `json:"name"`
						Arguments *string `json:"arguments"`
					} `json:"function_call"`
					ToolCalls []struct {
						Index    *int    `json:"index"`
						ID       *string `json:"id"`
						Function *struct {
							Name      *string `json:"name"`
							Arguments *string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			}{
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
				sseutil.WriteEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": ensureTextBlock(), "delta": map[string]any{"type": "text_delta", "text": *choice.Delta.Content}})
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
				state := ensureToolBlock(toolIndex, delta)
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
				state := ensureToolBlock(0, delta)
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

	if !textBlockOpen && len(toolBlocks) == 0 {
		ensureTextBlock()
	}
	stopTextBlock()
	for _, idx := range toolOrder {
		state := toolBlocks[idx]
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
			usedTool = true
		}
		if state.started {
			sseutil.WriteEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": state.blockIndex})
		}
	}

	stopReason := protocol.MapStopReason(finishReason)
	if usedTool {
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
