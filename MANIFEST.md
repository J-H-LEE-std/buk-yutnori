# Manifest

## Root
- `README.md`: 문서 인덱스
- `AGENTS.md`: 에이전트 작업 규칙
- `CONTRIBUTING.md`: 기여와 Git workflow 요약
- `CODEX_START_HERE.md`: 첫 구현 절차
- `requirements-ci.txt`: 문서·명세 CI Python 의존성
- `REPORT.md`: 정합성 수정 전 검토 보고서
- `REPORT_RESOLUTION.md`: 확정 결정 반영 및 검증 결과

## docs
- `00_authority_and_status.md`: 요구사항 상태와 우선순위
- `01_product_scope.md`: 제품 범위
- `02_game_rules.md`: 정본 게임 규칙
- `03_turn_queue_state_machine.md`: 턴·큐 상태 머신
- `04_board_graph_and_movement.md`: 보드 그래프 및 이동
- `05_room_match_lifecycle.md`: 방과 경기 생명주기
- `06_network_reconnection.md`: 네트워크와 재접속
- `07_auth_profiles_data.md`: 인증·프로필·전적
- `08_chat_spectator_admin.md`: 채팅·관전·관리
- `09_client_ui.md`: 클라이언트 UI
- `10_deployment_operations.md`: 배포와 운영
- `11_test_plan.md`: 테스트 계획
- `12_open_items.md`: 남은 기술·콘텐츠 결정
- `13_implementation_plan.md`: 단계별 구현 계획
- `14_glossary.md`: 용어
- `adr/0001_sqlite_v1_and_scale_out.md`: v1 SQLite 선택과 PostgreSQL 전환 조건
- `adr/0002_command_idempotency_and_event_commit.md`: 멱등 처리와 DB 저장 후 이벤트 확정
- `adr/0003_start_confirmation_and_pause_timers.md`: 10초 시작 확인과 타이머 보존 일시 정지
- `adr/0004_c_raylib_wasm_client.md`: C raylib/WASM 클라이언트와 브라우저 경계
- `adr/0005_http_google_auth_session_boundary.md`: Go HTTP Google 인증과 자체 세션 경계
- `development/git-workflow.md`: 브랜치·커밋·PR·리뷰·병합·릴리스 정본
- `development/github-settings.md`: bootstrap과 GitHub 저장소 설정 초안

## GitHub

- `.github/pull_request_template.md`: PR 분류·검증·리뷰 체크리스트
- `.github/ISSUE_TEMPLATE/meaningful-change.yml`: 의미 있는 변경과 고위험 변경 추적용 Issue 템플릿
- `.github/ISSUE_TEMPLATE/config.yml`: Issue 템플릿 설정
- `.github/workflows/pr-policy.yml`: 브랜치·PR·리뷰 증빙 정책 검사
- `.github/workflows/spec-validation.yml`: 문서·스키마·보드·Go 검사와 필수 집계 체크
- `.github/workflows/release-tag-policy.yml`: annotated SemVer 마일스톤 태그 검사

## tools

- `tools/check_pr_policy.py`: PR 정책 검사기
- `tools/validate_docs.py`: Markdown 기본 구조와 로컬 링크 검사기
- `tools/validate_specs.py`: YAML·JSON Schema·보드 검증기

## client

- `client/Makefile`: C 네이티브 테스트와 raylib WebAssembly 빌드
- `client/README.md`: 로컬 테스트·WASM 빌드 절차와 고정 도구 버전
- `client/include/buk_client/bridge.h`: JavaScript에 공개하는 좁은 C ABI
- `client/include/buk_client/state.h`: 브라우저 입력을 받는 표시 상태 계약
- `client/src/state.c`: raylib와 분리된 표시 상태 구현
- `client/src/main.c`: raylib 프레임 콜백과 WASM 공개 함수
- `client/tests/browser_input_test.mjs`: Chrome의 DOM 편집과 C/WASM 입력 동기화 회귀 테스트
- `client/tests/state_test.c`: UTF-8 입력 및 경계 단위 테스트
- `client/web/shell.html`: 캔버스와 한글 IME HTML 셸

## server

- `cmd/server/main.go`: 로컬 인증 수직 프로토타입 서버 진입점
- `internal/auth/`: Google 외부 식별자와 해시된 자체 세션 도메인
- `internal/auth/googleid/`: Google 공식 ID 토큰 검증 어댑터
- `internal/httpapi/auth_handler.go`: 인증 JSON HTTP API와 hardened cookie
- `internal/server/app.go`: 인증 API와 생성된 WASM 정적 파일 조합

## assets
- `board_reference/README.md`: 윷판 참고자료 설명
- `board_reference/yut_board_reference.png`: 전체 윷판 기준 그림

## spec
- `room_settings.yaml`
- `yut_probabilities.yaml`
- `turn_state_machine.yaml`
- `cpu_policy.yaml`
- `board_graph.yaml`

## schemas
- `room_settings.schema.json`
- `protocol_envelope.schema.json`
- `ws_client_command.schema.json`
- `ws_server_response.schema.json`
- `ws_server_event.schema.json`
- `game_snapshot.schema.json`
- `http_auth.schema.json`
- `examples/client_commands.json`
- `examples/server_responses.json`
- `examples/server_events.json`
- `examples/game_snapshot.json`
- `examples/http_auth.json`

## reference
- `requirements_qa_finished.md`: 사용자가 작성한 완성 Q&A 원본
- `decision_addendum.md`: Q&A 이후 대화에서 추가 확정된 규칙
