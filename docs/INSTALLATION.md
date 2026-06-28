# 설치 및 설정

`sleepy-llm-router` (`slr`) 설치부터 클라이언트 연결까지 순서대로 설명합니다. 프로젝트의 목적과 배경은 [README](./README.md)를 보세요.

## 1. 설치

### npm으로 설치 (권장)

```bash
npm install -g sleepy-llm-router
```

### 로컬 파일로 설치

저장소를 clone한 뒤 로컬에서 직접 설치할 수 있습니다.

```bash
# 1. 저장소 clone
git clone https://github.com/sleepysoong/sleepy-llm-router
cd sleepy-llm-router

# 2. 의존성 설치 및 빌드
npm install
npm run build

# 3-1. npm pack으로 설치 (tarball 생성 후 전역 설치)
npm pack
npm install -g ./sleepy-llm-router-*.tgz

# 3-2. npm link로 설치 (개발용, 소스 변경 시 자동 반영)
npm link
```

설치 후 `slr` 명령어를 사용할 수 있습니다.

```bash
slr --version
```

설치 시 백그라운드 프로세스가 자동으로 뜨지 **않습니다**. 필요할 때 직접 실행하세요.

Node.js 20 이상이 필요합니다.

## 2. Provider API 키 설정

`slr` 은 provider 키를 다음 순서로 읽습니다.

1. 프로세스/전역 환경의 `OPENROUTER_API_KEY` / `NVIDIA_API_KEY`
2. `~/.sleepy-llm-router/.env`

`~/.sleepy-llm-router/.env` 예시는 아래와 같습니다.

```bash
OPENROUTER_API_KEY=sk-or-...
NVIDIA_API_KEY=nvapi-...
```

키가 설정된 provider만 사용됩니다.

## 3. 모델 설정

`slr`은 설정 파일(`~/.sleepy-llm-router/config.json`)의 `selectedModelIds` 배열에 있는 모델만 라우팅 후보로 사용합니다. 수동으로 설정 파일을 편집하거나, 설정 API를 통해 모델 목록을 관리할 수 있습니다.

## 4. 로컬 프록시 실행

```bash
slr start
```

프록시를 foreground로 실행하고 request/response 라우팅 로그를 출력합니다. `Ctrl+C` 로 종료합니다.

기본 포트는 `4567` 입니다. 필요하면 포트를 바꿀 수 있습니다.

```bash
slr start --port 4600    # 프록시를 4600 포트에서 실행
```

## 5. 클라이언트 연결

### OpenAI 호환 클라이언트

OpenCode, Hermes Agent, OpenClaw 등 OpenAI 호환 클라이언트에서는 아래 값을 사용합니다.

```text
baseURL=http://localhost:4567/v1
```

필요한 엔드포인트:

- `GET /v1/models`
- `POST /v1/chat/completions`

### Anthropic 호환 클라이언트 (Claude Code)

아래 환경변수를 설정합니다.

```bash
export ANTHROPIC_BASE_URL=http://localhost:4567/anthropic
export ANTHROPIC_AUTH_TOKEN=slr-local
export ANTHROPIC_API_KEY=
```

필요한 엔드포인트:

- `POST /anthropic/v1/messages`
- `POST /anthropic/messages` (alias)
- `POST /anthropic/v1/messages/count_tokens`
- `POST /anthropic/messages/count_tokens` (alias)

`slr` 은 로컬 Anthropic 인증 헤더를 받아서 선택된 모델에 맞는 provider 키로 요청을 forward합니다. provider가 자체 Anthropic 호환 엔드포인트를 노출하면 (예: OpenRouter의 Anthropic surface) `slr` 은 그쪽을 우선 사용하고, 그렇지 않으면 텍스트와 일반적인 클라이언트 tool-use 흐름을 Anthropic/OpenAI 형태로 번역해 fallback합니다. Token count는 provider tokenizer의 정확한 값이 아니라 로컬 호환성 추정치입니다.

## 6. 진단

| 명령어 | 용도 |
| --- | --- |
| `slr doctor` | config 경로, provider 키 출처, 선택 모델 수, 캐시 모델 수를 출력합니다. |
| `slr usage` | 모델별 요청 수와 가능한 token 합계를 출력합니다. |
| `slr usage --json` | usage 관측치를 JSON으로 출력합니다. |

`doctor` 는 설정을 변경하지 않습니다. Streaming 요청은 `usage` 요청 수에 포함되지만, stream passthrough에서는 보통 token 합계를 얻을 수 없습니다.

## 7. 라우팅 규칙

- 설정 파일의 `selectedModelIds` 배열 순서대로 모델이 라우팅됩니다.
- 요청에 모델 이름이 명시되어 있으면 `slr` 은 그 모델을 그대로 사용합니다. provider prefix가 붙은 로컬 모델 ID는 매칭되는 upstream 모델 ID도 인식합니다.
- 그룹 모델명 (`slr/fast`, `slr/balanced`, `slr/capable`, 그리고 `haiku`/`sonnet`/`opus`) 은 해당 그룹에 선택된 모델이 있으면 그 그룹 안에서만 라우팅합니다. 빈 그룹은 전체 선택 목록으로 fallback합니다.

## 8. 개발

`slr` 자체를 작업할 때는 아래 명령어를 사용합니다.

| 명령어 | 용도 |
| --- | --- |
| `git clone https://github.com/sleepysoong/sleepy-llm-router` | 저장소를 clone합니다. |
| `cd sleepy-llm-router` | 프로젝트 디렉터리로 이동합니다. |
| `npm install` | 의존성을 설치합니다. |
| `npm test` | 테스트 전체를 실행합니다. |
| `npm run typecheck` | TypeScript 타입 검사를 실행합니다. |
| `npm run build` | `dist` 를 빌드합니다. |
