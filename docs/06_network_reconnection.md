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

### 방 목록·생성·입장 HTTP 경로

v1 WebSocket command envelope은 모든 명령에 `room_id` 멤버십을 요구하고 join 타입이
없으므로, 방 입장은 방 상태 변경이 아니라 요청/응답 경계로 HTTP에서 수행한다.

- `GET /api/v1/rooms`: 로그인 세션으로 현재 개설된 방 목록을 조회한다.
- `POST /api/v1/rooms`: 방을 생성하고 생성자를 첫 플레이어(준비 `false`)로
  입장시킨다. 제목과 비밀번호는 `Creation` 계약을 따른다.
- `POST /api/v1/rooms/{room_id}/join`: 플레이어 또는 관전자로 입장한다.
  플레이어는 A/B 팀을 지정하고 새 준비 상태는 `false`다. 비밀번호가 설정된 방은
  검증에 성공해야 입장할 수 있으며 미제출(`password_required`)과 불일치
  (`invalid_password`)를 구분해 응답한다. 두 코드는 방 목록의 `has_password`로도
  알 수 있는 정보만 노출한다. 플레이어+관전자 합계는 20명을 넘을 수 없다.

방 API payload는 `schemas/http_rooms.schema.json`을 따른다. 생성과 입장은
`X-Buk-Request: 1` 헤더를 요구하고 응답은 캐시하지 않는다. 방 비밀번호 원문은
보관하지 않고 digest만 유지한다. v1 WebSocket command에는 아직 퇴장·강퇴 타입이
없으며 이 전송 계약은 `docs/12_open_items.md`에 미결로 남는다.

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
- Milestone 3부터 모든 방·경기 command는 정식 실행기로 향한다. `SELECT_TEAM`과
  `SET_READY`는 HTTP 입장 이후의 방 레지스트리 대기실 상태를 변경하며, 존재하지
  않는 방은 `ROOM_NOT_FOUND`(재시도 가능)로 거부하고, 플레이어가 아닌 발신자
  (`ROOM_PLAYER_REQUIRED`)와 준비 완료 플레이어의 팀 변경
  (`READY_TEAM_CHANGE_BLOCKED`)은 재시도 불가능한 결정적 거부로 보존한다.
