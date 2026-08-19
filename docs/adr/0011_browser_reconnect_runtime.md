# ADR-0011: 브라우저 WebSocket 재연결과 C sequence bridge

- 상태: 채택
- 결정일: 2026-08-19

## 맥락

ADR-0009는 snapshot과 누락 event bundle 계약을, C protocol state는 sequence staging과
롤백 방지를 정의했다. 그러나 브라우저 셸은 WebSocket 종료 뒤 새 연결을 만들지 않았고
JavaScript에서 C state를 호출할 ABI도 없었다.

Milestone 2 서버는 아직 authoritative 경기 상태나 snapshot 생성기를 소유하지 않는다.
따라서 이번 단계는 채팅 transport의 실제 재연결과 향후 경기 reconnect가 사용할
클라이언트 runtime 경계를 구현하되 서버 snapshot을 가장하지 않아야 한다.

## 결정

- 로그인 상태의 예상하지 못한 종료는 250ms, 500ms, 1초, 2초, 5초의 최대 5회
  backoff로 재연결한다. 성공 시 횟수를 초기화하고 한도 소진 시 새로고침을 안내한다.
- 로그아웃은 예약 timer를 취소하고 자동 재연결을 비활성화한다.
- 채팅은 연결이 열려 있을 때만 전송하며 이전 연결의 pending command를 자동 재전송하지
  않는다. 채팅 기록 비복구는 ADR-0010을 따른다.
- C bridge는 sequence를 10진 문자열로 받아 strict `uint64_t`로 변환한다. 부호,
  공백, 소수점과 overflow 문자열은 거부한다.
- JavaScript는 synchronization response의 version, direction, type, room/match routing과
  연속된 safe-integer sequence를 검사한 뒤 C state에 staging한다.
- invalid bundle은 기존 확정 sequence를 유지하고 state-changing command gate를 잠근다.
- 경기 scope가 설정되면 새 연결에서 마지막 확정 sequence로 `RECONNECT`를 보낸다.
  `RESYNC_REQUIRED`는 새 `command_id`와 `last_sequence=0`으로 한 번만 재요청한다.
- scope 변경은 이전 pending command를 폐기하며, response는 command 전송 시 기록한
  room/match와 일치할 때만 적용한다.
- 현재 prototype에는 경기 scope가 없으므로 실제 서버에는 `RECONNECT`를 보내지 않는다.
  C/JS bundle 경계는 mock response를 사용하는 Chrome headless 회귀로 검증한다.

## 결과

- 채팅 WebSocket은 일시적 연결 종료 뒤 자동으로 다시 연결될 수 있다.
- 향후 room/match UI는 동일한 bridge와 command gate를 사용해 server snapshot runtime에
  연결할 수 있다.
- 실제 snapshot 생성과 replay가 추가되기 전까지 이 ADR은 Issue #48 전체 완료를
  의미하지 않는다.
