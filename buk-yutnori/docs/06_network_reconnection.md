# 네트워크와 재접속

## 통신 분리

### HTTP API
- Google 로그인 교환
- 세션 상태
- 프로필 조회/수정
- 방 목록
- 방 생성
- 운영자 API

### WebSocket
- 방 상태
- 팀 선택과 준비
- 게임 시작
- 윷 던지기
- 결과·말·경로 선택
- 서버 게임 이벤트
- 채팅
- 관전
- 연결 상태
- 재접속 동기화

## 메시지 원칙

이 절의 envelope는 WebSocket 메시지에만 적용한다. HTTP API 요청과 응답에는 적용하지 않는다.

모든 WebSocket 메시지는 최소한 다음을 가진다.

- `version`
- `direction`: `client_command`, `server_response` 또는 `server_event`
- `type`
- `payload`

방향과 범위에 따라 다음 필드를 추가로 요구한다.

- 클라이언트 명령은 고유한 `command_id`가 필수다.
- 서버 이벤트는 단조 증가하는 `sequence`가 필수다.
- 명령 처리 응답은 원래 명령의 `command_id`를 포함하고, 요청과 응답의 상관관계가 필요한 경우 동일한 `request_id`를 사용한다.
- 방 관련 메시지는 `room_id`가 필수다.
- 경기 관련 메시지는 `room_id`와 `match_id`가 모두 필수다.
- 명령, 명령 처리 응답, 이벤트의 `type` 및 payload는 각각 `schemas/ws_client_command.schema.json`, `schemas/ws_server_response.schema.json`, `schemas/ws_server_event.schema.json`으로 검증한다.

### 최소 클라이언트 명령

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

### 최소 서버 이벤트

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

## 서버 권위형

서버만 다음을 변경한다.

- RNG
- 결과 큐
- 현재 턴
- 말 위치
- 업기·잡기
- 완주
- 시간 초과
- CPU 행동
- 승패
- 경기 무효

## 중복과 순서

- 각 명령의 멱등 키는 인증된 `(user_id, command_id)` 쌍이다.
- 같은 사용자는 처리 결과 보존 기간 안에 같은 `command_id`를 다른 명령 내용에 재사용할 수 없다. 내용이 다르면 프로토콜 오류다.
- 최초 처리 시 성공과 거부 결과를 모두 멱등 결과로 기록한다.
- 같은 명령의 재전송은 상태를 다시 적용하거나 새 도메인 이벤트를 만들지 않는다.
- 중복 요청에는 최초 응답과 최초 처리에서 생성된 이벤트 sequence 범위를 재전송한다.
- 방 명령의 멱등 결과는 방이 닫힐 때까지, 경기 명령의 결과는 적어도 해당 경기와 소속 방이 닫힐 때까지 보존한다. 방 폐쇄 뒤 감사 보존 기간은 별도 데이터 보존 정책을 따른다.
- 서버 이벤트는 경기별 단조 증가 `sequence`
- 클라이언트는 마지막 적용 `sequence`를 보관
- 결과 큐의 각 토큰은 재접속 전후에도 변하지 않는 `token_id`와 생성 원인을 가진다.

### 명령 처리 응답

- 서버는 명령마다 `COMMAND_RESULT` 응답을 보낸다.
- 응답의 `status`는 `accepted` 또는 `rejected`다.
- 응답은 원래 `command_id`를 필수로 포함한다.
- 명령이 서버 이벤트를 만들었다면 최초·마지막 sequence를 포함한다.
- 중복 명령에는 최초 처리 때 저장한 동일 응답을 다시 보낸다. 중복 수신 자체로 새 sequence를 소비하지 않는다.
- 명령이 아직 처리 중이면 같은 키의 동시 실행을 만들지 않고 최초 처리가 끝난 뒤 그 결과를 공유한다.

## 재접속

- 경기 종료 전까지 재접속 허용
- 복구 항목:
  - 방
  - 팀
  - 플레이어 순서
  - 경기 상태
  - 현재 턴
  - 남은 시간
  - 결과 큐
  - 말 위치
  - 최근 채팅
  - 플레이어/관전자 권한
- 전체 스냅샷과 누락 이벤트를 결합하는 방식을 기본으로 한다.

### 스냅샷 계약

`schemas/game_snapshot.schema.json`은 다음을 표현한다.

- 방 ID와 경기 ID
- A/B 팀, 플레이어 목록 및 팀 내 순서
- 플레이어와 관전자 권한, 연결 상태 및 CPU 제어 상태
- 현재 턴 플레이어, 턴 단계, 요구 입력 및 타이머
- 안정적인 ID와 생성 원인을 포함한 결과 큐
- 모든 말의 상태, 현재 칸, 업기 묶음, 위치 그룹 및 `actual_previous_space`
- 홈 체크포인트 상태와 북 목적지
- 일시 정지 사용 여부와 종료 시각
- 최근 채팅
- 스냅샷 경계 `sequence`

스냅샷은 해당 `sequence`까지의 상태를 원자적으로 포함한다. 이후 누락 이벤트 스트림의 첫 이벤트는 반드시 `snapshot.sequence + 1`이어야 한다. 스냅샷 생성과 이벤트 조회 사이에 새 이벤트가 발생하더라도 서버는 같은 직렬화 경계에서 스냅샷을 만들고 그보다 큰 sequence만 반환해야 한다. 클라이언트는 공백이나 중복을 발견하면 해당 스냅샷을 적용하지 않고 재동기화를 요청한다.

## 다중 접속

- 게임 중이 아닐 때: 마지막 접속이 기존 연결을 종료
- 게임 중일 때: 새 연결을 재접속으로 간주
- 하나의 계정은 하나의 조작 연결만 가짐

## 서버 재시작

- v1은 진행 중 경기 복구 없음
- 서버 재시작 시 진행 중 경기를 무효 처리
- 무효 이유를 로그에 저장
- 관련 방은 폐쇄
