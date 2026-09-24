package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/upstream"
)

type chatCompletionStreamShim interface {
	Next() bool
	Current() openaisdk.ChatCompletionChunk
	Err() error
	Close() error
}

type queuedResponseEvent struct {
	typeName string
	payload  []byte
}

type streamedToolCall struct {
	callIndex   int64
	outputIndex int
	itemID      string
	callID      string
	name        string
	arguments   string
	started     bool
}

// chatCompletionEventStream adapts typed Chat Completions chunks into the
// Responses SSE event family consumed by both downstream protocol encoders.
type chatCompletionEventStream struct {
	inner            chatCompletionStreamShim
	responseID       string
	model            string
	createdAt        int64
	sequence         int64
	started          bool
	ended            bool
	finalized        bool
	finish           string
	text             string
	refusal          string
	reasoningContent string
	textID           string
	textOutput       int
	textStarted      bool
	refusalID        string
	refusalOutput    int
	refusalStarted   bool
	nextOutput       int
	tools            map[int64]*streamedToolCall
	usage            *openaisdk.CompletionUsage
	queue            []queuedResponseEvent
	current          queuedResponseEvent
	err              error
}

func openChatCompletionStream(ctx context.Context, client openaisdk.Client, c routing.Candidate, rawBody []byte) (EventStream, error) {
	params, requestOptions, err := upstream.ResponsesToChatCompletionRequest(rawBody, c.UpstreamModel)
	if err != nil {
		return nil, err
	}
	params.StreamOptions.IncludeUsage = param.NewOpt(true)
	stream := client.Chat.Completions.NewStreaming(ctx, params, requestOptions...)
	return &chatCompletionEventStream{
		inner: stream, responseID: "resp_" + uuid.NewString(), model: c.UpstreamModel,
		createdAt: time.Now().Unix(), tools: map[int64]*streamedToolCall{},
	}, nil
}

func (s *chatCompletionEventStream) Next() bool {
	if len(s.queue) > 0 {
		s.pop()
		return true
	}
	if s.ended {
		return false
	}
	if !s.started {
		s.started = true
		s.enqueue("response.created", map[string]any{"response": s.response("in_progress", nil, nil)})
		s.pop()
		return true
	}
	for s.inner.Next() {
		chunk := s.inner.Current()
		s.captureUsage(&chunk.Usage)
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				continue
			}
			if choice.Delta.Content != "" {
				s.addText(choice.Delta.Content)
			}
			if choice.Delta.Refusal != "" {
				s.addRefusal(choice.Delta.Refusal)
			}
			var vendorFields struct {
				ReasoningContent string `json:"reasoning_content"`
			}
			if json.Unmarshal([]byte(choice.Delta.RawJSON()), &vendorFields) == nil {
				s.reasoningContent += vendorFields.ReasoningContent
			}
			for _, delta := range choice.Delta.ToolCalls {
				s.addToolDelta(delta)
			}
			if choice.FinishReason != "" {
				s.finish = choice.FinishReason
			}
		}
		if len(s.queue) > 0 {
			s.pop()
			return true
		}
	}
	if err := s.inner.Err(); err != nil {
		s.err = err
		s.ended = true
		return false
	}
	if s.finish == "" {
		s.finish = "stop"
	}
	if err := s.finishOutput(); err != nil {
		s.err = err
		s.ended = true
		return false
	}
	status := "completed"
	eventName := "response.completed"
	var incomplete any
	if s.finish == "length" {
		status = "incomplete"
		eventName = "response.incomplete"
		incomplete = map[string]any{"reason": "max_output_tokens"}
	}
	s.enqueue(eventName, map[string]any{"response": s.response(status, incomplete, s.outputItems())})
	s.finalized = true
	s.ended = true
	if len(s.queue) > 0 {
		s.pop()
		return true
	}
	return false
}

func (s *chatCompletionEventStream) captureUsage(usage *openaisdk.CompletionUsage) {
	if usage == nil || usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 {
		return
	}
	copy := *usage
	s.usage = &copy
}

