# Claude Code / Codex 출력 API 호환성 개선 계획

## 1. 목표와 결정 사항

이 저장소는 localhost LLM 라우터다. 클라이언트에는 OpenAI Responses
`POST /v1/responses`(Codex)와 Anthropic Messages `POST /v1/messages`(Claude
Code)를 제공하지만, **모든 업스트림 호출은 OpenAI Responses 호환 API**로 보낸다.
이 작업의 목표는 두 클라이언트 출력의 비스트리밍·SSE 계약을 공식 문서와
대조하고, 특히 텍스트·도구 호출·종료·오류의 실제 사용 경로를 안정화하는 것이다.

- 전체 API 표면을 검토한다. 다만 Anthropic 고유 기능을 OpenAI Responses만으로
  동일하게 재현할 수 없으므로 **최선 변환**을 유지한다. 완전한 Anthropic API
  동등성을 주장하지 않으며, 정보 손실과 미지원 범위를 `README.md`에 명시한다.
- 네이티브 Anthropic 업스트림이나 새로운 라우팅 백엔드는 추가하지 않는다.
- 정상 OpenAI Responses 이벤트는 가급적 원형을 보존하고, 필요한 가상 모델명
  변경만 정확한 필드에 적용한다. 기존 라우팅·precommit/failover 정책은
  프로토콜 정합성에 필요한 부분만 바꾼다.
- 프로덕션 변환에는 명시적 타입/상태를 사용한다. 공식 OpenAI Go SDK의
  Responses 이벤트 union을 활용하고, 공식 Anthropic Go SDK는 출력 파싱·
  스트림 누적의 **계약 테스트**에 사용한다. Anthropic 요청용 `Param` 타입을
  서버 응답 타입으로 재사용하지 않는다.

## 2. 현재 구현과 확인된 결함

주요 진입점은 `internal/server/responses*.go`, `internal/server/messages*.go`이며
프로토콜 변환은 `internal/protocol/openai`, `internal/protocol/anthropic`에 있다.
수정 전에 아래 결함을 재현하는 테스트를 먼저 추가한다.

1. `internal/protocol/anthropic/stream.go`: `response.output_item.added`의
   `item.call_id`로 도구 블록을 저장하지만 `response.function_call_arguments.delta`
   의 `item_id`로 조회한다. 공식 이벤트에서는 `call_id=call_…`와
   `item.id=fc_…`가 다르다. 따라서 JSON 인자가 누락될 수 있고, 완료 이벤트는
   임의의 도구 블록을 닫거나 다른 블록의 상태까지 지울 수 있다.
2. Anthropic 스트림의 `response.failed`/`response.incomplete` 처리에는 유효한
   종결 또는 오류 이벤트가 없다. `messages_stream.go`는 전송 오류·idle 종료 후
   SSE 오류를 보내지 않으며 정상 EOF만으로 성공 처리할 수 있다.
3. `internal/protocol/anthropic/response.go`는 OpenAI reasoning 요약을
   `signature:""`인 Anthropic `thinking`으로 만들어 진짜 서명된 thinking인
   것처럼 보이게 한다. 입력 변환은 `thinking`/`redacted_thinking`을 버린다.
4. `internal/protocol/openai/response.go`는 스트림의 `response.model` 대신
   최상위 `model`을 추가한다. `responses_stream.go`의 가짜
   `response.failed`에는 공식 이벤트 필수 필드가 없다. 실패/불완전 종료도
   성공 및 response affinity로 기록될 수 있다.
5. 현재 테스트는 텍스트 스트림과 단순 비스트리밍 도구 출력을 주로 확인해
   실제 도구 인자 델타, 동시 호출, 실패/불완전 종료를 놓친다.

## 3. 구현 순서와 이벤트 계약

### A. 타입과 공통 상태

- OpenAI SDK `ResponseStreamEventUnion`의 이벤트 타입과 해당 필드로
  변환한다. JSON 텍스트에 특정 단어가 포함되는지 검사해 이벤트 종류나
  ID를 판단하지 않는다. SDK가 모르는 확장 이벤트를 전달해야 하는 OpenAI
  경로에서는 raw JSON을 보존한다.
