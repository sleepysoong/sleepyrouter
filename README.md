# sleepyrouter

`sleepyrouter` 는 코딩 에이전트를 여러 무료 provider 중 설정된 순서대로 라우팅하는 로컬 프록시입니다. OpenAI 또는 Anthropic 호환 에이전트의 baseURL을 `localhost` 로 바꾸고 free 모델 몇 개를 골라두면, rate-limit이나 quota 문제가 생겨도 `sleepyrouter` 가 요청을 계속 흘려보냅니다.

## 왜 필요한가

Free tier 코딩 에이전트는 스펙 시트에서는 멀쩡해 보이지만, 실제로 돌려보면 몇 가지 문제가 생깁니다.

**Rate limit이 작업 중간에 끊습니다.** OpenRouter나 NVIDIA의 free 모델은 429를 예고 없이 던집니다. 잘 돌던 실행이 도구 호출 한 번에 멈추고, 사람이 직접 다시 시도해야 합니다.

**Quota가 마르면 provider를 손으로 갈아끼워야 합니다.** 한 provider의 free quota가 떨어지면 키와 baseURL을 직접 바꿔야 합니다. 에이전트 설정은 그 변화를 스스로 따라잡지 않습니다.

**Free 카탈로그가 자주 바뀝니다.** 모델이 새로 생기고, 사라지고, deprecated 표시가 붙고, 조용히 에러를 뱉기 시작합니다.

## sleepyrouter이 하는 일

쓸 free 모델의 allowlist를 `sleepyrouter` 에 넘기면 `http://localhost:4567` 에서 로컬 프록시로 동작합니다. 내부에서는 다음 일을 처리합니다.

| 기능 | 처리 방식 |
| --- | --- |
| 요청 라우팅 | 설정된 모델 순서대로 요청을 라우팅합니다. |
| 클라이언트 호환성 | OpenAI 호환 `/v1` 과 Anthropic 호환 `/anthropic` surface를 노출하고, Anthropic tool-use fallback과 로컬 token count도 지원합니다. |

에이전트는 `localhost` 만 바라봅니다. provider 전환은 그 아래에서 조용히 일어납니다.

## API 키 발급

`sleepyrouter`은 트래픽만 전달합니다. 지원되는 provider(OpenRouter, NVIDIA, GitHub Copilot) 중 하나 이상에서 직접 키를 발급받아야 합니다.

