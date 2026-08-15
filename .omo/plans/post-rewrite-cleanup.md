# post-rewrite-cleanup - Work Plan

## TL;DR (For humans)

**What you'll get:** Copilot 모델이 설정에 있어도 서버가 정상 기동되고, 죽은 코드가 제거되고, 서버 로그가 실제 라우팅 경로(그룹 매칭 vs 기본 순서)를 정확히 표시하며, 중복된 요청 처리 코드가 하나로 정리되고, 문서가 Go 리라이트의 현실과 일치하게 됩니다.

**Why this approach:** 버그 수정은 테스트를 먼저 쓰고(TDD), 리팩토링은 현재 동작을 고정하는 테스트부터 추가한 뒤 손대기 때문에 리라이트 직후 코드를 안전하게 정리할 수 있습니다. 각 작업은 커밋 전에 빌드·정적검사·테스트를 통과해야 푸쉬됩니다.

**What it will NOT do:** README에만 있던 status/doctor 명령을 새로 만들지 않고(문서에서 제거), latency 기반 라우팅이나 에러 처리 개편 같은 구조 변경도 하지 않습니다.

**Effort:** Medium
**Risk:** Low - 모든 변경이 테스트 게이트를 통과해야 하고, 가장 큰 리팩토링은 characterization 테스트가 선행됨
**Decisions to sanity-check:** status/doctor는 구현 대신 문서에서 제거(승인됨), npm 설치 지시는 go install로 교체(실제 동작 확인됨)

Your next move: 플랜대로 실행 시작 (사용자가 ultrawork 사전 승인). Full execution detail follows below.

---

> TL;DR (machine): Medium effort, Low risk — 6 tasks: copilot 검증 버그 수정(TDD), 죽은 코드 삭제, 라우팅 사유 로그 수정, characterization 테스트+핸들러 중복 제거, 문서 동기화; 태스크당 커밋+푸쉬, 최종 검증 웨이브 포함

## Scope
### Must have
1. **T1**: `copilot/` 모델 접두사가 start 검증을 통과 (TDD: 순수 함수 추출 + 단위 테스트)
2. **T2**: 죽은 코드 삭제 — 패키지 4개(`internal/srlog`, `internal/usagelog`, `internal/httpx`, `internal/srv/selection`), 빈 디렉토리 `internal/srv/router`, 함수 `writeOpenAIAsAnthropic`
3. **T3**: `routing.OrderedCandidates`가 `RouteReason` 반환 → 서버 로그가 실제 라우팅 사유 표시. `ChooseModel`/`ChooseGroupedModel`/`RouteChoice` 삭제
4. **T4**: server.go 두 채팅 핸들러의 failover 루프 단일화 (T4a characterization 테스트 선행)
5. **T5**: 문서 7파일을 Go 리라이트 현실과 동기화 (24곳 stale 경로 + npm 설치 + 미구현 status/doctor 제거)
6. 태스크당 1커밋+푸쉬 (origin main), 각 커밋 전 build+vet+test 그린 게이트

### Must NOT have (guardrails, anti-slop, scope boundaries)
- `status`/`doctor` 명령 신규 구현 금지 (사용자가 문서 제거 선택)
- latency 기반 라우팅 신규 구현 금지
- panic 기반 readBody 에러 처리 개편 금지 (동작 중, 리스크만 큼)
- 요청당 config.json 읽기 캐싱 금지 (로컬 프록시, 실害 없음 — YAGNI)
- provider 추가/변경, 프로토콜 번역 로직 변경 금지
- npm 배포 파이프라인 구축 금지
- T4b에서 동작 변경 금지 (순수 리팩토링; characterization 테스트가 그린 유지되는 것이 증거)
- `.omo/` 디렉토리는 커밋하지 않음 (로컬 플래닝 아티팩트)