func (s *chatCompletionEventStream) addText(delta string) {
	if !s.textStarted {
		s.textStarted = true
		s.textID = "msg_" + uuid.NewString()
		s.textOutput = s.nextOutput
		s.nextOutput++
		s.enqueue("response.output_item.added", map[string]any{
			"output_index": s.textOutput,
			"item":         map[string]any{"id": s.textID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}},
		})
		s.enqueue("response.content_part.added", map[string]any{
			"item_id": s.textID, "output_index": s.textOutput, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
		})
	}
	s.text += delta
	s.enqueue("response.output_text.delta", map[string]any{
		"item_id": s.textID, "output_index": s.textOutput, "content_index": 0, "delta": delta,
	})
}

func (s *chatCompletionEventStream) addRefusal(delta string) {
	if !s.refusalStarted {
		s.refusalStarted = true
		s.refusalID = "msg_" + uuid.NewString()
		s.refusalOutput = s.nextOutput
		s.nextOutput++
		s.enqueue("response.output_item.added", map[string]any{
			"output_index": s.refusalOutput,
			"item":         map[string]any{"id": s.refusalID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}},
		})
		s.enqueue("response.content_part.added", map[string]any{
			"item_id": s.refusalID, "output_index": s.refusalOutput, "content_index": 0,
			"part": map[string]any{"type": "refusal", "refusal": ""},
		})
	}
	s.refusal += delta
	s.enqueue("response.refusal.delta", map[string]any{
		"item_id": s.refusalID, "output_index": s.refusalOutput, "content_index": 0, "delta": delta,
	})
}

func (s *chatCompletionEventStream) addToolDelta(delta openaisdk.ChatCompletionChunkChoiceDeltaToolCall) {
	tool := s.tools[delta.Index]
	if tool == nil {
		tool = &streamedToolCall{callIndex: delta.Index, outputIndex: -1}
		s.tools[delta.Index] = tool
	}
	wasStarted := tool.started
	argumentDelta := delta.Function.Arguments
	if delta.ID != "" {
		tool.callID = delta.ID
	}
	if delta.Function.Name != "" {
		tool.name += delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		tool.arguments += delta.Function.Arguments
	}
	if !tool.started && tool.callID != "" && tool.name != "" {
		tool.started = true
		tool.itemID = "fc_" + uuid.NewString()
		tool.outputIndex = s.nextOutput
		s.nextOutput++
		s.enqueue("response.output_item.added", map[string]any{
			"output_index": tool.outputIndex,
			"item":         map[string]any{"id": tool.itemID, "type": "function_call", "status": "in_progress", "call_id": tool.callID, "name": tool.name, "arguments": ""},
		})
	}
	if !wasStarted && tool.started && tool.arguments != "" {
		s.enqueue("response.function_call_arguments.delta", map[string]any{
			"item_id": tool.itemID, "output_index": tool.outputIndex, "delta": tool.arguments,
		})
	} else if wasStarted && argumentDelta != "" {
		s.enqueue("response.function_call_arguments.delta", map[string]any{
			"item_id": tool.itemID, "output_index": tool.outputIndex, "delta": argumentDelta,
		})
	}
}

func (s *chatCompletionEventStream) finishOutput() error {
	if s.finalized {
		return nil
	}
	if s.textStarted {
		s.enqueue("response.output_text.done", map[string]any{
			"item_id": s.textID, "output_index": s.textOutput, "content_index": 0, "text": s.text,
		})
		s.enqueue("response.content_part.done", map[string]any{
			"item_id": s.textID, "output_index": s.textOutput, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": s.text, "annotations": []any{}},
		})
		s.enqueue("response.output_item.done", map[string]any{
			"output_index": s.textOutput,
			"item":         map[string]any{"id": s.textID, "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": s.text, "annotations": []any{}}}},
		})
	}
	if s.refusalStarted {
		s.enqueue("response.refusal.done", map[string]any{
			"item_id": s.refusalID, "output_index": s.refusalOutput, "content_index": 0, "refusal": s.refusal,
		})
		s.enqueue("response.content_part.done", map[string]any{
			"item_id": s.refusalID, "output_index": s.refusalOutput, "content_index": 0,
			"part": map[string]any{"type": "refusal", "refusal": s.refusal},
		})
		s.enqueue("response.output_item.done", map[string]any{
			"output_index": s.refusalOutput,
			"item":         map[string]any{"id": s.refusalID, "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "refusal", "refusal": s.refusal}}},
		})
	}
	indices := make([]int64, 0, len(s.tools))
	for index := range s.tools {
		indices = append(indices, index)
	}
	sort.Slice(indices, func(i, j int) bool { return s.tools[indices[i]].outputIndex < s.tools[indices[j]].outputIndex })
	for _, index := range indices {
		tool := s.tools[index]
		if !tool.started {
			return fmt.Errorf("Chat Completions stream returned incomplete tool call index %d", index)
		}
		arguments := tool.arguments
		if arguments == "" {
			arguments = "{}"
		}
		s.enqueue("response.function_call_arguments.done", map[string]any{
			"item_id": tool.itemID, "output_index": tool.outputIndex, "arguments": arguments,
		})
		s.enqueue("response.output_item.done", map[string]any{
			"output_index": tool.outputIndex,
			"item":         map[string]any{"id": tool.itemID, "type": "function_call", "status": "completed", "call_id": tool.callID, "name": tool.name, "arguments": arguments},
		})
	}
	s.finalized = true
	return nil
}

