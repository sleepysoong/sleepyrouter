# OpenAI Responses protocol

`internal/protocol/openai` + `internal/server/responses*.go`.

- `Parse`: raw 보존 + `model/stream/previous_response_id` + requirements 추출.
  `model` 누락 → 400.
- Upstream 호출 전 `model`만 교체 (`sjson`, unknown field 보존).
  SDK `ResponseNewParams`의 `apijson` extras도 보존.
- Non-stream: 성공 시 client-facing `model`을 요청값으로 rewrite,
  완료된 응답에 한해 `response_id → provider/model` affinity 저장,
  실패·불완전 응답은 성공으로 기록하지 않음.
- Stream: precommit 버퍼 (32 events / 64 KiB / 2s) 후 commit.
  `response.created`만으로는 commit 안 함.
  commit 전 실패 → failover, commit 후 실패 → 공식 `error` SSE로 종료
  (이어붙이기 금지). SDK의 raw 이벤트 JSON을 보존하고 `response.model`만
  client-facing 모델명으로 rewrite. 완료된 stream ID만 affinity 저장.
- `previous_response_id`:
  affinity hit → 해당 모델만 시도, miss → 첫 candidate에만 전달.
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
