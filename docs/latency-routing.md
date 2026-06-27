# Latency Routing

Use this route for routing logic, candidate ordering, and request fallback behavior.

## Current routing model

- Implementation anchor: [src/latency/router.ts](../src/latency/router.ts).
- `chooseModel` honors a requested model only when it is in the selected list, even when it is a group alias. Server routing normalizes provider upstream IDs to selected local IDs before calling the router.
- `chooseGroupedModel` and server retry ordering recognize `slr/fast`, `slr/balanced`, `slr/capable`, plus `haiku`, `sonnet`, and `opus` aliases. Non-empty groups route and retry only within that configured group; empty groups fall back to the full selected list.
- Generic or unknown requests choose the first model in the selected list (config-file order).
- `orderedCandidates` orders retry candidates by status rank (healthy first, other failures last), then by selected order.
- No latency probing, background probing, or cooldown mechanisms are used. Routing is purely config-order based.

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
