<p align="center">
  <img src="assets/logo.png" width="120" alt="sleepyrouter logo" />
</p>

# sleepyrouter v2

localhost 전용 LLM routing gateway. 여러 OpenAI-compatible provider/model을
하나의 **ordered failover pool**로 묶고, 로컬 클라이언트에게 두 가지
인터페이스를 동시에 제공한다.

- `POST /v1/responses` — OpenAI Responses API (Codex / OpenAI client)
- `POST /v1/messages`, `POST /v1/messages/count_tokens` — Anthropic Messages API (Claude Code)

핵심 원칙: **모델 선택과 클라이언트 프로토콜은 독립적**이다.
Claude Code 요청도 동일한 OpenAI-compatible model pool을 사용한다.
차이는 edge protocol뿐이다.

단일 Go binary. `net/http` + 공식 OpenAI Go SDK. LiteLLM 없음.

## Install

```bash
go build -o bin/sleepyrouter ./cmd/sleepyrouter
```

Go >= 1.25. 의존성은 `go.mod`에 pin.

## Quick start

```bash
mkdir -p ~/.sleepyrouter
cp config.example.toml ~/.sleepyrouter/config.toml
# ~/.sleepyrouter/.env 에 키 입력:
#   OPENCODE_API_KEY=...
#   NVIDIA_API_KEY=...

./bin/sleepyrouter validate
./bin/sleepyrouter doctor
./bin/sleepyrouter serve            # 127.0.0.1:4567
./bin/sleepyrouter serve --port 4599
```

## Config

TOML. 기본 위치 `~/.sleepyrouter/config.toml`
(`SLEEPYROUTER_HOME` 설정 시 `$HOME/config.toml` + `.env` + `usage.db`).

```toml
version = 1
[server]
host = "127.0.0.1"
port = 4567
[routing]
default_group = "coding"
unknown_model_policy = "default_group"  # or "error"
[groups]
coding = ["zen/model-a", "nvidia/model-b"]
[aliases]
"sleepy-claude-coding" = "coding"
```

- `groups` array 순서가 **절대적인 routing priority**다. sort 금지.
- `modelGroups` 키 순서도 보존된다.
- group/model/alias 이름 충돌은 validation error.
- `unknown_model_policy = "default_group"` 이면 모르는 model도 기본 그룹으로 라우트
  (Codex/Claude가 자기 표준 모델명을 보낼 때 유용). `"error"`면 404/400.
- `transport = "responses"` 같은 설정은 없다. upstream은 전부
  OpenAI-compatible이라는 전제다.

Hot reload: `config.toml` + `.env` 변경은 재시작 없이 반영.
parent directory를 watch, 300ms debounce, 전체 validate 성공 시에만 atomic swap.
실패하면 기존 정상 config 유지. 각 request는 시작 시 snapshot 하나를 잡고
끝까지 사용한다.

## Providers

| ID | default env | default base_url |
|---|---|---|
| `zen` | `OPENCODE_API_KEY` | `https://opencode.ai/zen/v1` |
| `nvidia` | `NVIDIA_API_KEY` | `https://integrate.api.nvidia.com/v1` |
| `gemini` | `GEMINI_API_KEY` | `https://generativelanguage.googleapis.com/v1beta/openai/` |
| `openrouter` | `OPENROUTER_API_KEY` | `https://openrouter.ai/api/v1` |

커스텀 provider도 코드 없이 등록 가능:

```toml
[providers.foo]
base_url = "https://..."
api_key_env = "FOO_API_KEY"
```

키 우선순위: 프로세스 env > `~/.sleepyrouter/.env`.
**키 없음은 서버 fatal이 아니다.** 해당 candidate를 skip하고 다음으로 간다.
`[providers.x] enabled = false`, `[models."x/y"] enabled = false` 도 즉시 반영.

Upstream 통신은 공식 OpenAI Go SDK, `WithMaxRetries(0)`.
retry/failover는 sleepyrouter가 소유한다 (같은 endpoint가 SDK 때문에
중복 호출되지 않음을 테스트로 보장).

