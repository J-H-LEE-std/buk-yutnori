# ADR-0013: 고정 경기 재접속 수직 프로토타입

- 상태: 채택
- 결정일: 2026-08-21

## 맥락

ADR-0009와 ADR-0011은 재동기화 bundle 및 브라우저 runtime 계약을 정의했지만,
Milestone 2 서버에는 실제 snapshot source가 없었다. ADR-0012로 같은 방 command와
snapshot 생성을 직렬화할 actor 경계가 마련되었으므로 Issue #48을 닫으려면 실제
WebSocket에서 `RECONNECT`를 처리하는 최소 수직 경로가 필요하다.

정식 방 생성·입장·권한, 경기 command, timer, SQLite event store는 Issue #48의 범위가
아니다. 이들을 선행 구현하거나 실제 경기 상태인 것처럼 만들지 않으면서도 schema와
sequence 계약을 실제 서버 응답으로 검증할 수 있어야 한다.

## 결정

- Milestone 2 runtime은 기존 `prototype-room` 안에 고정 `prototype-match` scope 하나를
  둔다. 이 scope는 재접속 배선 검증 전용이며 정식 room/match registry가 아니다.
- runtime 생성 시 고정 scope가 `starting` 상태가 된 room 초기화 이벤트 하나를 방
  sequence `1`로 확정한다. 이 bootstrap 이벤트는 생성 전에 연결이 없으므로 방송하지
  않으며, 최신 snapshot에 반영한다. 이후 채팅을 포함한 모든 이벤트는 같은 room
  sequence를 계속 사용한다.
- 고정 snapshot은 `schemas/game_snapshot.schema.json`의 전체 형태를 사용한다. 정식
  참가자와 게임 command가 없으므로 A/B team의 player 목록, 참가자, 결과 큐, 말,
  stack과 위치 group은 빈 배열이고 상태는 `starting`이다.
- 고정 room의 모든 command는 ADR-0012의 actor를 통과한다. `RECONNECT` executor는 actor
  안에서 최신 room sequence를 읽고 그 경계의 snapshot을 만든다. 따라서 snapshot
  이후 event 배열은 현재 prototype에서 빈 배열이며, snapshot 생성과 응답 사이의
  chat 확정도 actor 순서로 분리된다.
- 승인된 `RECONNECT`는 sequence를 소비하지 않는다. `last_sequence`가 최신 경계보다
  크거나 `match_id`가 고정 scope와 다르면 `RESYNC_REQUIRED`, `retriable=true`로
  거부한다. 존재하지 않는 room은 기존처럼 `ROOM_NOT_FOUND`, `retriable=true`다.
- 승인 결과는 기존 `(user_id, command_id)` 멱등 경계에 보존한다. 같은 명령을 나중에
  다시 보내도 최초 snapshot 경계를 반환한다. 일시적 `RESYNC_REQUIRED`는 보존하지
  않아 동일 명령을 다시 실행할 수 있다.
- 브라우저는 인증과 WASM runtime 준비가 모두 끝나면 고정 scope를 설정한다. 새
  WebSocket이 열릴 때 실제 서버에 `RECONNECT`를 보내고 bundle staging 완료 뒤에만
  state-changing command gate를 연다. 로그아웃 시 scope와 pending 동기화를 지운다.
- runtime 종료는 actor admission을 먼저 닫고, 수락된 실행이 끝난 뒤 processor의
  room 멱등 결과와 room sequence를 함께 제거한다.

## 결과

- 실제 인증 WebSocket과 브라우저 새로고침 경로가 mock snapshot source 없이 같은
  재동기화 계약을 사용한다.
- 현재 prototype에는 게임 상태 변경 이벤트가 없으므로 replay 배열이 비어 있는 것이
  정상이다. 비어 있지 않은 event 연속성은 protocol/C/JavaScript 계약 테스트가 계속
  검증한다.
- 채팅 기록은 ADR-0010에 따라 snapshot이나 replay에 포함하지 않는다. 최신 snapshot
  경계가 채팅 sequence까지 덮으므로 채팅 비복구가 sequence 공백을 만들지 않는다.
- 정식 room/match registry, 참가자 상태, 게임 snapshot 조립, event 저장·조회는 이
  임시 고정 scope를 대체해야 하며 Milestone 3 이후 기능의 범위다.
