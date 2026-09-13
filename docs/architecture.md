# Architecture

                    ┌────────────────────┐
                    │    Shared Core     │
                    │ Router, Failover,  │
                    │ Registry, Config,  │
                    │ Usage, Affinity    │
                    └─────────┬──────────┘
                              │
             ┌────────────────┴────────────────┐
             │                                 │
     OpenAI Pipeline                   Anthropic Pipeline
     POST /v1/responses                POST /v1/messages
     raw preserve + model rewrite      typed parse → Responses →
                                       Anthropic encode

Upstream은 전부 OpenAI-compatible이며 공식 OpenAI Go SDK로 호출한다.
`WithMaxRetries(0)` — retry/failover는 sleepyrouter가 소유.

## Dependency directions (enforced by review)

```
server → protocol → routing → upstream → provider/config
```

금지:

- routing → protocol/openai, protocol/anthropic
- provider → protocol/*
- config → server
- usage → protocol

## Invariants

1. protocol handler가 candidate order를 결정하지 않는다. Router만.
2. router가 wire type을 모른다.
3. provider가 client protocol을 모른다.
4. group array 순서를 바꾸지 않는다.
5. SDK retry = 0.
6. stream commit 후 cross-model failover 금지.
7. invalid hot reload는 active config를 유지.
8. key 없음은 fatal이 아니다.
9. secret을 log하지 않는다.
10. unknown optional field를 삭제하지 않는다.
11. tool call ID relation 유지.
12. previous_response_id를 무작정 이동 금지.
13. client disconnect → upstream cancel.
14. storage failure ≠ inference failure.
