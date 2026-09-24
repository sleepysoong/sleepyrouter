# Anthropic Messages protocol

`internal/protocol/anthropic` + `internal/server/messages*.go`.

- `Parse`: string shorthand + block list, `model`/`max_tokens` 필수,
  unknown content type은 400 (silent drop 금지).
- `ToResponses`: system → instructions, messages → input items,
  `tool_use` → function_call, `tool_result` → function_call_output
  (ID relation 보존, schema/number는 raw 유지),
  `tool_choice` 매핑 (auto→auto, any→required, tool→function, none→none),
  image/document → Responses input content, `output_config.effort/format` 매핑.
  `thinking.type=adaptive` 는 `Reasoning=true` 라우팅 요구에만 사용하고,
  고정 budget thinking은 거부한다.
- `ToMessage`: upstream output → `msg_sr_*` + 요청 model echo +
  stop_reason (`tool_use`/`max_tokens`/`refusal`/`end_turn`) + usage 및 cache counters
  (fabrication 금지). refusal text는 텍스트 block으로 유지한다. OpenAI reasoning
  summary를 서명 없는 Anthropic thinking으로 만들지 않는다.
- `StreamEncoder`: stateful. `message_start → content_block_* →
  message_delta → message_stop` 순서 보장. Responses `item.id`/
  `output_index`로 도구 델타를 구별하고 `call_id`를 클라이언트 도구 ID로 사용.
  partial JSON은 완료 전 파싱하지 않음. 실패는 `event: error`,
  `max_output_tokens`는 `stop_reason=max_tokens`. Messages SSE의 무응답 구간에는
  synthetic `ping`을 보내되 ping으로 stream idle timeout을 연장하지 않는다.
- `count_tokens`: local conservative estimator (`×1.2`, overcount 선호).
- `GET /v1/models`: configured group/model list. Claude Code gateway model discovery 지원.

지원 경계: 일반 텍스트·클라이언트 함수 도구는 변환한다. image/document 입력과
멀티모달 tool_result는 Responses content item으로 변환하며, `is_error`만 텍스트
표식으로 바뀌어 의미가 완전히 같지 않다. 서명된 thinking, Anthropic prompt
caching/cache_control, beta 헤더, Anthropic 전용 server/MCP tools는 동등하게
지원되지 않는다. 의미가 다른 `stop_sequences`, `top_k`, `context_management` 등의
알려진 옵션은 조용히 무시하지 않고 요청을 거부한다. 상세 목록은
[Claude Code 호환성 노트](claude-code-compatibility.md)를 참조.

## 변환 표면 점검

| Anthropic 방향 | 상태 | 현재 처리 |
| --- | --- | --- |
| `messages` 텍스트/system 텍스트 | 지원 | Responses input/instructions로 변환. in-history `system` role도 보존하지만 block boundary/cache marker는 제외. assistant/system 텍스트는 message string으로 replay. |
| 사용자 image/document | 업스트림 의존 | base64/URL image는 `input_image`, 문서는 `input_file`, text document는 `input_text`로 변환. |
| `tool_use` / `tool_result` | 지원/손실 | function call/output에 같은 call ID. `is_error`는 text prefix로만 전달. |
| tool schema·`tool_choice` | 부분 지원 | function schema, strict, parallel disable을 변환. server tools와 deferred tools는 미지원. |
| `thinking` / `redacted_thinking` history | 손실 있는 최선 변환 | 입력 block은 생략. adaptive는 reasoning route 요건만, `output_config.effort`는 upstream setting으로 변환. |
| `output_config.format` | 변환 지원 | JSON schema를 Responses strict structured output으로 매핑. 선택한 provider가 지원해야 함. |
| Anthropic `cache_control`, beta | 미지원 | Anthropic marker/TTL/header 의미는 전달하지 않는다. provider 자체 implicit caching은 별개로 가능. |
| Responses 출력 refusal | 변환 지원 | refusal 문구를 Anthropic text block으로 유지하고 `stop_reason=refusal`을 반환. Chat Completions refusal delta도 Responses refusal 이벤트로 변환. |
| Responses citations/annotations/reasoning/hosted tool output | 손실/미지원 | Anthropic 동등 block이 없는 출력은 생략. |
| Responses 완료 / 토큰 한도 / 거절 / 실패·전송 중단 | 지원 | `end_turn`·`tool_use`·`refusal` / `max_tokens` / `event: error`로 구분. |
| usage / `count_tokens` | 부분 지원 | 실제 응답 usage는 업스트림 값을 사용. 사전 count는 로컬 추정치. |

새 Responses 이벤트나 Anthropic 베타 블록은 자동 변환되지 않는다. 기능이
필요하면 공식 계약과 대상 업스트림의 지원 여부를 확인하고 별도 매핑·테스트를
추가한다.
