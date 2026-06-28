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

`slr`은 설정 파일(`~/.sleepy-llm-router/config.json`)의 `modelGroups`에 정의된 모델만 라우팅 후보로 사용합니다. 각 그룹에 모델을 넣으면 해당 그룹 순서대로 라우팅됩니다. `defaultGroup`으로 지정된 그룹은 인식할 수 없는 모델 요청이 들어올 때 사용됩니다.

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

## 6. 사용량 확인

| 명령어 | 용도 |
| --- | --- |
| `slr usage` | 모델별 누적 요청 수, 실패 수, 토큰 사용량을 테이블로 출력합니다. |
| `slr usage --date 20260203` | 특정 날짜의 사용량만 출력합니다. |
| `slr usage --week 27` | 특정 주의 사용량만 출력합니다. (올해 기준) |

사용 기록은 `~/.sleepy-llm-router/usage.jsonl`에 JSONL 형식으로 저장됩니다.

## 7. 라우팅 규칙

요청된 이름에 따라 다음 순서로 처리해요:

1. **등록된 그룹 이름** → 해당 그룹의 모델을 순서대로 시도
2. **그 외 전부** → `defaultGroup`으로 라우팅

`slr/` 접두사는 매칭 전에 제거돼요. 모델 ID는 매칭되지 않아요 — 그룹명만 라우팅에 사용돼요. 레거시 별칭 `haiku`/`sonnet`/`opus`도 지원돼요.

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
