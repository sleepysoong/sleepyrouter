# Anthropic Messages protocol

`internal/protocol/anthropic` + `internal/server/messages*.go`.

- `Parse`: string shorthand + block list, `model`/`max_tokens` 필수,
  unknown content type은 400 (silent drop 금지).
- `ToResponses`: system → instructions, messages → input items,
  `tool_use` → function_call, `tool_result` → function_call_output
  (ID relation 보존, schema/number는 raw 유지),
  `tool_choice` 매핑 (auto→auto, any→required, tool→function, none→none),
  image → Responses image input, thinking → `Reasoning=true` 요구사항만.
- `ToMessage`: upstream output → `msg_sr_*` + 요청 model echo +
  stop_reason (`tool_use`/`max_tokens`/`end_turn`) + usage (fabrication 금지).
- `StreamEncoder`: stateful. `message_start → content_block_* →
  message_delta → message_stop` 순서 보장. partial JSON은 파싱 시도 안 함.
- `count_tokens`: local conservative estimator (`×1.2`, overcount 선호).
- `GET /v1/models`: group/model/alias superset. `claude-*` alias로 discovery.
