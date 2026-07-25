# 명세 정합성 수정 결과

작성일: 2026-07-22  
후속 갱신: 2026-07-23  
상태: 확정 결정 및 구현 시작 조건 문서화 완료

## 1. 작업 범위

`README.md`, `AGENTS.md`, `CODEX_START_HERE.md`, `docs/`, `spec/`, `schemas/`, `assets/board_reference/` 전체를 다시 읽고 `REPORT.md`의 지적과 이번 작업의 확정 결정을 반영했다.

이번 작업에서는 명세, 기계 판독 정책, JSON Schema 및 검증 예제만 수정했다. Go 서버, C/C++ 클라이언트, WebSocket 제품 구현, DB 마이그레이션은 만들지 않았다.

참고 이미지와 `spec/board_graph.yaml`을 다시 대조한 결과 노드 이름과 외곽·중앙 연결 구조의 충돌은 발견하지 못했다. 북 후보 태그는 이미지에 표현되는 항목이 아니므로 이번 후보 변경은 시각적 판 구조를 변경하지 않는다.

## 2. 수정한 파일

### 루트 및 작업 안내

- `CODEX_START_HERE.md`
  - 수직 기술 검증이 폐기 가능한 스파이크이며 제품 기능 PR 순서를 대체하지 않는다고 명시
- `MANIFEST.md`
  - 신규 스키마, 예제 및 보고서 반영
- `REPORT.md`
  - 정합성 수정 전 기록임을 표시하고 이 보고서로 연결
- `REPORT_RESOLUTION.md`
  - 이번 작업 결과 작성

### 정제 문서

- `docs/00_authority_and_status.md`
- `docs/02_game_rules.md`
- `docs/03_turn_queue_state_machine.md`
- `docs/04_board_graph_and_movement.md`
- `docs/06_network_reconnection.md`
- `docs/11_test_plan.md`
- `docs/12_open_items.md`
- `docs/14_glossary.md`

### reference 문서

- `docs/reference/decision_addendum.md`
- `docs/reference/requirements_qa_finished.md`

### 기계 판독 정책

- `spec/board_graph.yaml`
- `spec/cpu_policy.yaml`
- `spec/turn_state_machine.yaml`
- 삭제: `spec/board_graph_constraints.yaml`

### JSON Schema와 예제

- 교체: `schemas/protocol_envelope.schema.json`
- 교체: `schemas/game_snapshot.schema.json`
- 추가: `schemas/ws_client_command.schema.json`
- 추가: `schemas/ws_server_event.schema.json`
- 추가: `schemas/examples/client_commands.json`
- 추가: `schemas/examples/server_events.json`
- 추가: `schemas/examples/game_snapshot.json`

`README.md`, `AGENTS.md`, 나머지 `docs/` 및 `spec/`, `schemas/room_settings.schema.json`, `assets/board_reference/`는 다시 검토했으나 이번 결정과 충돌하지 않아 변경하지 않았다.

### 2026-07-23 후속 시작 조건 문서화

- `README.md`, `CODEX_START_HERE.md`, `MANIFEST.md`
- `docs/00_authority_and_status.md`
- `docs/03_turn_queue_state_machine.md`
- `docs/05_room_match_lifecycle.md`
- `docs/06_network_reconnection.md`
- `docs/07_auth_profiles_data.md`
- `docs/10_deployment_operations.md`
- `docs/11_test_plan.md`
- `docs/12_open_items.md`
- `docs/13_implementation_plan.md`
- 추가: `docs/adr/0001_sqlite_v1_and_scale_out.md`
- 추가: `docs/adr/0002_command_idempotency_and_event_commit.md`
- 추가: `docs/adr/0003_start_confirmation_and_pause_timers.md`
- `spec/turn_state_machine.yaml`
- `schemas/protocol_envelope.schema.json`
- `schemas/ws_client_command.schema.json`
- `schemas/ws_server_event.schema.json`
- `schemas/examples/client_commands.json`
- `schemas/examples/server_events.json`
- 추가: `schemas/ws_server_response.schema.json`
- 추가: `schemas/examples/server_responses.json`

