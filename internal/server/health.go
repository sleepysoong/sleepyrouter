package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/buildinfo"
)

var startTime = time.Now()

// Health is always ok when serving (key absence is not fatal).
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	var gen uint64
	if cur := s.deps.Store.Current(); cur != nil {
		gen = cur.Generation
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "service": "sleepyrouter", "version": buildinfo.Version,
		"config_generation": gen, "uptime_seconds": int(time.Since(startTime).Seconds()),
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.deps.Store.Current() == nil {
		http.Error(w, `{"ok":false}`, http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version": buildinfo.Version, "commit": buildinfo.Commit,
		"build_date": buildinfo.BuildDate, "go": buildinfo.GoVersion(),
	})
}
