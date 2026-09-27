package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openaisdk "github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/provider"
	"github.com/sleepysoong/sleepyrouter/internal/server"
	"github.com/sleepysoong/sleepyrouter/internal/state"
)

// POST /hoard/v1/responses: the Responses contract plus a routing trace.
// Failure modes covered: failover chain, skipped (no key) candidates, client
// error stopping routing, every candidate failing, unknown model, credential
// echo in upstream errors, streams failing before/after commit, and the plain
// /v1/responses endpoint staying unchanged.

type hoardTrace struct {
	RequestedModel   string   `json:"requested_model"`
	RouteReason      string   `json:"route_reason"`
	Candidates       []string `json:"candidates"`
	SelectedModel    string   `json:"selected_model"`
	SelectedProvider string   `json:"selected_provider"`
	Attempts         []struct {
		Index         int    `json:"index"`
		Model         string `json:"model"`
		Provider      string `json:"provider"`
		UpstreamModel string `json:"upstream_model"`
		Outcome       string `json:"outcome"`
		ErrorClass    string `json:"error_class"`
		StatusCode    int    `json:"status_code"`
		Reason        string `json:"reason"`
		FailedOver    *bool  `json:"failed_over"`
	} `json:"attempts"`
}

func (tr hoardTrace) outcomes() string {
	var parts []string
	for _, a := range tr.Attempts {
		p := a.Model + "=" + a.Outcome
		if a.ErrorClass != "" {
			p += "/" + a.ErrorClass
		}
		if a.FailedOver != nil {
			p += fmt.Sprintf("/over=%v", *a.FailedOver)
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, " ")
}

const (
	hoardKeyZen = "sk-zen-SECRET-1234567890"
	hoardKeyNv  = "sk-nv-SECRET-0987654321"
	hoardKeyOR  = "sk-or-SECRET-abcdefabcd"
)

// hoardServer: group coding = zen/a, nvidia/b, openrouter/c, gemini/d (gemini has no key).
func hoardServer(t *testing.T, baseA, baseB, baseC string) *server.Server {
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
stream_idle = "5s"
[providers.zen]
base_url = "` + baseA + `"
api_key_env = "H_ZEN"
[providers.nvidia]
base_url = "` + baseB + `"
api_key_env = "H_NV"
[providers.openrouter]
base_url = "` + baseC + `"
api_key_env = "H_OR"
[providers.gemini]
base_url = "http://127.0.0.1:1/v1"
api_key_env = "H_GEMINI_UNSET"
[models."zen/a"]
provider = "zen"
upstream_model = "a"
[models."nvidia/b"]
provider = "nvidia"
upstream_model = "vendor/b"
[models."openrouter/c"]
provider = "openrouter"
upstream_model = "c"
[models."gemini/d"]
provider = "gemini"
upstream_model = "d"
[groups]
coding = ["zen/a", "gemini/d", "nvidia/b", "openrouter/c"]
`
	cfg, err := config.Parse([]byte(toml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	store, err := config.NewStore(cfg, map[string]string{"H_ZEN": hoardKeyZen, "H_NV": hoardKeyNv, "H_OR": hoardKeyOR}, 1)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return server.New(server.Deps{Store: store, Affinity: state.New(nil), Registry: provider.DefaultRegistry()})
}

// providerErr is the real OpenAI-style error envelope providers send.
func providerErr(typ, code, msg string) string {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"type": typ, "code": code, "message": msg, "param": nil}})
	return string(b)
}

func upstreamJSON(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func post(t *testing.T, srv *server.Server, path, body string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func traceOf(t *testing.T, body []byte) hoardTrace {
	t.Helper()
	var v struct {
		SR *struct {
			Routing hoardTrace `json:"routing"`
		} `json:"sleepyrouter"`
		Routing *hoardTrace `json:"routing"` // data of the sleepyrouter.routing SSE event
	}
	if err := json.Unmarshal(body, &v); err != nil || (v.SR == nil && v.Routing == nil) {
		t.Fatalf("no routing trace in %s (err=%v)", body, err)
	}
	if v.Routing != nil {
		return *v.Routing
	}
	return v.SR.Routing
}

func TestHoardNonStreamFailoverTrace(t *testing.T) {
	a := upstreamJSON(429, providerErr("rate_limit_error", "rate_limited", "slow down"))
	defer a.Close()
	b := upstreamJSON(503, providerErr("server_error", "overloaded", "model overloaded, try again"))
	defer b.Close()
	c := upstreamJSON(200, successBody("resp_c", "c", "hello from c"))
	defer c.Close()
	srv := hoardServer(t, a.URL+"/v1", b.URL+"/v1", c.URL+"/v1")

	st, body := post(t, srv, "/hoard/v1/responses", `{"model":"coding","input":"hi"}`)
	if st != 200 {
		t.Fatalf("status=%d body=%s", st, body)
	}
	// Still a Responses object for OpenAI clients.
	var resp responses.Response
	if err := json.Unmarshal(body, &resp); err != nil || resp.ID != "resp_c" || resp.OutputText() != "hello from c" || string(resp.Model) != "coding" {
		t.Fatalf("not a valid Responses body: id=%q text=%q model=%q err=%v\n%s", resp.ID, resp.OutputText(), resp.Model, err, body)
	}
	tr := traceOf(t, body)
	t.Logf("trace: %s", tr.outcomes())
	if tr.RequestedModel != "coding" || tr.RouteReason != "model-group" || tr.SelectedModel != "openrouter/c" || tr.SelectedProvider != "openrouter" {
		t.Fatalf("route summary: %+v", tr)
	}
	if strings.Join(tr.Candidates, ",") != "zen/a,gemini/d,nvidia/b,openrouter/c" {
		t.Fatalf("candidates must keep config order: %v", tr.Candidates)
	}
	want := "zen/a=failed/rate_limit/over=true gemini/d=skipped/unknown nvidia/b=failed/upstream/over=true openrouter/c=succeeded"
	if tr.outcomes() != want {
		t.Fatalf("attempts:\n got %s\nwant %s", tr.outcomes(), want)
	}
	at := tr.Attempts
	if at[0].StatusCode != 429 || !strings.Contains(at[0].Reason, "slow down") {
		t.Fatalf("rate-limit reason lost: %+v", at[0])
	}
	if !strings.HasPrefix(at[1].Reason, "missing_api_key") {
		t.Fatalf("skip reason: %q", at[1].Reason)
	}
	if at[2].StatusCode != 503 || at[2].UpstreamModel != "vendor/b" || !strings.Contains(at[2].Reason, "overloaded") {
		t.Fatalf("upstream failure detail: %+v", at[2])
	}
	for i, x := range at {
		if x.Index != i+1 {
			t.Fatalf("attempt index %d = %d", i, x.Index)
		}
	}
}

func TestHoardReasonNeverEmptyForOpaqueUpstreamErrors(t *testing.T) {
	a := upstreamJSON(503, `<html>Service Unavailable</html>`)
	defer a.Close()
	b := upstreamJSON(500, `{}`)
	defer b.Close()
	c := upstreamJSON(200, successBody("resp_c", "c", "ok"))
	defer c.Close()
	srv := hoardServer(t, a.URL+"/v1", b.URL+"/v1", c.URL+"/v1")
	_, body := post(t, srv, "/hoard/v1/responses", `{"model":"coding","input":"hi"}`)
	tr := traceOf(t, body)
	for _, at := range tr.Attempts {
		if at.Outcome == "failed" && strings.Trim(at.Reason, ": ") == "" {
			t.Fatalf("empty reason for %s: %q", at.Model, at.Reason)
		}
	}
	if !strings.Contains(tr.Attempts[0].Reason, "503") {
		t.Fatalf("status in fallback reason: %q", tr.Attempts[0].Reason)
	}
}

func TestHoardClientErrorStopsRoutingAndSaysSo(t *testing.T) {
	a := upstreamJSON(400, providerErr("invalid_request_error", "invalid_value", "input must not be empty"))
	defer a.Close()
	never := upstreamJSON(200, successBody("resp_x", "x", "should not be called"))
	defer never.Close()
	srv := hoardServer(t, a.URL+"/v1", never.URL+"/v1", never.URL+"/v1")

	st, body := post(t, srv, "/hoard/v1/responses", `{"model":"coding","input":""}`)
	if st != 400 {
		t.Fatalf("status=%d body=%s", st, body)
	}
	var env struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	_ = json.Unmarshal(body, &env)
	if env.Error.Code != "invalid_request" {
		t.Fatalf("OpenAI error envelope kept: %s", body)
	}
	tr := traceOf(t, body)
	if tr.outcomes() != "zen/a=failed/client/over=false" || tr.SelectedModel != "" {
		t.Fatalf("client error must stop routing, got %s", tr.outcomes())
	}
	if !strings.Contains(tr.Attempts[0].Reason, "must not be empty") {
		t.Fatalf("reason: %q", tr.Attempts[0].Reason)
	}
}

func TestHoardAllFailedAndCredentialScrubbed(t *testing.T) {
	// Upstreams echo the key they received (some providers do in auth errors).
	echo := func(status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, providerErr("server_error", "bad_key", "bad key "+key+" rejected"))
		}))
	}
	a, b, c := echo(500), echo(401), echo(502)
	defer a.Close()
	defer b.Close()
	defer c.Close()
	srv := hoardServer(t, a.URL+"/v1", b.URL+"/v1", c.URL+"/v1")

	st, body := post(t, srv, "/hoard/v1/responses", `{"model":"coding","input":"hi"}`)
	if st != 502 {
		t.Fatalf("status=%d body=%s", st, body)
	}
	for _, k := range []string{hoardKeyZen, hoardKeyNv, hoardKeyOR} {
		if strings.Contains(string(body), k) {
			t.Fatalf("provider credential leaked to client: %s", body)
		}
	}
	tr := traceOf(t, body)
	want := "zen/a=failed/upstream/over=true gemini/d=skipped/unknown nvidia/b=failed/auth/over=true openrouter/c=failed/upstream/over=false"
	if tr.outcomes() != want {
		t.Fatalf("attempts:\n got %s\nwant %s", tr.outcomes(), want)
	}
	if !strings.Contains(tr.Attempts[0].Reason, "[redacted]") || tr.Attempts[2].StatusCode != 401 {
		t.Fatalf("reasons should be kept but scrubbed: %+v", tr.Attempts)
	}
}

func TestHoardUnknownModelAndPlainEndpointUnchanged(t *testing.T) {
	ok := upstreamJSON(200, successBody("resp_1", "a", "hi"))
	defer ok.Close()
	srv := hoardServer(t, ok.URL+"/v1", ok.URL+"/v1", ok.URL+"/v1")

	// Unknown models fall back to the default group (docs/routing.md); the trace says so.
	st, body := post(t, srv, "/hoard/v1/responses", `{"model":"nope/missing","input":"hi"}`)
	tr := traceOf(t, body)
	if st != 200 || tr.RequestedModel != "nope/missing" || tr.RouteReason != "fallback-default-group" {
		t.Fatalf("fallback trace: %d %+v", st, tr)
	}
	st, body = post(t, srv, "/hoard/v1/responses", `{"input":"hi"}`)
	if st != 400 || !strings.Contains(string(body), `"sleepyrouter"`) {
		t.Fatalf("missing model: %d %s", st, body)
	}

	st, body = post(t, srv, "/v1/responses", `{"model":"coding","input":"hi"}`)
	if st != 200 || strings.Contains(string(body), "sleepyrouter") {
		t.Fatalf("/v1/responses must not change: %d %s", st, body)
	}
}

// --- streaming -------------------------------------------------------------

func sseUpstream(frames [][2]string, cut bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, f := range frames {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", f[0], f[1])
			fl.Flush()
		}
		if cut {
			// Abort mid-stream: no terminal event.
			hj, ok := w.(http.Hijacker)
			if ok {
				if conn, _, err := hj.Hijack(); err == nil {
					_ = conn.Close()
				}
			}
		}
	}))
}

var okFrames = [][2]string{
	{"response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"resp_s","object":"response","model":"c","status":"in_progress","output":[]}}`},
	{"response.output_text.delta", `{"type":"response.output_text.delta","sequence_number":1,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"hello"}`},
	{"response.completed", `{"type":"response.completed","sequence_number":2,"response":{"id":"resp_s","object":"response","model":"c","status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`},
}

