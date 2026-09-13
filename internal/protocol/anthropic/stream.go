package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// StreamEncoder converts Responses stream events to Anthropic SSE events.
// It is stateful: block indices, open text/tool blocks, token counts.
type StreamEncoder struct {
	MessageID       string
	RequestedModel  string
	NextIndex       int
	TextOpen        bool
	TextIndex       int
	ToolIndexByCall map[string]int
	ToolIDByIndex   map[string]string
	ToolNameByIndex map[string]string
	InputTokens     int64
	OutputTokens    int64
	StopReason      string
	Started         bool
}

// NewStreamEncoder creates an encoder for one client stream.
func NewStreamEncoder(requestedModel string) *StreamEncoder {
	return &StreamEncoder{
		MessageID: "msg_sr_" + uuid.NewString(), RequestedModel: requestedModel,
		ToolIndexByCall: map[string]int{}, ToolIDByIndex: map[string]string{}, ToolNameByIndex: map[string]string{},
		StopReason: "end_turn",
	}
}

// StartEvents returns message_start (call once before first content).
func (e *StreamEncoder) StartEvents() []SSEEvent {
	e.Started = true
	body, _ := json.Marshal(map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": e.MessageID, "type": "message", "role": "assistant",
			"content": []any{}, "model": e.RequestedModel,
			"stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})
	return []SSEEvent{{Event: "message_start", Data: string(body)}}
}

// SSEEvent is one Anthropic SSE frame.
type SSEEvent struct {
	Event string
	Data  string
}

// HandleResponsesEvent maps one Responses event payload to 0+ Anthropic events.
func (e *StreamEncoder) HandleResponsesEvent(typeName, payload string) []SSEEvent {
	var out []SSEEvent
	switch typeName {
	case "response.output_text.delta":
		text := extractDeltaText(payload)
		if text == "" {
			return nil
		}
		if !e.TextOpen {
			e.TextIndex = e.NextIndex
			e.NextIndex++
			e.TextOpen = true
			b, _ := json.Marshal(map[string]any{
				"type": "content_block_start", "index": e.TextIndex,
				"content_block": map[string]any{"type": "text", "text": ""},
			})
			out = append(out, SSEEvent{Event: "content_block_start", Data: string(b)})
		}
		b, _ := json.Marshal(map[string]any{
			"type": "content_block_delta", "index": e.TextIndex,
			"delta": map[string]any{"type": "text_delta", "text": text},
		})
		out = append(out, SSEEvent{Event: "content_block_delta", Data: string(b)})
	case "response.output_text.done", "response.content_part.done":
		if e.TextOpen {
			b, _ := json.Marshal(map[string]any{"type": "content_block_stop", "index": e.TextIndex})
			out = append(out, SSEEvent{Event: "content_block_stop", Data: string(b)})
			e.TextOpen = false
		}
	case "response.output_item.added":
		callID, name := extractFunctionStart(payload)
		if callID != "" {
			if e.TextOpen {
				b, _ := json.Marshal(map[string]any{"type": "content_block_stop", "index": e.TextIndex})
				out = append(out, SSEEvent{Event: "content_block_stop", Data: string(b)})
				e.TextOpen = false
			}
			idx := e.NextIndex
			e.NextIndex++
			e.ToolIndexByCall[callID] = idx
			e.ToolIDByIndex[itoa(idx)] = callID
			e.ToolNameByIndex[itoa(idx)] = name
			e.StopReason = "tool_use"
			b, _ := json.Marshal(map[string]any{
				"type": "content_block_start", "index": idx,
				"content_block": map[string]any{"type": "tool_use", "id": callID, "name": name, "input": map[string]any{}},
			})
			out = append(out, SSEEvent{Event: "content_block_start", Data: string(b)})
		}
	case "response.function_call_arguments.delta":
		frag, callID := extractArgsDelta(payload)
		idx, ok := e.ToolIndexByCall[callID]
		if !ok && callID == "" {
			// Attach to most recent tool block.
			for _, v := range e.ToolIndexByCall {
				idx, ok = v, true
				break
			}
		}
		if !ok {
			return nil
		}
		b, _ := json.Marshal(map[string]any{
			"type": "content_block_delta", "index": idx,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": frag},
		})
		out = append(out, SSEEvent{Event: "content_block_delta", Data: string(b)})
	case "response.function_call_arguments.done", "response.output_item.done":
		// Close any open tool block matching payload, else most recent.
		closed := false
		callID, _ := extractFunctionStart(payload)
		if callID != "" {
			if idx, ok := e.ToolIndexByCall[callID]; ok {
				b, _ := json.Marshal(map[string]any{"type": "content_block_stop", "index": idx})
				out = append(out, SSEEvent{Event: "content_block_stop", Data: string(b)})
				closed = true
			}
		}
		if !closed {
			for _, idx := range e.ToolIndexByCall {
				b, _ := json.Marshal(map[string]any{"type": "content_block_stop", "index": idx})
				out = append(out, SSEEvent{Event: "content_block_stop", Data: string(b)})
				break
			}
		}
		e.ToolIndexByCall = map[string]int{}
	case "response.completed":
		inT, outT := extractUsage(payload)
		if inT > 0 {
			e.InputTokens = inT
		}
		if outT > 0 {
			e.OutputTokens = outT
		}
		if e.TextOpen {
			b, _ := json.Marshal(map[string]any{"type": "content_block_stop", "index": e.TextIndex})
			out = append(out, SSEEvent{Event: "content_block_stop", Data: string(b)})
			e.TextOpen = false
		}
		for _, idx := range e.ToolIndexByCall {
			b, _ := json.Marshal(map[string]any{"type": "content_block_stop", "index": idx})
			out = append(out, SSEEvent{Event: "content_block_stop", Data: string(b)})
		}
		e.ToolIndexByCall = map[string]int{}
		md, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": e.StopReason, "stop_sequence": nil},
			"usage": map[string]any{"output_tokens": e.OutputTokens},
		})
		out = append(out, SSEEvent{Event: "message_delta", Data: string(md)})
		ms, _ := json.Marshal(map[string]any{"type": "message_stop"})
		out = append(out, SSEEvent{Event: "message_stop", Data: string(ms)})
	case "response.failed", "response.incomplete":
		if e.StopReason == "end_turn" {
			e.StopReason = "end_turn"
		}
	}
	// Track usage if present in any event.
	if inT, outT := extractUsage(payload); inT > 0 || outT > 0 {
		if inT > 0 {
			e.InputTokens = inT
		}
		if outT > 0 {
			e.OutputTokens = outT
		}
	}
	return out
}

