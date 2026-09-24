package anthropic

import (
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/responses"
)

// SSEEvent is one Anthropic server-sent event.
type SSEEvent struct {
	Event string
	Data  string
}

// StreamEncoder converts one OpenAI Responses stream to an Anthropic stream.
// OpenAI item IDs identify deltas; call IDs identify client-facing tool calls.
type StreamEncoder struct {
	MessageID                string
	RequestedModel           string
	NextIndex                int
	TextOpen                 bool
	TextIndex                int
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
	InputTokensKnown         bool
	OutputTokensKnown        bool
	CacheCreationKnown       bool
	CacheReadKnown           bool
	RefusalText              string
	StopReason               string
	Started                  bool
	Terminal                 bool
	Success                  bool
	toolsByItem              map[string]*streamTool
	toolsByOutput            map[int]*streamTool
}

type streamTool struct {
	itemID    string
	callID    string
	index     int
	arguments string
	closed    bool
}

func NewStreamEncoder(requestedModel string) *StreamEncoder {
	return &StreamEncoder{
		MessageID: "msg_sr_" + uuid.NewString(), RequestedModel: requestedModel,
		StopReason: "end_turn", toolsByItem: map[string]*streamTool{},
		toolsByOutput: map[int]*streamTool{},
	}
}

func event(name string, body any) SSEEvent {
	b, _ := json.Marshal(body)
	return SSEEvent{Event: name, Data: string(b)}
}

func (e *StreamEncoder) StartEvents() []SSEEvent {
	if e.Started {
		return nil
	}
	e.Started = true
	return []SSEEvent{event("message_start", wireMessageStart{
		Type: "message_start",
		Message: wireMessage{ID: e.MessageID, Type: "message", Role: "assistant",
			Content: []wireContentBlock{}, Model: e.RequestedModel,
			Usage: wireUsage{
				InputTokens: e.InputTokens, OutputTokens: e.OutputTokens,
				CacheCreationInputTokens: e.CacheCreationInputTokens,
				CacheReadInputTokens:     e.CacheReadInputTokens,
			}},
	})}
}

func (e *StreamEncoder) HandleResponsesEvent(typeName, payload string) []SSEEvent {
	if e.Terminal {
		return nil
	}
	var upstream responses.ResponseStreamEventUnion
	if err := upstream.UnmarshalJSON([]byte(payload)); err != nil {
		return e.Fail("api_error", "invalid upstream stream event")
	}
	inT, outT, cacheCreation, cacheRead, hasIn, hasOut, hasCacheCreation, hasCacheRead := extractUsage(payload)
	if hasIn {
		e.InputTokens = inT
		e.InputTokensKnown = true
	}
	if hasOut {
		e.OutputTokens = outT
		e.OutputTokensKnown = true
	}
	if hasCacheCreation {
		e.CacheCreationInputTokens = cacheCreation
		e.CacheCreationKnown = true
	}
	if hasCacheRead {
		e.CacheReadInputTokens = cacheRead
		e.CacheReadKnown = true
	}
	switch typeName {
	case "response.output_text.delta":
		return e.textDelta(upstream.Delta)
	case "response.refusal.delta":
		e.StopReason = "refusal"
		e.RefusalText += upstream.Delta
		return e.textDelta(upstream.Delta)
	case "response.refusal.done":
		e.StopReason = "refusal"
		if upstream.Refusal != e.RefusalText {
			if e.RefusalText == "" && upstream.Refusal != "" {
				e.RefusalText = upstream.Refusal
				return e.textDelta(upstream.Refusal)
			}
			return e.Fail("api_error", "upstream refusal text mismatch")
		}
		return e.closeText()
	case "response.output_text.done", "response.content_part.done":
		return e.closeText()
	case "response.output_item.added":
		if upstream.Item.Type != "function_call" {
			return nil
		}
		item := upstream.Item.AsFunctionCall()
		if item.ID == "" || item.CallID == "" || item.Name == "" || !upstream.JSON.OutputIndex.Valid() {
			return e.Fail("api_error", "incomplete upstream function call")
		}
		outputIndex := int(upstream.OutputIndex)
		if e.toolsByItem[item.ID] != nil || e.toolsByOutput[outputIndex] != nil {
			return e.Fail("api_error", "duplicate upstream function call")
		}
		out := e.closeText()
		tool := &streamTool{itemID: item.ID, callID: item.CallID, index: e.NextIndex}
		e.NextIndex++
		e.toolsByItem[tool.itemID] = tool
		e.toolsByOutput[outputIndex] = tool
		e.StopReason = "tool_use"
		out = append(out, event("content_block_start", wireBlockStart{
			Type: "content_block_start", Index: tool.index,
			ContentBlock: wireContentBlock{Type: "tool_use", ID: tool.callID, Name: item.Name, Input: json.RawMessage("{}")},
		}))
		if item.Arguments != "" {
			out = append(out, e.toolDelta(tool, item.Arguments))
		}
		return out
	case "response.function_call_arguments.delta":
		tool := e.findTool(upstream.ItemID, upstream.OutputIndex, upstream.JSON.OutputIndex.Valid())
		if tool == nil || tool.closed {
			return e.Fail("api_error", "unmatched upstream function arguments")
		}
		if upstream.Delta == "" {
			return nil
		}
		return []SSEEvent{e.toolDelta(tool, upstream.Delta)}
	case "response.function_call_arguments.done":
		tool := e.findTool(upstream.ItemID, upstream.OutputIndex, upstream.JSON.OutputIndex.Valid())
		if tool == nil {
			return e.Fail("api_error", "unmatched upstream function arguments")
		}
		return e.finishTool(tool, upstream.Arguments)
	case "response.output_item.done":
		if upstream.Item.Type != "function_call" {
			return nil
		}
		item := upstream.Item.AsFunctionCall()
		tool := e.findTool(item.ID, upstream.OutputIndex, upstream.JSON.OutputIndex.Valid())
		if tool == nil {
			return e.Fail("api_error", "unmatched upstream function call")
		}
		return e.finishTool(tool, item.Arguments)
	case "response.completed":
		return e.Finish(e.StopReason)
	case "response.incomplete":
		if upstream.Response.IncompleteDetails.Reason == "max_output_tokens" {
			out := e.Finish("max_tokens")
			e.Success = false // a well-formed truncated message is not a completed response
			return out
		}
		return e.Fail("api_error", "upstream response incomplete")
	case "response.failed", "error":
		return e.Fail("api_error", "upstream response failed")
	}
	return nil
}

