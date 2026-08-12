// sandbox/harness.go is a self-contained end-to-end test harness for
// sleepyrouter. It spins up fake OpenAI + Anthropic upstream servers, points
// the real sleepyrouter binary (built from ./cmd/sleepyrouter) at them via
// env overrides, launches it on an alternate port, and runs an aggressive
// battery of curl-driven HTTP tests against both wire surfaces.
//
// Run:   go run ./sandbox        (after: go build -o bin/sleepyrouter ./cmd/sleepyrouter)
//
// It exits 0 only if every check passes.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// capture records each upstream request so tests can assert what sleepyrouter
// actually sent (model id, body shape, headers).
type capture struct {
	mu      sync.Mutex
	requests []upstreamCall
}

type upstreamCall struct {
	method  string
	url     string
	headers http.Header
	body    []byte
}

func (c *capture) add(m, u string, h http.Header, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, upstreamCall{m, u, h, b})
}

func (c *capture) snapshot() []upstreamCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]upstreamCall, len(c.requests))
	copy(out, c.requests)
	return out
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ SANDBOX FAILED: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("\n✅ SANDBOX PASSED: all aggressive checks green")
}

func run() error {
	// ---- 1. Build the real binary ----
	binPath := filepath.Join(os.TempDir(), "sleepyrouter-sandbox")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/sleepyrouter")
	cmd.Dir = repoRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, out)
	}
	defer os.Remove(binPath)

	// ---- 2. Stand up fake upstreams ----
	oraC := &capture{}
	anthC := &capture{}
	upstream := newFakeUpstream(oraC, anthC)
	srv := &http.Server{Handler: upstream}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	upstreamURL := "http://" + ln.Addr().String()
	go srv.Serve(ln)
	defer srv.Shutdown(context.Background())
	fmt.Printf("fake upstream on %s\n", upstreamURL)
	upstreamRegistry.Store(oraC, upstream)
	upstreamRegistry.Store(anthC, upstream)

	// ---- 3. Isolated SLEEPYROUTER_HOME ----
	home, err := os.MkdirTemp("", "sleepyrouter-sandbox-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(home)
	if err := writeSandboxConfig(home, upstreamURL); err != nil {
		return err
	}

	// ---- 4. Launch the real sleepyrouter binary ----
	port, err := freePort()
	if err != nil {
		return err
	}
	env := os.Environ()
	env = append(env,
		"SLEEPYROUTER_HOME="+home,
		// point every provider's base URL at the fake upstream
		"SLEEPYROUTER_OPENROUTER_BASE_URL="+upstreamURL,
		"SLEEPYROUTER_NVIDIA_BASE_URL="+upstreamURL,
		"SLEEPYROUTER_GOOGLE_BASE_URL="+upstreamURL,
		"SLEEPYROUTER_ZEN_BASE_URL="+upstreamURL,
		"SLEEPYROUTER_COPILOT_BASE_URL="+upstreamURL,
		"SLEEPYROUTER_COPILOT_TOKEN_URL="+upstreamURL+"/copilot_internal/v2/token",
		// fake provider keys (any non-empty value activates the provider)
		"OPENROUTER_API_KEY=fake-or",
		"NVIDIA_API_KEY=fake-nvidia",
		"OPENCODE_API_KEY=fake-zen",
		"GOOGLE_API_KEY=fake-google",
		"GITHUB_COPILOT_TOKEN=fake-copilot-pat",
	)
	proc := exec.Command(binPath, "start", "--port", strconv.Itoa(port))
	proc.Env = env
	proc.Dir = repoRoot()
	stdoutR, _ := proc.StdoutPipe()
	stderrR, _ := proc.StderrPipe()
	// stderr -> terminal forward (helps debug)
	go func() { _, _ = io.Copy(os.Stderr, stderrR) }()
	if err := proc.Start(); err != nil {
		return fmt.Errorf("start sleepyrouter: %w", err)
	}
	defer func() {
		_ = proc.Process.Signal(os.Interrupt)
		_ = proc.Wait()
	}()
	portPat := regexp.MustCompile(`http://localhost:(\d+)`)
	var started int32
	doneCh := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(stdoutR)
		for sc.Scan() {
			line := sc.Text()
			fmt.Println("[sleepyrouter] " + line)
			if atomic.LoadInt32(&started) == 0 && portPat.MatchString(line) {
				atomic.StoreInt32(&started, 1)
				close(doneCh)
			}
		}
	}()
	select {
	case <-doneCh:
	case <-time.After(8 * time.Second):
		return errors.New("sleepyrouter did not announce its port within 8s")
	}
	base := fmt.Sprintf("http://localhost:%d", port)
	fmt.Printf("sleepyrouter on %s\n", base)

	// wait for /health to be ready
	if err := waitForHealth(base + "/health"); err != nil {
		return err
	}

	// ---- 5. Aggressive test battery ----
	type check struct {
		name string
		fn   func() error
	}
	checks := []check{
		{"health", func() error { return checkHealth(base) }},
		{"models list", func() error { return checkModels(base) }},
		{"openai non-stream text", func() error { return checkOpenAINonStream(base) }},
		{"openai stream text", func() error { return checkOpenAIStream(base) }},
		{"openai tool_use round trip", func() error { return checkOpenAIToolUse(base) }},
		{"anthropic non-stream text", func() error { return checkAnthropicNonStream(base) }},
		{"anthropic stream text", func() error { return checkAnthropicStream(base) }},
		{"anthropic thinking signature", func() error { return checkAnthropicThinking(base) }},
		{"anthropic tool_use block", func() error { return checkAnthropicToolUse(base) }},
		{"failover 5xx then success", func() error { return checkFailover(base, oraC) }},
		{"count_tokens", func() error { return checkCountTokens(base) }},
		{"404 unknown route", func() error { return checkNotFound(base) }},
		{"captured upstream request shape", func() error { return checkCapturedShape(oraC) }},
	}
	var failed int
	for _, c := range checks {
		fmt.Printf("\n→ %s\n", c.name)
		if err := c.fn(); err != nil {
			failed++
			fmt.Printf("   FAIL: %v\n", err)
		} else {
			fmt.Printf("   ok\n")
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d/%d checks failed", failed, len(checks))
	}
	return nil
}