type sseEvent struct{ name, data string }

func readSSE(t *testing.T, r io.Reader) []sseEvent {
	t.Helper()
	var out []sseEvent
	var cur sseEvent
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			cur.data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if cur.name != "" || cur.data != "" {
				out = append(out, cur)
			}
			cur = sseEvent{}
		}
	}
	return out
}

func streamThroughGateway(t *testing.T, srv *server.Server, path string) (int, string, []sseEvent) {
	t.Helper()
	gw := httptest.NewServer(srv.Handler())
	defer gw.Close()
	resp, err := http.Post(gw.URL+path, "application/json", strings.NewReader(`{"model":"coding","input":"hi","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), nil
	}
	return resp.StatusCode, "", readSSE(t, resp.Body)
}

func TestHoardStreamAnnouncesRouteBeforeFirstEvent(t *testing.T) {
	a := upstreamJSON(500, errBody("internal", "boom"))
	defer a.Close()
	b := sseUpstream([][2]string{{"response.created", `{"type":"response.created","sequence_number":0,"response":{"id":"r"}}`}, {"response.failed", `{"type":"response.failed","sequence_number":1,"response":{"id":"r","status":"failed"}}`}}, false)
	defer b.Close()
	c := sseUpstream(okFrames, false)
	defer c.Close()
	srv := hoardServer(t, a.URL+"/v1", b.URL+"/v1", c.URL+"/v1")

	st, raw, evs := streamThroughGateway(t, srv, "/hoard/v1/responses")
	if st != 200 || len(evs) == 0 {
		t.Fatalf("status=%d body=%s", st, raw)
	}
	if evs[0].name != "sleepyrouter.routing" {
		t.Fatalf("first event must be the route, got %q", evs[0].name)
	}
	tr := traceOf(t, []byte(evs[0].data))
	want := "zen/a=failed/upstream/over=true gemini/d=skipped/unknown nvidia/b=failed/upstream/over=true openrouter/c=streaming"
	if tr.outcomes() != want {
		t.Fatalf("stream trace:\n got %s\nwant %s", tr.outcomes(), want)
	}
	if !strings.Contains(tr.Attempts[2].Reason, "failed before output") {
		t.Fatalf("pre-commit failure reason: %q", tr.Attempts[2].Reason)
	}
	var names []string
	for _, e := range evs[1:] {
		names = append(names, e.name)
	}
	if strings.Join(names, ",") != "response.created,response.output_text.delta,response.completed" {
		t.Fatalf("upstream events must follow unchanged and in order: %v", names)
	}
	if !strings.Contains(evs[3].data, `"model":"coding"`) {
		t.Fatalf("model rewrite kept: %s", evs[3].data)
	}
}

func TestHoardStreamBrokenAfterCommitReportsSelectedFailure(t *testing.T) {
	a := sseUpstream(okFrames[:2], true)
	defer a.Close()
	srv := hoardServer(t, a.URL+"/v1", a.URL+"/v1", a.URL+"/v1")

	_, raw, evs := streamThroughGateway(t, srv, "/hoard/v1/responses")
	if len(evs) < 3 {
		t.Fatalf("events=%v raw=%s", evs, raw)
	}
	last := evs[len(evs)-1]
	if last.name != "error" {
		t.Fatalf("broken stream must end with error, got %v", evs)
	}
	var env struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal([]byte(last.data), &env)
	if env.Type != "error" {
		t.Fatalf("error event shape: %s", last.data)
	}
	tr := traceOf(t, []byte(last.data))
	if tr.outcomes() != "zen/a=failed/upstream_eof/over=false" && tr.outcomes() != "zen/a=failed/upstream/over=false" {
		t.Fatalf("committed failure must be on the selected candidate, no failover: %s", tr.outcomes())
	}
}

func TestHoardStreamAllFailedBeforeCommitIsJSONWithTrace(t *testing.T) {
	a := upstreamJSON(500, errBody("internal", "boom"))
	defer a.Close()
	srv := hoardServer(t, a.URL+"/v1", a.URL+"/v1", a.URL+"/v1")
	st, raw, _ := streamThroughGateway(t, srv, "/hoard/v1/responses")
	if st != 502 {
		t.Fatalf("status=%d %s", st, raw)
	}
	tr := traceOf(t, []byte(raw))
	if len(tr.Attempts) != 4 || tr.Attempts[3].Outcome != "failed" || *tr.Attempts[3].FailedOver {
		t.Fatalf("trace: %s", tr.outcomes())
	}
}

func TestHoardPlainStreamHasNoRoutingEvent(t *testing.T) {
	c := sseUpstream(okFrames, false)
	defer c.Close()
	srv := hoardServer(t, c.URL+"/v1", c.URL+"/v1", c.URL+"/v1")
	_, _, evs := streamThroughGateway(t, srv, "/v1/responses")
	for _, e := range evs {
		if e.name == "sleepyrouter.routing" || strings.Contains(e.data, "sleepyrouter") {
			t.Fatalf("/v1/responses stream changed: %v", evs)
		}
	}
}

// The official OpenAI Go client, pointed at the Hoard endpoint, must still read
// both the JSON and the streamed response (the extra field/event are tolerated).
func TestHoardOfficialClientCompatibility(t *testing.T) {
	a := upstreamJSON(429, providerErr("rate_limit_error", "rate_limited", "slow down"))
	defer a.Close()
	stream := sseUpstream(okFrames, false)
	defer stream.Close()
	json200 := upstreamJSON(200, successBody("resp_j", "c", "json ok"))
	defer json200.Close()

	for _, tc := range []struct {
		name   string
		baseC  string
		stream bool
	}{{"json", json200.URL + "/v1", false}, {"stream", stream.URL + "/v1", true}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := hoardServer(t, a.URL+"/v1", a.URL+"/v1", tc.baseC)
			gw := httptest.NewServer(srv.Handler())
			defer gw.Close()
			client := openaisdk.NewClient(openaioption.WithBaseURL(gw.URL+"/hoard/v1"), openaioption.WithAPIKey("unused"), openaioption.WithMaxRetries(0))
			params := responses.ResponseNewParams{Model: "coding", Input: responses.ResponseNewParamsInputUnion{OfString: openaisdk.String("hi")}}
			if !tc.stream {
				resp, err := client.Responses.New(context.Background(), params)
				if err != nil || resp.OutputText() != "json ok" {
					t.Fatalf("SDK json: %v %+v", err, resp)
				}
				if !strings.Contains(resp.RawJSON(), `"sleepyrouter"`) {
					t.Fatalf("trace not reachable through SDK RawJSON")
				}
				return
			}
			s := client.Responses.NewStreaming(context.Background(), params)
			var text string
			var sawRouting, completed bool
			for s.Next() {
				ev := s.Current()
				switch ev.Type {
				case "sleepyrouter.routing":
					sawRouting = strings.Contains(ev.RawJSON(), `"attempts"`)
				case "response.output_text.delta":
					text += ev.Delta
				case "response.completed":
					completed = true
				}
			}
			if err := s.Err(); err != nil {
				t.Fatalf("SDK stream error: %v", err)
			}
			if text != "hello" || !completed || !sawRouting {
				t.Fatalf("text=%q completed=%v routing=%v", text, completed, sawRouting)
			}
		})
	}
}

// Streaming upstream HTTP errors surface lazily (first Next()), not from DoStream.
// They must be classified like non-stream errors: status, class, a message without
// the internal upstream URL, and client errors must stop routing.
func TestHoardStreamUpstreamHTTPErrorsAreClassified(t *testing.T) {
	a := upstreamJSON(429, providerErr("rate_limit_error", "rate_limited", "slow down"))
	defer a.Close()
	b := upstreamJSON(401, providerErr("authentication_error", "invalid_api_key", "bad key"))
	defer b.Close()
	c := sseUpstream(okFrames, false)
	defer c.Close()
	srv := hoardServer(t, a.URL+"/v1", b.URL+"/v1", c.URL+"/v1")

	_, raw, evs := streamThroughGateway(t, srv, "/hoard/v1/responses")
	if len(evs) == 0 {
		t.Fatalf("no stream: %s", raw)
	}
	tr := traceOf(t, []byte(evs[0].data))
	want := "zen/a=failed/rate_limit/over=true gemini/d=skipped/unknown nvidia/b=failed/auth/over=true openrouter/c=streaming"
	if tr.outcomes() != want {
		t.Fatalf("stream classification:\n got %s\nwant %s", tr.outcomes(), want)
	}
	if tr.Attempts[0].StatusCode != 429 || tr.Attempts[2].StatusCode != 401 {
		t.Fatalf("status codes lost on the stream path: %+v", tr.Attempts)
	}
	for _, at := range tr.Attempts {
		if strings.Contains(at.Reason, "http://") {
			t.Fatalf("internal upstream URL leaked into reason: %q", at.Reason)
		}
	}
	if !strings.Contains(tr.Attempts[0].Reason, "slow down") {
		t.Fatalf("reason: %q", tr.Attempts[0].Reason)
	}
}

func TestHoardStreamClientErrorStopsRouting(t *testing.T) {
	a := upstreamJSON(400, providerErr("invalid_request_error", "invalid_value", "input must not be empty"))
	defer a.Close()
	var hits int32
	never := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(500)
	}))
	defer never.Close()
	srv := hoardServer(t, a.URL+"/v1", never.URL+"/v1", never.URL+"/v1")
	st, raw, _ := streamThroughGateway(t, srv, "/hoard/v1/responses")
	if st != 400 {
		t.Fatalf("stream client error must return 400 like non-stream, got %d %s", st, raw)
	}
	if hits != 0 {
		t.Fatalf("client error must not fail over (other candidates hit %d times)", hits)
	}
	tr := traceOf(t, []byte(raw))
	if tr.outcomes() != "zen/a=failed/client/over=false" {
		t.Fatalf("trace: %s", tr.outcomes())
	}
}

// Regression (shared stream path, not Hoard-specific): a streaming client error
// used to fail over across every candidate and end as 502 on /v1/responses and
// /v1/messages, while the non-stream path correctly returned 400 at once.
func TestStreamClientErrorReturns400OnPlainEndpoints(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"/v1/responses", `{"model":"coding","input":"hi","stream":true}`},
		{"/v1/messages", `{"model":"coding","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"hi"}]}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			a := upstreamJSON(400, providerErr("invalid_request_error", "invalid_value", "bad input"))
			defer a.Close()
			var hits int32
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; w.WriteHeader(500) }))
			defer other.Close()
			srv := hoardServer(t, a.URL+"/v1", other.URL+"/v1", other.URL+"/v1")
			gw := httptest.NewServer(srv.Handler())
			defer gw.Close()
			resp, err := http.Post(gw.URL+tc.path, "application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 400 || hits != 0 {
				t.Fatalf("status=%d otherHits=%d body=%s", resp.StatusCode, hits, b)
			}
			if strings.Contains(string(b), "sleepyrouter") {
				t.Fatalf("plain endpoint must not carry the trace: %s", b)
			}
		})
	}
}

