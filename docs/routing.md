# Routing

`internal/routing` + `internal/config`.

- `RouteRequest{RequestedModel, Requirements{Tools,Vision,Reasoning}}`
- `Resolve`: exact group → exact model → alias → unknown policy.
- `FilterCandidates`: disabled model/provider, explicit `unsupported` 제외.
  순서 유지, dedupe 대신 validation에서 duplicate를 에러로.
- `ClassifyStatus` / `ClassifyBody`: 400/422를 unsupported-feature와
  client-error로 분리. string matching은 여기만.
- Determinism: 동일 snapshot+model+capability → 동일 list.

Validation (reload 전 전체, 하나라도 실패하면 reject):

- TOML/duration/port, provider base_url, model provider/upstream_model,
  group 멤버 존재, duplicate, default group 존재, alias target/collision,
  group/model/alias 충돌.
