package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/provider"
	"github.com/sleepysoong/sleepyrouter/internal/server"
	"github.com/sleepysoong/sleepyrouter/internal/state"
	"github.com/sleepysoong/sleepyrouter/internal/usage"
)

type mockUpstream struct {
	t       *testing.T
	status  int
	body    string
	hits    atomic.Int32
	name    string
	respond func(w http.ResponseWriter, r *http.Request) (int, string)
}

func (m *mockUpstream) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.hits.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"type":"not_found","code":"x","message":"bad path","param":""}`))
			return
		}
		if m.respond != nil {
			st, body := m.respond(w, r)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(st)
			_, _ = w.Write([]byte(body))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(m.status)
		_, _ = w.Write([]byte(m.body))
	}
}

func successBody(id, model, text string) string {
	return `{"id":"` + id + `","object":"response","created_at":1,"model":"` + model + `","output":[{"type":"message","content":[{"type":"output_text","text":"` + text + `"}]}],"usage":{"input_tokens":3,"output_tokens":7}}`
}

func errBody(code, msg string) string {
	return `{"type":"server_error","code":"` + code + `","message":"` + msg + `","param":""}`
}

func testServer(t *testing.T, baseA, baseB, baseC, baseD string) *server.Server {
	t.Helper()
	toml := `
