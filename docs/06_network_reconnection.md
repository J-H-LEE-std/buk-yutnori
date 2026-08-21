# 네트워크와 재접속

## 통신 분리

### HTTP API
- Google 로그인 교환
- 세션 상태
- 프로필 조회/수정
- 방 목록
- 방 생성
- 운영자 API

### 인증 HTTP 경로

- `GET /api/v1/auth/config`: 브라우저에 공개 Google web client ID 전달
- `POST /api/v1/auth/google`: Google ID 토큰을 내부 사용자와 자체 세션으로 교환
- `GET /api/v1/auth/session`: 현재 자체 세션 검증
- `DELETE /api/v1/auth/session`: 현재 자체 세션 폐기

인증 HTTP payload는 `schemas/http_auth.schema.json`을 따른다. Google 로그인과
로그아웃은 같은 origin JavaScript가 보내는 JSON 요청만 사용하며
`X-Buk-Request: 1` 헤더를 요구한다. 인증 응답은 캐시하지 않는다.

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

### 인증 WebSocket 경계

- endpoint는 `GET /api/v1/ws`다.
- 브라우저는 별도 토큰 query나 JavaScript로 읽은 식별자를 보내지 않고
  `__Host-buk_session` HttpOnly 쿠키로 인증한다.
- 서버는 세션 검증에 성공한 내부 `user_id`만 application session에 전달하며,
  쿠키 원문을 그 아래 계층이나 로그에 전달하지 않는다.
- `Origin`이 없거나 Origin host와 요청 Host가 다르면 인증 조회와 upgrade 전에
  거부한다. 로컬 개발은 HTTP/WS, 운영은 HTTPS/WSS를 사용한다.
- client command는 UTF-8 텍스트 JSON만 허용하고 한 메시지를 16 KiB로 제한한다.
  binary는 close `1003`, 과대 메시지는 `1009`, 비정상 v1 command는 `1008`로
  fail closed 처리한다.
- WebSocket 압축은 벤치마크로 필요성이 확인되기 전까지 사용하지 않는다.
- Milestone 2 멱등 application 기반 단계에서는 유효한 command를 인증된
  `(user_id, command_id)` processor로 전달한다. `SEND_CHAT`은 고정
  `prototype-room`의 메모리형 application executor가 처리하고, 로그인한 WebSocket
  연결은 이 방의 event stream에 자동 구독한다. `RECONNECT`는 같은 방의 고정
  `prototype-match` 최신 snapshot을 actor 경계 안에서 반환한다. 이 둘을 제외한
  방·경기 command는 상태를 적용하지 않고 `APPLICATION_UNAVAILABLE` 일시적 거부를
  반환한다. 이 응답은 `error.retriable=true`이므로 방 생명주기 결과로 보존하지 않는다.

라이브러리와 계층 분리 결정은
`docs/adr/0006_authenticated_websocket_transport.md`를 따른다.

### 브라우저 재연결 경계

- 로그인 상태에서 예상하지 못한 WebSocket 종료가 발생하면 브라우저는 250ms,
  500ms, 1초, 2초, 5초 뒤 순서대로 최대 5회 새 연결을 시도한다.
- 연결에 성공하면 재시도 횟수를 초기화한다. 5회가 모두 실패하면 자동 재연결을
  중단하고 사용자가 새로고침하도록 안내한다.
- 로그아웃은 예약된 재연결을 취소하고 새 연결을 만들지 않는다.
- 채팅 입력은 연결이 열려 있을 때만 허용한다. 이전 연결의 미확인 채팅 command를
  새 연결에서 자동 재전송하지 않는다.
- 진행 중 경기 scope가 설정된 클라이언트는 새 연결이 열린 뒤 마지막 확정
  `sequence`로 `RECONNECT`를 보낸다. `RESYNC_REQUIRED`를 받으면 새 `command_id`와
  `last_sequence=0`으로 전체 재동기화를 한 번만 다시 요청한다.
