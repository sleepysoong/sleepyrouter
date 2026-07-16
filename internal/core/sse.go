package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

type StreamUsage struct {
	InputTokens  *int
	OutputTokens *int
	TotalTokens  *int
}

func writeSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func writeSSEEvent(w http.ResponseWriter, event string, data any) {
	jsonData, _ := utils.MarshalJSONHelper(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(jsonData))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// completeSseFrames splits an SSE buffer into complete frames and returns the remainder.
func completeSseFrames(buffer string) ([]string, string) {
	frames := []string{}
	cursor := 0
	for {
		idx := strings.Index(buffer[cursor:], "\n\n")
		if idx < 0 {
			// Also check for \r\n\r\n
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

func parseSSEReaderToken(value any) *int {
	if number, ok := value.(float64); ok && number >= 0 && number == float64(int(number)) {
		return utils.IntPointer(maxInt(0, int(number)))
	}
	if number, ok := value.(int); ok && number >= 0 {
		return utils.IntPointer(number)
	}
	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// PipeWebStreamToNode reads an upstream SSE stream and pipes it through to the client,
// extracting usage information from the final chunk.
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
		// Write raw line + newline to client
		fmt.Fprintf(w, "%s\n", line)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Parse for usage
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
			if v := parseSSEReaderToken(chunk.Usage.PromptTokens); v != nil {
				usage.InputTokens = v
			} else if v := parseSSEReaderToken(chunk.Usage.InputTokens); v != nil {
				usage.InputTokens = v
			}
			if v := parseSSEReaderToken(chunk.Usage.CompletionTokens); v != nil {
				usage.OutputTokens = v
			} else if v := parseSSEReaderToken(chunk.Usage.OutputTokens); v != nil {
				usage.OutputTokens = v
			}
			if v := parseSSEReaderToken(chunk.Usage.TotalTokens); v != nil {
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

// PipeOpenAIStreamAsAnthropic reads an OpenAI streaming response and converts it to
// Anthropic SSE events for the client.
func PipeOpenAIStreamAsAnthropic(body io.ReadCloser, w http.ResponseWriter, model string) {
	writeSSEHeaders(w)
	writeSSEEvent(w, "message_start", map[string]any{
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
		writeSSEEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
		writeSSEEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		writeSSEEvent(w, "message_stop", map[string]any{"type": "message_stop"})
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
			writeSSEEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": textBlockIndex, "content_block": map[string]any{"type": "text", "text": ""}})
			textBlockOpen = true
		}
		return textBlockIndex
	}
	stopTextBlock := func() {
		if textBlockOpen && textBlockIndex >= 0 {
			writeSSEEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": textBlockIndex})
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
			writeSSEEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": state.blockIndex, "content_block": map[string]any{"type": "tool_use", "id": state.id, "name": state.name, "input": map[string]any{}}})
			state.started = true
			usedTool = true
			if state.bufferedArguments != "" {
				writeSSEEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": state.blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": state.bufferedArguments}})
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
			if v := parseSSEReaderToken(chunk.Usage.CompletionTokens); v != nil {
				outputTokens = *v
			}
			if v := parseSSEReaderToken(chunk.Usage.OutputTokens); v != nil {
				outputTokens = *v
			}
		}
		if choice != nil && choice.Delta != nil {
			if choice.Delta.Content != nil && *choice.Delta.Content != "" {
				writeSSEEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": ensureTextBlock(), "delta": map[string]any{"type": "text_delta", "text": *choice.Delta.Content}})
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
						writeSSEEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": state.blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": partialJson}})
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
						writeSSEEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": state.blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": partialJson}})
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
			writeSSEEvent(w, "content_block_start", map[string]any{"type": "content_block_start", "index": state.blockIndex, "content_block": map[string]any{"type": "tool_use", "id": state.id, "name": valueOr(state.name, "tool"), "input": map[string]any{}}})
			if state.bufferedArguments != "" {
				writeSSEEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": state.blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": state.bufferedArguments}})
			}
			state.started = true
			usedTool = true
		}
		if state.started {
			writeSSEEvent(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": state.blockIndex})
		}
	}

	stopReason := MapStopReason(finishReason)
	if usedTool {
		stopReason = "tool_use"
	}
	writeSSEEvent(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": outputTokens}})
	writeSSEEvent(w, "message_stop", map[string]any{"type": "message_stop"})
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