## 3. P0/P1 지적 처리 결과

### P0 — 북 후보 태그 불일치: 해결

무작위 북 목적지를 다음 10개로 통일했다.

- 뒷도, 뒷개, 뒷걸, 뒷윷
- 찌도, 찌개, 찌걸, 찌윷
- 속윷, 속모

변경 내용:

- `mo_do`에서 `buk_candidate` 제거
- `back_mo_do`, `back_mo_gae`에서 `buk_candidate` 제거
- `sok_yut`, `sok_mo`에 `buk_candidate` 추가
- 명시적 후보 목록에서도 뒷모도·뒷모개를 제거하고 속윷·속모 추가
- 정제 문서와 reference 문서의 후보 목록 동기화

검증 결과 태그 집합과 명시적 후보 집합이 완전히 같고 각각 10개다.

### P0 — 전체 보드 상태 충돌: 해결

- `spec/board_graph.yaml`을 기계 판독 가능한 전체 보드 정본으로 재확인했다.
- `docs/00_authority_and_status.md`에서 전체 노드와 연결이 미결정이라는 항목을 제거했다.
- 부분 보드 파일 `spec/board_graph_constraints.yaml`을 삭제했다.
- `docs/12_open_items.md`에 전체 보드를 구현 차단 항목으로 남기지 않았다.
- 참고 이미지는 시각 자료이고 논리 구현은 YAML을 따른다는 기존 원칙을 유지했다.

### P1 — 구현 순서 충돌: 해결

`CODEX_START_HERE.md`의 수직 기술 검증을 폐기 가능한 기술 스파이크로 명시했다. 제품 기능 코드의 PR 순서는 `AGENTS.md`와 `docs/13_implementation_plan.md`를 따른다.

### P0 — 백도 경로 상태 누락: 해결

nullable `actual_previous_space` 한 개를 사용하는 최소 모델을 채택했다. 자세한 내용은 4절에 기록한다.

### P0 — 완주 거리 정의 누락: 해결

합법적인 전진 경로 중 최소 이동 칸 수로 확정하고 문서, 보드 정책, CPU 정책 및 테스트 계획에 반영했다.

### P0 — WebSocket 계약 부족: 초안 해결

공통 envelope와 방향별 명령·이벤트 스키마를 분리했다. 요청된 최소 9개 명령과 17개 이벤트를 `oneOf`로 검증한다.

실제 구현 전에 정해야 할 멱등 범위, `request_id` 상관 이벤트, 오류 코드 등은 추측하지 않고 `docs/12_open_items.md`에 남겼다.

### P0 — 재접속 스냅샷 부족: 초안 해결

요청된 방, 경기, 참여자, 턴, 큐, 말, 경로, 북, 정지, 채팅 상태를 모두 구조화했다. 최상위와 모든 주요 하위 객체는 `additionalProperties: false`다.

### P1 — 멱등성과 sequence 범위: 핵심 정책 해결

다음은 스키마 수준에서 확정했다.

- 클라이언트 명령의 `command_id` 필수
- 서버 이벤트의 `sequence` 필수
- 방/경기 범위별 ID 필수
- 결과 토큰의 안정적 ID와 생성 원인
- 스냅샷과 후속 이벤트의 원자적 sequence 경계

후속 결정으로 멱등 키를 `(user_id, command_id)`로 확정했다. 성공과 거부를 포함한 최초 결과를 방 폐쇄까지 보존하고, 중복 요청에는 상태를 다시 적용하지 않은 채 최초 `COMMAND_RESULT`와 이벤트 sequence 범위를 재전송한다. 경기 전 room/chat sequence 범위는 여전히 open item이다.

### P1 — 시작 확인 제한 시간: 해결

