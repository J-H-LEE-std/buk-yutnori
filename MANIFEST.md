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
- `examples/client_commands.json`
- `examples/server_responses.json`
- `examples/server_events.json`
- `examples/game_snapshot.json`

## reference
- `requirements_qa_finished.md`: 사용자가 작성한 완성 Q&A 원본
- `decision_addendum.md`: Q&A 이후 대화에서 추가 확정된 규칙
