# 클라이언트 호환성

OpenAI 호환 클라이언트 동작, Anthropic 호환 클라이언트 동작, 응답 번역, 스트리밍 호환성에 이 경로를 사용하세요. 제품 개요는 [README.md](../README.md)에, 전체 설정은 [INSTALLATION.md](INSTALLATION.md)에 있어요.

## 지원되는 로컬 서피스

| 서피스 | 엔드포인트 | 소스 앵커 | 테스트 |
| --- | --- | --- | --- |
| 헬스체크 | `GET /health` | [server.go](../server.go) | `server_test.go` |
| OpenAI 모델 | `GET /v1/models` | [server.go](../server.go), [catalog.go](../catalog.go) | `server_test.go`, `openrouter_test.go`, `nvidia_test.go` |
| OpenAI 채팅 | `POST /v1/chat/completions` | [server.go](../server.go), [sse.go](../sse.go) | `server_test.go` |
| Anthropic 메시지 | `POST /anthropic/v1/messages`, `POST /anthropic/messages` | [server.go](../server.go), [translate.go](../translate.go) | `server_test.go`, `translate_test.go` |
| Anthropic 토큰 카운트 | `POST /anthropic/v1/messages/count_tokens`, `POST /anthropic/messages/count_tokens` | [server.go](../server.go) | `server_test.go` |

## 호환성 모델

- OpenAI 호환 클라이언트는 `url=http://localhost:4567/v1`으로 설정하세요 (일부 클라이언트는 `baseURL` 또는 `base_url`으로 표기해요).
- Anthropic 호환 클라이언트는 `ANTHROPIC_BASE_URL=http://localhost:4567/anthropic`으로 설정하고, 업스트림 인증은 라우팅된 모델에 설정된 프로바이더 키에서 오기 때문에 로컬 플레이스홀더 인증을 사용할 수 있어요.
- 클라이언트는 전체 선택된 풀에서 라우팅하도록 `sleepyrouter`을 요청하거나, `sleepyrouter/fast`, `sleepyrouter/balanced`, `sleepyrouter/capable`로 필터링할 수 있어요. `haiku`, `sonnet`, `opus`는 같은 그룹의 별칭으로 허용돼요. `sleepyrouter`이 반환한 특정 모델 ID도 요청을 고정시킬 수 있어요.
- Anthropic 요청은 프로바이더가 제공하는 Anthropic 호환 엔드포인트가 있으면 먼저 시도하고, 없으면 텍스트와 클라이언트 도구 사용 블록에 대한 Anthropic/OpenAI 번역으로 대체해요.
- Anthropic 토큰 카운팅은 클라이언트 호환성을 위한 로컬 보수적 추정치를 반환해요. 프로바이더 토크나이저 정확도와는 달라요.
- 멀티모달 Anthropic 블록은 프로바이더가 Anthropic 호환 서피스를 노출하면 최선의 노력을 통과시키고, 그렇지 않으면 지원되지 않거나 거부돼요.

## 호환성 작업 필수 경로

1. `AGENTS.md`에서 시작하고, `docs/index.md`, 그리고 이 파일을 읽으세요.
2. `research/client-compatibility.md`에서 클라이언트별 발견사항과 알려진 격차를 읽으세요.
3. `server.go`, `translate.go`, `sse.go`에서 엔드포인트, 번역, SSE 동작을 확인하세요.
4. 테스트: `server_test.go`와 `translate_test.go`를 확인하세요.
5. 요청/응답 형태를 변경할 때 두 프로토콜 서피스 모두 검증하세요.

## 계약 검사

- 소스와 테스트가 구현하지 않는 한 지원되지 않는 비채팅 엔드포인트를 노출하지 마세요.
- 클라이언트 요청을 업스트림으로 전달하기 전에 선택된 모델/무료 모델 필터링을 보존하세요.
- 로컬 전용 인증 의미를 보존하세요: 로컬 클라이언트 헤더는 수용하지만 서버 측에서 구성된 업스트림 프로바이더 키를 사용하세요.
- 선택된 클라이언트 서피스와 호환되는 스트리밍 콘텐츠 타입을 유지하세요.

## 업데이트 규칙

엔드포인트 지원, 번역 동작, 스트리밍 동작, 또는 클라이언트 설정 가이드가 변경되면 이 페이지와 `research/client-compatibility.md`를 업데이트하세요. 클라이언트별 실험은 research/decisions에 보관하세요.