10초로 확정했다. 미응답자는 퇴장시키고 시작을 취소하며, 남은 전원의 준비 상태를 해제한다. 지연 응답은 취소된 시작을 되살리지 않는다.

### P1 — 준비 상태 초기화 범위: 미결정 유지

어떤 변경이 전원 또는 일부 플레이어의 준비 상태를 초기화하는지 제품 결정이 필요하므로 open item으로 유지했다.

### P1 — 일시 정지 세부 상태: 해결

1분 단위 1~30분으로 확정했다. 활성 타이머의 남은 시간을 보존하고, 정지 중에도 전원 이탈 30초 감시는 계속한다. 저장 장애 자동 정지는 사용자 일시 정지 사용량과 분리했다.

### P1 — 감사 로그 저장 실패 정책: 해결

경기 상태 변경 이벤트는 DB 저장 성공 뒤 상태와 sequence를 확정한다. 최초 저장 실패 시 자동 정지하고 3회 재시도하며, 모두 실패하면 경기를 무효 처리한다. 채팅 저장 실패는 경기를 중단하지 않는다.

## 4. 채택한 경로 이력 모델

### 모델

각 말은 다음 두 값을 가진다.

- `current_space_id`
- nullable `actual_previous_space`

전체 이동 이력 스택은 사용하지 않는다.

### 상태 전이

- 일반 전진 이동: 도착 직전에 실제로 통과한 칸을 `actual_previous_space`로 저장
- 백도: `actual_previous_space`로 이동하고, 백도 직전의 현재 칸을 새 `actual_previous_space`로 저장
- 연속 백도: 매번 위 전이를 반복하므로 직전 두 위치를 기준으로 동작
- 업기 경로 충돌: 마지막으로 도착하여 업기를 발생시킨 말 또는 묶음의 값을 전체 묶음이 승계
- 업기 불가 동일 칸 공존: 각 말이 독립된 값을 유지
- 북 위치 그룹 이동: 이동 전 공통 위치를 이동한 모든 말의 `actual_previous_space`로 설정
- 북 후 백도: 북 이동 직전 위치로 복귀
- 잡힘: 출발 대기로 돌아가면서 `null`
- 완주: `null`
- 출발 대기: `null`
- 홈 체크포인트: 진입 직전 칸을 유지
- `도 → 참먹이` 백도: 참먹이에서 `actual_previous_space = do`; 다음 백도는 도로 복귀

### 충분성 검토

확정된 규칙은 백도가 한 번에 한 칸의 실제 직전 위치만 요구하고, 백도 후 직전 위치를 다시 현재 출발 칸으로 갱신하도록 정했다. 북과 업기도 다음 역행 기준을 명시적으로 하나의 칸으로 덮어쓴다. 따라서 현재 칸과 직전 칸 한 쌍으로 모든 확정 사례를 표현할 수 있으며 전체 경로 스택은 불필요하다.

## 5. 완주 거리 계산 정의

`remaining_forward_distance`는 현재 위치에서 완주에 도달할 수 있는 합법적인 전진 경로 중 최소 이동 칸 수다.

계산 모델:

- 홈 체크포인트 `참먹이` 뒤에 거리 1의 가상 완주 지점을 둔다.
- 판 위 말의 거리는 현재 노드에서 합법적인 전진 경로로 참먹이에 도달하는 거리와 마지막 완주 1칸을 합한 값이다.
- 선택 지름길에서는 가능한 합법적 전진 경로의 최소값을 사용한다.
- 강제 지름길에서는 각 분기의 강제 간선만 사용한다.
- 내부 경로에 진입한 말은 현재 노드에서 도달 가능한 전진 간선만 사용한다.
- 홈 체크포인트 거리는 1이다.
- 출발 대기와 완주 상태는 거리 비교 대상에서 제외한다.
- 북 대상과 CPU의 `move_piece_closest_to_finish`가 동일한 함수를 사용한다.

