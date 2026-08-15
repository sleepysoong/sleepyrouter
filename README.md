# sleepyrouter

`sleepyrouter`는 코딩 에이전트를 여러 무료 provider 중 설정된 순서대로 라우팅하는 로컬 프록시입니다. OpenAI 또는 Anthropic 호환 에이전트의 baseURL을 `localhost`로 바꾸고 free 모델 몇 개를 골라두면, rate-limit이나 quota 문제가 생겨도 `sleepyrouter`가 요청을 자동 페일오버하여 계속 흘려보냅니다.

TypeScript와 [Vercel AI SDK](https://github.com/vercel/ai) 및 [Bun](https://bun.sh)을 기반으로 동작합니다.

## 왜 필요한가

Free tier 코딩 에이전트는 스펙 시트에서는 멀쩡해 보이지만, 실제로 돌려보면 몇 가지 문제가 생깁니다.

**Rate limit이 작업 중간에 끊습니다.** OpenRouter나 NVIDIA의 free 모델은 429를 예고 없이 던집니다. 잘 돌던 실행이 도구 호출 한 번에 멈추고, 사람이 직접 다시 시도해야 합니다.

**Quota가 마르면 provider를 손으로 갈아끼워야 합니다.** 한 provider의 free quota가 떨어지면 키와 baseURL을 직접 바꿔야 합니다. 에이전트 설정은 그 변화를 스스로 따라잡지 않습니다.

**Free 카탈로그가 자주 바뀝니다.** 모델이 새로 생기고, 사라지고, deprecated 표시가 붙고, 조용히 에러를 뱉기 시작합니다.

## sleepyrouter가 하는 일

쓸 free 모델의 allowlist를 `sleepyrouter`에 넘기면 `http://localhost:4567`에서 로컬 프록시로 동작합니다. 내부에서는 다음 일을 처리합니다.

| 기능 | 처리 방식 |
| --- | --- |
| 요청 라우팅 | 설정된 모델 순서대로 요청을 라우팅하고 실패 시 자동 페일오버합니다. |
| 클라이언트 호환성 | OpenAI 호환 `/v1` 과 Anthropic 호환 `/anthropic` surface를 노출하고, Anthropic tool-use 및 로컬 token count도 지원합니다. |
| Vercel AI SDK 기반 | Vercel AI SDK(`ai`, `@ai-sdk/openai-compatible`)를 사용하여 다양한 provider 통합 및 스트리밍을 제공합니다. |

에이전트는 `localhost`만 바라봅니다. provider 전환은 그 아래에서 조용히 일어납니다.

## API 키 발급

`sleepyrouter`는 트래픽만 전달합니다. 지원되는 provider(OpenRouter, NVIDIA, GitHub Copilot, Google, Zen) 중 하나 이상에서 직접 키를 발급받아야 합니다.

- **OpenRouter** — [openrouter.ai](https://openrouter.ai) 키 발급 (`OPENROUTER_API_KEY`)
- **NVIDIA** — [build.nvidia.com](https://build.nvidia.com) 키 발급 (`NVIDIA_API_KEY`)
- **GitHub Copilot** — Personal Access Token (`GITHUB_COPILOT_TOKEN`)
- **Google Gemini** — Google AI Studio 키 발급 (`GOOGLE_API_KEY` 또는 `GEMINI_API_KEY`)
- **Zen / OpenCode** — OpenCode 키 발급 (`OPENCODE_API_KEY`)

가지고 있는 키를 `~/.sleepyrouter/.env`에 넣어 두면, `sleepyrouter`는 키가 설정된 provider만 사용합니다.

## Quick Start (Bun)

### 설치 및 실행

```bash
# Bun 설치 (설치되어 있지 않은 경우)
curl -fsSL https://bun.sh/install | bash

# 의존성 설치
bun install

# sleepyrouter 시작 (기본 포트: 4567)
bun start

# 또는 옵션 지정
bun run src/main.ts start --port 4567
```

### CLI 사용법

```bash
# 서버 시작
bun run src/main.ts start [--port 4567]

# 사용량 확인
bun run src/main.ts usage [--date YYYYMMDD | --week NN]

# 버전 확인
bun run src/main.ts --version
```

### 테스트 및 타입 체크

```bash
# unit & integration 테스트 실행
bun test

# TypeScript 타입 체크
bun run typecheck
```

## 설정 예제

`sleepyrouter` 설정은 `~/.sleepyrouter/config.json`에 JSON 형식으로 저장합니다.

```json
{
  "port": 4567,
  "modelGroups": {
    "fast": ["fast-llama", "fast-phi"],
    "balanced": ["balanced-llama", "capable-gpt4o"],
    "capable": ["capable-gpt4o", "capable-mistral"]
  },
  "defaultModelGroup": "balanced",
  "models": {
    "fast-llama": {
      "provider": "nvidia",
      "name": "meta/llama-3.1-8b-instruct"
    },
    "fast-phi": {
      "provider": "openrouter",
      "name": "microsoft/phi-3-mini-128k-instruct:free"
    },
    "balanced-llama": {
      "provider": "nvidia",
      "name": "meta/llama-3.1-70b-instruct"
    },
    "capable-gpt4o": {
      "provider": "google",
      "name": "gemini-3.6-flash"
    },
    "capable-mistral": {
      "provider": "nvidia",
      "name": "mistralai/mistral-large-2-instruct"
    }
  }
}
```

## 라이선스

[MIT License](LICENSE.md)
