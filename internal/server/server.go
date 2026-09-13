package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/provider"
	"github.com/sleepysoong/sleepyrouter/internal/state"
	"github.com/sleepysoong/sleepyrouter/internal/usage"
)

// Deps wires shared core.
type Deps struct {
	Store    *config.Store
	Usage    *usage.Store
	Affinity *state.Affinity
	Registry *provider.Registry
	Logger   *slog.Logger
	Home     string
}

// Server is the localhost gateway.
type Server struct {
	deps Deps
	mux  *http.ServeMux
}

func New(d Deps) *Server {
	if d.Registry == nil {
		d.Registry = provider.DefaultRegistry()
	}
	if d.Affinity == nil {
		d.Affinity = state.New(nil)
	}
	s := &Server{deps: d, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return chain(s.mux, requestIDMiddleware, recoverMiddleware(s.deps.Logger), logMiddleware(s.deps.Logger))
}

// NewRequestID returns a UUIDv7 (unique across restarts).
func NewRequestID() string {
	if id, err := uuid.NewV7(); err == nil {
		return id.String()
	}
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func withTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, d)
}