version = 1
[server]
host = "127.0.0.1"
port = 4567
[routing]
default_group = "coding"
[timeouts]
request = "10s"
first_event = "5s"
stream_idle = "10s"
[providers.zen]
enabled = true
base_url = "` + baseA + `"
api_key_env = "TEST_ZEN_KEY"
[providers.nvidia]
enabled = true
base_url = "` + baseB + `"
api_key_env = "TEST_NVIDIA_KEY"
[providers.openrouter]
enabled = true
base_url = "` + baseC + `"
api_key_env = "TEST_OR_KEY"
[providers.gemini]
enabled = true
base_url = "` + baseD + `"
api_key_env = "TEST_GEMINI_KEY"
[models."zen/a"]
provider = "zen"
upstream_model = "a"
[models."nvidia/b"]
provider = "nvidia"
upstream_model = "b"
[models."openrouter/c"]
provider = "openrouter"
upstream_model = "c"
[models."gemini/d"]
provider = "gemini"
upstream_model = "d"
[groups]
coding = ["zen/a", "nvidia/b", "openrouter/c", "gemini/d"]
`
	cfg, err := config.Parse([]byte(toml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	dotenv := map[string]string{
		"TEST_ZEN_KEY": "zk", "TEST_NVIDIA_KEY": "nk", "TEST_OR_KEY": "ok", "TEST_GEMINI_KEY": "gk",
	}
	store, err := config.NewStore(cfg, dotenv, 1)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ustore := usage.Open(t.TempDir()+"/usage.db", true)
	t.Cleanup(func() { ustore.Close() })
	return server.New(server.Deps{Store: store, Usage: ustore, Affinity: state.New(nil), Registry: provider.DefaultRegistry()})
}

func doResponses(t *testing.T, srv *server.Server, model string) (int, []byte, http.Header) {
	t.Helper()
	body := `{"model":"` + model + `","input":"hi"}`
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	resp := rec.Result()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header
}

func TestFailoverABC(t *testing.T) {
	var a, b, c, d mockUpstream
	srvA := httptest.NewServer((&a).handler())
	defer srvA.Close()
	a.status, a.body = 429, errBody("rate_limited", "slow down")
	srvB := httptest.NewServer((&b).handler())
	defer srvB.Close()
	b.status, b.body = 500, errBody("internal", "boom")
	srvC := httptest.NewServer((&c).handler())
	defer srvC.Close()
	c.status, c.body = 200, successBody("resp_1", "c", "hello")
	srvD := httptest.NewServer((&d).handler())
	defer srvD.Close()
	d.status, d.body = 200, successBody("resp_d", "d", "nope")

	srv := testServer(t, srvA.URL+"/v1", srvB.URL+"/v1", srvC.URL+"/v1", srvD.URL+"/v1")
	st, body, hdr := doResponses(t, srv, "coding")
	if st != 200 {
		t.Fatalf("status=%d body=%s", st, body)
	}
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, body)
	}
	if v["model"] != "coding" {
		t.Fatalf("client-facing model = %v, want coding", v["model"])
	}
	if hdr.Get("X-SleepyRouter-Model") != "openrouter/c" || hdr.Get("X-SleepyRouter-Attempts") != "3" {
		t.Fatalf("headers = %v", hdr)
	}
	if a.hits.Load() != 1 || b.hits.Load() != 1 || c.hits.Load() != 1 {
		t.Fatalf("hits a=%d b=%d c=%d", a.hits.Load(), b.hits.Load(), c.hits.Load())
	}
	if d.hits.Load() != 0 {
		t.Fatalf("d must not be called, hits=%d", d.hits.Load())
	}
}

func TestNoSDKRetry(t *testing.T) {
	var a mockUpstream
	srvA := httptest.NewServer((&a).handler())
	defer srvA.Close()
	a.status, a.body = 500, errBody("internal", "boom")
	// All others also fail so we can count A precisely.
	var b mockUpstream
	srvB := httptest.NewServer((&b).handler())
	defer srvB.Close()
	b.status, b.body = 200, successBody("resp_b", "b", "ok")
	srv := testServer(t, srvA.URL+"/v1", srvB.URL+"/v1", srvB.URL+"/v1", srvB.URL+"/v1")
	st, _, _ := doResponses(t, srv, "coding")
	if st != 200 {
		t.Fatalf("status=%d", st)
	}
	if a.hits.Load() != 1 {
		t.Fatalf("provider A hits = %d, want 1 (SDK retry must be 0)", a.hits.Load())
	}
}

func TestAffinitySticky(t *testing.T) {
	var a, b mockUpstream
	srvA := httptest.NewServer((&a).handler())
	defer srvA.Close()
	a.status, a.body = 200, successBody("resp_aff_1", "a", "first")
	srvB := httptest.NewServer((&b).handler())
	defer srvB.Close()
	b.status, b.body = 200, successBody("resp_other", "b", "second")
	srv := testServer(t, srvA.URL+"/v1", srvB.URL+"/v1", srvB.URL+"/v1", srvB.URL+"/v1")
	st, body, _ := doResponses(t, srv, "coding")
	if st != 200 {
		t.Fatalf("first: %d %s", st, body)
	}
	// Second request pins to A via previous_response_id.
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"coding","input":"follow","previous_response_id":"resp_aff_1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("second: %d %s", rec.Code, rec.Body.String())
	}
	if b.hits.Load() != 0 {
		t.Fatalf("affinity violated: b hits=%d, want 0", b.hits.Load())
	}
	if a.hits.Load() != 2 {
		t.Fatalf("a hits=%d, want 2", a.hits.Load())
	}
}

func TestAnthropicNonStream(t *testing.T) {
	var a mockUpstream
	srvA := httptest.NewServer((&a).handler())
	defer srvA.Close()
	a.status, a.body = 200, successBody("resp_9", "a", "hello-claude")
	srv := testServer(t, srvA.URL+"/v1", srvA.URL+"/v1", srvA.URL+"/v1", srvA.URL+"/v1")
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"coding","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var v struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.Model != "coding" || v.StopReason != "end_turn" {
		t.Fatalf("got %+v", v)
	}
}

func TestHealthAndModels(t *testing.T) {
	srv := testServer(t, "https://example.invalid/v1", "https://example.invalid/v1", "https://example.invalid/v1", "https://example.invalid/v1")
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("health %d", rec.Code)
	}
	req2 := httptest.NewRequest("GET", "/v1/models", nil)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("models %d", rec2.Code)
	}
}
