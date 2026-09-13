package openai

import (
	"github.com/tidwall/sjson"
)

// RewriteResponseModel sets client-facing model to the requested virtual model.
func RewriteResponseModel(raw []byte, requestedModel string) []byte {
	if requestedModel == "" {
		return raw
	}
	out, err := sjson.SetBytes(raw, "model", requestedModel)
	if err != nil {
		return raw
	}
	return out
}

// RewriteStreamEventModel rewrites "model" in a stream event payload if present.
func RewriteStreamEventModel(payload, requestedModel string) string {
	if requestedModel == "" {
		return payload
	}
	out, err := sjson.Set(payload, "model", requestedModel)
	if err != nil {
		return payload
	}
	// sjson.Set on payload without model would add it; avoid that.
	// Check original had model.
	has := false
	for i := 0; i+7 <= len(payload); i++ {
		if payload[i:i+7] == `"model"` {
			has = true
			break
		}
	}
	if !has {
		return payload
	}
	return out
}
