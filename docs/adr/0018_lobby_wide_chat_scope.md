# ADR-0018: 인증 로비 전체 채팅의 독립 scope와 비차단 로그

- 상태: 채택
- 결정일: 2026-08-29
- 대상: Issue #129

## 맥락

Milestone 2의 `prototype-room` 채팅은 두 인증 브라우저의 전송·멱등·느린 구독자
경계를 검증하기 위한 메모리 fixture였다. 메인 로비는 방 목록과 별개의 전체 채팅을
요구하지만, 그 채팅을 실제 방 membership나 경기 sequence에 얹으면 방을 보지 않는
로그인 사용자도 입장 권한을 가져야 하거나, 경기 replay와 부가 채팅의 실패 정책이
서로 간섭한다.

채팅은 저장 대상이지만 `docs/07`은 저장 실패가 채팅 전달이나 경기를 중단하지
않아야 한다고 정한다. 또한 ADR-0010은 새 연결에 과거 채팅을 복구하지 않는다고
정한다.

## 결정

- 정식 전체 채팅 scope의 `room_id`는 영구 문자열 **`lobby`**다. 실제 생성·입장하는
  방이 아니며 모든 인증 WebSocket 연결은 이 scope만 자동 구독한다. `prototype-room`
  또는 다른 `room_id`의 `SEND_CHAT`은 retriable `ROOM_NOT_FOUND`로 거부한다.
- `lobby`는 레지스트리 방·경기와 독립된 단조 sequence 공간을 소유한다. `CHAT_MESSAGE`
  는 `room_id=lobby`와 그 공간의 sequence를 가지며 `RECONNECT` 대상이 아니다. 새
  연결은 구독 완료 이후 확정된 이벤트만 본다.
- `SEND_CHAT`의 1~200 Unicode code point 검증, 사용자별 1초 3건 승인 제한, 5초 내
  동일 텍스트 거부, 5초 16번째 유효 시도부터 1분 차단, `command_id` 멱등과 16개
  bounded 구독 queue는 ADR-0008의 확정 규칙을 유지한다. queue가 찬 연결만 1013으로
  닫고 다른 구독자와 명령 실행은 유지한다.
- 승인된 이벤트는 먼저 메모리 sequence를 확정하고 활성 구독자에게 전달한다. 같은
  wire event를 SQLite `room_events`의 `room_id=lobby` 행으로 별도 worker가 비동기
  최선노력 append한다. append 실패나 128개 로그 queue 포화는 구조화 로그로 남기며,
  채팅 결과·전달·경기 상태를 되돌리거나 막지 않는다. 이 행은 보존·감사 조회용이고
  WebSocket replay source가 아니다.
- 기동 시 SQLite의 `MAX(sequence)`로 저장된 `lobby` 행의 최대 sequence만 복원해 다음
  이벤트에 사용한다. 따라서 정상 종료 뒤 서버를 다시 띄워도 `(lobby, sequence)`
  기본키와 충돌하지 않고, 전체 채팅 로그를 메모리에 읽지 않는다.
- 발신자 표시는 아직 stable `sender_user_id`다. 닉네임·공개 프로필·보존 기간·제재와
  방 전용 채팅은 별도 요구사항으로 남긴다.

## 결과

- 메인 로비와 게임 화면은 하나의 인증 전체 채팅을 보며, 관전자도 로그인 상태면
  참여할 수 있다. 채팅은 방 입장·팀·경기 권한을 바꾸지 않는다.
- synthetic `lobby` sequence에 영속 행이 없더라도 채팅은 replay하지 않으므로 경기
  `RESYNC_REQUIRED` 연속성 판정에 영향을 주지 않는다.
- ADR-0008의 고정 prototype scope 결정은 이 ADR로 대체한다. 그 문서는 Milestone 2
  검증의 역사적 근거로만 유지한다.
