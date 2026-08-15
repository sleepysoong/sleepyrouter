---
title: handler 중복 제거 (Ponytail ultra)
description: >
  HandleAnthropicMessage 내부의 ProtocolAnthropic 폴백 경로와
  ProtocolOpenAI 경로에서 문자 그대로 중복된 'ChatCompletion 후 응답 처리' 블록을
  추출하고, empty-response guard 로직을 통일합니다.
  PipeOpenAIStreamAsAnthropic 내부의 루프 내 struct 재선언도 hoist.
---

## 배경

- 기존 `.omo/plans/post-rewrite-cleanup.md`의 T4가 `internal/srv/server.go`를 참조하지만,
  실제 핸들러는 이미 `internal/handler/`로 마이그레이션되었습니다.
- `TryModelCandidates` + `ModelAttemptFunc` (`candidates.go`, 78 LOC)는
  이미 양쪽 라우트의 failover 루프를 통일했습니다.
- **남은 중복은 두 라우트의 attempt 클로저 내부에만 존재합니다.**

## 기존 상태 확인

- `go build ./...` — GREEN
- `go test ./...` — GREEN
- characterization 테스트:
  TestServer_OpenAIStreamResponse, TestServer_NVIDIAAnthropicStream,
  TestServer_OpenRouterAnthropicFallback, TestServer_AnthropicAllFailed502,
  TestServer_RejectsEmptyChoicesAndRetries

## 중복 현황

### T1 (HIGH): HandleAnthropicMessage 내 동일 블록 2회 등장

L310-328 (ProtocolAnthropic 폴백 ChatCompletion) 와 L370-388 (ProtocolOpenAI ChatCompletion) 이
문자 그대로 동일한 코드:

```go
t := triedCount
st.LogTriedCount = &t
if st.Stream {
    recordSuccessfulUsage(store, model, nil)
    PipeOpenAIStreamAsAnthropic(upstream.Body, w, modelID)
} else {
    data, err := utils.ResponseJSON(upstream)
    if err != nil {
        return false, err.Error()
    }
    in, out, _ := UsageFromResponse(data)
    st.LastInputTokens = in
    st.LastOutputTokens = out
    recordSuccessfulUsage(store, model, data)
    WriteJSON(w, upstream.StatusCode, protocol.OpenAIToAnthropic(data, modelID))
}
return true, ""
```

→ `finishChatCompletionAsAnthropic(ctx, w, store, model, upstream, modelID, st, triedCount) (bool, string)`로 추출.

### T2 (MEDIUM): empty-response guard 통합

- OpenAI 라우트 (L268-276): `choices`만 빈 검사
- Anthropic 라우트 Messages 직접 경로 (L342-352): `choices` OR `content` 빈 검사

둘 다 `store.AppendUsage(..., Success: false)` + 에러 문자열 반환이 동일.
헬퍼로는 recordEmptyFailure 만들고, 사용 지점에서 분기 조건만 선택:
- OpenAI: `len(choices) == 0`
- Anthropic: `!hasChoicesArr && !hasContentArr`

`go test ./...`로 확인하며 회추가로 테스트 추가하지 않음
(기존 characterization 테스트로 충분합니다).

### T3 (LOW): PipeOpenAIStreamAsAnthropic 루프 내 struct 재선언

L205-224의 `openAIStreamToolCall`, `openAIStreamChoice` 타입이
`for scanner.Scan()` 루프 **안에서** 매 루프마다 재선언됩니다.
의미상 아무런 동작 변화 없음 한, 패키지 레벨로 올리면 불필요한 할당 제거.

## 제한 조건

- 기능 동작하지 않음 (Ponytail ultra: 수체 가장 심플한 조치)
- `ReadBody`/`panic` 기반 경로**는** 전혀 만지지 않음 (기 존 계획 금지)
- `internal/srv/server_test.go`의 characteriziation 테스트가 레션 검사
- 새 테스트, 새 파일, 새 추상화는 만들지 않음
- 추출하는 헬퍼는 모두 `internal/handler/` 내에 unexported로 정의