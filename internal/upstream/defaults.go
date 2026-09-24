package upstream

import (
	"encoding/json"
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/routing"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ApplyProviderDefaults fills omitted top-level fields from model config on
// the raw body. Explicit client values always win (spec: model defaults only
// for omitted fields). Call after RewriteModel, before SDK unmarshal.
func ApplyProviderDefaults(raw []byte, c routing.Candidate) ([]byte, error) {
	out := raw
	var err error
	for k, v := range c.Model.Extra {
		if k == "" || k == "model" || gjson.GetBytes(out, k).Exists() {
			continue
		}
		out, err = sjson.SetBytes(out, k, v)
		if err != nil {
			return raw, err
		}
	}
	return out, nil
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
