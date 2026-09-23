package anthropic

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// ToMessage converts a Responses result to an Anthropic message body.
// requestedModel is echoed; actual route goes to headers/usage.
func ToMessage(responsesRaw []byte, requestedModel string) ([]byte, error) {
	var v struct {
		Status string            `json:"status"`
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
	if v.Status == "failed" || v.Status == "cancelled" {
		return nil, errors.New("upstream response failed")
	}
	if v.Status == "incomplete" && (v.IncompleteDetails == nil || v.IncompleteDetails.Reason != "max_output_tokens") {
		return nil, errors.New("upstream response incomplete")
	}
	if v.Status != "" && v.Status != "completed" && v.Status != "incomplete" {
		return nil, errors.New("upstream response is not complete")
	}
	var content []wireContentBlock
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
					content = append(content, wireContentBlock{Type: "text", Text: wireString(c.Text)})
				}
			}
			if probe.Text != "" {
				content = append(content, wireContentBlock{Type: "text", Text: wireString(probe.Text)})
			}
		case "function_call":
			id := probe.CallID
			if id == "" {
				id = probe.ID
			}
			if id == "" {
				id = "toolu_" + uuid.NewString()[:8]
			}
			input := json.RawMessage("{}")
			if probe.Arguments != "" {
				var m map[string]json.RawMessage
				if err := json.Unmarshal([]byte(probe.Arguments), &m); err == nil && m != nil {
					input = json.RawMessage(probe.Arguments)
				} else {
					return nil, errors.New("invalid upstream function arguments")
				}
			}
			content = append(content, wireContentBlock{Type: "tool_use", ID: id, Name: probe.Name, Input: input})
			toolCount++
		case "reasoning":
			// An OpenAI summary has no Anthropic thinking signature.
		}
	}
	if content == nil {
		content = []wireContentBlock{}
	}
	stop := "end_turn"
	if v.IncompleteDetails != nil && v.IncompleteDetails.Reason == "max_output_tokens" {
		stop = "max_tokens"
	} else if toolCount > 0 {
		stop = "tool_use"
	}
	var inTok, outTok int64
	if v.Usage != nil {
		inTok = v.Usage.InputTokens
		outTok = v.Usage.OutputTokens
	}
	msg := wireMessage{
		ID: "msg_sr_" + uuid.NewString(), Type: "message", Role: "assistant",
		Content: content, Model: requestedModel, StopReason: wireString(stop),
		Usage: wireUsage{InputTokens: inTok, OutputTokens: outTok},
	}
	return json.Marshal(msg)
}
