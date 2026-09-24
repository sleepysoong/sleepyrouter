package upstream_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/anthropic"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/upstream"
)

func TestRewriteModelPreservesUnknown(t *testing.T) {
	raw := []byte(`{"model":"coding","input":"hi","future_feature":{"x":1},"stream":false}`)
	out, err := upstream.RewriteModel(raw, "vendor/model-b")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	s := string(out)
	if !contains(s, `"model":"vendor/model-b"`) {
		t.Fatalf("model not rewritten: %s", s)
	}
	if !contains(s, "future_feature") {
		t.Fatalf("unknown field dropped: %s", s)
	}
}

func TestResponsesToChatCompletionConvertsMessagesAndTools(t *testing.T) {
	raw := []byte(`{
		"model":"local-alias",
		"instructions":"system instructions",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"find a file"}]},
			{"type":"function_call","call_id":"call_prev","name":"search","arguments":"{\"query\":\"x\"}"},
			{"type":"function_call_output","call_id":"call_prev","output":"found"}
		],
		"tools":[{"type":"function","name":"search","description":"search files","parameters":{"type":"object","properties":{"query":{"type":"string"}}},"strict":true}],
		"tool_choice":{"type":"function","name":"search"},
		"max_output_tokens":123,
		"parallel_tool_calls":false,
		"temperature":0.2
	}`)
	params, err := upstream.ResponsesToChatCompletion(raw, "deepseek-ai/deepseek-v4.1-flash")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal typed SDK params: %v", err)
	}
	var got struct {
		Model    string `json:"model"`
		Messages []struct {
			Role      string `json:"role"`
			Content   any    `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
		Tools             []json.RawMessage `json:"tools"`
		ToolChoice        json.RawMessage   `json:"tool_choice"`
		MaxTokens         int64             `json:"max_tokens"`
		ParallelToolCalls bool              `json:"parallel_tool_calls"`
		Temperature       float64           `json:"temperature"`
	}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("inspect typed request: %v (%s)", err, encoded)
	}
	if got.Model != "deepseek-ai/deepseek-v4.1-flash" || got.MaxTokens != 123 || got.ParallelToolCalls || got.Temperature != 0.2 {
		t.Fatalf("request top-level fields = %+v", got)
	}
	if len(got.Messages) != 4 || got.Messages[0].Role != "system" || got.Messages[1].Role != "user" || got.Messages[2].ToolCalls[0].ID != "call_prev" || got.Messages[3].ToolCallID != "call_prev" {
		t.Fatalf("converted messages = %+v", got.Messages)
	}
	if len(got.Tools) != 1 || len(got.ToolChoice) == 0 || string(got.ToolChoice) == `"auto"` {
		t.Fatalf("tools/tool_choice missing: tools=%s choice=%s", got.Tools, got.ToolChoice)
	}
}

func TestResponsesToChatCompletionRejectsStatefulResponsesID(t *testing.T) {
	_, err := upstream.ResponsesToChatCompletion([]byte(`{"input":"hi","previous_response_id":"resp_1"}`), "model")
	if err == nil {
		t.Fatal("expected previous_response_id to be rejected")
	}
}

func TestResponsesToChatCompletionPreservesReasoningEffort(t *testing.T) {
	params, err := upstream.ResponsesToChatCompletion([]byte(`{"input":"think","reasoning":{"effort":"high"}}`), "some-other-model")
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if params.ReasoningEffort != "high" {
		t.Fatalf("reasoning_effort = %q, want high", params.ReasoningEffort)
	}
}

func TestExecuteNonStreamMapsReasoningEffortForDeepSeekV41(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_reasoning","object":"chat.completion","created":42,"model":"deepseek-ai/deepseek-v4.1-flash","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"reasoned"}}],"usage":{"prompt_tokens":8,"completion_tokens":1,"total_tokens":9}}`))
	}))
	defer server.Close()

	provider := &config.RuntimeProvider{ID: "nim", BaseURL: server.URL + "/v1", APIKey: "test-key", WireAPI: "chat_completions"}
	candidate := routing.Candidate{LocalModelID: "nim/model", ProviderID: "nim", UpstreamModel: "deepseek-ai/deepseek-v4.1-flash", Provider: provider}
	_, err := upstream.ExecuteNonStream(context.Background(), upstream.NewClient(provider), candidate,
		[]byte(`{"input":"think","reasoning":{"effort":"high"}}`), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotBody["reasoning_effort"] != float64(75) {
		t.Fatalf("wire reasoning_effort = %#v, want numeric 75", gotBody["reasoning_effort"])
	}
}