func extractDeltaText(payload string) string {
	var v struct {
		Delta string `json:"delta"`
		Text  string `json:"text"`
	}
	if err := json.Unmarshal([]byte(payload), &v); err == nil {
		if v.Delta != "" {
			return v.Delta
		}
		if v.Text != "" {
			return v.Text
		}
	}
	// Nested delta object.
	var w struct {
		Delta struct {
			Text string `json:"text"`
		} `json:"delta"`
	}
	if err := json.Unmarshal([]byte(payload), &w); err == nil {
		return w.Delta.Text
	}
	return ""
}

func extractFunctionStart(payload string) (callID, name string) {
	var v struct {
		Item struct {
			CallID string `json:"call_id"`
			ID     string `json:"id"`
			Name   string `json:"name"`
			Type   string `json:"type"`
		} `json:"item"`
		CallID string `json:"call_id"`
		ID     string `json:"id"`
		Name   string `json:"name"`
		Type   string `json:"type"`
	}
	if err := json.Unmarshal([]byte(payload), &v); err != nil {
		return "", ""
	}
	callID = v.Item.CallID
	if callID == "" {
		callID = v.Item.ID
	}
	if callID == "" {
		callID = v.CallID
	}
	if callID == "" {
		callID = v.ID
	}
	name = v.Item.Name
	if name == "" {
		name = v.Name
	}
	if v.Item.Type != "" && v.Item.Type != "function_call" && v.Type != "function_call" && callID == "" {
		return "", ""
	}
	if strings.Contains(payload, "function_call") || callID != "" {
		return callID, name
	}
	return "", ""
}

func extractArgsDelta(payload string) (frag, callID string) {
	var v struct {
		Delta   string `json:"delta"`
		Partial string `json:"partial_json"`
		CallID  string `json:"call_id"`
		ItemID  string `json:"item_id"`
		ID      string `json:"id"`
	}
	if err := json.Unmarshal([]byte(payload), &v); err == nil {
		frag = v.Delta
		if frag == "" {
			frag = v.Partial
		}
		callID = v.CallID
		if callID == "" {
			callID = v.ItemID
		}
		if callID == "" {
			callID = v.ID
		}
		return frag, callID
	}
	return "", ""
}

func extractUsage(payload string) (int64, int64) {
	var v struct {
		Usage *struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
		Response *struct {
			Usage *struct {
				InputTokens  int64 `json:"input_tokens"`
				OutputTokens int64 `json:"output_tokens"`
			} `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(payload), &v); err != nil {
		return 0, 0
	}
	if v.Usage != nil {
		return v.Usage.InputTokens, v.Usage.OutputTokens
	}
	if v.Response != nil && v.Response.Usage != nil {
		return v.Response.Usage.InputTokens, v.Response.Usage.OutputTokens
	}
	return 0, 0
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
