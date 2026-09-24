# Routing

`internal/routing` + `internal/config`.

- `RouteRequest{RequestedModel, Requirements{Tools,Vision,Reasoning}}`
- `Resolve`: exact group → exact model → default group.
- `FilterCandidates`: missing model/provider, explicit `unsupported` 제외.
  순서 유지, dedupe 대신 validation에서 duplicate를 에러로.
- `ClassifyStatus` / `ClassifyBody`: 400/422를 unsupported-feature와
  client-error로 분리. string matching은 여기만.
- Determinism: 동일 snapshot+model+capability → 동일 list.

Validation (reload 전 전체, 하나라도 실패하면 reject):

- TOML/duration/port, provider base_url, model provider/upstream_model,
  group 멤버 존재, duplicate, 필수 default group 존재, group/model 충돌.
- provider는 TOML에 선언된 것만 등록한다. 삭제된 provider를 모델이 참조하면
  validation error다. 알 수 없는 모델명은 항상 기본 그룹으로 간다.
- 예전 `aliases`, `unknown_model_policy`, provider/model `enabled`, body limit,
  shutdown grace 설정 키는 허용하지 않는다. 수신 body limit은 64MiB,
  종료 대기 시간은 10초로 고정된다.
