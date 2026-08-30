<p align="center">
  <img src="assets/logo.png" width="120" alt="sleepyrouter logo" />
</p>

# sleepyrouter

코딩 에이전트의 OpenAI 호환 요청을 무료 LLM 프로바이더로 라우팅하는 로컬 프록시.
후보 모델을 설정 순서대로 시도하고, 실패하면 다음 후보로 자동 전환한다(failover).

Python 3.12+ · FastAPI + OpenAI & Google SDKs

## 설치 및 실행

```bash
pip install -e .

python main.py start                # 포트 4567 기본
python main.py start --port 4599
```

설정과 사용량 기록은 `~/.sleepyrouter/` 아래에 저장된다(`SLEEPYROUTER_HOME`으로 변경 가능).

## API

| 엔드포인트 | 설명 |
|---|---|
| POST /v1/chat/completions | OpenAI 호환 채팅 완성. 스트리밍 지원 |
| GET /v1/models | 등록된 모델 목록 |
| GET /health | 헬스 체크 |

## 라우팅

요청의 `model` 필드를 다음 순서로 해석한다.

1. `modelGroups`의 그룹 이름과 일치하면 그룹 내 모델을 나열 순서대로 시도
2. 등록된 모델 ID와 직접 일치하면 해당 모델 하나만 시도
3. 어느 쪽도 아니면 `defaultModelGroup`으로 폴백

후보마다 API 키가 없거나 호출이 실패하면 다음 후보로 넘어가고, 전부 실패하면 502를 반환한다. 스트리밍 응답은 첫 청크 수신 전까지만 페일오버할 수 있다.

호출 결과는 `~/.sleepyrouter/usage.jsonl`에 기록되며 `usage` 명령으로 조회한다.

```bash
python main.py usage                    # 최근 사용량 요약
python main.py usage --date 20260822    # 날짜 필터
python main.py usage --week 34          # 주 단위 필터
```

## 설정

`~/.sleepyrouter/config.json`. 저장할 때마다 자동으로 반영된다(재시작 불필요).

```json
{
  "port": 4567,
  "modelGroups": {
    "max": [
      "openrouter/claude-3-7-sonnet",
      "google/gemini-2.5-flash",
      "nvidia/glm-5.2",
      "zen/deepseek-v4-flash"
    ]
  },
  "defaultModelGroup": "max",
  "models": {
    "openrouter/claude-3-7-sonnet": {
      "provider": "openrouter",
      "name": "anthropic/claude-3.7-sonnet",
      "maxEffort": "high"
    },
    "nvidia/glm-5.2": {
      "provider": "nvidia",
      "name": "z-ai/glm-5.2"
    }
  }
}
```

- 그룹 간 우선순위는 `modelGroups` 키의 나열 순서를 따른다.
- `models`의 키는 `<source>/<모델명>` 형식의 로컬 ID이고, `name`은 프로바이더에 보낼 실제 모델 ID다.
- `inputPrice`, `outputPrice`(달러/백만 토큰), `apiBase`, `maxEffort`, `thinkingBudget`은 선택값이다.

## 프로바이더와 API 키

| 소스 | 환경 변수 | 비고 |
|---|---|---|
| openrouter | `OPENROUTER_API_KEY` | |
| nvidia | `NVIDIA_API_KEY` | |
| copilot | `GITHUB_COPILOT_TOKEN` | GitHub PAT를 내부 토큰으로 교환해 사용 |
| google | `GOOGLE_API_KEY`, `GEMINI_API_KEY` | 공식 OpenAI 호환 엔드포인트 연동 |
| zen | `OPENCODE_API_KEY` | |
| freebuff | `FREEBUFF_API_KEY` | 없으면 manicode credentials.json 참조 |

키는 프로세스 환경 변수 또는 `~/.sleepyrouter/.env`에서 읽는다(환경 변수 우선).

레지스트리에 없는 커스텀 소스는 `{SOURCE}_API_KEY` 형식의 환경 변수를 찾는다(예: 소스가 `together`면 `TOGETHER_API_KEY`).

## 개발

```bash
pip install -e ".[dev]"

ruff check .
mypy sleepyrouter
pytest -q
```

## 라이선스

MIT — [LICENSE.md](LICENSE.md)