- room/match scope가 바뀌면 이전 scope의 pending `RECONNECT`를 폐기한다. 응답은
  command를 보낼 때 기록한 room/match와 일치할 때만 적용한다.
- state-changing command는 C protocol state가 synchronization 완료를 확정한 뒤에만
  허용한다. invalid bundle은 기존 확정 sequence를 유지하고 command gate를 잠근다.
- JSON number와 WASM `uint64_t` 경계에서 정밀도를 잃지 않도록 sequence는
  JavaScript와 C ABI 사이에서 10진 문자열로 전달한다.

세부 결정은 `docs/adr/0011_browser_reconnect_runtime.md`와
`docs/adr/0013_fixed_prototype_reconnect_runtime.md`를 따른다. 현재 Milestone 2
브라우저는 인증과 WASM 준비 뒤 고정 `prototype-room`/`prototype-match` scope를
설정하고 실제 WebSocket `RECONNECT`로 schema-valid 최신 snapshot을 적용한다. 정식
방·경기 상태와 비어 있지 않은 replay event source는 Milestone 3 이후 구현이 대체한다.

## 메시지 원칙

이 절의 envelope는 WebSocket 메시지에만 적용한다. HTTP API 요청과 응답에는 적용하지 않는다.

모든 WebSocket 메시지는 최소한 다음을 가진다.

- `version`
- `direction`: `client_command`, `server_response` 또는 `server_event`
- `type`
- `payload`

방향과 범위에 따라 다음 필드를 추가로 요구한다.

- 클라이언트 명령은 고유한 `command_id`가 필수다.
- 서버 이벤트는 해당 방 생명주기 안에서 단조 증가하는 `sequence`가 필수다.
- 모든 서버 이벤트는 sequence 범위를 식별하는 `room_id`가 필수다.
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
- `CHAT_MESSAGE`
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
- 최초 처리 시 accepted 결과와 결정적 rejected 결과를 멱등 결과로 기록한다.
- accepted 결과와 `error.retriable=false`인 결정적 rejected 결과를 보존한다.
  `error.retriable=true`인 일시적 거부와 application 실행 오류는 영구 멱등 결과로
  보존하지 않으며 같은 명령의 재시도를 다시 실행할 수 있다.
- 같은 명령의 재전송은 상태를 다시 적용하거나 새 도메인 이벤트를 만들지 않는다.
- 중복 요청에는 최초 응답과 최초 처리에서 생성된 이벤트 sequence 범위를 재전송한다.
- 동일 명령 비교는 decode된 `version`, `direction`, `type`, `request_id`, `command_id`,
  `room_id`, `match_id`, `payload`를 사용하므로 JSON 필드 순서는 무관하다. 같은
  `(user_id, command_id)`에 이 내용 중 하나라도 다르면 기존 결과를 덮어쓰지 않고
  WebSocket `1008 command_id_conflict`로 거부한다.
- 방 명령의 멱등 결과는 방이 닫힐 때까지, 경기 명령의 결과는 적어도 해당 경기와 소속 방이 닫힐 때까지 보존한다. 방 폐쇄 뒤 감사 보존 기간은 별도 데이터 보존 정책을 따른다.
- 방 생명주기 소유자는 새 command 유입을 원자적으로 중단한 뒤 processor의
  `ForgetClosedRoom` 경계를 호출한다. 이 경계는 완료 결과를 제거하고 이미 실행
  중인 한 건은 중복과 결과를 공유한 채 완료한 직후 제거한다. processor는 방 입장
  권한이나 폐쇄 상태 자체를 판정하지 않는다.
- 서버 이벤트는 방 생성부터 폐쇄까지 방별 단조 증가 `sequence`를 사용한다. 첫
  이벤트는 `1`이고, 방의 현재 경계 `0`은 아직 이벤트가 없음을 뜻한다. 현재
  `game_snapshot`은 게임 시작 뒤 상태만 표현하므로 `sequence`가 항상 `1` 이상이다.
- 대기실, 채팅, 경기와 경기 종료 뒤 대기실 이벤트는 같은 방 sequence를 공유한다.
  경기 시작·종료와 재대결은 값을 초기화하지 않으며 `match_id`는 별도 sequence
  범위를 만들지 않는다.
