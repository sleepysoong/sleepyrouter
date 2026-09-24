package server_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	openaisdk "github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

// Contract test: each official client parses output from the real HTTP gateway
// while a local Responses server supplies identical tool-call events.
func TestOfficialClientsReadGatewayStreams(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			http.Error(w, "bad path", 404)
			return
		}
		requestBody, _ := io.ReadAll(r.Body)
		if strings.Contains(string(requestBody), `"function_call_output"`) {
			if !strings.Contains(string(requestBody), `"call_id":"call_1"`) {
				t.Errorf("tool result lost call_id: %s", requestBody)
			}
			if !strings.Contains(string(requestBody), `"output":"ok"`) {
				t.Errorf("tool result content did not round-trip: %s", requestBody)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(successBody("resp_2", "a", "file contents")))
			return
		}
		if strings.Contains(string(requestBody), `"mcp__filesystem__read_file"`) && !strings.Contains(string(requestBody), `"reasoning":{"effort":"low"}`) {
			t.Errorf("thinking-enabled MCP request lost its reasoning effort: %s", requestBody)
		}
		if strings.Contains(string(requestBody), `"mcp__filesystem__read_file"`) && !strings.Contains(string(requestBody), `"path":{"type":"string"}`) {
			t.Errorf("local MCP tool schema did not round-trip: %s", requestBody)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		frames := [][2]string{
			{"response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response","model":"a","status":"in_progress","output":[]}}`},
			{"response.output_item.added", `{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"mcp__filesystem__read_file","arguments":""}}`},
			{"response.function_call_arguments.delta", `{"type":"response.function_call_arguments.delta","sequence_number":2,"item_id":"fc_1","output_index":0,"delta":"{\"path\":\"a\"}"}`},
			{"response.function_call_arguments.done", `{"type":"response.function_call_arguments.done","sequence_number":3,"item_id":"fc_1","output_index":0,"arguments":"{\"path\":\"a\"}"}`},
			{"response.output_item.done", `{"type":"response.output_item.done","sequence_number":4,"output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"mcp__filesystem__read_file","arguments":"{\"path\":\"a\"}"}}`},
			{"response.completed", `{"type":"response.completed","sequence_number":5,"response":{"id":"resp_1","object":"response","model":"a","status":"completed","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"mcp__filesystem__read_file","arguments":"{\"path\":\"a\"}"}],"usage":{"input_tokens":6,"output_tokens":7,"total_tokens":13,"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":1}}}}`},
		}
		for _, frame := range frames {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", frame[0], frame[1])
		}
	}))
	defer upstream.Close()
	gateway := httptest.NewServer(testServer(t, upstream.URL+"/v1", upstream.URL+"/v1", upstream.URL+"/v1", upstream.URL+"/v1").Handler())
	defer gateway.Close()

	openaiClient := openaisdk.NewClient(openaioption.WithAPIKey("dummy"), openaioption.WithBaseURL(gateway.URL+"/v1"))
	openaiStream := openaiClient.Responses.NewStreaming(context.Background(), responses.ResponseNewParams{
		Model: "coding", Input: responses.ResponseNewParamsInputUnion{OfString: openaisdk.String("hi")},
	})
	var complete bool
	for openaiStream.Next() {
		ev := openaiStream.Current()
		if ev.Type == "response.completed" {
			complete = true
			if !strings.Contains(ev.RawJSON(), `"model":"coding"`) {
				t.Fatalf("model not rewritten: %s", ev.RawJSON())
			}
		}
	}
	if err := openaiStream.Err(); err != nil {
		t.Fatalf("OpenAI SDK stream: %v", err)
	}
	if !complete {
		t.Fatal("OpenAI SDK missed completion")
	}
	_ = openaiStream.Close()

	anthropicClient := anthropicsdk.NewClient(anthropicoption.WithAPIKey("dummy"), anthropicoption.WithBaseURL(gateway.URL))
	tool := anthropicsdk.ToolUnionParamOfTool(anthropicsdk.ToolInputSchemaParam{
		Properties: map[string]any{"path": map[string]any{"type": "string"}},
		Required:   []string{"path"},
	}, "mcp__filesystem__read_file")
	thinking := anthropicsdk.ThinkingConfigParamOfEnabled(1024)
	anthropicStream := anthropicClient.Messages.NewStreaming(context.Background(), anthropicsdk.MessageNewParams{
		Model: "coding", MaxTokens: 4096, Thinking: thinking,
		Tools:    []anthropicsdk.ToolUnionParam{tool},
		Messages: []anthropicsdk.MessageParam{anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("hi"))},
	})
	var message anthropicsdk.Message
	for anthropicStream.Next() {
		if err := message.Accumulate(anthropicStream.Current()); err != nil {
			t.Fatalf("Anthropic SDK accumulate: %v", err)
		}
	}
	if err := anthropicStream.Err(); err != nil {
		t.Fatalf("Anthropic SDK stream: %v", err)
	}
	_ = anthropicStream.Close()
	if len(message.Content) != 1 || message.Content[0].AsToolUse().ID != "call_1" || message.Content[0].AsToolUse().Name != "mcp__filesystem__read_file" || string(message.Content[0].AsToolUse().Input) != `{"path":"a"}` || string(message.StopReason) != "tool_use" {
		t.Fatalf("Anthropic SDK message: %+v", message)
	}
	if message.Usage.InputTokens != 3 || message.Usage.OutputTokens != 7 {
		t.Fatalf("Anthropic SDK usage: %+v", message.Usage)
	}
	if message.Usage.CacheCreationInputTokens != 1 || message.Usage.CacheReadInputTokens != 2 {
		t.Fatalf("Anthropic SDK cache usage: %+v", message.Usage)
	}
	followup, err := anthropicClient.Messages.New(context.Background(), anthropicsdk.MessageNewParams{
		Model: "coding", MaxTokens: 4096,
		Thinking: thinking, Tools: []anthropicsdk.ToolUnionParam{tool},
		Messages: []anthropicsdk.MessageParam{
			anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("read a")),
			anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock("call_1", map[string]any{"path": "a"}, "mcp__filesystem__read_file")),
			anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock("call_1", "ok", false)),
		},
	})
	if err != nil {
		t.Fatalf("Anthropic SDK tool-result followup: %v", err)
	}
	if len(followup.Content) != 1 || followup.Content[0].Text != "file contents" {
		t.Fatalf("followup=%+v", followup)
	}
}

func TestOfficialClientsReadGatewayNonStreams(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(successBody("resp_text", "a", "hello")))
	}))
	defer upstream.Close()
	gateway := httptest.NewServer(testServer(t, upstream.URL+"/v1", upstream.URL+"/v1", upstream.URL+"/v1", upstream.URL+"/v1").Handler())
	defer gateway.Close()
	openaiClient := openaisdk.NewClient(openaioption.WithAPIKey("dummy"), openaioption.WithBaseURL(gateway.URL+"/v1"))
	response, err := openaiClient.Responses.New(context.Background(), responses.ResponseNewParams{
		Model: "coding", Input: responses.ResponseNewParamsInputUnion{OfString: openaisdk.String("hi")},
	})
	if err != nil || string(response.Model) != "coding" {
		t.Fatalf("OpenAI SDK response=%+v err=%v", response, err)
	}
	anthropicClient := anthropicsdk.NewClient(anthropicoption.WithAPIKey("dummy"), anthropicoption.WithBaseURL(gateway.URL))
	message, err := anthropicClient.Messages.New(context.Background(), anthropicsdk.MessageNewParams{
		Model: "coding", MaxTokens: 32,
		Messages: []anthropicsdk.MessageParam{anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("hi"))},
	})
	if err != nil || len(message.Content) != 1 || message.Content[0].Text != "hello" || string(message.StopReason) != "end_turn" {
		t.Fatalf("Anthropic SDK message=%+v err=%v", message, err)
	}
}
