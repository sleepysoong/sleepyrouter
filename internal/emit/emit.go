// Package emit converts GoAI provider results and stream chunks back into
// the OpenAI and Anthropic wire formats that sleepyrouter exposes, preserving
// thought signatures, reasoning content, tool calls, and usage accounting.
package emit

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"

	"github.com/zendev-sh/goai/provider"

	"github.com/sleepysoong/sleepyrouter/internal/protocol"
)

// ---- OpenAI non-stream ----

// OpenAIResponse marshals a GenerateResult into an OpenAI chat.completion
// JSON object. An empty result (no text, reasoning, or tool calls) produces
// a "choices": [] payload, which upstream callers treat as a failed attempt
// (mirrors the legacy passthrough of an empty upstream response).
func OpenAIResponse(result provider.GenerateResult, model string) ([]byte, error) {
	message := map[string]any{
		"role":    "assistant",
		"content": nil,
	}
	if result.Text != "" {
		message["content"] = result.Text
	}
	if result.Reasoning != "" {
		message["reasoning_content"] = result.Reasoning
	}
	// Non-stream message-level thought signature (e.g. Google's
	// message.thought_signature) rides in ProviderMetadata["openai"].
	if pm := result.ProviderMetadata["openai"]; pm != nil {
		if sig := thoughtSig(pm); sig != "" {
			message["thought_signature"] = sig
		}
	}
	if tcs := toolCallsWire(result.ToolCalls); len(tcs) > 0 {
		message["tool_calls"] = tcs
	}

	choices := []any{}
	if result.Text != "" || result.Reasoning != "" || len(result.ToolCalls) > 0 || result.FinishReason != "" {
		choices = append(choices, map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": openAIFinishReason(result.FinishReason),
		})
	}

	resp := map[string]any{
		"id":      responseID("chatcmpl"),
		"object":  "chat.completion",
		"created": createdEpoch(result.Response),
		"model":   model,
		"choices": choices,
		"usage":   usageWire(result.Usage),
	}
	if id := result.Response.ID; id != "" {
		resp["id"] = id
	}
	return json.Marshal(resp)
}

// ---- OpenAI streaming (SSE) ----

