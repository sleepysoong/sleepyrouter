package routing

import (
	"strings"
	"time"
)

// ClassifyStatus maps HTTP status to error class for failover decisions.
// Note: 400/422 need body inspection; this is the status-only fallback.
func ClassifyStatus(status int) ErrorClass {
	switch status {
	case 408, 409:
		return ErrorUpstream
	case 429:
		return ErrorRateLimit
	case 500, 502, 503, 504:
		return ErrorUpstream
	case 401, 403:
		return ErrorAuth
	case 404:
		return ErrorModelUnavailable
	case 400, 422:
		return ErrorUnknown // refined by body classifier
	default:
		if status >= 500 {
			return ErrorUpstream
		}
		if status >= 400 {
			return ErrorClient
		}
		return ErrorUnknown
	}
}

// ClassifyBody refines 400/422: unsupported-feature vs client error.
// Isolated string matching lives only here.
func ClassifyBody(status int, errType, code, message string) ErrorClass {
	base := ClassifyStatus(status)
	if status != 400 && status != 422 {
		return base
	}
	hay := strings.ToLower(errType + " " + code + " " + message)
	unsupportedHints := []string{
		"unsupported", "not supported", "does not support", "unsupported parameter",
		"unknown parameter", "invalid tool", "tool_choice", "reasoning", "thinking",
		"response_format", "parallel_tool_calls", "max_tokens", "model_not_found",
		"model not found", "no such model",
	}
	for _, h := range unsupportedHints {
		if strings.Contains(hay, h) {
			return ErrorUnsupportedFeature
		}
	}
	modelHints := []string{"overloaded", "capacity", "unavailable", "try again"}
	for _, h := range modelHints {
		if strings.Contains(hay, h) {
			return ErrorModelUnavailable
		}
	}
	return ErrorClient
}

// ClassifyTimeout helpers.
func ClassifyNetErr(timeout bool) ErrorClass {
	if timeout {
		return ErrorTimeout
	}
	return ErrorNetwork
}

var _ = time.Second
