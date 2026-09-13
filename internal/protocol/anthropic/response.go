package anthropic

import (
	"encoding/json"

	"github.com/google/uuid"
)

// ToMessage converts a Responses result to an Anthropic message body.
// requestedModel is echoed; actual route goes to headers/usage.
func ToMessage(responsesRaw []byte, requestedModel string) ([]byte, error) {
	var v struct {
		Output []json.RawMessage `json:"output"`
		Usage  *struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	}
	if err := json.Unmarshal(responsesRaw, &v); err != nil {
		return nil, err
	}
	var content []any
	toolCount := 0
	for _, item := range v.Output {
		var probe struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			ID        string `json:"id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(item, &probe); err != nil {
			continue
		}
		switch probe.Type {
		case "message":
			for _, c := range probe.Content {
				if c.Type == "output_text" && c.Text != "" {
					content = append(content, map[string]any{"type": "text", "text": c.Text})
				}
			}
			if probe.Text != "" {
				content = append(content, map[string]any{"type": "text", "text": probe.Text})
			}
		case "function_call":
			id := probe.CallID
			if id == "" {
				id = probe.ID
			}
			if id == "" {
				id = "toolu_" + uuid.NewString()[:8]
			}
			var input any = map[string]any{}
			if probe.Arguments != "" {
				var m any
				if err := json.Unmarshal([]byte(probe.Arguments), &m); err == nil {
					input = m
				} else {
					input = map[string]any{"_raw": probe.Arguments}
				}
			}
			content = append(content, map[string]any{
				"type": "tool_use", "id": id, "name": probe.Name, "input": input,
			})
			toolCount++
		case "reasoning":
			// Do not fabricate thinking blocks (spec). Skip unless summary present.
			var rs struct {
				Summary []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"summary"`
			}
			if err := json.Unmarshal(item, &rs); err == nil {
				for _, s := range rs.Summary {
					if s.Text != "" {
						content = append(content, map[string]any{"type": "thinking", "thinking": s.Text, "signature": ""})
					}
				}
			}
		}
	}
	if content == nil {
		content = []any{}
	}
	stop := "end_turn"
	if toolCount > 0 {
		stop = "tool_use"
	} else if v.IncompleteDetails != nil && v.IncompleteDetails.Reason == "max_output_tokens" {
		stop = "max_tokens"
	}
	var inTok, outTok int64
	if v.Usage != nil {
		inTok = v.Usage.InputTokens
		outTok = v.Usage.OutputTokens
	}
	msg := map[string]any{
		"id":   "msg_sr_" + uuid.NewString(),
		"type": "message", "role": "assistant",
		"content": content, "model": requestedModel,
		"stop_reason": stop, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": inTok, "output_tokens": outTok},
	}
	return json.Marshal(msg)
}