## Verification strategy
> Zero human intervention - all verification is agent-executed.
- Test decision: **T1=TDD (RED→GREEN)**, **T4a=characterization tests-first (기존 코드에서 GREEN 확인 후 T4b)**, **T2/T5=삭제/문서 (기존 테스트 그린 + grep 게이트)**, **T3=기존 테스트 기계적 갱신 + 신규 서버 레벨 테스트**. Framework: Go 표준 `testing`.
- **Per-commit gate (모든 태스크 공통)**: `go build ./... && go vet ./... && go test ./...` 그린 확인 후에만 push. 깨진 중간 커밋 방지 (Metis #4).
- Evidence: .omo/evidence/task-<N>-post-rewrite-cleanup.<ext>

## Execution strategy
### Parallel execution waves
- **Wave 1** (병렬, git 명령 금지 — 오케스트레이터가 완료 후 순차 커밋/푸쉬): T1(cli), T2(삭제), T5(docs) — 파일 집합 무교차
- **Wave 2**: T3 (routing + srv/server.go)
- **Wave 3**: T4a → T4b (같은 파일 server.go, 순차)
- **Final**: F1-F4 병렬 검증

git index race 방지: 병렬 에이전트는 절대 git 명령을 실행하지 않고, 변경 파일 목록만 보고. 오케스트레이터가 태스크별 경로만 `git add <paths>` 해 순차 커밋+푸쉬.

### Dependency matrix
| Todo | Depends on | Blocks | Can parallelize with |
| --- | --- | --- | --- |
| 1 (T1) | — | — | 2, 3 |
| 2 (T2) | — | 4 (soft: 같은 srv 패키지 정리) | 1, 3 |
| 3 (T5) | — | — | 1, 2 |
| 4 (T3) | 2 (soft) | 5, 6 | — |
| 5 (T4a) | 4 | 6 | — |
| 6 (T4b) | 5 | F1-F4 | — |

## Todos
> Implementation + Test = ONE todo. Never separate.
<!-- APPEND TASK BATCHES BELOW THIS LINE WITH edit/apply_patch - never rewrite the headers above. -->
- [ ] 1. T1: copilot/ 접두사 검증 버그 수정 (TDD)
  What to do / Must NOT do: (RED) `internal/cli/start_cmd_test.go` 신규 작성 — `invalidModelIDs(groups types.ModelGroups) []string` 테스트: ① `copilot/gpt-4o` 포함 시 빈 결과(허용) ② `nvidia/`, `openrouter/` 허용 ③ 접두사 없는 ID는 `"그룹명: id"` 형태로 반환. 실행해 실패 확인(함수 미존재). (GREEN) `internal/cli/start_cmd.go`에 순수 함수 `invalidModelIDs` 추출 — `strings.HasPrefix`로 `nvidia/`, `openrouter/`, `copilot/` 3개 허용. 에러 메시지(:77)를 "nvidia/, openrouter/ 또는 copilot/ 접두사가 필요해요"로 갱신. 로컬 `startsWith` 헬퍼(:127-129) 삭제. `RunStartCommand`의 인라인 루프(:63-83)가 새 함수 호출하도록 교체(os.Exit 유지). Must NOT: 검증 외 동작 변경, 다른 파일 수정, git 명령 실행.
  Parallelization: Wave 1 | Blocked by: — | Blocks: —
  References: internal/cli/start_cmd.go:62-83 (검증 루프), :77 (에러 메시지), :127-129 (startsWith), internal/srv/server_test.go:279-282 (copilot이 downstream에서 이미 지원됨의 증거), internal/providers/copilot.go (provider 존재), internal/cli/usage_test.go (cli 테스트 스타일 참조)
  Acceptance criteria: `go test ./internal/cli/ -run TestInvalidModelIDs -v` 그린 + `go build ./... && go vet ./... && go test ./...` 전부 그린 + `grep -n "startsWith" internal/cli/start_cmd.go` 결과 0건
  QA scenarios: happy=빌드된 바이너리로 copilot/ 모델 포함 임시 config → `sleepyrouter start --port 14567`이 검증 통과(서버 기동, SIGTERM으로 종료). failure=접두사 없는 모델 config → exit 1 + copilot/ 포함한 새 에러 메시지 출력. Evidence .omo/evidence/task-1-post-rewrite-cleanup.txt
  Commit: Y | fix(cli): allow copilot/ prefix in start model validation

- [ ] 2. T2: 죽은 코드 삭제
  What to do / Must NOT do: 사전 체크 `grep -rn "srlog\|usagelog\|internal/httpx\|srv/selection" --include="*.md" --include="*.yml" --include="*.yaml" --include="*.json" /root/sleepyrouter` (비-Go 참조 확인, 결과 기록). 삭제: `internal/srlog/`, `internal/usagelog/`, `internal/httpx/`, `internal/srv/selection/` 디렉토리 통째로, `internal/srv/router/` (빈 디렉토리 — git diff 없음, 로컬 rm), `internal/srv/model_selection.go`에서 `writeOpenAIAsAnthropic` 함수(:210부터 함수 끝까지)와 전용 import 정리. 참고: `srv/model_selection.go`가 LIVE copy, `srv/selection/selection.go`가 죽은 near-duplicate (검증됨: selection importer 0건, server.go는 model_selection.go의 selectedModelSelection 사용). Must NOT: 다른 함수/파일 수정, git 명령 실행.
  Parallelization: Wave 1 | Blocked by: — | Blocks: 4 (soft)
  References: internal/srlog/srlog.go, internal/usagelog/usagelog.go, internal/httpx/httpx.go, internal/srv/selection/selection.go, internal/srv/model_selection.go:210 (writeOpenAIAsAnthropic)
  Acceptance criteria: `go build ./... && go vet ./... && go test ./...` 전부 그린 + `grep -rn "sleepyrouter/internal/srlog\|sleepyrouter/internal/usagelog\|sleepyrouter/internal/httpx\|sleepyrouter/internal/srv/selection\|writeOpenAIAsAnthropic" --include="*.go" /root/sleepyrouter` 결과 0건
  QA scenarios: happy=전체 테스트 스위트 그린. failure=해당 없음(삭제 작업). Evidence .omo/evidence/task-2-post-rewrite-cleanup.txt
  Commit: Y | chore: remove dead packages and unused function

- [ ] 3. T5: 문서 7파일 현실화
  What to do / Must NOT do: ① stale `internal/core/*` 참조 24곳(architecture.md 9, client-compatibility.md 8, provider-guide.md 5, latency-routing.md 2)을 매핑표대로 수정: core/server.go→internal/srv/server.go, core/translate.go→internal/srv/translate.go, core/sse.go→internal/sseutil/sseutil.go, core/router.go→internal/routing/router.go, core/router_test.go→internal/routing/router_test.go, core/config.go→internal/cfg/config.go, core/{openrouter,nvidia,copilot,catalog}.go→internal/providers/*. ② TS-era 심볼 `listAvailableFreeModels`(architecture.md) → 실제 Go 심볼로 교체 (providers/catalog.go의 exported 함수명 확인 후). ③ README.md(루트) + docs/README.md에서 `status`/`doctor` 행 삭제, 컨텍스트 티어 문장(README.md:96, docs/README.md:84)은 status 언급 없이 리워딩(예: 기동 시 모델 그룹 출력 또는 models-cache 참조). ④ npm 설치 지시 전부 교체: `go install github.com/sleepysoong/sleepyrouter/cmd/sleepyrouter@latest` (2026-07-18 실증 성공) + 소스 빌드 `git clone https://github.com/sleepysoong/sleepy-llm-router && cd sleepy-llm-router && go install ./cmd/sleepyrouter`. 개발자 섹션의 `npm install && npm run build`/`npm pack`/`npm link` → `go build ./...`/`go install ./cmd/sleepyrouter`. 대상: README.md, docs/README.md, docs/INSTALLATION.md, docs/architecture.md, docs/latency-routing.md, docs/provider-guide.md, docs/client-compatibility.md. Must NOT: Go 코드 수정, 문서 링크가 실제 존재하지 않는 파일을 가리키는 것, git 명령 실행.
  Parallelization: Wave 1 | Blocked by: — | Blocks: —
  References: docs/architecture.md:10-13, docs/latency-routing.md:28-29, docs/provider-guide.md:7,24, docs/client-compatibility.md:9-13, README.md:43,52-53,96, docs/README.md:39,49-50,84, docs/INSTALLATION.md:10,23,28, internal/cli/main.go:63-98 (실제 명령: start/usage/version/help)
  Acceptance criteria: `grep -rn "internal/core" /root/sleepyrouter/docs /root/sleepyrouter/README.md` 0건 + `grep -rni "npm" /root/sleepyrouter/docs /root/sleepyrouter/README.md` 0건 + `grep -rn "sleepyrouter status\|sleepyrouter doctor" /root/sleepyrouter/docs /root/sleepyrouter/README.md` 0건 + 문서 내 모든 상대 링크가 존재하는 파일을 가리킴 (스크립트로 검증)
  QA scenarios: happy=위 grep 게이트 전부 0건 + 링크 존재 검증 통과. failure=해당 없음. Evidence .omo/evidence/task-3-post-rewrite-cleanup.txt
  Commit: Y | docs: sync documentation with Go rewrite reality

- [ ] 4. T3: 라우팅 사유 API + 서버 로그 정확화
  What to do / Must NOT do: ① `internal/routing/router.go`: `CandidateIDs`가 `([]string, RouteReason)` 반환 — normalized 그룹명이 groups에 매칭되면 `RouteModelGroup`, 아니면 `RouteFallbackOrder` (기존 Choose*의 reason 로직 :44-50과 동일). `OrderedCandidates`도 `([]string, RouteReason)` 반환(슬라이스 복사 유지). `ChooseModel`, `ChooseGroupedModel`, `RouteChoice` struct 삭제. `RouteReason` 타입+상수는 유지. ② `internal/srv/server.go`: :167,:301 호출을 새 시그니처로 갱신하고 반환된 reason을 사용, :194,:328의 `routeReason = "fallback-order"` 하드코딩 삭제. ③ `internal/routing/router_test.go`: 8곳 call site 기계적 갱신 — 값 assertion(ids[0], reason)은 동일 유지, error check는 삭제(에러 반환 없어짐), ChooseModel 호출은 defaultGroup="" 인자 추가. ④ 신규 서버 레벨 테스트(server_test.go): RequestLogger로 ServerLogEvent 캡처 — 명시적 그룹명 요청 시 `RouteReason == "model-group"`, "auto" 요청 시 `"fallback-order"` assertion. Must NOT: 라우팅 매칭 로직 자체 변경, 다른 핸들러 로직 수정, git 명령 실행.
  Parallelization: Wave 2 | Blocked by: 2 (soft) | Blocks: 5, 6
  References: internal/routing/router.go:9-67 (전체), internal/srv/server.go:167,194,301,328, internal/srv/request_logging.go:36,110-111, internal/routing/router_test.go (8 call sites), internal/srv/server_test.go:48-62 (RequestLogger 하네스)
  Acceptance criteria: `go test ./internal/routing/ ./internal/srv/ -v` 그린 + `go build ./... && go vet ./... && go test ./...` 전부 그린 + `grep -n "ChooseModel\|ChooseGroupedModel\|RouteChoice" --include="*.go" -r /root/sleepyrouter` 결과 0건 + 신규 서버 테스트가 model-group/fallback-order 양쪽 검증
  QA scenarios: happy=신규 서버 레벨 테스트 통과(그룹 요청→model-group 로그). failure=라우팅 테스트 전부 그린(매칭 동작 무변경 증거). Evidence .omo/evidence/task-4-post-rewrite-cleanup.txt
  Commit: Y | fix(routing): log actual route reason instead of hardcoded fallback-order

- [ ] 5. T4a: server.go 미커버 경로 characterization 테스트
  What to do / Must NOT do: `internal/srv/server_test.go`(또는 신규 server_characterization_test.go)에 현재 동작을 고정하는 테스트 추가: ① OpenAI 스트리밍 — POST /v1/chat/completions stream:true → SSE chunk 흐름(mock upstream이 SSE 반환) ② Anthropic 스트리밍 — POST /anthropic/v1/messages stream:true → anthropic SSE 이벤트 ③ ProtocolAnthropic 분기 — openrouter/ 모델로 /anthropic/v1/messages 호출 → upstream /messages 엔드포인트 호출됨; upstream이 404/405 반환 시 ChatCompletion fallback + 번역 동작 ④ 502 전부 실패 — 모든 후보 실패 시 502 + 라우트별 body 형태(anthropic 라우트는 "type":"api_error" 포함) ⑤ missing-key skip — 키 없는 provider 후보는 건너뛰고 다음 후보 시도. 이 테스트들은 **현재 코드에서 GREEN이어야 함**(characterization). 하나라도 현재 코드에서 실패하면 = 기존 버그 발견 → 수정하지 말고 중단 보고. Must NOT: 프로덕션 코드 일절 수정, git 명령 실행.
  Parallelization: Wave 3 | Blocked by: 4 | Blocks: 6
  References: internal/srv/server.go:144-275 (OpenAI 핸들러), :278-475 (Anthropic 핸들러, ProtocolAnthropic 분기 :343-435), internal/srv/server_test.go:18-96 (테스트 하네스: withTestServerHandler, testRequest, tempServerStore, mockResponse, utils.HTTPClientFunc), internal/srv/streams.go, internal/sseutil/sseutil.go
  Acceptance criteria: 신규 테스트 전부 현재 코드에서 GREEN + `go build ./... && go vet ./... && go test ./...` 전부 그린
  QA scenarios: happy=5개 characterization 테스트 GREEN. failure=현재 코드에서 실패 시 중단 보고(버그 발견). Evidence .omo/evidence/task-5-post-rewrite-cleanup.txt
  Commit: Y | test(srv): characterize streaming, anthropic-protocol, and failure paths

- [ ] 6. T4b: server.go 핸들러 failover 루프 단일화
  What to do / Must NOT do: 두 핸들러(POST /v1/chat/completions ~132줄, POST /anthropic/v1/messages ~198줄)의 공통 후보 순회/usage 기록/에러 수집 루프를 공통 함수로 추출. 변동점 5개는 파라미터/콜백으로 분리: ① 프로토콜 분기(MessageProtocol, OpenRouter만 Anthropic 프로토콜) ② 404/405 Messages→ChatCompletion fallback+번역 ③ 스트림 파이프(PipeWebStreamToNode vs PipeOpenAIStreamAsAnthropic) ④ 빈 응답 검사(choices만 vs choices+content) ⑤ 502 body 형태(anthropic 라우트만 "type":"api_error"). 순수 리팩토링 — 동작 변경 금지. T4a 테스트+기존 테스트가 전 과정 그린 유지가 증거. Must NOT: 동작 변경, 테스트 수정(그린 유지를 위한 것 제외하고도 원칙적으로 테스트는 무수정), git 명령 실행.
  Parallelization: Wave 3 | Blocked by: 5 | Blocks: F1-F4
  References: internal/srv/server.go:144-475 (두 핸들러 전체), internal/srv/model_selection.go (withUpstreamModel, usageFromResponse, recordSuccessfulUsage/recordUpstreamFailure), T4a가 추가한 characterization 테스트
  Acceptance criteria: `go build ./... && go vet ./... && go test ./...` 전부 그린 + server.go 순 라인 수 감소 + 두 핸들러가 공통 루프 호출로 위임
  QA scenarios: happy=전체 테스트 스위트 그린(특히 T4a 5개). failure=하나라도 깨지면 리팩토링 중단·되돌리기. Evidence .omo/evidence/task-6-post-rewrite-cleanup.txt
  Commit: Y | refactor(srv): unify chat handler failover loop

## Final verification wave
> Runs in parallel after ALL todos. ALL must APPROVE. Surface results and wait for the user's explicit okay before declaring complete.
- [ ] F1. Plan compliance audit — 6개 태스크 산출물이 플랜대로인지 diff 기반 감사
- [ ] F2. Code quality review — oracle 에이전트로 변경분 리뷰
- [ ] F3. Real manual QA — 빌드 바이너리로 curl 매트릭스 실행 (환경: NVIDIA_API_KEY만 존재): ① GET /health→200 ② GET /v1/models→묵료 모델 목록 ③ POST /v1/chat/completions(non-stream)→200 + nvidia 모델 라우팅(openrouter 후보는 키 없어 skip되는 실제 failover 관측) ④ POST /v1/chat/completions(stream)→SSE chunk ⑤ POST /anthropic/v1/messages→200 anthropic 형태 ⑥ 로그에 route=model-group 또는 fallback-order 정확 표시 ⑦ copilot/ 모델 포함 config로 start 성공(T1 검증). Evidence .omo/evidence/final-qa-post-rewrite-cleanup.txt
- [ ] F4. Scope fidelity — Must NOT have 목록 위반 여부 확인

## Commit strategy
태스크당 1커밋+푸쉬, origin main, 순차 진행. 각 커밋 전 `go build ./... && go vet ./... && go test ./...` 그린 게이트. 병렬 웨이브의 에이전트는 git 명령 금지 — 오케스트레이터가 태스크별 경로만 선별 `git add` 해 순차 커밋. 커밋 메시지는 각 todo의 Commit 라인. `.omo/`는 커밋 제외.

## Success criteria
- 6개 태스크 커밋+푸쉬 완료, 각 커밋이 게이트 통과
- copilot/ 모델 포함 config로 `sleepyrouter start` 성공
- 서버 로그가 그룹 매칭 시 route=model-group 표시
- `grep -rn "internal/core" docs/ README.md` 0건, npm/status/doctor 언급 0건
- 죽은 코드 0건 (grep 게이트)
- server.go 핸들러 중복 제거 + characterization 테스트 포함 전체 스위트 그린
- Final verification wave F1-F4 전부 APPROVE
