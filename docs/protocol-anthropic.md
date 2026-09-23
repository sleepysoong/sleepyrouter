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
  OpenAI reasoning summary를 서명 없는 Anthropic thinking으로 만들지 않는다.
- `StreamEncoder`: stateful. `message_start → content_block_* →
  message_delta → message_stop` 순서 보장. Responses `item.id`/
  `output_index`로 도구 델타를 구별하고 `call_id`를 클라이언트 도구 ID로 사용.
  partial JSON은 완료 전 파싱하지 않음. 실패는 `event: error`,
  `max_output_tokens`는 `stop_reason=max_tokens`.
- `count_tokens`: local conservative estimator (`×1.2`, overcount 선호).
- `GET /v1/models`: group/model/alias superset. `claude-*` alias로 discovery.

지원 경계: 일반 텍스트·클라이언트 함수 도구는 변환한다. 이미지 입력은
업스트림 의존. 서명된 thinking, prompt caching/cache_control, Anthropic 전용
서버 도구·베타 의미는 보존되지 않는다. 멀티모달 tool_result는 JSON 문자열로
바뀌며 `stop_sequences`는 현재 업스트림에 전달되지 않는다. 자세한 분류는
README의 “프로토콜 호환성과 한계”를 참조.

## 변환 표면 점검

| Anthropic 방향 | 상태 | 현재 처리 |
| --- | --- | --- |
| `messages` 텍스트/system 텍스트 | 지원 | Responses input/instructions로 변환. system 블록의 비텍스트 메타데이터는 제외. |
| 사용자 이미지 | 업스트림 의존 | base64/URL을 Responses `input_image`로 변환. |
| `tool_use` / 텍스트 `tool_result` | 지원 | Responses `function_call` / `function_call_output`에 동일 call ID 사용. |
| 블록 목록 또는 이미지 포함 `tool_result` | 손실 있는 최선 변환 | 블록을 JSON 문자열 출력으로 전달. 이미지/파일 의미는 보존하지 않음. |
| tool schema·`tool_choice` | 부분 지원 | 함수 도구와 기본 선택 정책을 변환. Anthropic 전용 서버 도구/세부 설정은 보존하지 않음. |
| `thinking` / `redacted_thinking` | 손실 있는 최선 변환 | 입력 블록을 생략하고 reasoning capability만 라우팅에 반영. 출력 reasoning summary도 생략. |
| `stop_sequences`, `cache_control`, beta 확장 | 미지원 | Responses 요청에 동일 의미로 전달하지 않음. |
| Responses 출력 `message.output_text` / `function_call` | 지원 | Anthropic `text` / `tool_use`로 변환. |
| Responses refusal·annotations/citations·reasoning·멀티모달/호스팅 도구 출력 | 손실/미지원 | Anthropic 동등 블록을 합성하지 않고 현재 변환 출력에서는 생략. |
| Responses 완료 / 토큰 한도 / 실패·전송 중단 | 지원 | `end_turn` 또는 `tool_use` / `max_tokens` / `event: error`로 구분. |
| usage / `count_tokens` | 부분 지원 | 실제 응답 usage는 업스트림 값을 사용. 사전 count는 로컬 추정치. |

새 Responses 이벤트나 Anthropic 베타 블록은 자동 변환되지 않는다. 기능이
필요하면 공식 계약과 대상 업스트림의 지원 여부를 확인하고 별도 매핑·테스트를
추가한다.
