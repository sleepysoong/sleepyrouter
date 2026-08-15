---
slug: post-rewrite-cleanup
status: awaiting-approval
intent: clear
pending-action: write .omo/plans/post-rewrite-cleanup.md
approach: 분석에서 확인된 5개 이슈를 우선순위대로 5개 태스크(태스크당 1커밋+푸쉬)로 수정. TDD(버그 수정) + characterization test(리팩토링).
---

# Draft: post-rewrite-cleanup

## Components (topology ledger)
- C1 copilot-validation-fix | start가 copilot/ 모델을 허용 | active | internal/cli/start_cmd.go:71
- C2 dead-code-purge | 미사용 패키지 4개+빈 디렉토리 1개+함수 1개 삭제, 빌드 그린 | active | internal/{srlog,usagelog,httpx,srv/selection,srv/router}
- C3 routing-reason-api | OrderedCandidates가 reason 반환, 서버 로그 정확화, Choose* 삭제 | active | internal/routing/router.go, internal/srv/server.go:194,328
- C4 server-dedup | 두 채팅 핸들러의 공통 failover 루프 단일화 | active | internal/srv/server.go
- C5 docs-sync | 문서 7파일을 코드 현실과 일치 | active | docs/*.md, README.md

## Open assumptions (announced defaults)
- npm 설치 지시문 | `go install github.com/sleepysoong/sleepyrouter/cmd/sleepyrouter@latest` + 소스 빌드로 교체 | package.json 부재(레포에서 npm 배포 불가) | reversible(문서)
- panic 기반 readBody 에러 처리 | 유지 (동작함, 리라이트 직후 구조 변경 리스크 회피) | ponytail: 작동하는 코드는 건드리지 않음 | reversible
- 요청당 config.json 디스크 읽기 | 유지 (로컬 프록시, 실害 없음) | YAGNI | reversible
- ChooseModel/ChooseGroupedModel 삭제 | C3에서 OrderedCandidates가 reason을 반환하게 한 뒤 삭제, 테스트는 새 API로 동일 assertion 유지 | 커버리지 손실 없음 | reversible(git)

## Findings (cited - path:lines)
- start_cmd.go:71 — 검증이 `nvidia/`, `openrouter/`만 허용. `copilot/` 누락. os.Exit(1) 직접 호출(63-83)이라 테스트 불가 → 순수 함수 추출 필요.
- srv/server_test.go:279-282 — copilot upstream ID는 이미 지원(TestModelUpstreamID_MultiSlash). 검증만 누락 확인.
- 죽은 코드: internal/srlog/, internal/usagelog/, internal/httpx/, internal/srv/selection/ — import 0건(테스트 포함 전체 grep). internal/srv/router/ — 빈 디렉토리. model_selection.go:210 writeOpenAIAsAnthropic — 호출자 0건.
- router.go:39-67 ChooseModel/ChooseGroupedModel — 프로덕션 호출 0건, router_test.go에서만 사용(8곳).
- server.go:167,301 — OrderedCandidates 프로덕션 호출 2곳. server.go:194,328 — routeReason 하드코딩 "fallback-order".
- routing.RouteReason 상수 존재(router.go:11-14): RouteModelGroup="model-group", RouteFallbackOrder="fallback-order". request_logging.go:110-111이 route= 로그 출력.
- stale 문서: architecture.md(5곳), latency-routing.md(2곳), provider-guide.md(2곳), client-compatibility.md(5곳) — 모두 internal/core/* 참조.
- README.md(루트):43,52-53,96 + docs/README.md:39,49-50,84 + docs/INSTALLATION.md:10,23,28 — npm install 지시 + 존재하지 않는 status/doctor 명령 안내. package.json 없음.
- 실제 CLI 명령(cli/main.go:63-98): start, usage, --version, help뿐.
- server_test.go에 characterization 커버리지 이미 존재: health, count_tokens, models, non-free 400, OpenAI chat+usage, NVIDIA anthropic, empty-choices retry.

## Decisions (with rationale)
- C3 API 형태: `OrderedCandidates(...) ([]string, RouteReason)` — 후보와 사유를 한 결정에서 반환. Choose* 대체 가능해지며 서버가 실제 사유 로깅. 테스트는 기계적 갱신.
- C1: `invalidModelIDs(groups types.ModelGroups) []string` 순수 함수 추출 → 단위 테스트 가능 → RunStartCommand에서 호출. 허용 접두사에 copilot/ 추가 + 에러 메시지 갱신.
- C4: 두 핸들러의 후보 순회/usage 기록/에러 처리를 공통 함수로 추출. 스트림/비스트림, OpenAI/Anthropic 응답 차이는 콜백으로 분리. characterization test(server_test.go) 그린 유지가 게이트.
- 실행 순서: Wave1 [C1, C2, C5] 병렬(파일 무충돌) → Wave2 [C3] → Wave3 [C4] (C3, C4는 server.go 공유로 직렬).

## Scope IN
- C1 copilot 접두사 버그 수정 + 테스트
- C2 죽은 코드 삭제(패키지 4, 빈 디렉토리 1, 함수 1)
- C3 라우팅 reason API + 서버 로그 수정 + Choose* 삭제 + 테스트 갱신
- C4 server.go 핸들러 중복 제거 리팩토링
- C5 문서 7파일 현실화(architecture, latency-routing, provider-guide, client-compatibility, README×2, INSTALLATION)
- 태스크당 커밋+푸쉬 (origin main)

## Scope OUT (Must NOT have)
- status/doctor 명령 신규 구현 (별도 결정 전까지 — 질문 1 참조)
- latency 기반 라우팅 신규 구현
- panic 기반 에러 처리 개편, config 읽기 캐싱
- provider 추가/변경, 프로토콜 번역 로직 변경
- npm 배포 파이프라인 구축

## Open questions
- Q1: RESOLVED — 문서에서 제거 선택됨 (사용자 승인 2026-07-18)

## Approval gate
status: approved
pending action: Metis 갭 분석 → .omo/plans/post-rewrite-cleanup.md 작성 → 실행(사용자가 ultrawork 사전 승인)