- Anthropic 출력에 `message`, `text`/`tool_use` 콘텐츠, `message_start`,
  `content_block_start|delta|stop`, `message_delta`, `message_stop`, `error`
  각각의 wire DTO를 둔다. 필수 필드와 `type`/SSE `event` 일치를 테스트한다.
- 스트림마다 `started`, 열린 블록, `terminal` 상태를 관리한다. 성공은
  `response.completed` 수신 시에만 확정한다. 터미널 이벤트 중복/누락,
  전송 오류, 클라이언트 취소를 구분한다.

### B. Anthropic `tool_use` 변환 — 최우선

- 시작 이벤트의 `item.id` 및 `output_index` → 도구 상태를 매핑한다.
  `call_id`는 별도 보존하여 클라이언트 `tool_use.id`와 다음 요청의
  `tool_result.tool_use_id`에 사용한다. 위치/ID 없는 델타를 임의의 최신
  도구에 붙이지 않는다.
- `response.function_call_arguments.delta`의 `item_id` 또는
  `output_index`로 해당 블록에만 `input_json_delta.partial_json`을 보낸다.
  델타 문자열은 중간에 JSON 파싱하지 않는다. 복수 호출의 델타가 교차해도
  블록 인덱스가 일정해야 한다.
- `response.function_call_arguments.done`의 최종 `arguments`는 누락된
  인자만 보충하는 데 사용하고, `response.output_item.done`와 중복되어도
  블록을 정확히 한 번만 닫는다. 정상 `response.completed`에서는 최종
  인자가 JSON 객체인지 검증한다. 불일치/무효 인자는 조용히 `{}`로 위장하지
  않고 스트림 오류로 끝낸다. 단 `max_output_tokens`로 끝난 응답은 공식
  Anthropic 동작에 맞춰 잘린 partial JSON을 보존하고 `max_tokens`로 종료한다.
  `response.completed`에서 열린 블록을 정리할 때도 순서와 중복 없는 종료를
  보장한다.

### C. Anthropic 종료·오류 및 비스트리밍

- 정상 `response.completed`: 열린 블록 종료 → 누적 사용량 반영 →
  `message_delta`(정확한 `stop_reason`) → `message_stop`.
- `response.incomplete`에서 `incomplete_details.reason=max_output_tokens`는
  `stop_reason=max_tokens`로 종결한다. 기타 불완전 사유와
  `response.failed`는 정상 `end_turn`으로 위장하지 않고 Anthropic
  `event: error` / `{"type":"error","error":{"type":...,"message":...}}`
  형식으로 알린다. 이미 `message_stop`을 보낸 뒤에는 추가 터미널을 보내지
  않는다. 전송 오류/idle도 커밋 후라면 같은 오류 경로로 처리한다.
- 커밋 전 실패는 기존 failover 정책을 따른다. 커밋 후에는 후보를 바꿔
  출력에 이어 붙이지 않는다. 정상 완료 없이 EOF가 나면 실패로 기록한다.
- 비스트리밍 변환은 응답 `status`와 `incomplete_details`를 확인한다.
  실패를 가짜 `end_turn` 메시지로 만들지 않는다. reasoning 요약은
  서명 없는 `thinking`으로 만들지 않으며, 현재 최선 변환 방침대로 출력에서
  생략하고 README에 손실을 설명한다. 진짜 Anthropic 서명은 생성하지 않는다.

### D. OpenAI Responses 출력

- 비스트리밍 응답의 가상 모델명은 기존처럼 `model`에만 적용한다.
  스트림은 `response.created|in_progress|completed|failed|incomplete` 등
  실제 `response` 객체가 있는 이벤트의 `response.model`에만 적용한다.
  다른 이벤트와 확장 필드는 그대로 유지하고 최상위 `model`을 만들지 않는다.