// The Responses API accepts message items without "type" (EasyInputMessage:
// {"role","content"}, content as string or parts). The typed SDK decoder silently
// drops the WHOLE input array when an item lacks "type", so the model would
// answer an empty conversation. Every endpoint must forward the conversation.
func TestUntypedInputMessagesReachTheUpstream(t *testing.T) {
	var got []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = append(got, string(b))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, successBody("resp_1", "a", "ok"))
	}))
	defer up.Close()
	srv := hoardServer(t, up.URL+"/v1", up.URL+"/v1", up.URL+"/v1")
	body := `{"model":"zen/a","input":[` +
		`{"role":"system","content":"be brief"},` +
		`{"role":"user","content":[{"type":"input_text","text":"first question"},{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]},` +
		`{"role":"assistant","content":[{"type":"output_text","text":"first answer"}]},` +
		`{"role":"user","content":"second question"},` +
		`{"type":"function_call_output","call_id":"call_1","output":"tool result"}]}`
	for _, path := range []string{"/v1/responses", "/hoard/v1/responses"} {
		got = nil
		st, resp := post(t, srv, path, body)
		if st != 200 || len(got) != 1 {
			t.Fatalf("%s: status=%d upstream calls=%d %s", path, st, len(got), resp)
		}
		for _, want := range []string{"be brief", "first question", "data:image/png;base64,AAAA", "first answer", "second question", `"call_id":"call_1"`} {
			if !strings.Contains(got[0], want) {
				t.Fatalf("%s: upstream lost %q:\n%s", path, want, got[0])
			}
		}
	}
}
