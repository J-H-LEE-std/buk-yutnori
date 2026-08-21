# ADR-0009: 재접속 snapshot과 연속 event 번들

- 상태: 채택
- 결정일: 2026-08-19

## 맥락

`game_snapshot`과 `RECONNECT` 명령은 정의되어 있었지만 snapshot을 어떤 서버 응답에
담는지, 누락 event 공백을 어떤 오류로 표현하는지 정해지지 않았다. snapshot을 먼저
표시 상태에 적용한 뒤 event 공백을 발견하면 클라이언트가 서버에서 확정된 기존
상태를 부분적으로 덮어쓸 수 있다. 재동기화는 room event가 아니므로 sequence를
소비해서도 안 된다.

## 결정

- 승인된 `RECONNECT`는 기존 `COMMAND_RESULT` payload의 `synchronization`에
  `game_snapshot` 하나와 snapshot 경계 뒤의 `server_event` 배열을 포함한다.
- snapshot과 event는 같은 `room_id`를 사용한다. snapshot과 match-scoped event의
  `match_id`는 원래 명령과 같아야 한다.
- snapshot sequence는 클라이언트의 `last_sequence`보다 작을 수 없다. 첫 event는
  `snapshot.sequence + 1`이고 이후 event도 공백·중복·역전 없이 이어져야 한다.
- 재동기화는 새 event sequence를 만들지 않으므로 응답의 event sequence 범위는
  `null`이다. 다른 명령과 거부 응답의 `synchronization`도 `null`이다.
- 클라이언트가 서버보다 앞서 있거나 서버가 원자적이고 연속된 번들을 만들 수 없으면
  `RESYNC_REQUIRED`, `retriable=true`로 거부한다.
- 클라이언트 프로토콜 상태는 snapshot과 event sequence를 staging하고 전체 검증이
  끝난 뒤 한 번에 확정한다. 실패 시 staging 상태만 버리고 기존 확정 상태를 유지한다.

## 결과

- 재접속 응답은 기존 명령 상관관계와 멱등 재전송 경계를 그대로 사용한다.
- JSON Schema는 bundle 형태를 검사하고 Go 프로토콜 타입은 routing과 sequence 연속성을
  추가로 검사한다.
- C 프로토콜 상태 모듈은 raylib 렌더링과 분리된 네이티브 테스트로 부분 적용과
  롤백을 방지한다.
- 브라우저 backoff는 ADR-0011, room actor는 ADR-0012, 고정 scope의 실제 snapshot과
  WebSocket 통합은 ADR-0013에서 구현한다. 정식 event 저장·조회는 후속 SQLite 저장
  결정과 room/match application이 이 prototype을 대체할 때 연결한다.