**OpenRouter** — [openrouter.ai](https://openrouter.ai)에서 가입한 뒤 Keys 메뉴에서 키를 발급받습니다(prefix `sk-or-`). `:free` 모델은 하루 50회까지 사용할 수 있고, 크레딧을 $10 이상 충전하면 하루 1,000회로 늘어납니다. 무료 한도에는 신용카드가 필요하지 않습니다.

**NVIDIA** — [build.nvidia.com](https://build.nvidia.com)(NVIDIA Developer Program)에서 가입한 뒤 모델 카드의 "Get API Key" 버튼으로 발급받습니다(prefix `nvapi-`). 신용카드는 필요하지 않으며, rate-limit은 모델별로 적용됩니다.

**GitHub Copilot** — [GitHub Settings > Developer settings](https://github.com/settings/tokens)에서 Personal Access Token (PAT)을 발급받습니다. 토큰 환경 변수명은 `GITHUB_COPILOT_TOKEN` 입니다. GitHub Copilot Free/Pro 등 사용자 플랜에 따라 사용할 수 있는 모델 목록(gpt-4o, claude-sonnet-4 등)이 다릅니다.

가지고 있는 키를 `~/.sleepyrouter/.env`에 넣어 두면, `sleepyrouter`은 키가 설정된 provider만 사용합니다.

## 30초 만에 시도하기

### Go으로 설치

```bash
go build -o sleepyrouter ./cmd/sleepyrouter
mkdir -p ~/.sleepyrouter && echo 'OPENROUTER_API_KEY=sk-or-...' > ~/.sleepyrouter/.env
./sleepyrouter start        # http://localhost:4567 서빙
```

### 소스에서 빌드

```bash
git clone https://github.com/sleepysoong/sleepy-llm-router
cd sleepy-llm-router && go build -o sleepyrouter ./cmd/sleepyrouter
./sleepyrouter start
```

## 설정 예제

`sleepyrouter` 설정은 `~/.sleepyrouter/config.json`에 JSON 형식으로 저장합니다.

```json
{
  "port": 4567,
  "modelGroups": {
    "fast": ["fast-llama", "fast-phi"],
    "balanced": ["balanced-llama", "balanced-llama-70b"],
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
    "balanced-llama-70b": {
      "provider": "openrouter",
      "name": "meta-llama/llama-3.1-70b-instruct:free"
    },
    "capable-gpt4o": {
      "provider": "openrouter",
      "name": "openai/gpt-4o-mini:free"
    },
    "capable-mistral": {
      "provider": "nvidia",
      "name": "mistralai/mistral-large-2-instruct"
    }
  }
}
```

| 필드 | 설명 |
| --- | --- |
| `port` | 프록시 포트 (기본값: `4567`) |
| `modelGroups` | 모델 그룹별로 라우팅할 alias 목록 |
| `defaultModelGroup` | 모델 그룹을 지정하지 않았을 때 사용할 기본 그룹 (생략 시 첫 번째 그룹) |
| `groupOrder` | `start` 명령어의 `--group-order` 플래그로만 지정 (설정 파일 무시) |
| `models` | 각 alias의 provider와 upstream 모델명을 정의하는 맵 |

### 작동 방식

요청이 들어오면 `sleepyrouter`은 그룹 안의 모델을 순서대로 시도합니다. 첫 번째 모델이 실패(429 rate-limit, 5xx, 타임아웃)하면 다음 모델로 자동 페일오버합니다. 그룹의 모든 모델이 실패하면 다음 그룹으로 넘어갑니다.

```
요청 (group=balanced)
  → balanced-llama 시도 (upstream: meta/llama-3.1-70b-instruct)
    → 실패 (rate-limit)
  → balanced-llama-70b 시도 (upstream: meta-llama/llama-3.1-70b-instruct:free)
    → 성공
```

### 실제 사용 예

**OpenRouter + NVIDIA + Copilot 하루 종일 쓰기:**

```json
{
  "modelGroups": {
    "fast": ["fast-llama", "fast-gpt4o", "fast-phi"],
    "balanced": ["balanced-llama", "balanced-claude", "balanced-llama-70b"],
    "capable": ["capable-gpt4o", "capable-mistral", "capable-claude"]
  },
  "models": {
    "fast-llama":                 { "provider": "nvidia",     "name": "meta/llama-3.1-8b-instruct" },
    "fast-gpt4o":                 { "provider": "copilot",    "name": "gpt-4o-mini" },
    "fast-phi":                   { "provider": "openrouter", "name": "microsoft/phi-3-mini-128k-instruct:free" },
    "balanced-llama":             { "provider": "nvidia",     "name": "meta/llama-3.1-70b-instruct" },
    "balanced-claude":            { "provider": "copilot",    "name": "claude-sonnet-4" },
    "balanced-llama-70b":         { "provider": "openrouter", "name": "meta-llama/llama-3.1-70b-instruct:free" },
    "capable-gpt4o":              { "provider": "openrouter", "name": "openai/gpt-4o-mini:free" },
    "capable-mistral":            { "provider": "nvidia",     "name": "mistralai/mistral-large-2-instruct" },
    "capable-claude":             { "provider": "openrouter", "name": "anthropic/claude-3.5-sonnet:free" }
  }
}
```

**Zen 모델 (OpenCode API)만 사용:**

```json
{
  "modelGroups": {
    "default": ["zen-flash"]
  },
  "models": {
    "zen-flash": { "provider": "zen", "name": "deepseek-v4-flash-free" }
  }
}
```

```bash
# .env
OPENCODE_API_KEY=sk-...
```

**단일 그룹 + 간단한 설정:**

```json
{
  "modelGroups": {
    "models": ["llama-70b", "llama-70b-fallback"]
  },
  "models": {
    "llama-70b":          { "provider": "nvidia",     "name": "meta/llama-3.1-70b-instruct" },
    "llama-70b-fallback": { "provider": "openrouter", "name": "meta-llama/llama-3.1-70b-instruct:free" }
  }
}
```

### 그룹 순서 지정

`start` 명령어에 `--group-order` 플래그로 요청이 라우팅될 그룹 우선순위를 정할 수 있습니다.

```bash
sleepyrouter start --group-order="capable,balanced,fast"
```

이 순서는 모델 그룹 설정과 독립적으로 적용되며, 라우팅 시도 우선순위만 변경합니다. 요청이 `capable` 그룹의 모든 모델에서 실패하면 `balanced`로, 거기도 실패하면 `fast`로 넘어갑니다.

### API 키 설정

키는 `~/.sleepyrouter/.env` 파일에 저장합니다. KEY가 설정된 provider만 시작 시 활성화됩니다.

```bash
# ~/.sleepyrouter/.env
OPENROUTER_API_KEY=sk-or-...
NVIDIA_API_KEY=nvapi-...
GITHUB_COPILOT_TOKEN=ghp_...
OPENCODE_API_KEY=sk-...
```

`sleepyrouter start`를 실행하면 설정된 키가 있는 provider만 자동으로 감지하여 서빙합니다. 키가 하나도 없으면 서버가 시작되지 않습니다.

## 자주 쓰는 명령어

| 명령어 | 용도 |
| --- | --- |
| `sleepyrouter start` | 로컬 프록시를 foreground로 실행하고 request/response 라우팅 로그를 출력합니다. |
| `sleepyrouter usage` | 모델별 요청 수, 실패 수, 토큰 사용량을 출력합니다. |
| `sleepyrouter usage --date 20260203` | 특정 날짜의 사용량만 출력합니다. |
| `sleepyrouter usage --week 27` | 특정 주의 사용량만 출력합니다. |

## 에이전트에서 쓰기

OpenAI 호환 클라이언트(OpenCode, Hermes Agent, OpenClaw 등)에서는 다음 값을 사용합니다.

```text
baseURL=http://localhost:4567/v1
```

Anthropic 호환 클라이언트(Claude Code 등)에서는 다음 환경변수를 설정합니다.

```bash
export ANTHROPIC_BASE_URL=http://localhost:4567/anthropic
export ANTHROPIC_AUTH_TOKEN=sleepyrouter-local
export ANTHROPIC_API_KEY=
```

Claude Code의 모델 별칭도 `sleepyrouter` 그룹을 가리키도록 설정할 수 있습니다.

```bash
alias freeclaude='ANTHROPIC_BASE_URL=http://localhost:4567/anthropic ANTHROPIC_AUTH_TOKEN=sleepyrouter-local ANTHROPIC_API_KEY= CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 ANTHROPIC_DEFAULT_OPUS_MODEL=sleepyrouter/capable ANTHROPIC_DEFAULT_SONNET_MODEL=sleepyrouter/balanced ANTHROPIC_DEFAULT_HAIKU_MODEL=sleepyrouter/fast claude'
```

접두사 없는 `sleepyrouter`은 선택된 전체 풀로 라우팅되며, `sleepyrouter/capable`, `sleepyrouter/balanced`, `sleepyrouter/fast`는 각 모델 그룹으로 필터링됩니다. Claude 스타일 별칭인 `opus`, `sonnet`, `haiku`도 같은 그룹에 매핑됩니다.

Anthropic surface는 로컬 `count_tokens` 추정치도 제공하며, OpenAI 호환 provider route로 fallback되는 경우 일반적인 tool-use/tool-result 흐름을 번역합니다.

## 컨텍스트 크기 맞추기

`sleepyrouter`은 요청을 라우팅된 모델로 그대로 전달하며, 에이전트 세션에 누적된 대화를 자동으로 압축(compact)하거나 요약하거나 잘라내지 않습니다. 따라서 컨텍스트 오버플로우는 실제로 발생할 수 있습니다. 긴 세션이 1M 토큰 컨텍스트 모델에서 시작된 뒤 128k/200k 모델로 라우팅되거나 페일오버되면, 프롬프트가 작은 모델의 컨텍스트 윈도를 넘는 순간 업스트림 제공자가 요청을 거절할 수 있습니다.

모델을 고를 때는 라우팅 후보 풀마다 컨텍스트 크기 티어를 맞춰두세요. 모델의 컨텍스트 윈도 크기는 각 provider의 문서 페이지에서 확인할 수 있습니다.

## 아키텍처

`sleepyrouter`의 업스트림 전송 계층은 GoAI([github.com/zendev-sh/goai](https://github.com/zendev-sh/goai), v0.9.4, vendored)의 `LanguageModel` API(`ChatCompletion` / `Messages`) 위에 구축되어 있습니다. 기존의 손으로 짠 raw-HTTP provider 구현이 GoAI 모델 호출로 교체되었으며, 외부 계약(`providers.Provider` 인터페이스, 핸들러·라우팅 코드, OpenAI/Anthropic wire surface)은 그대로 유지됩니다.

```
핸들러/라우터 (변경 없음)
        │  providers.Provider 인터페이스 (변경 없음)
        ▼
providers: GoAI ChatCompletion/Messages 모델 호출
        │  adapt.OpenAIRequest / adapt.AnthropicRequest   (wire body → GenerateParams)
        │  emit.OpenAIResponse / emit.AnthropicResponse / *StreamSSE (결과 → wire)
        ▼
goai (vendored) — openai-compat · anthropic · gemini-compat 모델
```

주요 구성 요소:

| 구성 요소 | 역할 |
| --- | --- |
| `internal/adapt` | OpenAI chat.completions / Anthropic messages 요청 본문을 `provider.GenerateParams`로 변환. assistant thinking/reasoning, tool-call thought-signature, 이미지(anthropic base64 → data-URI), provider-defined tools(`computer_20241022` 등)을 보존합니다. |
| `internal/emit` | `GenerateResult`/스트림 `StreamChunk`를 OpenAI/Anthropic wire 응답으로 직렬화. 스트리밍 role/reasoning/content/tool_call delta, thinking `signature_delta`, 종료 시 usage 청크를 재현합니다. |
| `internal/providers` | Provider 인터페이스 유지. 각 provider 파일은 goai 모델을 per-call로 구성하며, Copilot token 캐시·갱신 로직은 원래 시맨틱을 유지합니다. |

### Vendored 패치

GoAI v0.9.4는 Claude Code ↔ Gemini 시그니처 연속성(thought-signature)을 기본 지원하지 않아 `vendor/`에 4개의 작은 패치를 적용했습니다. 재-vendor 시 다시 적용해야 합니다.

1. `openaicompat/messages.go` — `ConvertMessages`가 per-message/per-part `ProviderOptions`를 직렬화에 deep-merge(기존 `role`/`content`/`tool_calls`/`reasoning_content` 보호). 요청 방향의 tool-call 시그니처와 extra_fields가 라운드트립됩니다.
2. `openaicompat/openaicompat.go` — 스트림·비스트림 파싱에서 `thought_signature`/`signature`/`extra_content.google.thought_signature`를 캡처해 청크 `Metadata`(키 `thoughtSignature`)로 노출. 툴 콜은 3중 위치(tool_call·function·extra_fields)로 재출력됩니다.
3. `openaicompat/openaicompat.go` — reasoning 파싱 보존 + `WithIncludeReasoningContent` 옵션.
4. `anthropic.go` — `WithBetaFeatures` 옵션으로 Anthropic beta 헤더를 제어(빈 문자열이면 헤더 생략, OpenRouter가 Anthropic의 beta 목록을 거부).

### 추가 수정

리팩토링 과정에서 정규적으로 3개 패치가 추가로 적용되었습니다:

1. `internal/handler/model_selection.go` — Anthropic Messages 경로가 OpenAI Chat Completions allow-list 대신 `withUpstreamModelAnthropic`를 사용하도록 분리. 이전에는 OpenAI 전용 allow-list가 `thinking`, `system`, `stop_sequences` 등 Anthropic Messages 필드를 제거했기 때문에 thinking 요청이 업스트림에 도달하지 못했습니다.
2. `internal/adapt/adapt.go` — `thinking.budget_tokens`(snake_case, Anthropic wire)를 `thinking.budgetTokens`(camelCase, goai 읽는 키)로 정규화. 정규화 없이는 thinking budget이 업스트림에 도달하기 전에 조용히 사라집니다.
3. `internal/emit/emit.go` — `providerSignatures`/`messageSignature` 헬퍼. 비스트림 message-level thought signature를 `ProviderMetadata["openai"]`(OpenAI/Google 직접)와 `ProviderMetadata["anthropic"]["reasoning"][i]["signature"]`(Anthropic reasoning 블록) 양쪽에서 수집. goai가 reasoning 슬라이스를 `[]any`와 `[]map[string]any` 어느 쪽으로 저장하든 읽습니다.

### 샌드박스

`sandbox/` 디렉토리는 격리된 end-to-end 테스트 하네스입니다. fake OpenAI/Anthropic 업스트림을 띄우고 실제 `./cmd/sleepyrouter` 바이너리를 랜덤 포트에 실행한 뒤 13개의 curl 기반 검사를 수행합니다.

```bash
go run ./sandbox
```

각 실행은 별도 임시 디렉토리(`SLEEPYROUTER_HOME`)와 별도 랜덤 포트를 사용하며, 업스트림은 `SLEEPYROUTER_{OPENROUTER,NVIDIA,GOOGLE,ZEN,COPILOT}_BASE_URL` 환경 변수를 통해 가짜 업스트림으로 재지정됩니다. 검사 항목:

- health, models-list
- openai 비스트림 텍스트, openai 스트림 텍스트, openai tool_use 라운드트립
- anthropic 비스트림 텍스트, anthropic 스트림 텍스트, anthropic thinking 시그니처, anthropic tool_use 블록
- failover(첫 후보 5xx → 둘째 후보 성공)
- count_tokens, 404 알 수 없는 라우트, 업스트림 요청 shape 검증

13/13이 녹색이면 통과입니다.

## 라이선스

[MIT](./LICENSE.md)
