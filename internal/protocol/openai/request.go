package openai

import (
	"encoding/json"
	"fmt"

	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/sleepysoong/sleepyrouter/internal/upstream"
)

// ParsedRequest preserves raw JSON and extracts routing metadata.
type ParsedRequest struct {
	Raw                []byte
	RequestedModel     string
	Stream             bool
	Requirements       routing.Requirements
	PreviousResponseID string
}

// Parse validates minimal schema and extracts routing fields.
// Missing model -> error (protocol 400).
func Parse(raw []byte) (ParsedRequest, error) {
	var v struct {
		Model              *string         `json:"model"`
		Stream             *bool           `json:"stream"`
		PreviousResponseID *string         `json:"previous_response_id"`
		Tools              json.RawMessage `json:"tools"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return ParsedRequest{}, fmt.Errorf("malformed JSON: %w", err)
	}
	if v.Model == nil || *v.Model == "" {
		return ParsedRequest{}, fmt.Errorf("model is required")
	}
	pr := ParsedRequest{Raw: append([]byte{}, raw...), RequestedModel: *v.Model}
	if v.Stream != nil {
		pr.Stream = *v.Stream
	}
	if v.PreviousResponseID != nil {
		pr.PreviousResponseID = *v.PreviousResponseID
	}
	pr.Requirements = upstream.DeriveRequirements(raw)
	return pr, nil
}