func TestExecuteNonStreamMapsXHighReasoningEffortForDeepSeekV41(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_reasoning","object":"chat.completion","created":42,"model":"deepseek-ai/deepseek-v4.1-flash","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"reasoned"}}],"usage":{"prompt_tokens":8,"completion_tokens":1,"total_tokens":9}}`))
	}))
	defer server.Close()

	provider := &config.RuntimeProvider{ID: "nim", BaseURL: server.URL + "/v1", APIKey: "test-key", WireAPI: "chat_completions"}
	candidate := routing.Candidate{LocalModelID: "nim/model", ProviderID: "nim", UpstreamModel: "deepseek-ai/deepseek-v4.1-flash", Provider: provider}
	_, err := upstream.ExecuteNonStream(context.Background(), upstream.NewClient(provider), candidate,
		[]byte(`{"input":"think","reasoning":{"effort":"xhigh"}}`), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotBody["reasoning_effort"] != float64(90) {
		t.Fatalf("wire reasoning_effort = %#v, want numeric 90", gotBody["reasoning_effort"])
	}
}

func TestResponsesToChatCompletionPreservesLargeEagerToolList(t *testing.T) {
	const toolCount = 128
	tools := make([]map[string]any, 0, toolCount)
	for i := 0; i < toolCount; i++ {
		tools = append(tools, map[string]any{
			"name":         fmt.Sprintf("mcp__server__tool_%03d", i),
			"description":  "eager-loaded local MCP tool",
			"input_schema": map[string]any{"type": "object", "properties": map[string]any{}},
		})
	}
	anthropicRaw, err := json.Marshal(map[string]any{
		"model": "claude-sleepy", "max_tokens": 128,
		"messages": []any{map[string]any{"role": "user", "content": "use a local MCP tool"}},
		"tools":    tools,
	})
	if err != nil {
		t.Fatalf("marshal Anthropic request: %v", err)
	}
	parsed, err := anthropic.Parse(anthropicRaw)
	if err != nil {
		t.Fatalf("parse Anthropic request: %v", err)
	}
	responsesRaw, err := anthropic.ToResponses(parsed, "provider-model")
	if err != nil {
		t.Fatalf("convert Anthropic request to Responses: %v", err)
	}
	params, err := upstream.ResponsesToChatCompletion(responsesRaw, "provider-model")
	if err != nil {
		t.Fatalf("convert eager MCP tool list to Chat Completions: %v", err)
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal typed SDK params: %v", err)
	}
	var got struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("inspect typed tool list: %v", err)
	}
	if len(got.Tools) != toolCount {
		t.Fatalf("forwarded tool count = %d, want %d", len(got.Tools), toolCount)
	}
	if got.Tools[toolCount-1].Function.Name != "mcp__server__tool_127" {
		t.Fatalf("last forwarded tool = %q", got.Tools[toolCount-1].Function.Name)
	}
}

func TestChatCompletionsRoundTripMCPClientToolCall(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var body struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []struct {
				Role       string `json:"role"`
				ToolCallID string `json:"tool_call_id"`
				ToolCalls  []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			if len(body.Tools) != 1 || body.Tools[0].Function.Name != "mcp__filesystem__read_file" {
				t.Errorf("local MCP tool definition not forwarded: %+v", body.Tools)
			}
			_, _ = w.Write([]byte(`{"id":"chatcmpl_tool","object":"chat.completion","created":42,"model":"deepseek-ai/deepseek-v4.1-flash","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_mcp_1","type":"function","function":{"name":"mcp__filesystem__read_file","arguments":"{\"path\":\"/tmp/notes\"}"}}]}}],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`))
			return
		}
		if len(body.Messages) < 3 || body.Messages[0].Role != "assistant" || len(body.Messages[0].ToolCalls) != 1 || body.Messages[0].ToolCalls[0].ID != "call_mcp_1" || body.Messages[1].Role != "tool" || body.Messages[1].ToolCallID != "call_mcp_1" {
			t.Errorf("MCP result was not paired with the original call: %+v", body.Messages)
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl_done","object":"chat.completion","created":43,"model":"deepseek-ai/deepseek-v4.1-flash","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"file contents received"}}],"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16}}`))
	}))
	defer server.Close()

	provider := &config.RuntimeProvider{ID: "nim", BaseURL: server.URL + "/v1", APIKey: "test-key", WireAPI: "chat_completions"}
	client := upstream.NewClient(provider)
	candidate := routing.Candidate{LocalModelID: "nim/model", ProviderID: "nim", UpstreamModel: "deepseek-ai/deepseek-v4.1-flash", Provider: provider}
	tool := `{"type":"function","name":"mcp__filesystem__read_file","description":"Read a local file through MCP","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}`
	first := []byte(`{"instructions":"You can use local MCP tools.","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Read /tmp/notes"}]}],"tools":[` + tool + `]}`)
	result, err := upstream.ExecuteNonStream(context.Background(), client, candidate, first, nil)
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	var assistant struct {
		Output []struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"output"`
	}
	if err := json.Unmarshal(result.RawBody, &assistant); err != nil {
		t.Fatalf("decode tool response: %v", err)
	}
	if len(assistant.Output) != 1 || assistant.Output[0].Type != "function_call" || assistant.Output[0].CallID != "call_mcp_1" || assistant.Output[0].Name != "mcp__filesystem__read_file" {
		t.Fatalf("Responses-shaped MCP call = %+v", assistant.Output)
	}

	second := []byte(`{"input":[{"type":"function_call","call_id":"call_mcp_1","name":"mcp__filesystem__read_file","arguments":"{\"path\":\"/tmp/notes\"}"},{"type":"function_call_output","call_id":"call_mcp_1","output":"contents from MCP"},{"type":"message","role":"user","content":[{"type":"input_text","text":"Summarize it"}]}],"tools":[` + tool + `]}`)
	if _, err := upstream.ExecuteNonStream(context.Background(), client, candidate, second, nil); err != nil {
		t.Fatalf("tool-result turn: %v", err)
	}
	if requests != 2 {
		t.Fatalf("upstream calls = %d, want 2", requests)
	}
}

func TestChatCompletionsReplaysMCPToolReasoningAndCapturesNextState(t *testing.T) {
	var capturedPriorReasoning string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role             string          `json:"role"`
				Content          json.RawMessage `json:"content"`
				ReasoningContent string          `json:"reasoning_content"`
				ToolCallID       string          `json:"tool_call_id"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if len(body.Messages) != 3 {
			t.Errorf("message count = %d, want assistant/tool/user: %+v", len(body.Messages), body.Messages)
		} else {
			capturedPriorReasoning = body.Messages[0].ReasoningContent
			if body.Messages[0].Role != "assistant" || len(body.Messages[0].ToolCalls) != 1 || body.Messages[0].ToolCalls[0].ID != "call_mcp_1" ||
				body.Messages[0].ToolCalls[0].Function.Name != "mcp__filesystem__read_file" || body.Messages[1].Role != "tool" || body.Messages[1].ToolCallID != "call_mcp_1" || body.Messages[2].Role != "user" {
				t.Errorf("MCP call/result relationship changed: %+v", body.Messages)
			}
			if body.Messages[0].ReasoningContent != "private-prior-reasoning" {
				t.Errorf("prior provider reasoning was not replayed: %q", body.Messages[0].ReasoningContent)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_reasoned","object":"chat.completion","created":42,"model":"deepseek-ai/deepseek-v4.1-flash","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"The file says hello.","reasoning_content":"private-next-reasoning"}}],"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16}}`))
	}))
	defer server.Close()

	provider := &config.RuntimeProvider{ID: "nim", BaseURL: server.URL + "/v1", APIKey: "test-key", WireAPI: "chat_completions"}
	candidate := routing.Candidate{LocalModelID: "nim/model", ProviderID: "nim", UpstreamModel: "deepseek-ai/deepseek-v4.1-flash", Provider: provider}
	raw := []byte(`{
		"input":[
			{"type":"message","role":"assistant","content":"I am checking the file."},
			{"type":"function_call","call_id":"call_mcp_1","name":"mcp__filesystem__read_file","arguments":"{\"path\":\"/tmp/notes\"}"},
			{"type":"chat_reasoning","content":"private-prior-reasoning"},
			{"type":"function_call_output","call_id":"call_mcp_1","output":"file contents"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Summarize the file."}]}
		],
		"tools":[{"type":"function","name":"mcp__filesystem__read_file","description":"Read a local file through MCP","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}]
	}`)
	result, err := upstream.ExecuteNonStream(context.Background(), upstream.NewClient(provider), candidate, raw, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.ReasoningContent != "private-next-reasoning" {
		t.Fatalf("captured provider reasoning = %q", result.ReasoningContent)
	}
	if strings.Contains(string(result.RawBody), "private-next-reasoning") {
		t.Fatalf("provider-private reasoning leaked into bridged response: %s", result.RawBody)
	}
	if capturedPriorReasoning != "private-prior-reasoning" {
		t.Fatalf("captured prior reasoning field = %q", capturedPriorReasoning)
	}
}