// OpenAIStreamSSE writes provider.StreamChunk chunks to w as OpenAI chat
// completion SSE (the exact format the sleepyrouter pass-through consumers
// and Claude Code's OpenAI mode expect: role chunk, content/reasoning deltas,
// tool_call deltas with the 3-place thought signature, finish_reason, a
// trailing usage chunk, and "[DONE]").
func OpenAIStreamSSE(w io.Writer, chunks <-chan provider.StreamChunk, model string) {
	if c, ok := w.(io.Closer); ok {
		defer func() { _ = c.Close() }() // ends io.Pipe
	}

	ss := &openAIStreamState{sent: map[string]int{}, ordinal: map[string]int{}}
	sseEvent(w, map[string]any{
		"id":      responseID("chatcmpl"),
		"object":  "chat.completion.chunk",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": ""}}},
	})

	for chunk := range chunks {
		switch chunk.Type {
		case provider.ChunkText:
			if chunk.Text == "" {
				continue
			}
			sseEvent(w, streamChoice(map[string]any{"content": chunk.Text}, nil))
		case provider.ChunkReasoning:
			if chunk.Text == "" && thoughtSig(chunk.Metadata) == "" {
				continue
			}
			delta := map[string]any{}
			if chunk.Text != "" {
				delta["reasoning_content"] = chunk.Text
			}
			if sig := thoughtSig(chunk.Metadata); sig != "" {
				delta["thought_signature"] = sig
			}
			sseEvent(w, streamChoice(delta, nil))
		case provider.ChunkToolCallStreamStart:
			idx := ss.toolIndex(chunk.ToolCallID)
			tc := map[string]any{
				"index": idx, "id": chunk.ToolCallID, "type": "function",
				"function": map[string]any{"name": chunk.ToolName, "arguments": ""},
			}
			attachToolSig(tc, thoughtSig(chunk.Metadata))
			sseEvent(w, streamChoice(map[string]any{"tool_calls": []any{tc}}, nil))
			if chunk.ToolInput != "" {
				ss.sent[chunk.ToolCallID] = len(chunk.ToolInput)
				sseEvent(w, streamChoice(map[string]any{"tool_calls": []any{map[string]any{"index": idx, "function": map[string]any{"arguments": chunk.ToolInput}}}}, nil))
			}
		case provider.ChunkToolCallDelta:
			idx := ss.toolIndex(chunk.ToolCallID)
			if chunk.ToolInput == "" {
				continue
			}
			ss.sent[chunk.ToolCallID] += len(chunk.ToolInput)
			sseEvent(w, streamChoice(map[string]any{"tool_calls": []any{map[string]any{"index": idx, "function": map[string]any{"arguments": chunk.ToolInput}}}}, nil))
		case provider.ChunkToolCall:
			// ChunkToolCall carries the full accumulated arguments. Only the
			// bytes not already streamed as fragments are new (buffered-then-
			// flushed case); everything else was covered by the deltas.
			idx := ss.toolIndex(chunk.ToolCallID)
			remainder := chunk.ToolInput
			if n, ok := ss.sent[chunk.ToolCallID]; ok && n < len(remainder) {
				remainder = remainder[n:]
			} else if ok {
				remainder = ""
			}
			if remainder != "" {
				ss.sent[chunk.ToolCallID] += len(remainder)
				sseEvent(w, streamChoice(map[string]any{"tool_calls": []any{map[string]any{"index": idx, "function": map[string]any{"arguments": remainder}}}}, nil))
			}
		case provider.ChunkStepFinish:
			if fr := openAIFinishReason(chunk.FinishReason); fr != nil && !ss.sentFinish {
				ss.sentFinish = true
				sseEvent(w, streamChoice(map[string]any{}, fr))
			}
		case provider.ChunkFinish:
			if fr := openAIFinishReason(chunk.FinishReason); fr != nil && !ss.sentFinish {
				ss.sentFinish = true
				sseEvent(w, streamChoice(map[string]any{}, fr))
			}
			sseEvent(w, map[string]any{
				"choices": []any{},
				"usage":   usageWire(chunk.Usage),
			})
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		case provider.ChunkError:
			return
		}
	}
	// Writer closed without a finish chunk: terminate the stream cleanly.
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
}

// ---- Anthropic non-stream ----

// AnthropicResponse converts a GenerateResult into an Anthropic Messages JSON
// object by building the OpenAI-shaped intermediate and reusing the existing
// protocol translation layer.
func AnthropicResponse(result provider.GenerateResult, model string) ([]byte, error) {
	message := map[string]any{
		"role":    "assistant",
		"content": nil,
	}
	if result.Text != "" {
		message["content"] = result.Text
	}
	if result.Reasoning != "" {
		message["reasoning_content"] = result.Reasoning
	}
	// Non-stream message-level thought signature (e.g. Google's
	// message.thought_signature) rides in ProviderMetadata["openai"].
	if pm := result.ProviderMetadata["openai"]; pm != nil {
		if sig := thoughtSig(pm); sig != "" {
			message["thought_signature"] = sig
		}
	}
	if tcs := toolCallsWire(result.ToolCalls); len(tcs) > 0 {
		message["tool_calls"] = tcs
	}
	fr := ""
	if p := openAIFinishReason(result.FinishReason); p != nil {
		fr = *p
	}
	body := map[string]any{
		"id":      result.Response.ID,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": fr}},
		"usage":   usageWire(result.Usage),
	}
	if body["id"] == "" {
		body["id"] = responseID("chatcmpl")
	}
	anthropicMap := protocol.OpenAIToAnthropic(body, model)
	return json.Marshal(anthropicMap)
}

// ---- Anthropic streaming (SSE) ----

