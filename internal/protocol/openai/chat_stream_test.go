package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/anthropic"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/upstream"
)

type fakeChatCompletionStream struct {
	chunks []openaisdk.ChatCompletionChunk
	index  int
}

func (s *fakeChatCompletionStream) Next() bool {
	if s.index >= len(s.chunks) {
		return false
	}
	s.index++
	return true
}
func (s *fakeChatCompletionStream) Current() openaisdk.ChatCompletionChunk {
	return s.chunks[s.index-1]
}
func (s *fakeChatCompletionStream) Err() error   { return nil }
func (s *fakeChatCompletionStream) Close() error { return nil }

func TestChatCompletionStreamMapsTextAndUsage(t *testing.T) {
	inner := &fakeChatCompletionStream{chunks: []openaisdk.ChatCompletionChunk{
		{Choices: []openaisdk.ChatCompletionChunkChoice{{Index: 0, Delta: openaisdk.ChatCompletionChunkChoiceDelta{Content: "hello"}}}},
		{Choices: []openaisdk.ChatCompletionChunkChoice{{Index: 0, FinishReason: "stop"}}, Usage: openaisdk.CompletionUsage{PromptTokens: 7, CompletionTokens: 1, TotalTokens: 8}},
	}}
	stream := &chatCompletionEventStream{inner: inner, responseID: "resp_test", model: "test-model", createdAt: 1, tools: map[int64]*streamedToolCall{}}
	var names []string
	var completed map[string]any
	for stream.Next() {
		name, payload := stream.Event()
		names = append(names, name)
		if name == "response.completed" {
			if err := json.Unmarshal(payload, &completed); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if !containsEvent(names, "response.output_text.delta") || names[len(names)-1] != "response.completed" {
		t.Fatalf("events = %v", names)
	}
	response := completed["response"].(map[string]any)
	usage := response["usage"].(map[string]any)
	if usage["input_tokens"] != float64(7) || response["status"] != "completed" {
		t.Fatalf("final response = %v", response)
	}
}

func TestChatCompletionStreamMapsRefusalToResponsesAndAnthropic(t *testing.T) {
	chunk := func(raw string) openaisdk.ChatCompletionChunk {
		t.Helper()
		var decoded openaisdk.ChatCompletionChunk
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("decode chunk: %v", err)
		}
		return decoded
	}
	inner := &fakeChatCompletionStream{chunks: []openaisdk.ChatCompletionChunk{
		chunk(`{"id":"chatcmpl_refusal","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"refusal":"I can’t "},"finish_reason":null}]}`),
		chunk(`{"id":"chatcmpl_refusal","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"refusal":"help with that."},"finish_reason":null}]}`),
		chunk(`{"id":"chatcmpl_refusal","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`),
	}}
	stream := &chatCompletionEventStream{inner: inner, responseID: "resp_refusal", model: "test", createdAt: 1, tools: map[int64]*streamedToolCall{}}
	encoder := anthropic.NewStreamEncoder("claude-alias")
	var names []string
	var refusalText strings.Builder
	var finalResponse map[string]any
	_ = encoder.StartEvents()
	for stream.Next() {
		name, payload := stream.Event()
		names = append(names, name)
		if name == "response.completed" {
			if err := json.Unmarshal(payload, &finalResponse); err != nil {
				t.Fatal(err)
			}
		}
		for _, ev := range encoder.HandleResponsesEvent(name, string(payload)) {
			if ev.Event == "content_block_delta" {
				var body struct {
					Delta struct {
						Text string `json:"text"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(ev.Data), &body); err != nil {
					t.Fatal(err)
				}
				refusalText.WriteString(body.Delta.Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if !containsEvent(names, "response.refusal.delta") || !containsEvent(names, "response.refusal.done") || names[len(names)-1] != "response.completed" {
		t.Fatalf("mapped events = %v", names)
	}
	if refusalText.String() != "I can’t help with that." || !encoder.Success || encoder.StopReason != "refusal" {
		t.Fatalf("Anthropic refusal=%q success=%v stop=%q", refusalText.String(), encoder.Success, encoder.StopReason)
	}
	response := finalResponse["response"].(map[string]any)
	output := response["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("final output = %v", output)
	}
	message := output[0].(map[string]any)
	content := message["content"].([]any)
	if content[0].(map[string]any)["type"] != "refusal" || content[0].(map[string]any)["refusal"] != "I can’t help with that." {
		t.Fatalf("final refusal item = %v", message)
	}
}

func TestChatCompletionStreamKeepsProviderReasoningPrivateAndCapturesContinuation(t *testing.T) {
	chunk := func(raw string) openaisdk.ChatCompletionChunk {
		t.Helper()
		var decoded openaisdk.ChatCompletionChunk
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("decode chunk: %v", err)
		}
		return decoded
	}
	inner := &fakeChatCompletionStream{chunks: []openaisdk.ChatCompletionChunk{
		chunk(`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"reasoning_content":"private-"},"finish_reason":null}]}`),
		chunk(`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"reasoning_content":"reasoning"},"finish_reason":null}]}`),
		chunk(`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{"content":"visible answer"},"finish_reason":null}]}`),
		chunk(`{"id":"chatcmpl_1","object":"chat.completion.chunk","created":1,"model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`),
	}}
	stream := &chatCompletionEventStream{inner: inner, responseID: "resp_test", model: "test", createdAt: 1, tools: map[int64]*streamedToolCall{}}
	var eventPayloads strings.Builder
	for stream.Next() {
		_, payload := stream.Event()
		eventPayloads.Write(payload)
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if got := stream.ProviderReasoningContent(); got != "private-reasoning" {
		t.Fatalf("provider reasoning continuation = %q", got)
	}
	if strings.Contains(eventPayloads.String(), "private-") || strings.Contains(eventPayloads.String(), "reasoning\"") {
		t.Fatalf("provider-private reasoning leaked to downstream events: %s", eventPayloads.String())
	}
	if !strings.Contains(eventPayloads.String(), "visible answer") {
		t.Fatalf("visible assistant text missing from downstream events: %s", eventPayloads.String())
	}
}

