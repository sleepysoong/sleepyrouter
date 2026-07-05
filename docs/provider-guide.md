# 프로바이더 가이드

프로바이더 지원, 모델 목록 변경, 무료 모델 필터링, 프로바이더별 요청 동작에 이 경로를 사용하세요. 활성 작업이 프로바이더 구현을 소유하지 않는 한 프로바이더 런타임 코드를 편집하지 마세요.

## 현재 프로바이더 모델

- 소스 앵커: [src/providers/openrouter.ts](../src/providers/openrouter.ts), [src/providers/nvidia.ts](../src/providers/nvidia.ts), [src/providers/copilot.ts](../src/providers/copilot.ts), [src/providers/catalog.ts](../src/providers/catalog.ts).
- `src/providers/catalog.ts`의 `listAvailableFreeModels`는 `src/server/create-server.ts`에서 사용되는 멀티 프로바이더 진입점이에요. 새 프로바이더는 여기에 등록해야 해요.
- OpenRouter 모델 적격성은 `:free` ID 또는 텍스트 출력 지원이 있는 영(0) 프롬프트/완료/요청 가격을 허용해요.
- NVIDIA 모델 적격성은 업스트림 `/v1/models` 목록을 채팅 유사 항목으로 필터링해요: ID, 이름, 유형, 작업, 태그가 비채팅 패턴(임베드/리랭크/오디오/음성/비디오/번역/안전 등)과 일치하면 안 되고, 명시적 `task`가 채팅/생성/완료/인스트럭트로 읽혀야 해요.
- Copilot 모델 적격성은 공식 `/v1/models` 엔드포인트가 없기 때문에 하드코딩된 알려진 무료 모델 목록(gpt-4o, claude-sonnet-4 등)을 사용해요.
- 프로바이더 컨텍스트 길이는 `model-metadata` 브랜치의 원시 메타데이터 카탈로그에서 보강돼요. 카탈로그는 `npm run metadata:update`와 일일 `.github/workflows/update-model-metadata.yml` 워크플로우에서 공개 OpenRouter 및 NVIDIA 메타데이터 엔드포인트에서 새로고침돼요. Copilot 모델은 하드코딩된 컨텍스트 길이를 사용해요.
- NVIDIA와 Copilot 모델은 업스트림 모델 ID를 API 호출에 보존하면서 각각 로컬 `nvidia/` 및 `copilot/` ID로 노출돼요.
- 프로바이더 모델 카탈로그는 5분간 캐시되고, 로컬 모델 ID로 중복 제거되며, 현재 구성된 API 키가 있는 프로바이더로 필터링된 후 재사용돼요. 오래된 카탈로그는 정상 사용 전에 새로고침되고, 프로바이더 카탈로그 가져오기 실패 시에만 오래된 캐시 대체가 사용돼요.
- 프로바이더 요청 헬퍼는 지원되는 곳에서 채팅 완료와 Anthropic 호환 메시지를 전달해요.

## 프로바이더 작업 필수 경로

1. `AGENTS.md`에서 시작하고, `docs/index.md`, 그리고 이 파일을 읽으세요.
2. `research/providers.md`에서 프로바이더 발견사항, 후보 제약조건, 의사결정 기록을 읽으세요.
3. 소스 앵커를 확인하세요:
   - `src/providers`에서 어댑터와 모델 정규화.
   - [scripts/update-model-metadata.mjs](../scripts/update-model-metadata.mjs)에서 프로바이더 메타데이터 보강 (`model-metadata` 브랜치에 게시).
   - [src/server/create-server.ts](../src/server/create-server.ts)에서 선택된 모델 필터링과 요청 전달.
4. 테스트를 확인하세요:
   - `test/openrouter.test.ts`
   - `test/nvidia.test.ts`
   - `test/copilot.test.ts`
   - `test/catalog.test.ts`
   - `test/server.test.ts`의 프로바이더 관련 커버리지
5. 구현 전에 검증을 정의하세요: 프로바이더 단위 테스트, 서버 선택된 모델 필터링, 비밀 처리.

## 계약 검사

- 새 프로바이더는 선택된 모델 허용 목록을 우회하면 안 돼요.
- 새 프로바이더는 무료 또는 텍스트 적격 모델이 어떻게 식별되는지 문서화해야 해요.
- 프로바이더 오류는 로컬이고 실행 가능해야 해요. API 키나 프로바이더 토큰을 출력하지 마세요.
- `/v1/models`를 통해 노출되는 모델 ID는 하위 OpenAI 호환 클라이언트와 호환되어야 해요.

## 업데이트 규칙

프로바이더 적격성, 프로바이더 카탈로그 형태, 자격 증명 처리, 또는 요청 서피스가 변경되면 이 페이지와 `research/providers.md`를 업데이트하세요. 자세한 실험은 연구 또는 의사결정 기록에 보관하세요.