// AnthropicStreamSSE writes provider.StreamChunk chunks to w as Anthropic
// Messages SSE. It mirrors the legacy PipeOpenAIStreamAsAnthropic state
// machine: message_start, a thinking block with signature_delta, a text
// block, tool_use blocks with 3-place thought signatures and input_json_delta
// fragments, message_delta carrying stop_reason + usage, and message_stop.
func AnthropicStreamSSE(w io.Writer, chunks <-chan provider.StreamChunk, model string) {
	if c, ok := w.(io.Closer); ok {
		defer func() { _ = c.Close() }()
	}

	as := &anthropicStreamState{
		sent:        map[string]int{},
		ordinal:     map[string]int{},
		toolBlocks:  map[string]int{},
		toolStarted: map[string]bool{},
	}
	sseEvent(w, map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            responseID("msg"),
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": 0, "output_tokens": 0},
		},
	})

	for chunk := range chunks {
		switch chunk.Type {
		case provider.ChunkReasoning:
			if sig := thoughtSig(chunk.Metadata); sig != "" {
				as.pendingSignature = sig
			}
			if chunk.Text != "" {
				as.ensureThinkingBlock(w)
				sseEvent(w, map[string]any{
					"type":  "content_block_delta",
					"index": as.thinkingIndex,
					"delta": map[string]any{"type": "thinking_delta", "thinking": chunk.Text},
				})
			}
		case provider.ChunkText:
			if chunk.Text == "" {
				continue
			}
			as.ensureTextBlock(w)
			sseEvent(w, map[string]any{
				"type":  "content_block_delta",
				"index": as.textIndex,
				"delta": map[string]any{"type": "text_delta", "text": chunk.Text},
			})
		case provider.ChunkToolCallStreamStart:
			idx := as.toolIndex(chunk.ToolCallID)
			as.ensureToolBlock(w, chunk.ToolCallID, chunk.ToolName, thoughtSig(chunk.Metadata))
			if chunk.ToolInput != "" {
				as.sent[chunk.ToolCallID] = len(chunk.ToolInput)
				sseEvent(w, map[string]any{
					"type":  "content_block_delta",
					"index": idx,
					"delta": map[string]any{"type": "input_json_delta", "partial_json": chunk.ToolInput},
				})
			}
		case provider.ChunkToolCallDelta:
			idx := as.toolIndex(chunk.ToolCallID)
			if chunk.ToolInput == "" {
				continue
			}
			as.sent[chunk.ToolCallID] += len(chunk.ToolInput)
			sseEvent(w, map[string]any{
				"type":  "content_block_delta",
				"index": idx,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": chunk.ToolInput},
			})
		case provider.ChunkToolCall:
			idx := as.toolIndex(chunk.ToolCallID)
			remainder := chunk.ToolInput
			if n, ok := as.sent[chunk.ToolCallID]; ok && n < len(remainder) {
				remainder = remainder[n:]
			} else if ok {
				remainder = ""
			}
			if remainder != "" {
				as.sent[chunk.ToolCallID] += len(remainder)
				sseEvent(w, map[string]any{
					"type":  "content_block_delta",
					"index": idx,
					"delta": map[string]any{"type": "input_json_delta", "partial_json": remainder},
				})
			}
		case provider.ChunkStepFinish:
			if chunk.FinishReason != "" && !as.sentStop {
				as.stopReason = anthropicStopReason(chunk.FinishReason)
				as.sentStop = true
			}
		case provider.ChunkFinish:
			if chunk.FinishReason != "" && !as.sentStop {
				as.stopReason = anthropicStopReason(chunk.FinishReason)
				as.sentStop = true
			}
			as.finish(w, chunk)
			return
		case provider.ChunkError:
			as.finish(w, provider.StreamChunk{})
			return
		}
	}
	as.finish(w, provider.StreamChunk{})
}

// ---- shared helpers ----

type openAIStreamState struct {
	sent       map[string]int // toolCallID -> bytes already streamed as fragments
	ordinal    map[string]int // toolCallID -> index in the stream
	sentFinish bool
}

func (s *openAIStreamState) toolIndex(id string) int {
	if idx, ok := s.ordinal[id]; ok {
		return idx
	}
	idx := len(s.ordinal)
	s.ordinal[id] = idx
	s.sent[id] = 0
	return idx
}

