# Codex Start Here

## 목표

이 프로젝트는 Go 서버와 C raylib/WebAssembly 클라이언트로 구성되는 실시간 온라인 윷놀이 게임이다. 첫 구현에서는 전체 제품을 한 번에 만들지 말고, 아래의 수직 기술 검증과 순수 규칙 엔진부터 완성한다.

## Phase 0 — 명세 검증

먼저 다음을 수행한다.

1. 모든 `docs/`, `spec/`, `schemas/`, `CONTRIBUTING.md`를 읽는다.
2. 충돌·누락·구현 불가능 요소를 `docs/12_open_items.md`에 추가한다.
3. 채택된 `docs/adr/`를 읽고 아직 미결정인 다음 ADR만 작성한다.
   - 서버 디렉터리 구조
   - WebSocket 라이브러리
   - v1 DB는 `docs/adr/0001_sqlite_v1_and_scale_out.md`를 따르며, 그 전환 조건이 실제로 충족될 때만 후속 DB 전환 ADR을 작성한다.
4. `spec/board_graph.yaml`의 전체 노드·연결 무결성을 검사하고 그래프 로더 테스트를 작성한다.
5. `docs/development/git-workflow.md`와 `docs/development/github-settings.md`의
   bootstrap 조건과 필수 CI를 확인한다.

bootstrap이 끝나기 전에는 제품 기능 구현을 시작하지 않는다. bootstrap 종료 뒤에는
모든 변경을 작업 브랜치와 PR로 제출하며 `main`에 직접 커밋하지 않는다.

## Phase 1 — 수직 기술 검증

이 단계는 브라우저·인증·통신 경계의 위험만 확인하는 폐기 가능한 기술 스파이크다. 제품 규칙이나 임시 보드 구조를 제품 코드로 구현하지 않으며, 실제 제품 코드의 기능 PR 순서는 `AGENTS.md`와 `docs/13_implementation_plan.md`를 따른다.

성공 조건:

- 브라우저에서 raylib WASM 실행
- Google 로그인
- 서버 자체 30일 세션 발급
- 인증된 WebSocket 연결
- 한글 닉네임·채팅 입력
- 방 생성 및 두 클라이언트 입장
- 서버가 윷 결과 생성
- 두 클라이언트에 동일 이벤트 순서 전달
- 새로고침 후 기존 방·자리 복구

이 단계에서는 `spec/board_graph.yaml`을 실제 보드 정본으로 사용한다. 임시 단일 경로 보드는 고립된 단위 테스트 fixture로만 허용한다.

## Phase 2 — 순수 규칙 엔진

다음 모듈을 UI·네트워크 없이 구현한다.

- 방 설정 검증
- 윷 결과 추첨
- FIFO 결과 큐
- 윷/모 추가 던지기
- 잡기 즉시 추가 던지기
- 북 장벽 토큰 처리
- 말 상태와 위치 그룹
- 업기·잡기
- 완주 체크포인트
- CPU 선택 정책
- 타이머 만료에 따른 현재 턴 CPU 대체

## Phase 3 — 전체 보드 통합

- `spec/board_graph.yaml` 로더와 검증
- 선택/강제 지름길
- 중앙 방 진입·진출
- 경로 이력 기반 백도 역행
- 북 후보 위치 검증
- 참고 좌표 기반 raylib 렌더링
- 보드 그래프 전체 경로 테스트

## 완료 정의

- 명세의 대표 시나리오가 자동 테스트로 표현됨
- 테스트 모드에서 시드 재현 가능
- 명령 중복이 멱등 처리됨
- 서버 이벤트에 단조 증가 `sequence`가 있음
- 코드 변경과 명세 변경이 서로 추적 가능
