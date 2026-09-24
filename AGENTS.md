# Contributor and Agent Guide

This file is the working agreement for people and coding agents changing sleepyrouter. Follow it alongside the design and compatibility documents linked below. If implementation and documentation disagree, inspect the code and tests, then update the relevant documentation in the same change; do not silently make assumptions.

## Start here

Before changing behavior, read:

1. `README.md` for supported use, configuration, and known limitations.
2. `PLAN.md` for the agreed direction and scope.
3. `docs/architecture.md` and the relevant protocol document(s): `docs/protocol-openai.md`, `docs/protocol-anthropic.md`, and `docs/claude-code-compatibility.md`.
4. The target package's implementation and tests.

First inspect `git status --short --branch`. Treat every pre-existing modification and untracked file as user-owned. Do not overwrite, discard, stage, or include unrelated work. Stage explicit paths, never `git add .` or `git add -A` in a shared/dirty worktree.

## Project shape

sleepyrouter is a small, single-binary Go gateway. It accepts OpenAI Responses and Anthropic Messages requests, routes them to configured providers through the OpenAI SDK (including an explicitly selected Chat Completions compatibility bridge), and adapts responses back to the caller's protocol. It is not a general-purpose transparent proxy: SDK-typed requests and cross-protocol translation can be lossy. Consult the README before promising support for a field or provider feature.

| Area | Responsibility |
| --- | --- |
| `cmd/sleepyrouter` | CLI entry point and command wiring |
| `internal/server` | HTTP routes, middleware, request lifecycle, orchestration, and stream commitment |
| `internal/protocol/openai` | OpenAI Responses request/response and SSE wire handling |
| `internal/protocol/anthropic` | Anthropic Messages parsing, response adaptation, and SSE encoding |
| `internal/routing` | Deterministic model/group resolution and candidate selection; no wire translation |
| `internal/upstream` | Provider execution through the official OpenAI SDK, upstream streaming, and explicit compatibility bridges |
| `internal/provider` | Provider profiles, capabilities, and provider-specific configuration; no protocol adapters |
| `internal/config` | Parse, validate, load, and publish runtime configuration snapshots |
| `internal/state`, `internal/usage` | Persisted runtime state and usage accounting |
| `internal/logging`, `internal/buildinfo` | Logging and build metadata |
| `docs` | Architecture, protocol contracts, and compatibility boundaries |

Keep dependencies pointed inward by responsibility: the server composes packages; protocol adapters translate wire formats; routing selects candidates; upstream executes requests; provider/config expose data and capabilities. In particular, routing, provider, config, and usage must not depend on protocol adapters, and config must not depend on server. Avoid import cycles and do not move translation into routing.

## Behavioral invariants

- Preserve deterministic routing. Candidate/group order is configuration, not a set: do not sort it. Keep exact group/model matching, aliases, and unknown-model policy aligned with `docs/architecture.md` and tests.
- Retry/failover is owned by the gateway, not hidden SDK retries. Keep SDK retries disabled unless the design is deliberately changed and tested. Before any response bytes are committed, bounded precommit buffering may permit trying another candidate; after streaming is committed, never switch providers or concatenate a second response into the first.
- Propagate request contexts and cancellation through every upstream operation. A disconnected client must not leave an upstream request running unnecessarily.
- Runtime configuration snapshots are immutable after publication. Parse and validate a replacement before atomically publishing it; a failed reload must leave the last valid snapshot active. Missing provider credentials should affect candidate eligibility, not leak into logs or crash unrelated providers.
- Keep secrets out of logs, errors, test output, and committed example configuration. Never forward a caller's authorization header as an upstream provider credential.
- Keep protocol-specific types and semantics at adapter boundaries. Preserve identifiers and relationships (especially tool item IDs, call IDs, and tool results), terminal states, refusal information, usage, and model rewriting where the target protocol has an equivalent.
- Preserve unknown/raw fields where the SDK and adapter architecture allow it, but do not claim transparent pass-through when typed SDK models or translation discard fields. Add or update a documented limitation when loss is unavoidable.
- Streaming is a wire contract, not merely setting `stream: true`: preserve event order, incremental text/tool-argument deltas, terminal events, usage, valid SSE framing, and timely flushes. Buffer only what is necessary before commitment. Test both precommit errors and failures after commitment.
- Do not silently remove or force-disable a caller's thinking/reasoning configuration to make a request appear compatible. Preserve supported effort/settings and model defaults; when a provider or target protocol cannot represent a feature, handle it explicitly and document the exact limitation. Never fabricate provider-signed/private reasoning blocks.
- Treat client-declared tools (including Claude Code tools) separately from provider-hosted/server-side MCP, deferred tool discovery, and vendor-managed tools. Preserve tool schemas, call IDs, partial JSON arguments, tool-use stop reasons, and the tool-result continuation round trip when supported. Do not describe hosted MCP as supported merely because ordinary function tools work.
- OpenAI Responses continuation state such as `previous_response_id` is not automatically portable across providers/models. Respect the documented continuation policy; do not replay opaque state to an unrelated candidate.
- Persistence/accounting failures must not accidentally turn a successful inference into a retry that duplicates generation. Keep observability/state concerns separate from the inference result.