type anthropicStreamState struct {
	sent             map[string]int
	ordinal          map[string]int
	thinkingIndex    int
	thinkingBlock    bool
	textIndex        int
	textBlock        bool
	toolBlocks       map[string]int // toolCallID -> content block index
	toolStarted      map[string]bool
	pendingSignature string
	stopReason       string
	sentStop         bool
}

func newAnthropicState() *anthropicStreamState {
	return &anthropicStreamState{
		sent:        map[string]int{},
		ordinal:     map[string]int{},
		toolBlocks:  map[string]int{},
		toolStarted: map[string]bool{},
	}
}

func (as *anthropicStreamState) toolIndex(id string) int {
	if idx, ok := as.ordinal[id]; ok {
		return idx
	}
	idx := len(as.ordinal)
	as.ordinal[id] = idx
	as.sent[id] = 0
	as.toolBlocks[id] = -1
	as.toolStarted[id] = false
	return idx
}

func (as *anthropicStreamState) ensureThinkingBlock(w io.Writer) {
	if as.thinkingBlock {
		return
	}
	as.thinkingIndex = as.nextBlockIndex()
	as.thinkingBlock = true
	sseEvent(w, map[string]any{
		"type":  "content_block_start",
		"index": as.thinkingIndex,
		"content_block": map[string]any{
			"type": "thinking", "thinking": "",
		},
	})
}

func (as *anthropicStreamState) ensureTextBlock(w io.Writer) {
	if as.textBlock {
		return
	}
	as.textIndex = as.nextBlockIndex()
	as.textBlock = true
	sseEvent(w, map[string]any{
		"type":  "content_block_start",
		"index": as.textIndex,
		"content_block": map[string]any{
			"type": "text", "text": "",
		},
	})
}

func (as *anthropicStreamState) ensureToolBlock(w io.Writer, id, name, sig string) {
	if idx, ok := as.toolBlocks[id]; ok && idx >= 0 {
		return
	}
	idx := as.nextBlockIndex()
	as.toolBlocks[id] = idx
	as.toolStarted[id] = true
	cb := map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}}
	if sig != "" {
		cb["thought_signature"] = sig
		cb["signature"] = sig
		cb["extra_fields"] = map[string]any{"thought_signature": sig}
	}
	sseEvent(w, map[string]any{
		"type":          "content_block_start",
		"index":         idx,
		"content_block": cb,
	})
}

func (as *anthropicStreamState) nextBlockIndex() int {
	n := as.thinkingIndex
	if as.textIndex > n && as.textBlock {
		n = as.textIndex
	}
	for _, idx := range as.toolBlocks {
		if idx > n {
			n = idx
		}
	}
	return n + 1
}

func (as *anthropicStreamState) finish(w io.Writer, chunk provider.StreamChunk) {
	// Thinking block stop (signature_delta first, then content_block_stop).
	if as.thinkingBlock {
		if as.pendingSignature != "" {
			sseEvent(w, map[string]any{
				"type":  "content_block_delta",
				"index": as.thinkingIndex,
				"delta": map[string]any{"type": "signature_delta", "signature": as.pendingSignature},
			})
		}
		sseEvent(w, map[string]any{"type": "content_block_stop", "index": as.thinkingIndex})
	}
	if as.textBlock {
		sseEvent(w, map[string]any{"type": "content_block_stop", "index": as.textIndex})
	}
	// Tool blocks: flush any buffered (not-yet-started) calls then stop.
	for _, idx := range as.toolBlocks {
		if idx < 0 {
			continue
		}
		sseEvent(w, map[string]any{"type": "content_block_stop", "index": idx})
	}
	if as.stopReason == "" {
		as.stopReason = "end_turn"
	}

	usage := chunk.Usage
	delta := map[string]any{"stop_reason": as.stopReason, "stop_sequence": nil}
	if container, ok := chunk.Metadata["container"]; ok {
		delta["container"] = container
	}
	usageMap := map[string]any{
		"input_tokens":  usage.InputTokens,
		"output_tokens": usage.OutputTokens,
	}
	if usage.ReasoningTokens > 0 {
		usageMap["output_tokens_details"] = map[string]any{"thinking_tokens": usage.ReasoningTokens}
	}
	if iters, ok := chunk.Metadata["iterations"]; ok {
		usageMap["iterations"] = iters
	}
	md := map[string]any{"type": "message_delta", "delta": delta, "usage": usageMap}
	if cm, ok := chunk.Metadata["contextManagement"]; ok {
		md["context_management"] = cm
	}
	sseEvent(w, md)
	sseEvent(w, map[string]any{"type": "message_stop"})
}

