package upstream

import (
	"encoding/json"
	"strings"

	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
)

// applyModelDefaults fills omitted reasoning options from model config.
// Explicit client values always win.
func applyModelDefaults(params *responses.ResponseNewParams, c routing.Candidate) {
	if c.Model.ReasoningEffort != "" && params.Reasoning.Effort == "" {
		params.Reasoning.Effort = shared.ReasoningEffort(c.Model.ReasoningEffort)
	}
}

// ApplyModelDefaultsForTest is the exported form for protocol packages.
func ApplyModelDefaultsForTest(params *responses.ResponseNewParams, c routing.Candidate) {
	applyModelDefaults(params, c)
}

// DeriveRequirements inspects raw Responses body for capability needs.
func DeriveRequirements(raw []byte) routing.Requirements {
	var v struct {
		Tools     []json.RawMessage `json:"tools"`
		Input     json.RawMessage   `json:"input"`
		Reasoning *struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	var req routing.Requirements
	if err := json.Unmarshal(raw, &v); err != nil {
		return req
	}
	if len(v.Tools) > 0 {
		req.Tools = true
	}
	if v.Reasoning != nil && strings.TrimSpace(v.Reasoning.Effort) != "" {
		req.Reasoning = true
	}
	if len(v.Input) > 0 {
		s := strings.ToLower(string(v.Input))
		if strings.Contains(s, "input_image") || strings.Contains(s, "image_url") {
			req.Vision = true
		}
	}
	return req
}