func (e *StreamEncoder) textDelta(delta string) []SSEEvent {
	if delta == "" {
		return nil
	}
	var out []SSEEvent
	if !e.TextOpen {
		e.TextIndex = e.NextIndex
		e.NextIndex++
		e.TextOpen = true
		out = append(out, event("content_block_start", wireBlockStart{
			Type: "content_block_start", Index: e.TextIndex,
			ContentBlock: wireContentBlock{Type: "text", Text: wireString("")},
		}))
	}
	return append(out, event("content_block_delta", wireBlockDelta{
		Type: "content_block_delta", Index: e.TextIndex,
		Delta: wireTextDelta{Type: "text_delta", Text: delta},
	}))
}

func (e *StreamEncoder) closeText() []SSEEvent {
	if !e.TextOpen {
		return nil
	}
	e.TextOpen = false
	return []SSEEvent{event("content_block_stop", wireBlockStop{Type: "content_block_stop", Index: e.TextIndex})}
}

func (e *StreamEncoder) findTool(itemID string, outputIndex int64, hasOutputIndex bool) *streamTool {
	if itemID != "" {
		return e.toolsByItem[itemID]
	}
	if hasOutputIndex {
		return e.toolsByOutput[int(outputIndex)]
	}
	return nil
}

func (e *StreamEncoder) toolDelta(tool *streamTool, fragment string) SSEEvent {
	tool.arguments += fragment
	return event("content_block_delta", wireBlockDelta{
		Type: "content_block_delta", Index: tool.index,
		Delta: wireInputJSONDelta{Type: "input_json_delta", PartialJSON: fragment},
	})
}

func (e *StreamEncoder) finishTool(tool *streamTool, final string) []SSEEvent {
	if tool.closed {
		if final != "" && final != tool.arguments {
			return e.Fail("api_error", "upstream function arguments mismatch")
		}
		return nil
	}
	var out []SSEEvent
	if final != "" && tool.arguments == "" {
		out = append(out, e.toolDelta(tool, final))
	}
	if final != "" && tool.arguments != final {
		return e.Fail("api_error", "upstream function arguments mismatch")
	}
	if tool.arguments == "" {
		out = append(out, e.toolDelta(tool, "{}"))
	}
	tool.closed = true
	return append(out, event("content_block_stop", wireBlockStop{Type: "content_block_stop", Index: tool.index}))
}