// ---- wire helpers ----

// toolCallsWire converts GoAI tool calls to OpenAI wire tool_calls, attaching
// the thought signature in the 3 places Claude Code checks.
func toolCallsWire(tcs []provider.ToolCall) []any {
	out := make([]any, 0, len(tcs))
	for _, tc := range tcs {
		fn := map[string]any{"name": tc.Name, "arguments": string(tc.Input)}
		item := map[string]any{"id": tc.ID, "type": "function", "function": fn}
		if sig := thoughtSig(tc.Metadata); sig != "" {
			attachToolSig(item, sig)
		}
		out = append(out, item)
	}
	return out
}

// attachToolSig places a thought signature at the tool_call level, the
// function level, and inside extra_fields (the 3-place convention).
func attachToolSig(tc map[string]any, sig string) {
	if sig == "" {
		return
	}
	tc["thought_signature"] = sig
	tc["signature"] = sig
	tc["extra_fields"] = map[string]any{"thought_signature": sig}
	if fn, ok := tc["function"].(map[string]any); ok {
		fn["thought_signature"] = sig
		fn["signature"] = sig
	}
}

// thoughtSig extracts the thought signature from chunk/result metadata,
// accepting the key conventions used across providers.
func thoughtSig(m map[string]any) string {
	for _, k := range []string{"thoughtSignature", "thought_signature", "signature"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// openAIFinishReason maps a GoAI finish reason to the OpenAI wire value.
func openAIFinishReason(fr provider.FinishReason) *string {
	if fr == "" {
		return nil
	}
	var s string
	switch fr {
	case provider.FinishStop:
		s = "stop"
	case provider.FinishToolCalls:
		s = "tool_calls"
	case provider.FinishLength:
		s = "length"
	case provider.FinishContentFilter:
		s = "content_filter"
	default:
		s = "stop"
	}
	return &s
}

// anthropicStopReason maps a GoAI finish reason to an Anthropic stop_reason
// via the existing protocol mapping (which expects OpenAI wire strings).
func anthropicStopReason(fr provider.FinishReason) string {
	wire := openAIFinishReason(fr)
	if wire == nil {
		return "end_turn"
	}
	return protocol.MapStopReason(*wire)
}

// usageWire renders GoAI usage as OpenAI wire usage, including reasoning
// tokens when present.
func usageWire(u provider.Usage) map[string]any {
	usage := map[string]any{
		"prompt_tokens":     u.InputTokens,
		"completion_tokens": u.OutputTokens,
		"total_tokens":      u.TotalTokens,
	}
	if usage["total_tokens"] == 0 && (u.InputTokens > 0 || u.OutputTokens > 0) {
		usage["total_tokens"] = u.InputTokens + u.OutputTokens
	}
	if u.ReasoningTokens > 0 {
		usage["completion_tokens_details"] = map[string]any{"reasoning_tokens": u.ReasoningTokens}
	}
	return usage
}

func streamChoice(delta map[string]any, finishReason *string) map[string]any {
	choice := map[string]any{"index": 0, "delta": delta}
	if finishReason != nil {
		choice["finish_reason"] = *finishReason
	}
	return map[string]any{
		"id":      responseID("chatcmpl"),
		"object":  "chat.completion.chunk",
		"choices": []any{choice},
	}
}

func sseEvent(w io.Writer, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n\n"))
}

func responseID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func createdEpoch(r provider.ResponseMetadata) int64 {
	return 0
}