func TestResponsesReasoningItemsRetainEncryptedContinuation(t *testing.T) {
	raw := []byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"done"}]},{"type":"reasoning","id":"rs_1","encrypted_content":"opaque-continuation","summary":[]},{"type":"function_call","call_id":"call_1","name":"tool","arguments":"{}"}]}`)
	items := upstream.ResponsesReasoningItems(raw)
	if len(items) != 1 {
		t.Fatalf("reasoning item count = %d", len(items))
	}
	var got struct {
		Type             string `json:"type"`
		EncryptedContent string `json:"encrypted_content"`
	}
	if err := json.Unmarshal(items[0], &got); err != nil {
		t.Fatalf("decode preserved item: %v", err)
	}
	if got.Type != "reasoning" || got.EncryptedContent != "opaque-continuation" {
		t.Fatalf("reasoning continuation changed: %s", items[0])
	}
}

func TestChatCompletionResponseBodyMapsUsageAndTruncation(t *testing.T) {
	var response openai.ChatCompletion
	if err := json.Unmarshal([]byte(`{
		"id":"chatcmpl_1","object":"chat.completion","created":42,"model":"deepseek-ai/deepseek-v4.1-flash",
		"choices":[{"index":0,"finish_reason":"length","message":{"role":"assistant","content":"partial"}}],
		"usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25,"prompt_tokens_details":{"cached_tokens":12,"cache_write_tokens":2}}
	}`), &response); err != nil {
		t.Fatalf("unmarshal SDK response: %v", err)
	}
	body, err := upstream.ChatCompletionResponseBody(&response)
	if err != nil {
		t.Fatalf("convert response: %v", err)
	}
	var got struct {
		Status            string `json:"status"`
		IncompleteDetails struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			InputTokens        int64 `json:"input_tokens"`
			InputTokensDetails struct {
				CachedTokens     int64 `json:"cached_tokens"`
				CacheWriteTokens int64 `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode Responses-shaped result: %v", err)
	}
	if got.Status != "incomplete" || got.IncompleteDetails.Reason != "max_output_tokens" || got.Output[0].Content[0].Text != "partial" {
		t.Fatalf("response = %+v", got)
	}
	if got.Usage.InputTokens != 20 || got.Usage.InputTokensDetails.CachedTokens != 12 || got.Usage.InputTokensDetails.CacheWriteTokens != 2 {
		t.Fatalf("usage = %+v", got.Usage)
	}
}

