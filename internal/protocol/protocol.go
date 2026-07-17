// Package protocol translates between Anthropic Messages API and OpenAI
// Chat Completions API request/response shapes.
//
// The package is split into two halves:
//
//   - request_anthropic_to_openai.go converts an inbound Anthropic
//     messages request into the form upstream OpenAI-compatible providers
//     can consume, including tool/tool_choice handling and the
//     system/stop fields that have slightly different names.
//   - response_openai_to_anthropic.go converts the upstream OpenAI
//     response back into what Anthropic-compatible callers expect, mapping
//     finish reasons and assembling tool_use blocks.
//
// The package has no router or HTTP concerns; it is pure data transforms
// over generic map[string]any so the upstream mapping can be unit-tested
// without spinning up a server.
package protocol

// Import side-blocks for go vet consistency. Implementation split into
// sibling files. No logic here.