func TestChatCompletionStreamToolCallFeedsAnthropicEncoder(t *testing.T) {
	inner := &fakeChatCompletionStream{chunks: []openaisdk.ChatCompletionChunk{
		{Choices: []openaisdk.ChatCompletionChunkChoice{{Index: 0, Delta: openaisdk.ChatCompletionChunkChoiceDelta{ToolCalls: []openaisdk.ChatCompletionChunkChoiceDeltaToolCall{{
			Index: 0, ID: "call_1", Type: "function", Function: openaisdk.ChatCompletionChunkChoiceDeltaToolCallFunction{Name: "mcp__filesystem__read_file", Arguments: `{"path":`},
		}}}}}},
		{Choices: []openaisdk.ChatCompletionChunkChoice{{Index: 0, Delta: openaisdk.ChatCompletionChunkChoiceDelta{ToolCalls: []openaisdk.ChatCompletionChunkChoiceDeltaToolCall{{
			Index: 0, Function: openaisdk.ChatCompletionChunkChoiceDeltaToolCallFunction{Arguments: `"/tmp/notes"}`},
		}}}}}},
		{Choices: []openaisdk.ChatCompletionChunkChoice{{Index: 0, FinishReason: "tool_calls"}}},
	}}
	stream := &chatCompletionEventStream{inner: inner, responseID: "resp_test", model: "test-model", createdAt: 1, tools: map[int64]*streamedToolCall{}}
	encoder := anthropic.NewStreamEncoder("claude-alias")
	var anthropicEvents []anthropic.SSEEvent
	anthropicEvents = append(anthropicEvents, encoder.StartEvents()...)
	for stream.Next() {
		name, payload := stream.Event()
		anthropicEvents = append(anthropicEvents, encoder.HandleResponsesEvent(name, string(payload))...)
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	if !encoder.Terminal || !encoder.Success || encoder.StopReason != "tool_use" {
		t.Fatalf("Anthropic stream result: terminal=%v success=%v stop=%s", encoder.Terminal, encoder.Success, encoder.StopReason)
	}
	var toolID, toolName, arguments string
	for _, event := range anthropicEvents {
		var body map[string]any
		if err := json.Unmarshal([]byte(event.Data), &body); err != nil {
			t.Fatalf("decode Anthropic event: %v", err)
		}
		switch event.Event {
		case "content_block_start":
			block := body["content_block"].(map[string]any)
			if block["type"] == "tool_use" {
				toolID = block["id"].(string)
				toolName = block["name"].(string)
			}
		case "content_block_delta":
			delta := body["delta"].(map[string]any)
			if delta["type"] == "input_json_delta" {
				arguments += delta["partial_json"].(string)
			}
		}
	}
	if toolID != "call_1" || toolName != "mcp__filesystem__read_file" || arguments != `{"path":"/tmp/notes"}` {
		t.Fatalf("tool output = id:%q name:%q arguments:%q", toolID, toolName, arguments)
	}
}

func TestChatCompletionSDKStreamingWireRequestAndResponse(t *testing.T) {
	var requestPath string
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"deepseek-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"NIM-OK\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"deepseek-test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"deepseek-test\",\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1,\"total_tokens\":6}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := &config.RuntimeProvider{ID: "nim", BaseURL: server.URL + "/v1", APIKey: "test-key", WireAPI: "chat_completions"}
	candidate := routing.Candidate{LocalModelID: "nim/model", ProviderID: "nim", UpstreamModel: "deepseek-ai/deepseek-v4.1-flash", Provider: provider}
	client := upstream.NewClient(provider)
	stream, err := openChatCompletionStream(context.Background(), client, candidate, []byte(`{"input":"hello","max_output_tokens":12,"reasoning":{"effort":"high"}}`))
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.Close()
	var names []string
	var completed map[string]any
	for stream.Next() {
		name, payload := stream.Event()
		names = append(names, name)
		if name == "response.completed" {
			if err := json.Unmarshal(payload, &completed); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if requestPath != "/v1/chat/completions" || requestBody["stream"] != true || requestBody["max_tokens"] != float64(12) || requestBody["reasoning_effort"] != float64(75) {
		t.Fatalf("request path/body = %q %v", requestPath, requestBody)
	}
	streamOptions, _ := requestBody["stream_options"].(map[string]any)
	if streamOptions["include_usage"] != true {
		t.Fatalf("stream_options = %v", streamOptions)
	}
	if !containsEvent(names, "response.output_text.delta") || names[len(names)-1] != "response.completed" {
		t.Fatalf("mapped events = %v", names)
	}
	response := completed["response"].(map[string]any)
	usage := response["usage"].(map[string]any)
	if usage["input_tokens"] != float64(5) || !strings.HasPrefix(response["id"].(string), "resp_") {
		t.Fatalf("completed response = %v", response)
	}
}

func containsEvent(events []string, want string) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}