## Go and API implementation style

- Use the Go version and module versions pinned in `go.mod`; use the official OpenAI and Anthropic SDKs where their typed APIs are the intended contract. Avoid introducing a second SDK or a broad dependency for a narrow helper.
- Keep code idiomatic and formatted with `gofmt`. Prefer small functions with explicit inputs and errors over generic frameworks, reflection, or speculative abstractions.
- Thread `context.Context` from the HTTP request to SDK calls, database operations, and any blocking work. Do not store request contexts in long-lived structs.
- Return errors with useful operation context while keeping credentials and sensitive request bodies redacted. Handle cancellation distinctly from provider/protocol errors where it changes retry behavior.
- Make defaults and provider capabilities explicit. Avoid hidden fallback behavior that changes the requested model, effort, tools, or wire API.
- When changing SDK-facing behavior, inspect the pinned SDK types and raw-response facilities. A SDK upgrade requires reviewing serialization/stream semantics and updating contract tests; do not assume newer fields are automatically preserved.
- Keep comments for invariants and non-obvious protocol decisions, not line-by-line narration. Update README/docs/config examples when supported behavior, configuration, or a compatibility boundary changes.

## Tests and verification

Tests use Go's standard `testing` package, table-driven cases, `httptest`, and temporary directories. Prefer deterministic tests with fake HTTP upstreams; never require live provider credentials or spend API credit in the normal test suite. Keep tests near the package they protect and name them `Test...`. Use `t.Helper()` in helpers.

For protocol changes, cover both JSON and streaming paths where applicable. Assert actual wire payloads/event names/order as well as decoded SDK behavior. Include relevant edge cases: tool-call identity and argument deltas, refusals, usage, stop/terminal events, model mapping, cancellation, malformed upstream events, and errors on each side of stream commitment. For configuration/routing changes, test validation, defaults, precedence, stable candidate order, and reload failure behavior.

Run the narrow relevant test first, then the repository checks appropriate to the change:

```sh
make test       # go test ./...
make vet        # go vet ./...
make lint       # gofmt check and go vet
make test-race  # go test -race ./... (especially for concurrency/state/reload changes)
make build      # build the CLI when executable wiring changes
git diff --check
```

Do not claim a check passed unless it was run. If a check cannot run, say why and report what was verified instead.

## Change and handoff discipline

- Keep a change scoped to the request. Do not add unrelated cleanup, config migration, dependency upgrades, or feature work.
- Update tests and user-facing docs with behavior changes; update `config.example.toml` when configuration semantics change. Avoid documenting aspirational support as implemented.
- Review the final diff and staged diff. In a dirty worktree, stage only files belonging to the task and verify no pre-existing changes entered the commit.
- Do not amend existing commits, rewrite history, force-push, or discard work. Commit or push only when explicitly requested by the user; use a normal, non-force push and stop if the remote has diverged.
- In the handoff, summarize behavior changed, tests run, known limitations, and any work intentionally left untouched.
