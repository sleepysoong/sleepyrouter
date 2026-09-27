# OpenAI Responses protocol

`internal/protocol/openai` + `internal/server/responses*.go`.

- `Parse`: raw 보존 + `model/stream/previous_response_id` + requirements 추출.
  `model` 누락 → 400.
- Upstream 호출 전 `model`만 교체 (`sjson`, unknown field 보존).
  같은 단계에서 `type` 없는 message item(`{"role","content"}` — Responses API의
  EasyInputMessage 형식)에 `"type":"message"`를 채운다. 공식 SDK의 typed
  `ResponseNewParams` decoder는 item 하나라도 `type`이 없으면 **input 배열 전체를
  조용히 버려서** 업스트림이 대화 없이 호출됐다(`TestUntypedInputMessagesReachTheUpstream`).
  SDK `ResponseNewParams`의 `apijson` extras도 보존.
- Non-stream: 성공 시 client-facing `model`을 요청값으로 rewrite,
  완료된 응답에 한해 `response_id → provider/model` affinity 저장,
  실패·불완전 응답은 성공으로 기록하지 않음.
- Stream 오류 분류: SDK stream은 지연 연결이라 upstream HTTP 오류(429/401/400…)가
  `DoStream`이 아니라 첫 `Next()` 이후 `Err()`로 나온다. precommit은 이를
  `SDKCaller.NormalizeStreamError`(= non-stream과 같은 `upstream.NormalizeError` + provider hook)로
  분류해 status·class·URL 없는 메시지를 non-stream과 동일하게 만든다. 그래서 stream에서도
  client error(400)는 failover 없이 즉시 400, 401/403은 같은 provider 건너뛰기가 적용된다
  (`/v1/responses`, `/v1/messages`, `/hoard/v1/responses` 공통).
- Stream: precommit 버퍼 (32 events / 64 KiB / 2s) 후 commit. 2s 타이머는 lifecycle 이벤트(created/in_progress/queued)만
  버퍼된 동안에는 commit하지 않는다. commit 전 `response.completed`에 보이는 output이 없으면
  (비었거나 reasoning뿐) `upstream returned empty response`로 failover.
  `response.created`만으로는 commit 안 함.
  commit 전 실패 → failover, commit 후 실패 → 공식 `error` SSE로 종료
  (이어붙이기 금지). SDK의 raw 이벤트 JSON을 보존하고 `response.model`만
  client-facing 모델명으로 rewrite. 완료된 stream ID만 affinity 저장.
- `previous_response_id`:
  affinity hit → 해당 모델만 시도, miss → 첫 candidate에만 전달.
- Provider `wire_api = "chat_completions"`:
  Responses 입력을 typed OpenAI Go SDK `ChatCompletionNewParams`로 변환하고,
  Chat Completion 결과를 Responses JSON/SSE shape로 재구성한다. Text, images,
  function tools, JSON format, token usage를 매핑하지만 provider-specific metadata는
  완전 보존하지 않는다. `previous_response_id`는 stateless Chat Completions endpoint에
  전달할 수 없어 명시적으로 거부한다.
- Error: OpenAI envelope. 400(클라이언트) / 404(unknown+error 정책) /
  502(all failed) / 503(no usable).

## 출력 표면 점검

| 항목 | 상태 | 현재 처리 |
| --- | --- | --- |
| 비스트리밍 Responses JSON | 지원 | 공식 Go SDK의 원본 응답 JSON을 사용하고 최상위 `model`만 가상 모델로 변경. |
| SSE의 알려진/확장 이벤트 | 지원/업스트림 의존 | SDK가 받은 원본 이벤트 JSON을 보존하고 `response.model`이 있을 때만 변경. 업스트림이 생성하지 않는 기능은 제공할 수 없음. |
| 도구 호출·reasoning·이미지/오디오 등 출력 item | 업스트림 의존 | 라우터는 OpenAI 쪽에서는 변환하지 않음. 선택한 Responses 호환 모델이 해당 item을 실제 지원해야 함. |
| `response.completed` / `response.failed` / `response.incomplete` / `error` | 지원 | 원본 터미널 이벤트를 전달하고 완료만 성공으로 기록. 커밋 후 전송 중단에는 `error` SSE를 생성. |
| `previous_response_id` | 부분 지원 | 완료 ID affinity는 프로세스 메모리 기반. 재시작/설정 변경 시 동일 업스트림 상태를 보장하지 않음. |

## Hoard endpoint — `POST /hoard/v1/responses`

`internal/server/hoard.go`. `/v1/responses`와 같은 handler(`handleResponses`)를 요청 context에
`routeTrace`를 실어 호출한다. 추적이 없는 요청(`/v1/responses`)에서는 모든 hook이 no-op이므로
기존 계약은 바뀌지 않는다(`TestHoardPlainStreamHasNoRoutingEvent` 등).

- 추적 내용: 요청 모델, route reason, 설정 순서 그대로의 candidate 목록, 선택된 모델/provider,
  attempt 목록(outcome, error_class, status_code, reason, failed_over, duration_ms, upstream_model).
- JSON(성공·오류): 최상위 `sleepyrouter.routing`을 `sjson`으로 추가. 원본 응답 필드는 그대로.
- Stream: precommit 동안 버퍼링은 동일. **commit 직후, 버퍼링된 첫 upstream 이벤트 앞에**
  `event: sleepyrouter.routing`(`{"type":"sleepyrouter.routing","routing":{...}}`, sequence_number 없음)을
  한 번 쓴다. commit 전에 실패한 후보는 그 이벤트에 `failed`로 나오고, 모든 후보가 commit 전에 실패하면
  기존처럼 JSON 오류(502/503) + 추적이다. commit 후 끊기면 게이트웨이의 `error` 이벤트에 추적을 싣고
  선택된 후보를 `failed`(failed_over=false)로 표시한다 — commit 후 failover 금지 불변식 그대로.
- 비밀 유지: reason은 `AttemptError.SafeMessage`에서 오며, 추가로 현재 snapshot의 provider API key
  문자열을 `[redacted]`로 치환한다. SDK가 오류 본문을 해석하지 못해 빈 메시지(`": : "`)가 되면
  HTTP 상태로 대체한다.
- 한계: 추적은 OpenAI 스키마 밖 확장이다. 공식 OpenAI Go SDK는 알 수 없는 필드/이벤트를 허용하는 것을
  `TestHoardOfficialClientCompatibility`로 확인했지만, 모든 클라이언트를 보장하지 않는다.
