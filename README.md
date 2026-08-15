# Online Yutnori — Project Specification Pack

이 디렉터리는 웹 기반 실시간 멀티플레이 윷놀이 게임의 구현 정본(canonical specification)이다.

## 기술 방향

- 게임 서버: Go
- 게임 클라이언트: C로 구현하는 raylib 기반 WebAssembly
- 인증: Google 로그인 검증 후 서버 자체 30일 세션
- 실시간 통신: 서버 권위형 WebSocket
- 일반 API: HTTPS 기반 HTTP API
- 초기 배포: 단일 VPS의 Docker 구성
- v1 데이터베이스: 같은 호스트의 로컬 영구 볼륨을 사용하는 SQLite
- 초기 목표 동시 접속자: 약 10~50명

## 읽는 순서

1. `AGENTS.md`
2. `docs/00_authority_and_status.md`
3. `docs/01_product_scope.md`
4. `docs/02_game_rules.md`
5. `docs/03_turn_queue_state_machine.md`
6. `docs/04_board_graph_and_movement.md`
7. 나머지 설계 문서
8. `CODEX_START_HERE.md`

기여와 Git 운영 절차는 `CONTRIBUTING.md`와
`docs/development/git-workflow.md`를 따른다.

## 정본 우선순위

충돌이 있을 때 다음 우선순위를 적용한다.

1. `docs/`의 정제된 명세
2. `spec/`의 기계 판독용 정책
3. `schemas/`의 메시지 및 설정 스키마
4. `docs/reference/decision_addendum.md`
5. `docs/reference/requirements_qa_finished.md`

Q&A 원본은 결정의 근거를 추적하기 위한 참고자료이며, 구현 시에는 정제된 명세를 우선한다.

## 윷판 정본

전체 윷판은 `assets/board_reference/yut_board_reference.png`의 구조를 따르며, `참먹이`를 출발점으로 사용한다. 기계 판독 가능한 정본은 `spec/board_graph.yaml`이다.

논리 연결은 확정되었으며, 렌더링 좌표는 참고 그림에서 추정한 초기값이므로 UI 구현 중 조정할 수 있다.
