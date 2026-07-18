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
go install github.com/sleepysoong/sleepyrouter/cmd/sleepyrouter@latest
mkdir -p ~/.sleepyrouter && echo 'OPENROUTER_API_KEY=sk-or-...' > ~/.sleepyrouter/.env
sleepyrouter start        # http://localhost:4567 서빙
```

### 소스에서 빌드

```bash
git clone https://github.com/sleepysoong/sleepy-llm-router
cd sleepy-llm-router && go install ./cmd/sleepyrouter
sleepyrouter start
```

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

모델을 고를 때는 라우팅 후보 풀마다 컨텍스트 크기 티어를 맞춰두세요. 현재는 인터페이스에서 컨텍스트 크기를 직접 노출하지 않으니, 모델 카탈로그(`docs/provider-guide.md`)나 provider 페이지를 참고하세요.

## 더 알아보기

- 설치, 모든 CLI 플래그, 진단은 [설치 및 설정](./docs/INSTALLATION.md)를 참고하세요.
- 라우팅 내부 동작은 [라우팅](./docs/latency-routing.md)를 참고하세요.
- Provider 카탈로그는 [프로바이더 가이드](./docs/provider-guide.md)를 참고하세요.
- 라이선스: [MIT](./LICENSE.md)
