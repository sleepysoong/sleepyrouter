package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/provider"
	"github.com/sleepysoong/sleepyrouter/internal/server"
	"github.com/sleepysoong/sleepyrouter/internal/state"
	"github.com/sleepysoong/sleepyrouter/internal/usage"
)

// This exercises the Claude Code-facing Messages contract through the real
// gateway and the NVIDIA-style Chat Completions wire adapter. Claude Code owns
// the MCP process; this test simulates its local tool execution between calls.
func TestClaudeCodeLocalMCPToolLoopThroughChatCompletions(t *testing.T) {
	const (
		toolName = "mcp__filesystem__read_file"
		toolID   = "call_mcp_1"
	)
	var upstreamCalls atomic.Int32

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("upstream Authorization = %q, want test key", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream request: %v", err)
			http.Error(w, "bad JSON", http.StatusBadRequest)
			return
		}
		call := upstreamCalls.Add(1)
		if body["model"] != "deepseek-ai/deepseek-v4.1-flash" {
			t.Errorf("upstream model = %v", body["model"])
		}
		if body["reasoning_effort"] != float64(25) {
			t.Errorf("thinking was not mapped to enabled reasoning: reasoning_effort = %v", body["reasoning_effort"])
		}
		assertChatToolSchema(t, body, toolName)

		switch call {
		case 1:
			stream, _ := body["stream"].(bool)
			if !stream {
				t.Errorf("first Chat Completions request must stream, body=%v", body)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			frames := []string{
				`{"id":"chatcmpl_tool","object":"chat.completion.chunk","created":1,"model":"deepseek-ai/deepseek-v4.1-flash","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_mcp_1","type":"function","function":{"name":"mcp__filesystem__read_file","arguments":"{\"path\":"}}]},"finish_reason":null}]}`,
				`{"id":"chatcmpl_tool","object":"chat.completion.chunk","created":1,"model":"deepseek-ai/deepseek-v4.1-flash","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"/tmp/notes\"}"}}]},"finish_reason":null}]}`,
				`{"id":"chatcmpl_tool","object":"chat.completion.chunk","created":1,"model":"deepseek-ai/deepseek-v4.1-flash","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
				`{"id":"chatcmpl_tool","object":"chat.completion.chunk","created":1,"model":"deepseek-ai/deepseek-v4.1-flash","choices":[],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`,
			}
			for _, frame := range frames {
				if _, err := fmt.Fprintf(w, "data: %s\n\n", frame); err != nil {
					t.Errorf("write first stream frame: %v", err)
					return
				}
			}
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		case 2:
			stream, _ := body["stream"].(bool)
			if stream {
				t.Errorf("tool-result follow-up should be a non-stream request, body=%v", body)
			}
			assertLocalToolResult(t, body, toolName, toolID, `{"path":"/tmp/notes"}`, "local MCP file content")
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"id":"chatcmpl_final","object":"chat.completion","created":2,"model":"deepseek-ai/deepseek-v4.1-flash","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"file contents received"}}],"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16}}`)
		default:
			t.Errorf("unexpected upstream call #%d", call)
			http.Error(w, "too many calls", http.StatusInternalServerError)
		}
	}))
	defer upstream.Close()

	gateway := newChatCompatibilityGateway(t, upstream.URL+"/v1")
	defer gateway.Close()
	client := anthropicsdk.NewClient(
		option.WithAPIKey("client-test-key"),
		option.WithBaseURL(gateway.URL),
	)
	tool := anthropicsdk.ToolUnionParamOfTool(anthropicsdk.ToolInputSchemaParam{
		Properties: map[string]any{"path": map[string]any{"type": "string"}},
		Required:   []string{"path"},
	}, toolName)
	thinking := anthropicsdk.ThinkingConfigParamOfEnabled(1024)
	tools := []anthropicsdk.ToolUnionParam{tool}
	firstMessages := []anthropicsdk.MessageParam{
		anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("Read /tmp/notes")),
	}

	stream := client.Messages.NewStreaming(context.Background(), anthropicsdk.MessageNewParams{
		Model: "claude-sleepy", MaxTokens: 4096, Thinking: thinking,
		Tools: tools, Messages: firstMessages,
	})
	var toolUse *anthropicsdk.ToolUseBlock
	var first anthropicsdk.Message
	for stream.Next() {
		if err := first.Accumulate(stream.Current()); err != nil {
			t.Fatalf("accumulate Claude Code-facing tool stream: %v", err)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("Claude Code-facing Messages stream: %v", err)
	}
	_ = stream.Close()
	if string(first.StopReason) != "tool_use" {
		t.Fatalf("first stop_reason = %q, want tool_use; message=%+v", first.StopReason, first)
	}
	for _, block := range first.Content {
		if block.Type == "tool_use" {
			value := block.AsToolUse()
			toolUse = &value
			break
		}
	}
	if toolUse == nil || toolUse.ID != toolID || toolUse.Name != toolName || string(toolUse.Input) != `{"path":"/tmp/notes"}` {
		t.Fatalf("decoded local MCP tool call = %+v", toolUse)
	}

	var toolInput map[string]any
	if err := json.Unmarshal(toolUse.Input, &toolInput); err != nil {
		t.Fatalf("decode local MCP tool input: %v", err)
	}
	followup, err := client.Messages.New(context.Background(), anthropicsdk.MessageNewParams{
		Model: "claude-sleepy", MaxTokens: 4096, Thinking: thinking, Tools: tools,
		Messages: []anthropicsdk.MessageParam{
			anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock("Read /tmp/notes")),
			anthropicsdk.NewAssistantMessage(anthropicsdk.NewToolUseBlock(toolUse.ID, toolInput, toolUse.Name)),
			// Claude Code runs its local MCP server here and sends the result back.
			anthropicsdk.NewUserMessage(anthropicsdk.NewToolResultBlock(toolUse.ID, "local MCP file content", false)),
		},
	})
	if err != nil {
		t.Fatalf("Claude Code-style local MCP tool-result request: %v", err)
	}
	if string(followup.StopReason) != "end_turn" || len(followup.Content) != 1 || followup.Content[0].Text != "file contents received" {
		t.Fatalf("final Anthropic SDK message = %+v", followup)
	}
	if got := upstreamCalls.Load(); got != 2 {
		t.Fatalf("upstream calls = %d, want tool call + tool result follow-up", got)
	}
}

