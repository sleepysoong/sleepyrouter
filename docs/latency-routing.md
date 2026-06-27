# Latency Routing

Use this route for routing logic, candidate ordering, and request fallback behavior.

## Current routing model

- Implementation anchor: [src/latency/router.ts](../src/latency/router.ts).
- `chooseModel` honors a requested model only when it is in the selected list. Server routing normalizes provider upstream IDs to selected local IDs before calling the router.
- `chooseGroupedModel` and server retry ordering recognize group names (configurable in `modelGroups`) plus legacy aliases `haiku`/`sonnet`/`opus`. Non-empty groups route and retry only within that configured group; empty groups fall back to the full selected list.
- Generic or unknown requests (empty string, `auto`, `default`, `slr`, `openrouter/free`) route to the **default group** if configured, otherwise to the first model in the selected list.
- `orderedCandidates` orders retry candidates by status rank (healthy first, other failures last), then by group order or selected order.

## Configurable groups

Groups are defined in the config file (`~/.sleepy-llm-router/config.json`) under `modelGroups`:

```json
{
  "selectedModelIds": ["nvidia/deepseek-r1", "nvidia/qwen-7b", "nvidia/llama-8b", "nvidia/gemma-7b"],
  "modelGroups": {
    "coding": ["nvidia/deepseek-r1", "nvidia/qwen-7b"],
    "chat": ["nvidia/llama-8b", "nvidia/gemma-7b"]
  },
  "defaultGroup": "coding"
}
```

- Each group has an ordered list of model IDs. The router tries them in order.
- When a request specifies a group name as the model (e.g. `"model": "coding"`), the router returns models from that group in config order.
- When a request specifies an unknown model name (not in `selectedModelIds` and not a group name), the router routes to the `defaultGroup` if set.
- If `defaultGroup` is not set, unknown requests fall back to the first group, then to the full selected list.
- Legacy aliases `haiku`→`fast`, `sonnet`→`balanced`, `opus`→`capable` are still supported.
- The `slr/` prefix is stripped before group name matching (e.g. `slr/coding` matches group `coding`).

## Required route for routing work

1. Start at `AGENTS.md`, then `docs/index.md`, then this file.
2. Inspect source anchor: `src/latency/router.ts` for routing and candidate selection behavior.
3. Inspect tests: `test/router.test.ts`.
4. Define verification before implementation: route-choice determinism, tie-breaking, retry ordering, and group routing.

## Contract checks

- Selected model order is a fallback contract; do not replace it with nondeterministic iteration.
- Model group order is also a fallback contract inside each group.
- Request success may update usage counters; failed provider attempts should not be treated as successful.

## Update rule

Update this page when route-choice semantics, candidate ordering, or retry behavior changes.