검증용 그래프 계산에서 선택 및 강제 정책 모두 29개 노드에 대해 완주 경로가 존재했다. 두 정책 모두 최소값 1, 최대값 11 범위였다.

## 6. 프로토콜 스키마 구조

### 공통 envelope

`schemas/protocol_envelope.schema.json`:

- WebSocket 전용이며 HTTP API에는 적용하지 않음
- `version = 1`
- `direction`: `client_command`, `server_response` 또는 `server_event`
- `type`, `payload`
- 선택적인 `request_id`, 범위별 `room_id`, `match_id`
- 방향별 스키마가 `command_id` 또는 `sequence`를 추가로 강제

### 클라이언트 명령

`schemas/ws_client_command.schema.json`:

- 모든 명령에 `command_id`와 `room_id` 필수
- 경기 명령에 `match_id` 필수
- 서버 event용 `sequence`는 허용하지 않음
- 시작 확인 응답을 포함한 10개 type과 각 payload를 `oneOf`로 검증

명령 목록:

- `SELECT_TEAM`
- `SET_READY`
- `START_GAME`
- `THROW_YUT`
- `SELECT_RESULT`
- `SELECT_PIECE`
- `SELECT_ROUTE`
- `SEND_CHAT`
- `RECONNECT`
- `CONFIRM_GAME_START`

### 서버 명령 처리 응답

`schemas/ws_server_response.schema.json`:

- `COMMAND_RESULT`로 명령 수락 또는 거부를 표현
- 원래 `command_id`와 필요시 동일한 `request_id`를 반환
- 최초 처리에서 생성된 이벤트 sequence 범위를 포함
- 중복 명령에 최초 응답을 그대로 재전송

### 서버 이벤트

`schemas/ws_server_event.schema.json`:

- 모든 이벤트에 `sequence` 필수
- 클라이언트용 `command_id`는 허용하지 않음
- 방 및 경기 이벤트의 범위 ID를 강제
- 17개 type과 각 payload를 `oneOf`로 검증

이벤트 목록:

- `ROOM_UPDATED`
- `GAME_STARTING`
- `GAME_STARTED`
- `TURN_STARTED`
- `YUT_RESULT`
- `RESULT_QUEUE_UPDATED`
- `MOVE_REQUIRED`
- `PIECE_MOVED`
- `PIECES_STACKED`
- `PIECES_CAPTURED`
- `BUK_RESOLVED`
- `CPU_CONTROL_STARTED`
- `PLAYER_RECONNECTED`
- `GAME_PAUSED`
- `GAME_RESUMED`
- `GAME_ENDED`
- `ERROR`

## 7. 재접속 스냅샷 구조

`schemas/game_snapshot.schema.json`은 다음을 필수로 표현한다.

- `room_id`, `match_id`, 경계 `sequence`, 경기 상태
- A/B 팀, 플레이어 ID 목록, 팀 내 순서
- 플레이어 및 관전자 목록
- 역할, 권한, 팀, 연결 상태, CPU 제어 상태와 이유
- 현재 턴 플레이어, 상태 머신 단계, 요구 입력
- 타이머 phase, 남은 시간, 절대 deadline
- 결과 큐의 안정적인 `token_id`, 결과, 생성 원인, 생성 플레이어
- 모든 말의 상태, 현재 칸, stack 및 position group 참조
- 말과 stack의 `actual_previous_space`
- 업기 묶음과 업기 여부와 무관한 위치 그룹
- 홈 체크포인트 상태
- 북 활성화 및 목적지
- 일시 정지 사용 여부, 현재 상태, 종료 시각
- 최근 채팅과 각 메시지 sequence

### 원자적 경계

스냅샷은 `snapshot.sequence`까지의 상태를 하나의 직렬화 경계에서 포함한다. 후속 누락 이벤트는 반드시 `snapshot.sequence + 1`부터 연속되어야 한다. 공백이나 중복이 있으면 클라이언트가 해당 동기화를 폐기하고 재요청하도록 명세했다.

