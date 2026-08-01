package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/protocol"
	"github.com/sleepysoong/sleepyrouter/internal/sseutil"
)

type openAIStreamToolCall struct {
	Index            *int           `json:"index"`
	ID               *string        `json:"id"`
	ThoughtSignature *string        `json:"thought_signature"`
	Signature        *string        `json:"signature"`
	ExtraFields      map[string]any `json:"extra_fields"`
	Function         *struct {
		Name             *string `json:"name"`
		Arguments        *string `json:"arguments"`
		ThoughtSignature *string `json:"thought_signature"`
		Signature        *string `json:"signature"`
	} `json:"function"`
}

type openAIStreamChoice struct {
	FinishReason any `json:"finish_reason"`
	Delta        *struct {
		Content          *string `json:"content"`
		ReasoningContent *string `json:"reasoning_content"`
		Reasoning        *string `json:"reasoning"`
		Thinking         *string `json:"thinking"`
		Thought          *string `json:"thought"`
		ThoughtSignature *string `json:"thought_signature"`
		Signature        *string `json:"signature"`
		// ponytail: Google nests the thinking signature under extra_content.google; top-level fields stay for OpenAI/OpenRouter
		ExtraContent *struct {
			Google *struct {
				ThoughtSignature *string `json:"thought_signature"`
			} `json:"google"`
		} `json:"extra_content"`
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
		w:                w,
		textBlockIndex:   -1,
		thinkingBlockIdx: -1,
		toolBlocks:       make(map[int]*openAIToolStreamState),
	}

	var (
		finishReason   any
		outputTokens   int
		thinkingTokens int
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
				CompletionTokens        any `json:"completion_tokens"`
				OutputTokens            any `json:"output_tokens"`
				CompletionTokensDetails *struct {
					ReasoningTokens any `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
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
			if chunk.Usage.CompletionTokensDetails != nil {
				if v := sseutil.ParseToken(chunk.Usage.CompletionTokensDetails.ReasoningTokens); v != nil {
					thinkingTokens = *v
				}
			}
		}
		if choice != nil && choice.Delta != nil {
			if choice.Delta.ThoughtSignature != nil && *choice.Delta.ThoughtSignature != "" {
				st.thinkingSignature = *choice.Delta.ThoughtSignature
			} else if choice.Delta.Signature != nil && *choice.Delta.Signature != "" {
				st.thinkingSignature = *choice.Delta.Signature
			} else if choice.Delta.ExtraContent != nil && choice.Delta.ExtraContent.Google != nil && choice.Delta.ExtraContent.Google.ThoughtSignature != nil {
				st.thinkingSignature = *choice.Delta.ExtraContent.Google.ThoughtSignature
			}
			thinkingText := ""
			switch {
			case choice.Delta.ReasoningContent != nil:
				thinkingText = *choice.Delta.ReasoningContent
			case choice.Delta.Reasoning != nil:
				thinkingText = *choice.Delta.Reasoning
			case choice.Delta.Thinking != nil:
				thinkingText = *choice.Delta.Thinking
			case choice.Delta.Thought != nil:
				thinkingText = *choice.Delta.Thought
			}
			if thinkingText != "" {
				sseutil.WriteEvent(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": st.ensureThinkingBlock(), "delta": map[string]any{"type": "thinking_delta", "thinking": thinkingText}})
			}
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
				if tc.ThoughtSignature != nil {
					delta["thought_signature"] = *tc.ThoughtSignature
				} else if tc.Signature != nil {
					delta["thought_signature"] = *tc.Signature
				} else if tc.Function != nil && tc.Function.ThoughtSignature != nil {
					delta["thought_signature"] = *tc.Function.ThoughtSignature
				} else if tc.Function != nil && tc.Function.Signature != nil {
					delta["thought_signature"] = *tc.Function.Signature
				} else if sig, ok := tc.ExtraFields["thought_signature"].(string); ok && sig != "" {
					delta["thought_signature"] = sig
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

	st.stopThinkingBlock()
	if !st.textBlockOpen && len(st.toolBlocks) == 0 && !st.thinkingBlockOpen {
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
	usage := map[string]any{"output_tokens": outputTokens}
	if thinkingTokens > 0 {
		usage["output_tokens_details"] = map[string]any{"thinking_tokens": thinkingTokens}
	}
	sseutil.WriteEvent(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": usage})
	sseutil.WriteEvent(w, "message_stop", map[string]any{"type": "message_stop"})
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
