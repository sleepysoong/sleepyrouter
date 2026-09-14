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
"claude-sleepy" = "coding"
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
```

- `POST /v1/messages` + `POST /v1/messages/count_tokens` 제공.
- `anthropic-version`, `anthropic-beta`, `X-Claude-Code-Session-Id` 등 보존
  (session은 observability용, upstream에 무단 전달 안 함).
- model discovery: `GET /v1/models` 가 group/model/alias superset 반환.
  picker에 보이게 하려면 `claude-*` alias 권장:
  `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1` + `[aliases] "claude-sleepy" = "coding"`.
- tool_use id ↔ tool_result tool_use_id 연결 보존. stream에서는 partial JSON을
  파싱하려 하지 않고 완료 시점에만 검증.

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
- client disconnect는 context로 upstream까지 전파.
- usage의 `output_tokens`는 upstream 값을 쓰고, 없을 때만 추정하지 않고 0 유지
  (Anthropic usage fabrication 금지).

## Stateful Responses caveat

`previous_response_id` 는 upstream server state와 연결될 수 있다.
`resp_123` 을 zen에서 받았는데 다음 요청을 nvidia로 보내면 nvidia는 모른다.

- 성공 시 `response_id → provider/model` affinity 저장 (memory + SQLite, TTL 30d).
- `previous_response_id`가 있으면 해당 provider/model에 sticky.
  실패해도 다른 provider로 blind failover하지 않는다.
- affinity에 없는 ID는 첫 candidate에만 전달하고 upstream 판정에 맡긴다.
  모든 candidate에 뿌리지 않는다.
- Anthropic Messages는 full history를 보내므로 영향이 적다.

## Troubleshooting

- `sleepyrouter validate`: parse/validate만 수행.
- `sleepyrouter doctor`: config/.env/key/group/port/DB/proxy 점검 (과금 호출 없음).
- `sleepyrouter models`: group 순서 + key 유무 (값 출력 안 함).
- `sleepyrouter usage [--today] [--week] [--date YYYYMMDD] [--model ID]`: requests/success/input/output 집계.
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
  `docs/protocol-openai.md`, `docs/protocol-anthropic.md`.

## License

MIT — [LICENSE.md](LICENSE.md)
