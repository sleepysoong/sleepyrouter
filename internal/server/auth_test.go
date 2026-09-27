package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/provider"
	"github.com/sleepysoong/sleepyrouter/internal/server"
	"github.com/sleepysoong/sleepyrouter/internal/state"
)

// Optional inbound auth ([server] auth_token_env). Failure modes: token not
// enforced on some API route, health checks locked out, Claude Code's x-api-key
// rejected, the token leaking into responses, the caller's credential forwarded
// upstream, a missing/empty env var silently disabling auth, and auth being
// forced on configs that never asked for it.

const inboundToken = "sr-local-TOKEN-123456"

func authServer(t *testing.T, upstreamURL string, withAuth bool, dotenv map[string]string) *server.Server {
	t.Helper()
	auth := ""
	if withAuth {
		auth = `auth_token_env = "SR_TEST_TOKEN"`
	}
	toml := `
version = 1
[server]
host = "127.0.0.1"
port = 4567
` + auth + `
[routing]
default_group = "coding"
[providers.zen]
base_url = "` + upstreamURL + `/v1"
api_key_env = "AUTH_ZEN"
[models."zen/a"]
provider = "zen"
upstream_model = "a"
[groups]
coding = ["zen/a"]
`
	cfg, err := config.Parse([]byte(toml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if dotenv == nil {
		dotenv = map[string]string{"AUTH_ZEN": "provider-key", "SR_TEST_TOKEN": inboundToken}
	}
	store, err := config.NewStore(cfg, dotenv, 1)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return server.New(server.Deps{Store: store, Affinity: state.New(nil), Registry: provider.DefaultRegistry()})
}

func call(t *testing.T, srv *server.Server, method, path, body string, hdr map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestInboundAuthToken(t *testing.T) {
	var gotAuth []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, successBody("resp_1", "a", "ok"))
	}))
	defer up.Close()
	srv := authServer(t, up.URL, true, nil)
	resp := `{"model":"coding","input":"hi"}`
	msg := `{"model":"coding","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`

	for _, tc := range []struct {
		name, method, path, body string
		hdr                      map[string]string
		want                     int
	}{
		{"responses no token", "POST", "/v1/responses", resp, nil, 401},
		{"hoard no token", "POST", "/hoard/v1/responses", resp, nil, 401},
		{"messages no token", "POST", "/v1/messages", msg, nil, 401},
		{"models no token", "GET", "/v1/models", "", nil, 401},
		{"count_tokens no token", "POST", "/v1/messages/count_tokens", msg, nil, 401},
		{"wrong bearer", "POST", "/hoard/v1/responses", resp, map[string]string{"Authorization": "Bearer nope"}, 401},
		{"token as bare header value", "POST", "/hoard/v1/responses", resp, map[string]string{"Authorization": inboundToken}, 401},
		{"bearer ok", "POST", "/hoard/v1/responses", resp, map[string]string{"Authorization": "Bearer " + inboundToken}, 200},
		{"bearer case-insensitive scheme", "POST", "/v1/responses", resp, map[string]string{"Authorization": "bearer " + inboundToken}, 200},
		{"claude code x-api-key", "POST", "/v1/messages", msg, map[string]string{"x-api-key": inboundToken}, 200},
		{"models with token", "GET", "/v1/models", "", map[string]string{"Authorization": "Bearer " + inboundToken}, 200},
		{"health stays public", "GET", "/health", "", nil, 200},
		{"version stays public", "GET", "/version", "", nil, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, body := call(t, srv, tc.method, tc.path, tc.body, tc.hdr)
			if st != tc.want {
				t.Fatalf("status=%d want %d body=%s", st, tc.want, body)
			}
			if strings.Contains(body, inboundToken) {
				t.Fatalf("token echoed in response: %s", body)
			}
			if st == 401 {
				if strings.HasPrefix(tc.path, "/v1/messages") {
					if !strings.Contains(body, `"authentication_error"`) {
						t.Fatalf("Anthropic-shaped error expected: %s", body)
					}
				} else if !strings.Contains(body, `"invalid_api_key"`) {
					t.Fatalf("OpenAI-shaped error expected: %s", body)
				}
			}
		})
	}
	for _, a := range gotAuth {
		if strings.Contains(a, inboundToken) || a != "Bearer provider-key" {
			t.Fatalf("upstream must get the provider key, never the caller's token: %q", a)
		}
	}
}

func TestInboundAuthOffByDefault(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, successBody("resp_1", "a", "ok"))
	}))
	defer up.Close()
	srv := authServer(t, up.URL, false, nil)
	if st, body := call(t, srv, "POST", "/hoard/v1/responses", `{"model":"coding","input":"hi"}`, nil); st != 200 {
		t.Fatalf("no auth configured must stay open: %d %s", st, body)
	}
}

// auth_token_env set but the variable missing/empty must NOT mean "open":
// fail closed so a typo in .env doesn't silently expose the gateway.
func TestInboundAuthFailsClosedWhenTokenMissing(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("upstream must not be called")
	}))
	defer up.Close()
	srv := authServer(t, up.URL, true, map[string]string{"AUTH_ZEN": "provider-key"})
	for _, hdr := range []map[string]string{nil, {"Authorization": "Bearer "}, {"Authorization": "Bearer anything"}} {
		if st, body := call(t, srv, "POST", "/hoard/v1/responses", `{"model":"coding","input":"hi"}`, hdr); st != 401 {
			t.Fatalf("missing token must fail closed: %d %s", st, body)
		}
	}
	if st, _ := call(t, srv, "GET", "/health", "", nil); st != 200 {
		t.Fatalf("health must stay public")
	}
}