// ---- Sandbox config ----

func writeSandboxConfig(home, upstream string) error {
	cfg := map[string]any{
		"port": 0, // overridden by --port; placeholder
		"modelGroups": map[string]any{
			"fast":     []string{"or-fast", "or-fast-2"},
			"balanced": []string{"zen-balanced", "google-balanced"},
			"copilot":  []string{"copilot-sonnet"},
		},
		"defaultModelGroup": "fast",
		"models": map[string]any{
			"or-fast":         map[string]any{"provider": "openrouter", "name": "fake/llama:free", "source": "openrouter"},
			"or-fast-2":       map[string]any{"provider": "openrouter", "name": "fake/llama:free", "source": "openrouter"},
			"nvidia-fast":     map[string]any{"provider": "nvidia", "name": "fake/llama-nvidia", "source": "nvidia"},
			"zen-balanced":    map[string]any{"provider": "zen", "name": "fake/deepseek", "source": "zen"},
			"google-balanced": map[string]any{"provider": "google", "name": "fake/gemini-flash", "source": "google"},
			"copilot-sonnet":  map[string]any{"provider": "copilot", "name": "fake/claude-sonnet", "source": "copilot"},
		},
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(home, "config.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	env := strings.Join([]string{
		"# sandbox env (placeholders)",
	}, "\n")
	return os.WriteFile(filepath.Join(home, ".env"), []byte(env), 0o644)
}

// ---- Fake upstream ----

// newFakeUpstream returns an HTTP handler that:
//  - replies with canned OpenAI chat.completions responses for /chat/completions
//  - replies with canned Anthropic messages responses for /messages (+ /v1/messages)
//  - handles copilot token exchange at /copilot_internal/v2/token
//  - can be put into "fail mode" per path via failMode to test failover
type fakeUpstream struct {
	captO *capture
	captA *capture
	mu    sync.Mutex
	failN int // fail the next N chat-completions requests
}

func newFakeUpstream(o, a *capture) *fakeUpstream {
	return &fakeUpstream{captO: o, captA: a}
}

func (f *fakeUpstream) failNext(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failN = n
}

func (f *fakeUpstream) shouldFail() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failN > 0 {
		f.failN--
		return true
	}
	return false
}

