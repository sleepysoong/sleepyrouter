# 아키텍처

이 패키지는 코딩 에이전트가 OpenAI 호환 및 Anthropic 호환 클라이언트를 선택된 무료 모델에 연결할 수 있게 해주는 로컬 Node.js 프록시예요. 이 페이지는 라우팅 맵으로 활용하고, 제품 사용법은 [README.md](../README.md)에, 작업별 경로는 `docs/index.md`에 있어요.

## 런타임 구조

| 영역 | 소스 앵커 | 책임 | 검증 |
| --- | --- | --- | --- |
| CLI 진입점 | [src/cli.ts](../src/cli.ts), `src/commands/*` | `slr` 명령어(start, status, doctor, usage)를 파싱해요. | `test/cli.test.ts`, `test/status.test.ts`, `test/usage.test.ts`, `test/doctor.test.ts` |
| 설정/저장소 | [src/config/store.ts](../src/config/store.ts), [src/config/env.ts](../src/config/env.ts), [src/config/paths.ts](../src/config/paths.ts) | 선택된 모델 ID, 사용량 카운터, API 키 조회를 저장해요. | `test/config.test.ts` |
| 프로바이더 어댑터 | [src/providers/openrouter.ts](../src/providers/openrouter.ts), [src/providers/nvidia.ts](../src/providers/nvidia.ts), [src/providers/catalog.ts](../src/providers/catalog.ts) | 적격 무료 모델을 나열하고 정규화하고, `listAvailableFreeModels`로 집계하고, 프로바이더별 ID를 보존하고, 프로바이더 요청을 전달해요. | `test/openrouter.test.ts`, `test/nvidia.test.ts`, `test/catalog.test.ts` |
| 라우팅 계층 | [src/latency/router.ts](../src/latency/router.ts) | 요청 매칭, 그룹 매칭, 또는 결정론적 설정 순서 기반으로 선택된 모델을 골라요. | `test/router.test.ts` |
| 로컬 서버 | [src/server/create-server.ts](../src/server/create-server.ts), [src/server/translate.ts](../src/server/translate.ts), [src/server/sse.ts](../src/server/sse.ts) | `/v1`과 `/anthropic` 경로를 노출하고, 대체 페이로드를 번역하고, SSE 응답을 스트리밍해요. | `test/server.test.ts`, `test/translate.test.ts` |

## 경계 규칙

- 문서 전용 변경은 명시적으로 구현을 요청하지 않는 한 `src/`의 런타임 동작을 변경하면 안 돼요.
- 프로바이더 작업은 `docs/provider-guide.md`에서 시작하고, `research/providers.md`, `src/providers`, 프로바이더 테스트를 확인해요.
- 라우팅 작업은 `docs/latency-routing.md`에서 시작하고, `src/latency`, 라우팅 테스트를 확인해요.
- 클라이언트 호환성 작업은 `docs/client-compatibility.md`에서 시작하고, `research/client-compatibility.md`, `src/server`, 프로토콜 테스트를 확인해요.

## 신뢰성 및 보안 참고사항

- API 키는 프로바이더별 환경변수 또는 `~/.sleepy-llm-router/.env`에서 읽어요. 문서, 테스트, 프로바이더 오류 처리에서 비밀을 로깅하지 마세요.
- 라우팅은 로컬이고 결정론적이에요: 요청은 설정 파일 순서대로 라우팅되거나, 특정 모델 이름으로 매칭되거나, 명명된 그룹에 속해요.
- 무료 모델 필터링은 안전 경계예요. 새 프로바이더 작업은 `/v1/models`이나 요청 라우팅에 노출하기 전에 무료/텍스트 적격 모델이 어떻게 식별되는지 정의해야 해요.

## 업데이트 규칙

최상위 모듈, 프로토콜 경계, 또는 검증 경로가 추가되면 이 페이지를 업데이트하세요. 상세 내용은 연결된 페이지를 참조하고 중복하지 마세요.