func newChatCompatibilityGateway(t *testing.T, upstreamURL string) *httptest.Server {
	t.Helper()
	toml := fmt.Sprintf(`
version = 1
[routing]
default_group = "coding"
[timeouts]
request = "15s"
first_event = "5s"
stream_idle = "10s"
[providers.nvidia]
base_url = %q
api_key_env = "TEST_NVIDIA_KEY"
wire_api = "chat_completions"
[models."nvidia/flash"]
provider = "nvidia"
upstream_model = "deepseek-ai/deepseek-v4.1-flash"
[models."nvidia/flash".capabilities]
tools = true
reasoning = true
[groups]
coding = ["nvidia/flash"]
`, upstreamURL)
	cfg, err := config.Parse([]byte(toml))
	if err != nil {
		t.Fatalf("parse Chat Completions gateway config: %v", err)
	}
	store, err := config.NewStore(cfg, map[string]string{"TEST_NVIDIA_KEY": "test-key"}, 1)
	if err != nil {
		t.Fatalf("build Chat Completions gateway config: %v", err)
	}
	usageStore := usage.Open(t.TempDir()+"/usage.db", true)
	t.Cleanup(func() { _ = usageStore.Close() })
	handler := server.New(server.Deps{
		Store: store, Usage: usageStore, Affinity: state.New(nil), Registry: provider.DefaultRegistry(),
	}).Handler()
	return httptest.NewServer(handler)
}

func assertChatToolSchema(t *testing.T, body map[string]any, wantName string) {
	t.Helper()
	items, ok := body["tools"].([]any)
	if !ok || len(items) != 1 {
		t.Errorf("upstream tools = %v, want one local MCP tool", body["tools"])
		return
	}
	tool, _ := items[0].(map[string]any)
	function, _ := tool["function"].(map[string]any)
	if function["name"] != wantName {
		t.Errorf("upstream tool name = %v, want %q", function["name"], wantName)
	}
	parameters, _ := function["parameters"].(map[string]any)
	properties, _ := parameters["properties"].(map[string]any)
	path, _ := properties["path"].(map[string]any)
	if path["type"] != "string" {
		t.Errorf("upstream MCP tool input schema = %v", parameters)
	}
}

func assertLocalToolResult(t *testing.T, body map[string]any, wantName, wantID, wantArgs, wantResult string) {
	t.Helper()
	messages, ok := body["messages"].([]any)
	if !ok {
		t.Errorf("upstream messages = %v", body["messages"])
		return
	}
	var foundCall, foundResult bool
	for _, item := range messages {
		message, _ := item.(map[string]any)
		switch message["role"] {
		case "assistant":
			calls, _ := message["tool_calls"].([]any)
			for _, callItem := range calls {
				call, _ := callItem.(map[string]any)
				function, _ := call["function"].(map[string]any)
				if call["id"] == wantID && function["name"] == wantName && function["arguments"] == wantArgs {
					foundCall = true
				}
			}
		case "tool":
			if message["tool_call_id"] == wantID && chatToolContentText(message["content"]) == wantResult {
				foundResult = true
			}
		}
	}
	if !foundCall || !foundResult {
		t.Errorf("local MCP tool call/result did not round-trip (call=%v result=%v): messages=%v", foundCall, foundResult, messages)
	}
}

func chatToolContentText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		var parts []string
		for _, item := range value {
			part, _ := item.(map[string]any)
			if text, ok := part["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}