func (f *fakeUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	path := r.URL.Path
	// Is this an Anthropic-shape request (sent by goai's anthropic.Chat
	// model, with anthropic-version header + max_tokens + messages)?
	 anthReq := r.Header.Get("anthropic-version") != "" || strings.Contains(path, "/messages")
	switch {
	case strings.HasSuffix(path, "/copilot_internal/v2/token"):
		f.captO.add(r.Method, path, r.Header, body)
		// echo back a session token expiring in 1h
		httpjson(w, 200, map[string]any{
			"token":     "copilot-session-token",
			"expires_at": float64(time.Now().Add(time.Hour).Unix()),
		})
	case strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/messages"):
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		// Anthropic-shape requests (anthropic.Chat model dials both
		// /chat/completions and /messages with Anthropic bodies).
		if anthReq {
			f.captA.add(r.Method, path, r.Header, body)
			stream, _ := req["stream"].(bool)
			if stream {
				serveAnthropicStream(w, req)
			} else {
				serveAnthropicNonStream(w, req)
			}
		} else {
			f.captO.add(r.Method, path, r.Header, body)
			if f.shouldFail() {
				httpjson(w, 500, map[string]any{"error": map[string]any{"message": "boom", "type": "server_error"}})
				return
			}
			stream, _ := req["stream"].(bool)
			if stream {
				serveOpenAIStream(w, req)
			} else {
				serveOpenAINonStream(w, req)
			}
		}
	default:
		httpjson(w, 404, map[string]any{"error": "unknown upstream path: " + path})
	}
}