func (s *chatCompletionEventStream) outputItems() []any {
	type output struct {
		index int
		item  any
	}
	items := make([]output, 0, len(s.tools)+2)
	if s.textStarted {
		items = append(items, output{index: s.textOutput, item: map[string]any{
			"id": s.textID, "type": "message", "status": "completed", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": s.text, "annotations": []any{}}},
		}})
	}
	if s.refusalStarted {
		items = append(items, output{index: s.refusalOutput, item: map[string]any{
			"id": s.refusalID, "type": "message", "status": "completed", "role": "assistant",
			"content": []any{map[string]any{"type": "refusal", "refusal": s.refusal}},
		}})
	}
	for _, tool := range s.tools {
		args := tool.arguments
		if args == "" {
			args = "{}"
		}
		items = append(items, output{index: tool.outputIndex, item: map[string]any{
			"id": tool.itemID, "type": "function_call", "status": "completed", "call_id": tool.callID,
			"name": tool.name, "arguments": args,
		}})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].index < items[j].index })
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.item)
	}
	return out
}

func (s *chatCompletionEventStream) response(status string, incomplete any, output []any) map[string]any {
	if output == nil {
		output = []any{}
	}
	response := map[string]any{
		"id": s.responseID, "object": "response", "created_at": s.createdAt,
		"status": status, "model": s.model, "output": output, "usage": s.responseUsage(),
		"incomplete_details": incomplete, "error": nil,
	}
	return response
}

func (s *chatCompletionEventStream) responseUsage() any {
	if s.usage == nil {
		return nil
	}
	u := s.usage
	return map[string]any{
		"input_tokens": u.PromptTokens, "output_tokens": u.CompletionTokens, "total_tokens": u.TotalTokens,
		"input_tokens_details": map[string]any{
			"cached_tokens":      u.PromptTokensDetails.CachedTokens,
			"cache_write_tokens": u.PromptTokensDetails.CacheWriteTokens,
		},
		"output_tokens_details": map[string]any{"reasoning_tokens": u.CompletionTokensDetails.ReasoningTokens},
	}
}

func (s *chatCompletionEventStream) enqueue(typeName string, body map[string]any) {
	body["type"] = typeName
	body["sequence_number"] = s.sequence
	s.sequence++
	data, _ := json.Marshal(body)
	s.queue = append(s.queue, queuedResponseEvent{typeName: typeName, payload: data})
}

func (s *chatCompletionEventStream) pop() {
	s.current = s.queue[0]
	s.queue = s.queue[1:]
}

func (s *chatCompletionEventStream) Event() (string, []byte) {
	return s.current.typeName, s.current.payload
}
func (s *chatCompletionEventStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.inner.Err()
}
func (s *chatCompletionEventStream) Close() error { return s.inner.Close() }

// ProviderReasoningContent returns a private continuation field for the
// Anthropic bridge. It is deliberately not included in any downstream event.
func (s *chatCompletionEventStream) ProviderReasoningContent() string {
	return s.reasoningContent
}

var _ EventStream = (*chatCompletionEventStream)(nil)
