# 문서 인덱스

이 디렉토리는 `sleepyrouter`의 유지보수 경로 맵이에요. 어떤 파일, 연구 노트, 검증이 작업에 적용되는지 결정할 때 여기서 시작하세요.

## 레포 개요

| 질문 | 답변 |
| --- | --- |
| 이 레포는 뭔가요? | 선택된 무료 OpenRouter/NVIDIA 모델로 코딩 에이전트 요청을 라우팅하는 `sleepyrouter`라는 이름의 Go 로컬 프록시예요. |
| 뭘 노출하나요? | 기본 포트 `4567`에서 OpenAI 호환 `/v1`과 Anthropic 호환 `/anthropic` 로컬 서피스를 제공해요. |
| 어떻게 사용하나요? | 전역 설치 후 `OPENROUTER_API_KEY` 또는 `NVIDIA_API_KEY`를 설정하고, `~/.sleepyrouter/config.json`에서 선택된 모델을 구성한 다음 `sleepyrouter start`를 실행하세요. |
| 런타임 동작은 어디에? | `main.go`, `cmd/sleepyrouter/main.go`, `server.go`, `router.go`, `catalog.go` 등에 있어요. |
| 사용자 설명서는 어디에? | 루트 `README.md`에 개요가, `docs/INSTALLATION.md`에 설정과 명령어가 있어요. |

## 경로

| 작업 | 여기서 시작 | 그 다음 확인 |
| --- | --- | --- |
| 프로바이더 지원 | [프로바이더 가이드](provider-guide.md) | [프로바이더 연구](../research/providers.md), `catalog.go`, `openrouter.go`, `nvidia.go`, `copilot.go`, `server.go`, `openrouter_test.go`, `nvidia_test.go`, `catalog_test.go` |
| 라우팅 | [라우팅](latency-routing.md) | `router.go`, `server.go`, `router_test.go` |
| 클라이언트 호환성 | [클라이언트 호환성](client-compatibility.md) | [클라이언트 연구](../research/client-compatibility.md), `server.go`, `translate.go`, `sse.go`, `server_test.go`, `translate_test.go` |
| 제품 동작 | [제품 노트](product.md) | [연구 인덱스](../research/index.md), `main.go`, `server.go`, `start_cmd.go`, `usage_cmd.go` |
| 아키텍처 경계 | [아키텍처](architecture.md) | [의사결정 기록](../research/decisions/README.md), `config.go`, `catalog.go`, `router.go`, `server.go` |

## 유지보수 규칙

- `README.md`는 최상위 why-중심 진입 문서예요. 설정과 CLI 참조는 [INSTALLATION.md](INSTALLATION.md)에 있어요.
- `docs/`의 프로젝트 유지보수 페이지는 간결하고 경로 중심으로 유지하세요.
- `research/`는 경로 페이지에 너무 상세한 재사용 가능한 발견사항과 의사결정 기록을 보관해요.
- `docs/`와 `research/`의 경로/연구 문서는 한국어로 유지하세요.

## 검증

다음을 실행하세요:

```bash
node scripts/check-docs.mjs
```

예상 커버리지: 필수 문서와 연구 파일이 존재하고, 로컬 마크다운 링크가 해석되고, 경로 페이지가 코드와 테스트 앵커를 가리키고, 유지보수 문서가 오래된 또는 origin 중심 표현을 피하고 있어요.

## 업데이트 규칙

최상위 경로, 소스 앵커, 테스트 앵커, 또는 연구 노트가 선호되는 진입점이 되면 이 인덱스를 업데이트하세요. 항목은 짧게 유지하고 상세 내용은 연결된 페이지로 이동시키세요.
