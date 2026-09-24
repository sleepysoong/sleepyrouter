# Claude Code compatibility notes

Checked against the official Claude Code gateway and Messages API documentation on
2026-09-24. This gateway accepts Anthropic Messages requests at the edge but
normalizes them into a Responses-shaped internal request, then converts them to the
configured provider's Responses or Chat Completions wire format. It is not an
Anthropic-compatible upstream proxy: Anthropic-only headers, beta features, cache
controls, and server tools cannot be forwarded transparently across that protocol
boundary.

Providers whose hosted API exposes Chat Completions can opt into a provider-level
`wire_api = "chat_completions"` bridge. The bridge reconstructs Responses-style SSE
from typed Chat Completions chunks; text and function-tool streaming are covered by
tests, but this is not byte- or feature-identical to a native Responses or Anthropic
upstream. NVIDIA's hosted DeepSeek-V4.1-Flash endpoint currently documents
`POST /v1/chat/completions`, not `/v1/responses` ([NVIDIA endpoint reference](https://docs.api.nvidia.com/nim/reference/nvidia-deepseek-v4_1-flash-infer)).

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
| `output_config.effort` | Responses `reasoning.effort` | Forwarded to Responses upstreams. For NVIDIA DeepSeek V4.1 Flash Chat Completions, the bridge converts categories to its documented numeric scale (`minimal=1`, `low=25`, `medium=50`, `high=75`, `xhigh=90`, `max=100`). These are explicit bridge policy values, not equivalent provider semantics. |
| Per-message `output_config` (beta) | Rejected | Responses has no general equivalent for changing Anthropic effort mid-history; silently dropping it would change the requested turn behavior. |
| `output_config.format` JSON schema | Responses strict `text.format` JSON schema | Generated format name is `anthropic_output`; unsupported format shapes fail validation. |
| `thinking.type=adaptive` | Mapped to Responses `reasoning.effort=medium` unless `output_config.effort` is supplied | Responses has no adaptive mode; medium is a bridge fallback, not an equivalent policy. |
| `thinking.type=disabled` | No upstream `reasoning` override | It does not disable a `reasoning_effort` default configured on the selected model. |
| `thinking.type=enabled` with token budget | Mapped to categorical Responses reasoning effort | `budget_tokens <=4096` → `low`, `<=8192` → `medium`, `<=32768` → `high`, otherwise `xhigh`; explicit `output_config.effort` overrides this fallback. The configured budget must be at least 1024 and less than `max_tokens`. This is not a token-for-token budget mapping. |
| OpenAI usage `cached_tokens` / `cache_write_tokens` | Stored as cache-read/cache-write counters | Counters appear only if the selected provider reports them. `input_tokens` remains the total input count in local usage storage. |

The gateway rejects known Anthropic-only request features when their semantics
would otherwise be silently lost: `context_management`, Anthropic `service_tier`
and `inference_geo`, `stop_sequences`, `top_k`, `container`, server-side
`mcp_servers`, top-level `cache_control`, tools with `defer_loading=true`, `tool_reference` blocks,
unsupported output formats, `output_config.task_budget`, message-level
`output_config`. Thinking is not rejected, but its Anthropic budget, returned
thinking blocks, and signatures cannot be preserved as OpenAI Responses reasoning.
Request metadata accepts only `user_id`, which is mapped to Responses metadata. Anthropic
version/beta request headers are ignored rather than forwarded to the Responses
upstream. Nested `cache_control` markers are accepted but dropped during conversion.
Some unknown JSON fields are ignored by the Go request structs; they are neither
guaranteed to be rejected nor preserved. New request fields may need an explicit
bridge before they are safe to use.

OpenAI-format `/v1/responses` requests also pass through the pinned OpenAI Go SDK's
typed `ResponseNewParams`. Known SDK fields are decoded and encoded again, with the
model rewritten and omitted model defaults applied; fields unknown to that SDK type
can be dropped. This is not a byte-preserving arbitrary-extension proxy.

Prompt-cache markers are a deliberate exception: `cache_control` embedded in
system/message/content blocks is accepted but not converted to an OpenAI explicit
cache breakpoint or TTL. The provider may still apply its own implicit cache to
the converted, stable prefix. The gateway itself does not cache prompts. OpenAI
Responses' SDK-known `prompt_cache_key` is passed to the provider when present, but
OpenAI-compatible providers may implement different caching behavior. Prompt caching
depends on prefix identity and provider/model routing; a cache hit is never guaranteed
by this gateway.

The Chat Completions bridge preserves the ordered prompt text, tool-call IDs, and
reported cached-token counters where present. It does not translate Anthropic
`cache_control` markers into provider-specific cache controls, and the hosted NIM
endpoint's cache policy and hit rate must be verified from actual usage responses.
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
- `GET /v1/models` exposes configured gateway groups and models for optional model
  discovery. It lists configuration entries even if their key is missing; it does
  not query the provider for availability.
  Claude Code discovery runs only with the Anthropic Messages connection
  (`ANTHROPIC_BASE_URL`) and `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1`, and can
  also be hidden by model-picker settings. Claude Code keeps discovered IDs containing
  `claude` or `anthropic` case-insensitively, so a group or model ID intended for
  the picker should include one of those strings (for example, `sleepy-claude-coding`).
  Discovery uses `GET /v1/models?limit=1000`, defaults to a three-second timeout, and
  treats redirects as failure.
- Context behavior depends on the model ID spelling. `coding` does not
  start with `claude-`, so it is an unrecognized custom spelling and
  `CLAUDE_CODE_MAX_CONTEXT_TOKENS` applies directly (unless `[1m]` changes the rules).
  If the ID resolves to a recognized Claude model, the variable applies only with
  `DISABLE_COMPACT=1`, which disables compaction. Check the current model-config
  documentation for the installed Claude Code version.
- Upstream `retry-after`, `x-should-retry`, and Anthropic rate-limit headers are
  not passed through as a native Anthropic gateway would. Errors are classified,
  normalized, and may trigger pre-commit candidate failover. Upstream error bodies
  are not forwarded unchanged; Claude Code's error-wording-based recovery paths may
  therefore not run. Locally rejected unsupported fields return a gateway-generated
  `400` before any upstream candidate is tried.

- Claude Code's ordinary local MCP tools are client-side custom tools: Claude Code
  starts the MCP servers, sends their tool schemas as `tools`, receives a normal
  `tool_use`, executes it locally, then sends `tool_result` on the next request.
  This bridge preserves that loop, including names and call IDs; the gateway does
  not connect to or execute the MCP servers itself. Anthropic-hosted MCP
  (`mcp_servers`/`mcp_toolset`) and deferred tool search (`defer_loading`/
  `tool_reference`) are not supported.
- No special launcher is required. Claude Code documents tool search as disabled
  by default when using a non-first-party custom `ANTHROPIC_BASE_URL`; this setting
  controls deferred tool search, not thinking or ordinary custom-tool/MCP calls.
  There is no need to set `ENABLE_TOOL_SEARCH=false`; leaving it unset keeps the
  default. Setting it to `true` can send `tool_reference`, which this bridge does
  not support. Eagerly including local MCP tool schemas can enlarge requests and
  may reduce cache-prefix reuse. Anthropic-hosted MCP and deferred tool search
  remain unsupported.

## References and regression inspiration

Official contracts:

- [Claude Code gateway compatibility](https://code.claude.com/docs/en/llm-gateway-protocol)
- [Claude Code model configuration](https://code.claude.com/docs/en/model-config)
- [Claude Code environment variables](https://code.claude.com/docs/en/env-vars)
- [Claude Code MCP servers](https://code.claude.com/docs/en/mcp)
- [Claude Code tool search](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool)
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