func serveOpenAINonStream(w http.ResponseWriter, req map[string]any) {
	modelID, _ := req["model"].(string)
	// tools present -> tool_calls
	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		httpjson(w, 200, map[string]any{
			"id":      "chatcmpl-fake-tool",
			"object":  "chat.completion",
			"model":   modelID,
			"created": 1700000000,
			"choices": []any{
				map[string]any{
					"index": 0,
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []any{
							map[string]any{
								"id":    "call_1",
								"type":  "function",
								"function": map[string]any{
									"name":      "get_weather",
									"arguments": `{"city":"Seoul"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 9, "total_tokens": 21},
		})
		return
	}
	httpjson(w, 200, map[string]any{
		"id":      "chatcmpl-fake",
		"object":  "chat.completion",
		"model":   modelID,
		"created": 1700000000,
		"choices": []any{
			map[string]any{
				"index":        0,
				"message":      map[string]any{"role": "assistant", "content": "안녕, sandbox 응답이야!"},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 7, "total_tokens": 12},
	})
}

func serveOpenAIStream(w http.ResponseWriter, req map[string]any) {
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	modelID, _ := req["model"].(string)
	const chunk = `data: %s

`
	emit := func(obj map[string]any) {
		b, _ := json.Marshal(obj)
		fmt.Fprintf(w, chunk, string(b))
		if flusher != nil {
			flusher.Flush()
		}
	}
	emit(map[string]any{
		"id":      "chatcmpl-fake-stream",
		"object":  "chat.completion.chunk",
		"model":   modelID,
		"created": 1700000000,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": ""}},
	})
	emit(map[string]any{
		"id":      "chatcmpl-fake-stream",
		"object":  "chat.completion.chunk",
		"model":   modelID,
		"created": 1700000000,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "stream-"}, "finish_reason": ""}},
	})
	emit(map[string]any{
		"id":      "chatcmpl-fake-stream",
		"object":  "chat.completion.chunk",
		"model":   modelID,
		"created": 1700000000,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "chunk!"}, "finish_reason": ""}},
	})
	emit(map[string]any{
		"id":      "chatcmpl-fake-stream",
		"object":  "chat.completion.chunk",
		"model":   modelID,
		"created": 1700000000,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
	})
	emitFinish(w, flusher)
}

func serveAnthropicNonStream(w http.ResponseWriter, req map[string]any) {
	modelID, _ := req["model"].(string)
	stopReason := "end_turn"
	content := []any{
		map[string]any{"type": "text", "text": "anthropic sandbox 응답"},
	}
	if _, ok := req["thinking"]; ok {
		content = append([]any{
			map[string]any{
				"type":     "thinking",
				"thinking": "let me think about it",
				"signature": "FAKE-SIG-1234567890",
			},
		}, content...)
	}
	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		stopReason = "tool_use"
		content = []any{
			map[string]any{
				"type":  "tool_use",
				"id":    "toolu_01",
				"name":  "get_weather",
				"input": map[string]any{"city": "Seoul"},
			},
		}
	}
	httpjson(w, 200, map[string]any{
		"id":           "msg_fake",
		"type":         "message",
		"role":         "assistant",
		"model":        modelID,
		"content":      content,
		"stop_reason":  stopReason,
		"stop_sequence": "",
		"usage":        map[string]any{"input_tokens": 11, "output_tokens": 8},
	})
}

func serveAnthropicStream(w http.ResponseWriter, req map[string]any) {
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	modelID, _ := req["model"].(string)
	sseEvent := func(typ string, data map[string]any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, string(b))
		if flusher != nil {
			flusher.Flush()
		}
	}
	sseEvent("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":      "msg_fake_stream",
			"type":    "message",
			"role":    "assistant",
			"content": []any{},
			"model":   modelID,
			"usage":   map[string]any{"input_tokens": 7, "output_tokens": 1},
		},
	})
	// thinking block + signature_delta when thinking requested
	if _, ok := req["thinking"]; ok {
		sseEvent("content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{"type": "thinking", "thinking": ""},
		})
		sseEvent("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "thinking_delta", "thinking": "너무 복잡"},
		})
		sseEvent("content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "signature_delta", "signature": "FAKE-STREAM-SIG-9876"},
		})
		sseEvent("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	}
	textIdx := 1
	if _, ok := req["thinking"]; !ok {
		textIdx = 0
	}
	sseEvent("content_block_start", map[string]any{
		"type":           "content_block_start",
		"index":          textIdx,
		"content_block": map[string]any{"type": "text", "text": ""},
	})
	sseEvent("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": textIdx,
		"delta": map[string]any{"type": "text_delta", "text": "안녕 "},
	})
	sseEvent("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": textIdx,
		"delta": map[string]any{"type": "text_delta", "text": "스트림"},
	})
	sseEvent("content_block_stop", map[string]any{"type": "content_block_stop", "index": textIdx})
	sseEvent("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": ""},
		"usage": map[string]any{"output_tokens": 3},
	})
	sseEvent("message_stop", map[string]any{"type": "message_stop"})
}

func emitFinish(w io.Writer, flusher http.Flusher) {
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// ---- HTTP helpers ----

func httpjson(w http.ResponseWriter, code int, obj any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	b, _ := json.Marshal(obj)
	_, _ = w.Write(b)
}

func repoRoot() string {
	if r, err := os.Getwd(); err == nil && strings.Contains(r, "sandbox") {
		return filepath.Dir(r)
	}
	return "."
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func waitForHealth(url string) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("health never returned 200")
}

func doPost(url, authHeader, authValue string, body map[string]any) (*http.Response, []byte, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set(authHeader, authValue)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data, nil
}

// ---- Test cases ----

func checkHealth(base string) error {
	resp, data, err := doGet(base + "/health")
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, data)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if m["ok"] != true {
		return fmt.Errorf("ok != true: %s", data)
	}
	return nil
}

func doGet(url string) (*http.Response, []byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, data, nil
}

func checkModels(base string) error {
	resp, data, err := doGet(base + "/v1/models")
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, data)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	dataArr, ok := m["data"].([]any)
	if !ok || len(dataArr) == 0 {
		return fmt.Errorf("empty models data: %s", data)
	}
	return nil
}

func checkOpenAINonStream(base string) error {
	_, data, err := doPost(base+"/v1/chat/completions", "Authorization", "Bearer sleepyrouter-local", map[string]any{
		"model":    "sleepyrouter/fast",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if err != nil {
		return err
	}
	var r map[string]any
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	if r["object"] != "chat.completion" {
		return fmt.Errorf("object=%v expected chat.completion: %s", r["object"], data)
	}
	choices, _ := r["choices"].([]any)
	if len(choices) == 0 {
		return fmt.Errorf("no choices: %s", data)
	}
	c0 := choices[0].(map[string]any)
	if c0["finish_reason"] != "stop" {
		return fmt.Errorf("finish=%v expected stop", c0["finish_reason"])
	}
	msg := c0["message"].(map[string]any)
	if strings.TrimSpace(msg["content"].(string)) == "" {
		return fmt.Errorf("empty content: %s", data)
	}
	return nil
}

func checkOpenAIStream(base string) error {
	body := map[string]any{
		"model":    "sleepyrouter/fast",
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", base+"/v1/chat/completions", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sleepyrouter-local")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		_, data := readAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, data)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	gotRole, gotContent, gotDone, gotFinish := false, false, false, false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			gotDone = true
			break
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		c := choices[0].(map[string]any)
		delta, _ := c["delta"].(map[string]any)
		if role, ok := delta["role"].(string); ok && role == "assistant" {
			gotRole = true
		}
		if content, ok := delta["content"].(string); ok && content != "" {
			gotContent = true
		}
		if fr, ok := c["finish_reason"]; ok {
			if s, ok := fr.(string); ok && s == "stop" {
				gotFinish = true
			}
		}
	}
	if !gotRole {
		return errors.New("stream: no role delta seen")
	}
	if !gotContent {
		return errors.New("stream: no content delta seen")
	}
	if !gotFinish {
		return errors.New("stream: no finish_reason=stop seen")
	}
	if !gotDone {
		return errors.New("stream: no [DONE] terminator")
	}
	return nil
}

func checkOpenAIToolUse(base string) error {
	_, data, err := doPost(base+"/v1/chat/completions", "Authorization", "Bearer sleepyrouter-local", map[string]any{
		"model":    "sleepyrouter/fast",
		"messages": []any{map[string]any{"role": "user", "content": "what's the weather in Seoul"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "get_weather",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}},
			},
		}},
	})
	if err != nil {
		return err
	}
	var r map[string]any
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	choices, _ := r["choices"].([]any)
	if len(choices) == 0 {
		return fmt.Errorf("no choices: %s", data)
	}
	c0 := choices[0].(map[string]any)
	if c0["finish_reason"] != "tool_calls" {
		return fmt.Errorf("finish=%v expected tool_calls: %s", c0["finish_reason"], data)
	}
	msg := c0["message"].(map[string]any)
	tcs, _ := msg["tool_calls"].([]any)
	if len(tcs) == 0 {
		return fmt.Errorf("no tool_calls: %s", data)
	}
	tc := tcs[0].(map[string]any)
	fn := tc["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		return fmt.Errorf("tool name=%v expected get_weather", fn["name"])
	}
	return nil
}

func checkAnthropicNonStream(base string) error {
	_, data, err := doPost(base+"/anthropic/v1/messages", "x-api-key", "sleepyrouter-local", map[string]any{
		"model":      "sleepyrouter/fast",
		"max_tokens": 100,
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
	})
	if err != nil {
		return err
	}
	var r map[string]any
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	if r["type"] != "message" {
		return fmt.Errorf("type=%v expected message: %s", r["type"], data)
	}
	if r["stop_reason"] == "" {
		return fmt.Errorf("no stop_reason: %s", data)
	}
	content, _ := r["content"].([]any)
	if len(content) == 0 {
		return fmt.Errorf("empty content: %s", data)
	}
	return nil
}

func checkAnthropicStream(base string) error {
	body := map[string]any{
		"model":      "sleepyrouter/fast",
		"max_tokens": 100,
		"stream":     true,
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", base+"/anthropic/v1/messages", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "sleepyrouter-local")
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		_, data := readAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, data)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	seen := map[string]bool{}
	var lastEvent string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			lastEvent = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		seen[lastEvent] = true
	}
	for _, ev := range []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"} {
		if !seen[ev] {
			return fmt.Errorf("missing SSE event: %s (seen: %v)", ev, seen)
		}
	}
	return nil
}

func checkAnthropicThinking(base string) error {
	_, data, err := doPost(base+"/anthropic/v1/messages", "x-api-key", "sleepyrouter-local", map[string]any{
		"model":      "sleepyrouter/fast",
		"max_tokens": 1000,
		"thinking":   map[string]any{"type": "enabled", "budget_tokens": 500},
		"messages":   []any{map[string]any{"role": "user", "content": "explain"}},
	})
	if err != nil {
		return err
	}
	var r map[string]any
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	content, _ := r["content"].([]any)
	var hasThinking, hasSig bool
	for _, block := range content {
		b, _ := block.(map[string]any)
		if b["type"] == "thinking" {
			hasThinking = true
			if _, ok := b["signature"]; ok {
				hasSig = true
			}
		}
	}
	if !hasThinking {
		return fmt.Errorf("no thinking block: %s", data)
	}
	if !hasSig {
		return fmt.Errorf("thinking block missing signature: %s", data)
	}
	return nil
}

func checkAnthropicToolUse(base string) error {
	_, data, err := doPost(base+"/anthropic/v1/messages", "x-api-key", "sleepyrouter-local", map[string]any{
		"model":      "sleepyrouter/fast",
		"max_tokens": 100,
		"messages":   []any{map[string]any{"role": "user", "content": "weather"}},
		"tools": []any{map[string]any{
			"name": "get_weather",
			"input_schema": map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}},
		}},
	})
	if err != nil {
		return err
	}
	var r map[string]any
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	content, _ := r["content"].([]any)
	var hasTool bool
	for _, block := range content {
		b, _ := block.(map[string]any)
		if b["type"] == "tool_use" {
			hasTool = true
			if b["name"] != "get_weather" {
				return fmt.Errorf("tool name=%v expected get_weather", b["name"])
			}
		}
	}
	if !hasTool {
		return fmt.Errorf("no tool_use block: %s", data)
	}
	if r["stop_reason"] != "tool_use" {
		return fmt.Errorf("stop_reason=%v expected tool_use: %s", r["stop_reason"], data)
	}
	return nil
}

func checkFailover(base string, capt *capture) error {
	// tell the fake upstream to fail the next 1 chat-completions, then succeed
	// (simulating the 1st provider request failing; sleepyrouter should fall over)
	fu := captToUpstream(capt)
	if fu == nil {
		return errors.New("no fake upstream handle for failover")
	}
	// tell the fake upstream to fail the next 2 chat-completions so
	// sleepyrouter's failover kicks in:
	//  1st: 502 -> fall over to fallback provider
	//  2nd: fallback also 502 -> 502 to client
	// We want success, so fail just 1 (the primary).
	fu.failNext(1)
	_, data, err := doPost(base+"/v1/chat/completions", "Authorization", "Bearer sleepyrouter-local", map[string]any{
		"model":    "sleepyrouter/fast",
		"messages": []any{map[string]any{"role": "user", "content": "failover test"}},
	})
	if err != nil {
		return err
	}
	var r map[string]any
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	if r["object"] != "chat.completion" {
		return fmt.Errorf("no completion after failover — got: %s", data)
	}
	return nil
}

func checkCountTokens(base string) error {
	_, data, err := doPost(base+"/anthropic/v1/messages/count_tokens", "x-api-key", "sleepyrouter-local", map[string]any{
		"model":    "sleepyrouter/fast",
		"messages": []any{map[string]any{"role": "user", "content": "hello world this is a token count test"}},
	})
	if err != nil {
		return err
	}
	var r map[string]any
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	if _, ok := r["input_tokens"]; !ok {
		return fmt.Errorf("no input_tokens: %s", data)
	}
	return nil
}

func checkNotFound(base string) error {
	resp, data, err := doGet(base + "/bogus/route")
	if err != nil {
		return err
	}
	if resp.StatusCode != 404 {
		return fmt.Errorf("status %d expected 404: %s", resp.StatusCode, data)
	}
	return nil
}

func checkCapturedShape(capt *capture) error {
	// Each upstream call captured should have the real model id (not the alias)
	// and the Authorization header (proving provider keying happened).
	// Wait: in the battery above, we only ran OpenAI-surface tests against
	// model sleepyrouter/fast. The fake upstream got the request with the
	// upstream model name injected by the handler. Assert it's a non-empty
	// string distinct from the alias.
	reqs := capt.snapshot()
	if len(reqs) == 0 {
		return errors.New("no upstream requests captured")
	}
	for _, r := range reqs {
		if !strings.Contains(r.url, "/chat/completions") {
			continue
		}
		var b map[string]any
		if err := json.Unmarshal(r.body, &b); err != nil {
			continue
		}
		model, _ := b["model"].(string)
		if model == "" || strings.HasPrefix(model, "sleepyrouter/") {
			return fmt.Errorf("upstream model still alias: %q (url=%s)", model, r.url)
		}
		auth := r.headers.Get("Authorization")
		if auth == "" {
			return fmt.Errorf("no Authorization header on upstream call to %s", r.url)
		}
	}
	return nil
}

// captToUpstream is a hack to let the failover test trigger failures on the
// fake upstream. Since the handler shares state via package-level captures,
// we stash the *fakeUpstream on the capture at construction time.
// (In a real test file this would be cleaner; for the sandbox harness it's fine.)
var upstreamRegistry sync.Map

func captToUpstream(c *capture) *fakeUpstream {
	if v, ok := upstreamRegistry.Load(c); ok {
		return v.(*fakeUpstream)
	}
	return nil
}

func readAll(r io.Reader) (io.Reader, []byte) {
	b, _ := io.ReadAll(r)
	return bytes.NewReader(b), b
}
