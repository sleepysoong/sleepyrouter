# 라우팅

라우팅 로직, 후보 순서 지정, 요청 대체 동작에 이 경로를 사용하세요.

## 라우팅 규칙

요청된 이름에 따라 다음 순서로 처리해요:

1. **등록된 그룹 이름** → 해당 그룹의 모델을 순서대로 시도해요.
2. **그 외 전부** (알 수 없는 이름, 모델 ID, `auto`, 빈 문자열 등) → `defaultGroup`으로 라우팅해요.

```
 요청              → 처리
──────────────────────────────────────
 "A"               → 그룹 A의 모델 순서대로
 "sleepyrouter/A"  → sleepyrouter/ 제거 후 → 그룹 A
 "deepseek-v4-pro" → defaultGroup (모델ID는 매칭 안 됨)
 "auto"            → defaultGroup
 ""                → defaultGroup
```

- `sleepyrouter/` 접두사는 매칭 전에 제거돼요 (예: `sleepyrouter/coding` → 그룹 `coding`).
- 레거시 별칭 `haiku`→`fast`, `sonnet`→`balanced`, `opus`→`capable`은 여전히 지원돼요.
- `defaultGroup`이 없으면 첫 번째 그룹이 기본값이에요.

## 소스 및 연구

- 라우터: [router.go](../router.go)
- 테스트: [router_test.go](../router_test.go)
- 연구: [연구 노트](../research/latency-routing.md)

## 업데이트 규칙

라우팅 우선순위, 후보 정렬 로직, 또는 레거시 별칭 매핑이 변경되면 이 문서와 `research/latency-routing.md`를 업데이트하세요.
