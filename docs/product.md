# Product

`sleepy-llm-router` (`slr`) is a local free-model proxy for coding agents. It gives OpenAI-compatible and Anthropic-compatible tools a localhost endpoint while selecting among user-approved free models in config-file order. User-facing overview remains in [README.md](../README.md), and full setup lives in [INSTALLATION.md](INSTALLATION.md).

## What it provides

- A CLI named `slr` for starting the local proxy, checking status, running diagnostics, and viewing model usage counters.
- OpenAI-compatible routes under `http://localhost:4567/v1`:
  - `GET /v1/models`
  - `POST /v1/chat/completions`
- Anthropic-compatible routes under `http://localhost:4567/anthropic`:
  - `POST /anthropic/v1/messages`
  - `POST /anthropic/messages`
- Local selection state and usage counters under `~/.sleepy-llm-router`.

## Product invariants

- The package does not auto-start during install; users explicitly run `slr start`.
- Only models listed in the config file's `selectedModelIds` array are eligible for request routing.
- If a request names a selected model, the proxy honors it; provider upstream IDs also match selected local models when available. Generic or unknown model names route to the first model in the config order.
- Model groups are configurable in the config file under `modelGroups`. Each group has an ordered list of model IDs. When a request specifies a group name, the router tries models in that group in order. A `defaultGroup` setting routes unknown model names to a fallback group.
- Supported provider adapters must preserve free/text eligibility and selected-model allowlisting.
- Unsupported modalities and non-chat endpoints remain out of scope for version `0.0.1` unless an implementation task changes the product contract.

## Agent task routes

| Task | Start here | Then inspect |
| --- | --- | --- |
| Provider support or model catalog behavior | `docs/provider-guide.md` | `research/providers.md`, `src/providers`, provider tests |
| Routing or candidate selection | `docs/latency-routing.md` | `src/latency/router.ts`, `test/router.test.ts` |
| OpenAI/Anthropic client compatibility | `docs/client-compatibility.md` | `research/client-compatibility.md`, `src/server`, server and translation tests |

## Update rule

Update this page when README-level product behavior changes. Keep user instructions in README and keep this page focused on product behavior, invariants, and routing.
