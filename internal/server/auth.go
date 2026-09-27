package server

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/protocol/anthropic"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/openai"
)

// authMiddleware enforces the optional inbound token ([server] auth_token_env).
//
//   - Accepted: `Authorization: Bearer <token>` (OpenAI clients, Hoard) or
//     `x-api-key: <token>` (Claude Code / Anthropic clients).
//   - Public: /health, /ready, /version (probes must work without secrets).
//   - Fail closed: auth configured but the token env var empty → every API call is
//     rejected, so a typo in .env never silently exposes the gateway.
//   - The caller's credential is only compared, never logged, echoed or forwarded
//     upstream (providers always get their own configured API key).
//
// The snapshot is read per request, so a reload that changes the token applies
// immediately.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := s.deps.Store.Current()
		if snap == nil || !snap.AuthRequired || isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if snap.AuthToken != "" && tokenMatches(presentedToken(r), snap.AuthToken) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="sleepyrouter"`)
		const msg = "missing or invalid sleepyrouter token"
		if strings.HasPrefix(r.URL.Path, "/v1/messages") {
			anthropic.WriteError(w, http.StatusUnauthorized, "authentication_error", msg)
			return
		}
		writeUnauthorized(w, msg)
	})
}

func isPublicPath(p string) bool {
	return p == "/health" || p == "/ready" || p == "/version"
}

func presentedToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
			return strings.TrimSpace(h[7:])
		}
		return ""
	}
	return strings.TrimSpace(r.Header.Get("x-api-key"))
}

func tokenMatches(got, want string) bool {
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	openai.WriteError(w, http.StatusUnauthorized, "invalid_api_key", msg)
}