- 업스트림이 보낸 유효한 `response.failed`/`response.incomplete`/`error`
  이벤트는 전달하되 성공으로 기록하지 않는다. 커밋 후 전송 오류에서는
  필수 `response`·`sequence_number`가 없는 가짜 `response.failed`를
  만들지 않는다. 공식 `error` 이벤트 형식으로 알리고 스트림을 닫는다.
  동일 스트림의 마지막 이벤트와 터미널 상태를 추적해 이중 오류를 막는다.
- 정상 완료된 응답 ID만 affinity에 저장한다. `status=failed`인
  비스트리밍 결과와 빈/중단된 스트림은 성공으로 기록하지 않는다.

### E. 표면 점검과 문서

- `POST /v1/responses`, `POST /v1/messages`, `POST /v1/messages/count_tokens`
  요청·응답·SSE의 주요 공식 필드/이벤트를 `docs/protocol-openai.md` 및
  `docs/protocol-anthropic.md`와 대조한다. 특히 텍스트, 이미지, 도구 결과,
  reasoning/thinking, stop reason, usage, 오류, 확장·베타 필드를
  **지원 / 손실 있는 최선 변환 / 미지원 또는 업스트림 의존**으로 분류한다.
- `README.md`에 별도의 **프로토콜 호환성과 한계** 절을 추가한다. 최소한
  서명된 thinking과 reasoning 요약, 캐시 제어, 멀티모달 도구 결과,
  Anthropic 고유 서버 도구/베타 필드, 확장 이벤트, 로컬 `count_tokens`
  추정치(정확한 Anthropic 토큰 수 아님), 업스트림별 기능 차이를 적는다.
  현재 코드가 실제로 제공하지 않는 기능을 지원한다고 주장하지 않는다.

## 4. 테스트와 완료 판정

1. 공식 예시 형태의 고정 fixture로 텍스트만, 도구 하나, `fc_…`/`call_…`
   구분, 두 도구의 교차 델타, 완료 이벤트에만 있는 인자, 중복 완료,
   `tool_result` 왕복을 검증한다. 출력 블록의 인덱스·ID·최종 JSON을
   검사하며 빈 `{}`로 인자가 바뀌면 실패한다.
2. 정상 완료, `max_output_tokens`, 기타 incomplete, upstream failed,
   커밋 전/후 전송 종료, idle, 취소를 양 프로토콜에서 검사한다.
   성공/실패 usage와 affinity도 HTTP 서버 테스트에서 검증한다.
3. 로컬 모의 업스트림 및 `httptest`로 비스트리밍/SSE의 실제 wire 출력을
   확인한다. 공식 OpenAI Go SDK가 Responses를 읽고, 공식 Anthropic Go
   SDK가 Messages를 파싱하고 스트림을 끝까지 누적할 수 있는 계약 테스트를
   추가한다. Anthropic SDK는 필요한 테스트 의존성으로만 추가한다.
4. `go test ./...`와 `go test -race ./internal/protocol/... ./internal/server/...`
   통과. 실제 Codex·Claude Code 연결은 계정/키가 제공될 때만 수동 점검하며
   자동 완료 조건으로 삼지 않는다.
5. 코드·테스트·README가 같은 호환 범위를 설명하는지 확인한다.
   검증 후 변경사항을 커밋하고 현재 추적 브랜치(`origin/main`)로 푸시한다.

## 5. 공식 레퍼런스

- OpenAI, [Function calling — Responses output items and streamed arguments](https://developers.openai.com/api/docs/guides/function-calling)
- OpenAI, [Streaming API responses](https://developers.openai.com/api/docs/guides/streaming-responses)
- OpenAI, [Responses streaming event schemas](https://developers.openai.com/api/reference/cli/resources/beta/subresources/responses)
- Anthropic, [Streaming messages — event order, tool JSON deltas and errors](https://platform.claude.com/docs/en/build-with-claude/streaming)
- Anthropic, [Create a Message — thinking signature and content types](https://platform.claude.com/docs/en/api/messages/create)
- Anthropic, [Go SDK — typed messages and stream accumulation](https://platform.claude.com/docs/en/cli-sdks-libraries/sdks/go)