func validToolArguments(arguments string) bool {
	var obj map[string]json.RawMessage
	return json.Unmarshal([]byte(arguments), &obj) == nil && obj != nil
}

// Finish closes all blocks and emits exactly one normal Anthropic terminator.
func (e *StreamEncoder) Finish(stopReason string) []SSEEvent {
	if e.Terminal {
		return nil
	}
	out := e.closeText()
	indices := make([]int, 0, len(e.toolsByOutput))
	for idx := range e.toolsByOutput {
		indices = append(indices, idx)
	}
	sort.Ints(indices)
	for _, idx := range indices {
		tool := e.toolsByOutput[idx]
		if !tool.closed {
			more := e.finishTool(tool, "")
			if e.Terminal {
				return more
			}
			out = append(out, more...)
		}
		if stopReason != "max_tokens" && !validToolArguments(tool.arguments) {
			return e.Fail("api_error", "invalid upstream function arguments")
		}
	}
	e.StopReason = stopReason
	e.Terminal = true
	e.Success = true
	usage := wireDeltaUsage{}
	if e.InputTokensKnown {
		usage.InputTokens = &e.InputTokens
	}
	if e.OutputTokensKnown {
		usage.OutputTokens = &e.OutputTokens
	}
	if e.CacheCreationKnown {
		usage.CacheCreationInputTokens = &e.CacheCreationInputTokens
	}
	if e.CacheReadKnown {
		usage.CacheReadInputTokens = &e.CacheReadInputTokens
	}
	out = append(out, event("message_delta", wireMessageDelta{
		Type: "message_delta", Delta: wireStopDelta{StopReason: stopReason},
		Usage: usage,
	}))
	return append(out, event("message_stop", wireMessageStop{Type: "message_stop"}))
}

// Fail terminates a committed stream without fabricating a normal completion.
func (e *StreamEncoder) Fail(errType, message string) []SSEEvent {
	if e.Terminal {
		return nil
	}
	e.Terminal = true
	e.Success = false
	return []SSEEvent{event("error", wireErrorEvent{Type: "error", Error: wireErrorBody{Type: errType, Message: message}})}
}

func extractUsage(payload string) (input, output, cacheCreation, cacheRead int64, hasInput, hasOutput, hasCacheCreation, hasCacheRead bool) {
	var v struct {
		Usage *struct {
			InputTokens        *int64 `json:"input_tokens"`
			OutputTokens       *int64 `json:"output_tokens"`
			InputTokensDetails *struct {
				CachedTokens     *int64 `json:"cached_tokens"`
				CacheWriteTokens *int64 `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
		Response *struct {
			Usage *struct {
				InputTokens        *int64 `json:"input_tokens"`
				OutputTokens       *int64 `json:"output_tokens"`
				InputTokensDetails *struct {
					CachedTokens     *int64 `json:"cached_tokens"`
					CacheWriteTokens *int64 `json:"cache_write_tokens"`
				} `json:"input_tokens_details"`
			} `json:"usage"`
		} `json:"response"`
	}
	if json.Unmarshal([]byte(payload), &v) != nil {
		return 0, 0, 0, 0, false, false, false, false
	}
	usage := v.Usage
	if v.Response != nil && v.Response.Usage != nil {
		usage = v.Response.Usage
	}
	if usage == nil {
		return 0, 0, 0, 0, false, false, false, false
	}
	if usage.InputTokens != nil {
		input, hasInput = *usage.InputTokens, true
	}
	if usage.OutputTokens != nil {
		output, hasOutput = *usage.OutputTokens, true
	}
	if usage.InputTokensDetails != nil {
		if usage.InputTokensDetails.CachedTokens != nil {
			cacheRead, hasCacheRead = *usage.InputTokensDetails.CachedTokens, true
		}
		if usage.InputTokensDetails.CacheWriteTokens != nil {
			cacheCreation, hasCacheCreation = *usage.InputTokensDetails.CacheWriteTokens, true
		}
	}
	if hasInput {
		input -= cacheRead + cacheCreation
		if input < 0 {
			input = 0
		}
	}
	return input, output, cacheCreation, cacheRead, hasInput, hasOutput, hasCacheCreation, hasCacheRead
}
