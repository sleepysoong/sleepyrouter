package streaming

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/sseutil"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

type openAIToolStreamState struct {
	blockIndex        int
	id                string
	name              string
	started           bool
	bufferedArguments string
	thoughtSignature  string
}

// anthropicStreamState carries the mutable scan state shared by the closures
// that PipeOpenAIStreamAsAnthropic previously defined inline.
type anthropicStreamState struct {
	w                 http.ResponseWriter
	nextBlockIndex    int
	textBlockIndex    int
	textBlockOpen     bool
	thinkingBlockIdx  int
	thinkingBlockOpen bool
	thinkingSignature string
	usedTool          bool
	toolBlocks        map[int]*openAIToolStreamState
	toolOrder         []int
	mu                sync.Mutex
}

func (s *anthropicStreamState) ensureThinkingBlock() int {
	if !s.thinkingBlockOpen {
		s.thinkingBlockIdx = s.nextBlockIndex
		s.nextBlockIndex++
		sseutil.WriteEvent(s.w, "content_block_start", map[string]any{"type": "content_block_start", "index": s.thinkingBlockIdx, "content_block": map[string]any{"type": "thinking", "thinking": ""}})
		s.thinkingBlockOpen = true
	}
	return s.thinkingBlockIdx
}

func (s *anthropicStreamState) stopThinkingBlock() {
	if s.thinkingBlockOpen && s.thinkingBlockIdx >= 0 {
		if s.thinkingSignature != "" {
			sseutil.WriteEvent(s.w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": s.thinkingBlockIdx, "delta": map[string]any{"type": "signature_delta", "signature": s.thinkingSignature}})
		}
		sseutil.WriteEvent(s.w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": s.thinkingBlockIdx})
		s.thinkingBlockOpen = false
		s.thinkingBlockIdx = -1
	}
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
	if sig, ok := delta["thought_signature"].(string); ok && sig != "" {
		state.thoughtSignature = sig
	}
	if !state.started && state.name != "" {
		s.stopTextBlock()
		cb := map[string]any{"type": "tool_use", "id": state.id, "name": state.name, "input": map[string]any{}}
		if state.thoughtSignature != "" {
			cb["thought_signature"] = state.thoughtSignature
			cb["signature"] = state.thoughtSignature
			cb["extra_fields"] = map[string]any{"thought_signature": state.thoughtSignature}
		}
		sseutil.WriteEvent(s.w, "content_block_start", map[string]any{"type": "content_block_start", "index": state.blockIndex, "content_block": cb})
		state.started = true
		s.usedTool = true
		if state.bufferedArguments != "" {
			sseutil.WriteEvent(s.w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": state.blockIndex, "delta": map[string]any{"type": "input_json_delta", "partial_json": state.bufferedArguments}})
			state.bufferedArguments = ""
		}
	}
	return state
}