- 클라이언트는 마지막 적용 `sequence`를 `room_id`별로 보관한다.
- 결과 큐의 각 토큰은 재접속 전후에도 변하지 않는 `token_id`와 생성 원인을 가진다.

동시 이벤트 확정은 방 application 생명주기 안에서 직렬화한다. 경기 상태 변경은
DB 저장 성공 뒤 sequence를 확정하고, 채팅은 전달 확정 시 sequence를 소비한 뒤
로그 저장 실패를 비동기 최선 노력으로 처리할 수 있다. 방 폐쇄 시에는 새 이벤트
유입을 중단하고 진행 중 확정을 끝낸 뒤 메모리 sequence 경계를 제거한다. 세부
근거는 `adr/0007_room_scoped_event_sequence.md`를 따른다.

### 명령 처리 응답

- 서버는 명령마다 `COMMAND_RESULT` 응답을 보낸다.
- 응답의 `status`는 `accepted` 또는 `rejected`다.
- 응답은 원래 `command_id`를 필수로 포함한다.
- 명령이 서버 이벤트를 만들었다면 최초·마지막 sequence를 포함한다.
- `RECONNECT`가 승인되면 이벤트 sequence 범위 대신 `synchronization`을 포함한다.
  이 객체는 `game_snapshot` 하나와 그 스냅샷 경계 뒤의 연속된 `server_event`
  배열을 가진다. 다른 명령의 응답과 거부 응답에서 `synchronization`은 `null`이다.
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
  - 플레이어/관전자 권한
- 전체 스냅샷과 누락 이벤트를 결합하는 방식을 기본으로 한다.
- 채팅 기록은 authoritative game snapshot 복구 대상이 아니다. 새로고침이나 재접속
  뒤에는 새 연결 이후의 `CHAT_MESSAGE`만 표시하며, 이전 채팅 목록이 비어 있어도
  게임 상태 재동기화 실패로 취급하지 않는다.

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
- 스냅샷 경계 `sequence`

스냅샷은 해당 방의 `sequence`까지의 상태를 원자적으로 포함한다. 이후 누락 이벤트 스트림의 첫 이벤트는 반드시 `snapshot.sequence + 1`이어야 한다. 스냅샷 생성과 이벤트 조회 사이에 새 이벤트가 발생하더라도 서버는 같은 직렬화 경계에서 스냅샷을 만들고 그보다 큰 sequence만 반환해야 한다. 클라이언트는 공백이나 중복을 발견하면 해당 스냅샷을 적용하지 않고 재동기화를 요청한다.

`RECONNECT`의 `last_sequence`가 서버 snapshot 경계보다 크거나 서버가 동일한
`room_id`·`match_id`의 연속된 번들을 만들 수 없으면 `COMMAND_RESULT`를
`RESYNC_REQUIRED`, `retriable=true`로 거부한다. 클라이언트는 기존 확정 표시 상태를
유지한 채 새 `command_id`로 전체 재동기화를 요청한다. 승인된 재동기화 자체는 새
room event sequence를 소비하지 않는다.

클라이언트는 snapshot과 모든 후속 event의 routing 및 sequence를 먼저 staging한다.
전체 번들이 검증된 뒤에만 표시 상태와 마지막 적용 sequence를 함께 교체한다. 검증
중 공백, 중복, 역전 또는 이전 확정 sequence보다 오래된 snapshot을 발견하면 부분
상태를 폐기하며 이미 확정된 표시 상태를 롤백하지 않는다.

## 다중 접속

- 게임 중이 아닐 때: 마지막 접속이 기존 연결을 종료
- 게임 중일 때: 새 연결을 재접속으로 간주
- 하나의 계정은 하나의 조작 연결만 가짐

## 서버 재시작

- v1은 진행 중 경기 복구 없음
- 서버 재시작 시 진행 중 경기를 무효 처리
- 무효 이유를 로그에 저장
- 관련 방은 폐쇄