## Model groups

```toml
[models."zen/model-a"]
provider = "zen"
upstream_model = "model-a"
reasoning_effort = "high"   # optional; client 명시값이 우선

[groups]
coding = ["zen/model-a", "nvidia/model-b"]
```

요청 `model` 해석 순서: exact group → exact model → alias → unknown policy.
capabilities는 3-state (`tools/vision/reasoning`):
explicit `false`만 제외, 생략(unknown)은 허용.

## Codex / OpenAI setup

`POST /v1/responses` 로 연결. request는 최대한 pass-through:
unknown field를 삭제하지 않고 `model`만 candidate upstream model로 교체.
`store=false`를 임의로 바꾸지 않는다.

## Claude Code setup

```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:4567"
export ANTHROPIC_API_KEY="local-dummy"   # auth mode none이면 무시됨
export ANTHROPIC_MODEL="sleepy-claude-coding"   # config의 alias
export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1
# 실제 모델 context window가 Claude Code 기본 가정보다 다를 때만 설정:
# export CLAUDE_CODE_MAX_CONTEXT_TOKENS=200000
```

- `POST /v1/messages` + `POST /v1/messages/count_tokens` 제공.
- `GET /v1/models` 는 group/model/alias를 반환한다. Claude Code는 ID에
  `claude` 또는 `anthropic`이 포함된 항목만 picker에 표시하므로 gateway alias도
  그 문자열을 포함하게 짓는다. discovery를 쓰려면
  `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1` 을 켠다.
- `X-Claude-Code-Session-Id` 는 usage 집계에 저장한다.
  `anthropic-version` / `anthropic-beta` 는 OpenAI Responses 업스트림으로
  전달하지 않으며, Anthropic beta 기능을 활성화하지도 않는다.
- `tool_use.id` ↔ `tool_result.tool_use_id` 관계를 검증하고 그대로 보존한다.
  Responses API로 변환하는 동안 stateless 대화 history 전체를 재전송한다.
