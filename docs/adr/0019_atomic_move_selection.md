# ADR-0019: 말 우선 UX를 위한 원자적 이동 선택

- 상태: 채택
- 결정일: 2026-08-31
- 추적: #149

## 배경

결과 큐의 `movement_order=free`에서는 한 턴에 여러 `ResultToken`이 남을 수 있다.
기존 `SELECT_RESULT` 뒤 `SELECT_PIECE` 두 command 경계는 서버에는 단순하지만,
사용자가 보드에서 먼저 말을 고르는 게임 화면 UX와 맞지 않는다. 두 command를
클라이언트가 연속 전송하면 중복·재연결·중간 거부에서 원자성이 깨진다.

## 결정

- 사람은 보드 또는 대기 장소에서 말을 먼저 선택한다. 완주 말은 완주 영역에 왕관
  등의 완료 표시로 남지만 후보가 아니다.
- 서버는 `MOVE_REQUIRED`와 `game_snapshot.current_turn.move_request`에 모든 합법
  `(token_id, piece_id, routes)` 조합을 `candidates`로 제공한다. 클라이언트는 규칙을
  재계산하지 않는다.
- 같은 말에 적용 가능한 토큰이 하나면 즉시, 둘 이상이면 사용자가 그 토큰을 고른
  뒤 `SELECT_MOVE {token_id, piece_id}` 하나를 전송한다.
- 서버는 `SELECT_MOVE` 하나를 하나의 상태 전이·한 SQLite transaction으로 처리한다.
  이 transaction 안에서는 `RESULT_SELECTED` 다음 `PIECE_SELECTED`를 순서대로
  저장·방송한다. 두 이벤트의 sequence는 같지 않으며 단조 증가한다.
- 선택한 조합에 normal/shortcut 모두 가능하면 그 뒤에만 `SELECT_ROUTE`를 요청한다.
  경로가 하나면 서버가 같은 상태 전이에서 이동을 확정한다.
- `movement_order=fifo`에서 후보 token은 선두 하나뿐이므로 토큰 UI를 열지 않고
  말 클릭만 받는다. `free`에서도 북은 기존 순서 장벽을 유지한다.
- `THROW_YUT`는 입력 command다. 서버가 이를 생성하거나 `TURN_ADDED` 이벤트를
  만들지 않는다. 윷·모·잡기 추가 던지기는 `YUT_RESULT.token.origin`과 다음
  `TURN_STARTED(required_input=throw)`로 표현한다.

## 결과

기존 공개 계약의 `SELECT_RESULT`와 `SELECT_PIECE`는 `SELECT_MOVE`로 대체한다.
후속 구현은 schema, 이벤트 replay reducer, C/WASM staging과 서버 executor를 한
변경에서 이 결정에 맞춰 이행한다. 구현 전까지 현 서버 동작은 이전 두 command
계약을 유지하므로, 이 ADR과 동기화한 문서·schema는 구현 목표 정본이다.
