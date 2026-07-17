package srv

import "github.com/sleepysoong/sleepyrouter/internal/protocol"

// Re-export the request/response translation helpers so existing callers
// (server.go and the white-box tests) keep compiling after translate.go
// was split out into the protocol package.
//
// ponytail: every file outside package srv now imports protocol.* directly;
// these shims exist only so legacy call sites don't churn.

func ExtractTextContent(content any) string { return protocol.ExtractTextContent(content) }

func AnthropicToOpenAI(body map[string]any, modelID string) map[string]any {
	return protocol.AnthropicToOpenAI(body, modelID)
}

func OpenAIToAnthropic(response map[string]any, fallbackModel string) map[string]any {
	return protocol.OpenAIToAnthropic(response, fallbackModel)
}

func MapStopReason(reason any) string { return protocol.MapStopReason(reason) }