## 8. 아직 남은 실제 미결정 사항

다음은 이번 프롬프트에 결정이 없어 구현하지 않았다. 상세 목록은 `docs/12_open_items.md`가 정본이다.

### 프로토콜

- 경기 전 room/chat sequence 범위
- sequence 공백 재동기화 요청과 표준 오류 코드
- 최근 채팅 복구 개수

### 방과 운영

- 방·팀 변경 시 준비 상태 초기화 대상
- 운영자 강제 종료의 전적 및 무효 사유

### 기존 제품 세부 정책

- 닉네임과 방 제목 정책
- 방 비밀번호 제한
- 로그 및 채팅 보존 기간
- 계정 삭제
- 최소 브라우저 버전
- 관리자 역할과 인증

## 9. 실행한 검사와 결과

2026-07-22에는 로컬 검증기로 최초 정합성 검사를 실행했다. 2026-07-23 후속 결정 반영 뒤에는 Python/PyYAML과 PowerShell `Test-Json`으로 다시 읽기 전용 검증했다.

### 파싱 및 메타 스키마

- 모든 YAML 파싱: 성공, 5개
- `schemas/` 아래 모든 JSON 파싱: 성공, 10개
- JSON Schema 파일 파싱: 성공, 6개

### 보드

- 노드 ID 중복: 없음
- 노드 수: 29
- 간선 수: 32
- 모든 edge 참조: 유효
- 참먹이에서 도달 가능한 노드: 29/29
- 렌더 좌표 집합과 노드 집합: 일치
- route choice가 가리키는 edge: 모두 존재
- 북 후보 태그와 명시 목록: 완전 일치
- 북 후보 수: 10
- 북 후보와 경로 선택 노드의 교집합: 없음

### 완주 거리

- 선택 지름길 정책의 모든 노드에서 완주 경로 존재: 성공
- 강제 지름길 정책의 모든 노드에서 완주 경로 존재: 성공

### 프로토콜과 스냅샷

- 클라이언트 명령 예제: 10/10 성공
- 서버 명령 처리 응답 예제: 2/2 성공
- 서버 이벤트 예제: 17/17 성공
- 재접속 스냅샷 예제: 1/1 성공
- 명령의 `command_id` 누락 거부: 성공
- 명령 처리 응답의 `command_id` 누락 거부: 성공
- 일시 정지 이벤트의 보존 타이머 누락 거부: 성공
- 경기 명령의 `match_id` 누락 거부: 성공
- 이벤트의 `sequence` 누락 거부: 성공
- 출발 대기 말에 경로 이력이 있는 잘못된 스냅샷 거부: 성공

최종 검사 출력:

```text
PARSE_OK yaml=5 json=10
BOARD_OK nodes=29 edges=32 reachable=29 buk=10
EXAMPLES_OK commands=10 responses=2 events=17 snapshots=1 negative=7
FINISH_DISTANCE_OK selectable nodes 29 min 1 max 11
FINISH_DISTANCE_OK forced nodes 29 min 1 max 11
```

## 10. 제품 코드 구현 시작 가능 여부

후속 결정으로 당시 네트워크 구현 차단 항목이던 시작 확인, 일시 정지, 멱등 처리와 저장 장애 정책은 해소되었다. v1 DB는 SQLite로 확정했으며 `docs/adr/0001_sqlite_v1_and_scale_out.md`의 부하·운영 조건이 확인될 때 PostgreSQL 전환을 재검토한다.

문서 기준으로는 저장소·순수 규칙 코어·최소 프로토콜의 구현을 시작할 수 있다. 다만 사용자가 이번 작업에서는 구현 시작을 명시적으로 보류했으므로 제품 코드, Go 서버, C/C++ 클라이언트와 DB 마이그레이션은 만들지 않았다.

아직 남은 항목은 해당 기능에 닿을 때 `docs/12_open_items.md`에서 확인하고, 새 게임 규칙을 추측하지 않는다.
