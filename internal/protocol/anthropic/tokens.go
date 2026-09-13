package anthropic

import (
	"encoding/json"
	"math"
)

// TokenCounter estimates tokens for count_tokens (conservative overcount).
type TokenCounter interface {
	CountRequest(raw []byte) (int64, error)
}

// SafetyFactor is configurable/testable (default 1.2).
var SafetyFactor = 1.2

type estimator struct{}

// NewCounter returns the v1 conservative estimator.
func NewCounter() TokenCounter { return estimator{} }

func (estimator) CountRequest(raw []byte) (int64, error) {
	var v struct {
		System   json.RawMessage `json:"system"`
		Messages []Message       `json:"messages"`
		Tools    []Tool          `json:"tools"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, err
	}
	total := 0
	total += estimateRaw(v.System, 8)
	for _, m := range v.Messages {
		total += estimateRaw(m.Content, 8) + 4
		total += len(m.Role) / 4
	}
	for _, t := range v.Tools {
		total += len(t.Name)/4 + 4
		total += len(t.Description)/4 + 4
		total += estimateRaw(t.InputSchema, 16)
	}
	if total < 1 {
		total = 1
	}
	return int64(math.Ceil(float64(total) * SafetyFactor)), nil
}

func estimateRaw(raw json.RawMessage, overhead int) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return len([]rune(s))/4 + overhead/4
	}
	// JSON structure: count runes/4 plus overhead.
	return len([]rune(string(raw)))/4 + overhead
}
