# Claude Code compatibility notes

Checked against the official Claude Code gateway and Messages API documentation on
2026-09-24. This gateway accepts Anthropic Messages requests at the edge but
translates them into OpenAI Responses requests. It is not an Anthropic-compatible
upstream proxy: Anthropic-only headers, beta features, cache controls, and server
tools cannot be forwarded transparently across that protocol boundary.

## Request and response mapping

| Claude Code / Anthropic input | Responses-side behavior | Boundary |
| --- | --- | --- |
| Top-level `system` | Flattened to Responses `instructions` | Text is retained, but block boundaries, attribution position, and `cache_control` markers are not. |
| `messages` text | User text uses Responses `input_text`; assistant/system text is replayed as message text | A message with role `system` inside the history is retained as a Responses system message for Claude Code gateway requests. |
| `tool_use` / `tool_result` | Mapped to `function_call` / `function_call_output` | Call IDs are retained; request validation rejects missing, orphaned, or duplicate tool results. Stateless history is replayed on every Messages request. |
| `tool_result.is_error` | Prepended with `[Tool execution failed]` | Responses function output has no equivalent error bit, so this is a signal, not exact semantics. |
| Tool `strict` and `tool_choice.disable_parallel_tool_use` | Mapped to Responses `strict` and `parallel_tool_calls=false` | Provider/model support and strict JSON Schema subsets vary; incompatible schemas can still be rejected upstream. |
| User `image` blocks | Responses `input_image`, from base64 data or URL | The chosen model/provider must support image input. |
| User `document` blocks | Text becomes `input_text`; URL/base64 becomes `input_file` | Supported file types, URL access, size limits, and vision behavior belong to the upstream provider. |
| `output_config.effort` | Responses `reasoning.effort` | This is a cross-provider best-effort mapping, not Anthropic's identical reasoning implementation. |
| Per-message `output_config` (beta) | Rejected | Responses has no general equivalent for changing Anthropic effort mid-history; silently dropping it would change the requested turn behavior. |
| `output_config.format` JSON schema | Responses strict `text.format` JSON schema | Generated format name is `anthropic_output`; unsupported format shapes fail validation. |
| `thinking.type=adaptive` | Used to require a reasoning-capable route; not sent as an upstream field | If supplied, `output_config.effort` is the only explicit reasoning setting mapped. |
| `thinking.type=enabled` with token budget | Rejected | Responses has no equivalent fixed Anthropic thinking-token budget. The old config `thinking_budget` is also rejected; use `reasoning_effort`. |
| OpenAI usage `cached_tokens` / `cache_write_tokens` | Stored as cache-read/cache-write counters | Counters appear only if the selected provider reports them. `input_tokens` remains the total input count in local usage storage. |

The gateway rejects known Anthropic-only request features when their semantics
would otherwise be silently lost: `context_management`, Anthropic `service_tier`
and `inference_geo`, `stop_sequences`, `top_k`, `container`, `mcp_servers`,
top-level `cache_control`, deferred tools / `tool_reference` blocks, unsupported
output formats, and fixed-budget thinking. Anthropic version/beta request headers
are ignored rather than forwarded to the Responses upstream. New request fields
may need an explicit bridge before they are safe to use.

Prompt-cache markers are a deliberate exception: `cache_control` embedded in
system/message/content blocks is accepted but not converted to an OpenAI explicit
cache breakpoint or TTL. The provider may still apply its own implicit cache to
the converted, stable prefix. OpenAI prompt caching depends on prefix identity and
provider/model routing; a cache hit is never guaranteed by this gateway.
Claude Code's system-attribution block is part of the prompt this non-Anthropic
upstream receives. Claude Code v2.1.181+ keeps it stable for a conversation through
a custom base URL; older versions could vary it per request and reduce prefix-cache
reuse. Prefer upgrading Claude Code; for older clients, review
`CLAUDE_CODE_ATTRIBUTION_HEADER=0` as described in the official gateway guide.

## Streaming and client assumptions

- Claude Code's Anthropic-format gateway path expects streamed bytes without long
  silent gaps. Anthropic SSE receives synthetic `ping` events at up to 15-second
  intervals. They do not reset the gateway's `stream_idle` timeout; its default
  is 330 seconds, slightly longer than Claude Code's documented 300-second byte
  watchdog.
- `HEAD /api/hello` is a best-effort Claude Code connection-warming probe. This
  endpoint is not implemented; official gateway guidance says startup probes may
  be rejected without breaking inference.
- `/v1/messages/count_tokens` exists, but returns a local character-based estimate,
  not Anthropic's tokenizer result.
- `GET /v1/models` exposes gateway groups, models, and aliases for optional model
  discovery. Claude Code currently keeps discovered IDs containing `claude` or
  `anthropic`, so a gateway alias intended for the picker should include one of
  those strings (for example, `sleepy-claude-coding`). Claude Code may assume a
  200K context window for an unknown model ID; set `CLAUDE_CODE_MAX_CONTEXT_TOKENS`
  when the routed model's real window differs.
- Upstream `retry-after`, `x-should-retry`, and Anthropic rate-limit headers are
  not passed through as a native Anthropic gateway would. Errors are classified,
  normalized, and may trigger pre-commit candidate failover.

## References and regression inspiration

Official contracts:

- [Claude Code gateway compatibility](https://code.claude.com/docs/en/llm-gateway-protocol)
- [Claude Code model configuration](https://code.claude.com/docs/en/model-config)
- [Anthropic Messages API](https://platform.claude.com/docs/en/api/messages/create)
- [Anthropic tool calls and `is_error`](https://platform.claude.com/docs/en/agents-and-tools/tool-use/handle-tool-calls)
- [Anthropic streaming events](https://platform.claude.com/docs/en/build-with-claude/streaming)
- [OpenAI Responses prompt caching and usage](https://developers.openai.com/api/docs/guides/prompt-caching)
- [OpenAI Responses file inputs](https://developers.openai.com/api/docs/guides/file-inputs)

Community reports that illustrate gateway failure modes tested or documented here:

- [Claude Code Router: thinking blocks with an OpenAI Responses upstream](https://github.com/musistudio/claude-code-router/issues/1686)
- [Claude Code Router: tool-result/call identity with a stateless Responses upstream](https://github.com/musistudio/claude-code-router/issues/1643)
- [LiteLLM: cache-marker handling regression](https://github.com/BerriAI/litellm/issues/30293)

These issues are examples of interoperability bugs, not protocol specifications.
The official API documents above remain authoritative.
