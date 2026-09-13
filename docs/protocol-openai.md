# OpenAI Responses protocol

`internal/protocol/openai` + `internal/server/responses*.go`.

- `Parse`: raw 보존 + `model/stream/previous_response_id` + requirements 추출.
  `model` 누락 → 400.
- Upstream 호출 전 `model`만 교체 (`sjson`, unknown field 보존).
  SDK `ResponseNewParams`의 `apijson` extras도 보존.
- Non-stream: 성공 시 client-facing `model`을 요청값으로 rewrite,
  `response_id → provider/model` affinity 저장, usage 기록.
- Stream: precommit 버퍼 (32 events / 64 KiB / 2s) 후 commit.
  `response.created`만으로는 commit 안 함.
  commit 전 실패 → failover, commit 후 실패 → 종료만 (이어붙이기 금지).
- `previous_response_id`:
  affinity hit → 해당 모델만 시도, miss → 첫 candidate에만 전달.
- Error: OpenAI envelope. 400(클라이언트) / 404(unknown+error 정책) /
  502(all failed) / 503(no usable).