func TestExecuteNonStreamUsesChatCompletionsSDKEndpoint(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl_test","object":"chat.completion","created":42,"model":"upstream-model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"NIM-OK"}}],"usage":{"prompt_tokens":8,"completion_tokens":1,"total_tokens":9}}`))
	}))
	defer server.Close()

	provider := &config.RuntimeProvider{ID: "nim", BaseURL: server.URL + "/v1", APIKey: "test-key", WireAPI: "chat_completions"}
	candidate := routing.Candidate{LocalModelID: "nim/model", ProviderID: "nim", UpstreamModel: "upstream-model", Provider: provider}
	result, err := upstream.ExecuteNonStream(context.Background(), upstream.NewClient(provider), candidate, []byte(`{"model":"local-model","input":"say NIM-OK","max_output_tokens":8}`), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("request path = %q", gotPath)
	}
	if gotBody["model"] != "upstream-model" || gotBody["max_tokens"] != float64(8) {
		t.Fatalf("upstream request body = %v", gotBody)
	}
	var response struct {
		ID     string `json:"id"`
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(result.RawBody, &response); err != nil {
		t.Fatalf("decode bridged response: %v", err)
	}
	if !strings.HasPrefix(response.ID, "resp_") || response.Output[0].Content[0].Text != "NIM-OK" {
		t.Fatalf("bridged response = %+v", response)
	}
}

func TestMeaningfulEvents(t *testing.T) {
	if upstream.IsMeaningfulEvent("response.created", `{}`) {
		t.Fatal("created should not commit")
	}
	if !upstream.IsMeaningfulEvent("response.output_text.delta", `{"delta":"hi"}`) {
		t.Fatal("text delta should commit")
	}
	if !upstream.IsMeaningfulEvent("response.refusal.delta", `{"delta":"no"}`) {
		t.Fatal("refusal delta should commit")
	}
	if !upstream.IsMeaningfulEvent("response.completed", `{}`) {
		t.Fatal("completed should commit")
	}
}

func TestDeriveRequirements(t *testing.T) {
	r := upstream.DeriveRequirements([]byte(`{"model":"m","tools":[{"type":"function"}],"input":"hi"}`))
	if !r.Tools {
		t.Fatal("tools")
	}
	r2 := upstream.DeriveRequirements([]byte(`{"model":"m","reasoning":{"effort":"high"},"input":"hi"}`))
	if !r2.Reasoning {
		t.Fatal("reasoning")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestApplyProviderDefaults(t *testing.T) {
	c := routing.Candidate{Model: config.RuntimeModel{
		Extra: map[string]any{"service_tier": "flex"},
	}}
	out, err := upstream.ApplyProviderDefaults([]byte(`{"model":"m","input":"hi"}`), c)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	for _, want := range []string{`"service_tier":"flex"`} {
		if !contains(string(out), want) {
			t.Fatalf("missing %s in %s", want, out)
		}
	}
	// Explicit client values always win.
	out2, err := upstream.ApplyProviderDefaults([]byte(`{"model":"m","service_tier":"auto","thinking":{"type":"disabled"}}`), c)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !contains(string(out2), `"service_tier":"auto"`) || contains(string(out2), "flex") || contains(string(out2), `"type":"disabled"`) == false || contains(string(out2), "1024") {
		t.Fatalf("client value overridden: %s", out2)
	}

	defaulted, err := upstream.ApplyProviderDefaults([]byte(`{"model":"m","input":"hi"}`), routing.Candidate{
		Model: config.RuntimeModel{ReasoningEffort: "high"},
	})
	if err != nil {
		t.Fatalf("reasoning default: %v", err)
	}
	if !contains(string(defaulted), `"reasoning":{"effort":"high"}`) {
		t.Fatalf("reasoning default missing: %s", defaulted)
	}

	explicit, err := upstream.ApplyProviderDefaults([]byte(`{"model":"m","input":"hi","reasoning":{"effort":"low"}}`), routing.Candidate{
		Model: config.RuntimeModel{ReasoningEffort: "high"},
	})
	if err != nil {
		t.Fatalf("explicit reasoning: %v", err)
	}
	if !contains(string(explicit), `"effort":"low"`) || contains(string(explicit), `"effort":"high"`) {
		t.Fatalf("model default overrode explicit effort: %s", explicit)
	}
}