- gateway/custom model ID의 context window와 capability 추정은 Claude Code 쪽
  가정에 좌우된다. 모델 context가 기본 추정과 다르면
  [`CLAUDE_CODE_MAX_CONTEXT_TOKENS`](https://code.claude.com/docs/en/model-config#correct-the-window-for-a-gateway-or-custom-model-id)
  를 맞춘다.

## 프로토콜 호환성과 한계

이 게이트웨이는 공식 Anthropic 서버가 아니다. Claude Code 요청도 모두
OpenAI Responses 호환 업스트림으로 변환하므로 **Anthropic API 전체와 동등하지
않다**. 아래 상태는 라우터의 변환 동작을 뜻하며, 실제 생성 가능 여부는 선택된
업스트림 모델의 기능에 달려 있다.

| 기능 | 상태 | 동작·한계 |
| --- | --- | --- |
| OpenAI Responses 텍스트·함수 호출 출력 | 지원 | SDK 응답을 기반으로 반환하고 요청한 가상 모델명을 표시한다. |
| OpenAI Responses SSE·오류 | 지원 | 정상 이벤트를 전달한다. 커밋 후 전송 중단은 `error` 이벤트로 알리며 다른 모델 출력을 이어 붙이지 않는다. |
| Anthropic Messages 텍스트·클라이언트 도구 사용 | 지원 | tool call ID와 출력 순서를 보존한다. 이전 assistant 텍스트는 Responses 대화 입력으로 재생한다. |
| Anthropic 이미지·document 입력 | 업스트림 의존 | user 이미지 base64/URL, document text/base64/URL을 Responses 입력 타입으로 변환한다. 실제 MIME·파일 형식과 모델 기능은 업스트림에 달려 있다. |
| `tool_result.is_error`·멀티모달 결과 | 손실 있는 변환 | text/image/document 결과를 가능한 범위에서 변환한다. `is_error`는 Responses에 동등한 오류 비트가 없어 텍스트 표식으로 전달된다. Anthropic server/MCP tools는 지원하지 않는다. |
| `output_config.effort` / `format` | 변환 지원 | effort를 Responses reasoning effort로, JSON schema를 strict structured output으로 변환한다. 모델/provider가 해당 기능을 지원해야 한다. |
| Anthropic thinking | 손실/부분 지원 | `adaptive`는 reasoning capability 라우팅에만 사용되고 그 자체는 전달되지 않는다. `output_config.effort`는 매핑한다. 고정 budget의 `thinking.enabled`는 Responses에 동등한 의미가 없어 400으로 거부한다. 과거 thinking 서명 블록은 재전송하지 않고 출력 reasoning summary도 생략한다. |
| Anthropic prompt caching·`cache_control`·베타 헤더 | Anthropic 방식 미지원 | cache marker/TTL과 beta 헤더를 Responses 전용 캐시 제어로 바꾸지 않는다. 업스트림 자체의 implicit cache는 안정된 prefix와 provider 지원 여부에 따라 동작할 수 있다. provider가 보고한 cached/cache-write token은 usage에 기록하지만 hit를 보장하지 않는다. |
| Anthropic 전용 옵션·server tools | 거부/미지원 | `context_management`, Anthropic `service_tier`/`inference_geo`, `stop_sequences`, `top_k`, `container`, `mcp_servers`, `defer_loading`/`tool_reference` 등 Responses와 의미가 맞지 않는 입력은 조용히 버리지 않고 거부한다. |
| `/v1/messages/count_tokens` | 추정치 | 로컬 추정기(`×1.2`)이며 공식 Anthropic 토크나이저의 정확한 사용량이 아니다. 컨텍스트 한계 근처에서는 여유를 둬야 한다. |

스트리밍 `response.incomplete`의 `max_output_tokens`는 Anthropic
`stop_reason=max_tokens`로 표시한다. 그 외 upstream 실패·중단은 정상
`end_turn`으로 위장하지 않고 스트림 오류로 알린다. 실제 Claude Code/Codex
버전과 업스트림이 사용하는 확장 기능까지 모두 검증했다는 뜻은 아니다.
이때 도구 호출이 JSON 인자 중간에서 잘릴 수 있으므로 클라이언트는
`max_tokens`를 확인하고 불완전한 도구 인자를 실행하지 않아야 한다.

상세한 변환 경계, Claude Code gateway 계약 및 조사한 유사 라우터 이슈는
[Claude Code 호환성 노트](docs/claude-code-compatibility.md)를 참고한다.

## Routing behavior

- 동일 snapshot + 동일 model + 동일 capability → 동일 ordered list (deterministic).
- failover 대상: key missing, 408/409/429/5xx, timeout, model unavailable,
  commit 전 stream 종료, unsupported feature.
- 즉시 종료: malformed JSON, body 초과, 필수 필드 누락, client invalid.
  다른 model로 보내도 실패하므로 failover 안 함.
- 400은 body까지 분류: 해당 모델이 옵션을 못 받으면 failover,
  request 자체가 잘못이면 400 종료.
- 같은 provider에서 401/403이 나면 해당 request 내 동일 provider 후보 skip.
- response debug header:
  `X-SleepyRouter-Request-ID/Model/Provider/Attempts/Config-Generation`.
- 외부 노출 header는 allowlist만. `Authorization`은 항상 provider key가 우선.

## Streaming behavior

- commit 기준은 첫 TCP byte가 아니라 **첫 meaningful event**
  (text/reasoning/tool delta, content 시작, completed, 버퍼 한계).
  `response.created`만 받고 죽으면 failover 가능.
- precommit 버퍼 기본: 32 events / 64 KiB / 첫 event 후 2s 중 먼저 도달 시 commit.
- **commit 이후 다른 model로 절대 failover하지 않는다.**
  response ID / tool ID / index가 바뀌어 client parser가 깨지기 때문이다.
- `first_event` / `stream_idle` timeout은 candidate failover 사유.
- Anthropic SSE는 quiet upstream 구간에 최대 15초 간격 `ping`을 내보낸다.
  기본 `stream_idle=330s` 는 Claude Code의 기본 300초 watchdog보다 약간 길게
  upstream 무응답 한계를 둔 값이다. ping은 클라이언트의 바이트 watchdog을
  만족시키지만 gateway 자체의 idle timer를 연장하지 않는다.
- client disconnect는 context로 upstream까지 전파.
- usage는 upstream 값만 쓰고 추정하지 않는다. `sleepyrouter usage` 의
  `cache-read`, `cache-write`, `cache-hit` 은 provider가 세부 usage를 반환할 때만
  유효하다. `cache-hit = cached_input_tokens / input_tokens` 이며 provider별 정의나
  미보고 여부를 동일하게 보장하지 않는다. usage가 없으면 DB 집계는 0이며,
  Anthropic 스트림은 알려진 누적 토큰만 `message_delta`에 보낸다. input usage는
  upstream이 완료 시점에만 알려주면 시작 이벤트의 0에서 최종 값으로 갱신된다.

## Stateful Responses caveat

`previous_response_id` 는 upstream server state와 연결될 수 있다.
`resp_123` 을 zen에서 받았는데 다음 요청을 nvidia로 보내면 nvidia는 모른다.

- 완료된 응답의 `response_id → provider/model` affinity를 프로세스 메모리에만 저장한다.
  재시작 시 사라지며 SQLite 영속성/TTL은 현재 구현되어 있지 않다.
- `previous_response_id`가 있으면 해당 provider/model에 sticky.
  실패해도 다른 provider로 blind failover하지 않는다.
- affinity에 없는 ID는 첫 candidate에만 전달하고 upstream 판정에 맡긴다.
  모든 candidate에 뿌리지 않는다.
- Anthropic Messages는 full history를 보내므로 영향이 적다.

## Troubleshooting

- `sleepyrouter validate`: parse/validate만 수행.
- `sleepyrouter doctor`: config/.env/key/group/port/DB/proxy 점검 (과금 호출 없음).
- `sleepyrouter models`: group 순서 + key 유무 (값 출력 안 함).
- `sleepyrouter usage [--today] [--week] [--date YYYYMMDD] [--model ID]`: requests/success/input/output/cache 집계.
- `GET /health`: `{ok, service, version, config_generation, uptime_seconds}`.
  키가 없다고 false가 되지 않는다.
- `GET /ready`, `GET /version`도 제공.
- 기본 로그에 prompt/tool 결과/API key/body 전체를 기록하지 않는다.
  secret header는 redaction. debug에서도 Authorization 제외.
- usage DB 실패는 inference를 실패시키지 않는다.

## Development

```bash
make test
make test-race
make lint
make build
```

- `POST /v1/chat/completions` 은 제공하지 않는다 (v1 비목표).
- `internal/routing` 은 `protocol/*` 을 import하지 않는다.
  `provider` 도 마찬가지. `go vet` + `gofmt` + `go test -race` 가 CI gate.
- 자세한 구조: `docs/architecture.md`, `docs/routing.md`,
  `docs/protocol-openai.md`, `docs/protocol-anthropic.md`,
  `docs/claude-code-compatibility.md`.

## 조사 참고

- [Claude Code gateway compatibility guide](https://code.claude.com/docs/en/llm-gateway-protocol)
- [Anthropic Messages API](https://platform.claude.com/docs/en/api/messages/create) · [tool calls](https://platform.claude.com/docs/en/agents-and-tools/tool-use/handle-tool-calls) · [streaming](https://platform.claude.com/docs/en/build-with-claude/streaming)
- [OpenAI prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching) · [Responses file inputs](https://developers.openai.com/api/docs/guides/file-inputs)

## License

MIT — [LICENSE.md](LICENSE.md)
