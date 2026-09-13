// Package logging provides structured slog setup with secret redaction.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

var sensitiveKeys = []string{"authorization", "x-api-key", "api-key", "api_key", "apikey", "openrouter_api_key", "nvidia_api_key", "gemini_api_key", "google_api_key", "opencode_api_key"}

// Redact replaces known secret header/value patterns.
func Redact(s string) string {
	lower := strings.ToLower(s)
	for _, k := range sensitiveKeys {
		if strings.Contains(lower, k) {
			return "[redacted]"
		}
	}
	return s
}

// New creates a slog logger. format is "text" or "json".
func New(level, format string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn", "warning":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lv}
	if strings.ToLower(format) == "json" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
