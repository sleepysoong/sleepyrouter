# 제품

`sleepy-llm-router`(`slr`)은 코딩 에이전트를 위한 로컬 무료 모델 프록시예요. OpenAI 호환 및 Anthropic 호환 도구에 로컬호스트 엔드포인트를 제공하면서, 설정 파일 순서에 따라 사용자 승인된 무료 모델을 선택해요. 사용자 대상 개요는 [README.md](../README.md)에, 전체 설정은 [INSTALLATION.md](INSTALLATION.md)에 있어요.

## 제공하는 것

- 시작, 상태 확인, 진단, 모델 사용량 카운터 조회를 위한 `slr` CLI.
- `http://localhost:4567/v1`의 OpenAI 호환 경로:
  - `GET /v1/models`
  - `POST /v1/chat/completions`
- `http://localhost:4567/anthropic`의 Anthropic 호환 경로:
  - `POST /anthropic/v1/messages`
  - `POST /anthropic/messages`
- `~/.sleepy-llm-router` 아래의 로컬 선택 상태와 사용량 카운터.

## 제품 불변식

- 설치 시 자동 시작하지 않아요. 사용자가 명시적으로 `slr start`를 실행해야 해요.
- 설정 파일의 `selectedModelIds` 배열에 나열된 모델만 요청 라우팅에 적격이에요.
- 요청이 선택된 모델을 지정하면 프록시가 수용해요. 프로바이더 업스트림 ID도 사용 가능한 경우 선택된 로컬 모델과 매칭돼요. 일반적이거나 알 수 없는 모델 이름은 설정 순서의 첫 번째 모델로 라우팅돼요.
- 모델 그룹은 설정 파일의 `modelGroups`에서 구성할 수 있어요. 각 그룹은 모델 ID의 정렬된 목록을 가져요. 요청이 그룹 이름을 지정하면 라우터는 해당 그룹의 모델을 순서대로 시도해요. `defaultGroup` 설정은 알 수 없는 모델 이름을 대체 그룹으로 라우팅해요.
- 지원되는 프로바이더 어댑터는 무료/텍스트 적격성과 선택된 모델 허용 목록을 보존해야 해요.
- 지원되지 않는 비채팅 엔드포인트와 멀티모달리티는 `0.0.1` 버전에서 범위 외에 있어요.

## 작업별 경로

| 작업 | 여기서 시작 | 그 다음 확인 |
| --- | --- | --- |
| 프로바이더 지원 또는 모델 카탈로그 동작 | `docs/provider-guide.md` | `research/providers.md`, `src/providers`, 프로바이더 테스트 |
| 라우팅 또는 후보 선택 | `docs/latency-routing.md` | `src/latency/router.ts`, `test/router.test.ts` |
| OpenAI/Anthropic 클라이언트 호환성 | `docs/client-compatibility.md` | `research/client-compatibility.md`, `src/server`, 서버 및 번역 테스트 |

## 업데이트 규칙

README 수준의 제품 동작이 변경되면 이 페이지를 업데이트하세요. 사용자 설명서는 README에 두고, 이 페이지는 제품 동작, 불변식, 라우팅에 집중하세요.