- `START_GAME`은 방장(`ROOM_HOST_REQUIRED`)만 요청할 수 있고 시작 조건 미충족은
  `START_CONDITIONS_NOT_MET`으로 거부된다. 요청이 받아들여지면 서버 기준 10초의
  시작 확인 창이 열리고(ADR-0003) 창 진행 중에는 팀·준비 변경과 입장이
  `START_CONFIRMATION_IN_PROGRESS`로 차단된다. `CONFIRM_GAME_START`는 창 안의
  로스터 플레이어 응답만 기록하고 전원 확인 시 경기가 started 상태로 확정되며,
  같은 전이 안에서 레지스트리가 실제 경기 런타임(도메인 RNG, 턴 머신, 보드 이동)
  을 조립하고 `GAME_STARTED`와 첫 `TURN_STARTED`를 방송한다(#82). 마감 만료는
  미응답자 제외와 잔여 전원 준비 해제를 하나의 방 상태 전이로 적용하며 늦은
  응답은 취소된 시작을 되살리지 않는다.
- 시작 확인 요청과 마감 시각을 알리는 `GAME_STARTING` 이벤트 방송 계약은
  ADR-0015로 확정되었다. RequestStart 수락 시 기존 스키마 그대로(match_id,
  confirmation_deadline_at)를 방 sequence로 방송하며 confirmation_deadline_at은
  표시용 벽시계 문자열이고 마감 판정은 서버 단조 시계다(ADR-0003). GAME_STARTING은
  활성 창의 유일한 match_id 경로이므로 허브가 창 종료까지 보관하고, 창 진행 중
  구독하는 클라이언트에게 최신 ROOM_UPDATED와 함께 재전달한다.
- 대기실 상태 변경 알림도 ADR-0015로 확정되었다. 멤버십·팀·준비 변화와 상태 전이마다
  ROOM_UPDATED(revision=해당 이벤트의 방 sequence, status 생명주기 매핑)를 방송하고,
  클라이언트는 신호를 받으면 HTTP 방 상세 조회로 상세를 당겨온다(pull-on-notify).
  창 진행 중 상세 조회 응답에는 활성 시작 확인 정보(match_id,
  confirmation_deadline_at)가 포함된다. `in_match` 중에는 같은 member-only 상세
  응답이 활성 런타임의 `active_match.match_id`를 제공한다. 이는 이미 진행 중인
  방에 새 관전자로 입장한 클라이언트가 `RECONNECT`에 사용할 수 있는 유일한 서버
  권위 scope이며, `lobby`·`starting`·`post_match`·`closed`에서는 생략한다. 구독은
  방 멤버십 보유자에게 허용되며, 구독 등록과 최신 상태 스냅샷 전달은 방 상태 변경과
  같은 임계구역에서 원자적으로 수행된다. 버퍼가 찬 느린 구독자는 채팅 선례와 같이
  드롭(fail-closed)되고, 재구독 시 재전달 계약으로 현재 상태를 회복한다.
- started 방의 경기 command(`THROW_YUT`, `SELECT_RESULT`, `SELECT_PIECE`,
  `SELECT_ROUTE`)는 레지스트리 소유 경기 런타임에서 실행된다(ADR-0016). 현재 턴
  플레이어가 아니면 `NOT_YOUR_TURN`, 단계가 맞지 않으면 `INVALID_TURN_ACTION`,
  스코프가 다르면 `MATCH_SCOPE_MISMATCH`, 진행 중 경기가 없으면 retriable
  `MATCH_NOT_ACTIVE`, 이벤트 정본 저장 장애 또는 차단된 방이면 retriable
  `EVENT_STORE_UNAVAILABLE`(ADR-0017)로 거부된다. 모든 상태 변경 이벤트는
  메모리·sequence 확정과 방송 전에 정본 저장소에 영속된다.
- `PAUSE_GAME`은 방장만(`ROOM_HOST_REQUIRED`), 경기당 1회
  (`PAUSE_ALREADY_USED`), 1~30분을 지정해 호출한다(docs/03 일시 정지,
  ADR-0003). 수락되면 활성 던지기·이동 창을 취소하고 종류와 남은 밀리초를 보존한
  채 `GAME_PAUSED(reason=host_request)`를 방송하며, 일시 정지 중 경기 명령은
  retriable `MATCH_PAUSED`로 거부되고 RECONNECT는 계속 허용된다. 방장의 조기
  재개와 예약 시각 만료는 같은 타이머를 보존된 남은 시간으로 되돌리고
  `GAME_RESUMED(host_request|pause_expired)`를 방송한다. 스냅샷의
  `pause{used,paused,ends_at}`와 `current_turn.phase=paused`,
  `timer.phase=paused`가 이 상태를 그대로 표현한다. 방장 연결 끊김 자동 재개와
  전원 이탈 감시는 presence 추적 후속 과제다. 던지기·선택 제한 시간이 만료되면 서버가 해당 턴에
  한해 CPU로 대체하고 `CPU_CONTROL_STARTED(reason=timeout)`를 방송한다(docs/03).
  경기 이벤트는 ROOM_UPDATED·GAME_STARTING과 같은 방 sequence 공간과 허브로
  live 방송되며, 저장·replay는 ADR-0014 구현 이후 과제다.
- `RECONNECT`는 고정 프로토타입 scope 없이 모든 방에 대해 처리된다. 멤버가 시작된
  방의 활성 `match_id`로 요청하면 현재 방 sequence 경계의 실제 game_snapshot을
  조립해 synchronization으로 반환하고 sequence를 소비하지 않는다(ADR-0009).
  snapshot 이후 event 배열은 스냅샷 경계 뒤의 저장된 연속 이벤트로 채워진다.
  모든 커밋된 방 이벤트는 방송 전에 정본 SQLite 저장소(ADR-0014)에 영속되며
  (확정 순서는 ADR-0017), 스냅샷이 항상 최신 경계에서 조립되는 동안에는 배열이
  빈 값으로 유지된다. 체크포인트 기반 과거 스냅샷이 도입되면 같은 경로가 비어
  있지 않은 replay를 서비스한다. 멤버가 아니면
  `ROOM_NOT_MEMBER`(재시도 불가), 존재하지 않는 방은 `ROOM_NOT_FOUND`(재시도 가능),
  스코프 불일치·진행 중 경기 없음·`last_sequence`가 경계보다 큰 경우는 retriable
  `RESYNC_REQUIRED`로 거부한다. 승인 결과만 `(user_id, command_id)` 멱등 경계에
  보존된다.

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
`docs/adr/0016_canonical_match_runtime.md`를 따른다. 브라우저 셸은 로그인 시
재접속 scope를 임의로 만들지 않는다. ADR-0013의 고정
`prototype-room`/`prototype-match` scope는 #82에서 은퇴했고, 재접속 machinery는
정식 방 라비 화면이 started 방의 실제 `match_id`(GAME_STARTING 방송)로
scope를 설정할 때 동일한 bundle staging 계약으로 동작한다. 비어 있지 않은 replay
event source(ADR-0014)도 후속 구현이다.

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

`MOVE_REQUIRED.payload`와 `game_snapshot.current_turn.move_request`는 같은 서버 권위형
선택 요청을 표현한다. 둘은 `required_input`, `token_ids`, `piece_ids`, `routes`를 가지며,
재접속 직후에도 클라이언트가 보드 규칙으로 후보를 재계산하지 않고 명령을 만들 수
있어야 한다. `select_route` 요청은 토큰과 말이 각각 정확히 하나이고 `routes`는
`normal`, `shortcut` 두 값이다. `none`과 `throw` 입력에는 snapshot의
`move_request`가 `null`이다.

### 최소 클라이언트 명령

- `SELECT_TEAM`
- `SET_READY`
- `START_GAME`
- `THROW_YUT`
- `SELECT_RESULT`
- `SELECT_PIECE`
- `SELECT_ROUTE`
- `PAUSE_GAME`
- `RESUME_GAME`
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
