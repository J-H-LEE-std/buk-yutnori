# ADR-0012: 방별 actor 명령 실행과 종료 경계

- 상태: 채택
- 결정일: 2026-08-21

## 맥락

방 대기실, 경기, 타이머, 이벤트 sequence와 재접속 snapshot은 같은 방의 상태를
공유한다. 이 변경을 여러 WebSocket goroutine과 HTTP handler가 각각 잠금으로 직접
적용하면 명령 순서, 이벤트 확정 순서와 방 폐쇄의 선형화 지점이 서로 달라질 수 있다.

ADR-0002는 방 폐쇄 시 새 명령 유입을 먼저 중단하고 진행 중 실행이 끝난 뒤 멱등
결과를 제거하도록 정했고, ADR-0007은 같은 방의 모든 이벤트 확정을 직렬화하도록
정했다. Milestone 3 방 서버를 구현하기 전에 이 두 경계를 소유할 실행 모델이
필요하다.

## 결정

- 살아 있는 방마다 application 계층의 actor goroutine 하나를 둔다. actor는 해당
  방의 authoritative command 실행 순서를 단독으로 소유한다.
- WebSocket과 향후 HTTP room router는 상태를 직접 변경하지 않고 해당 방 actor에
  명령을 제출한다. 서로 다른 방은 서로 다른 actor에서 독립적으로 실행한다.
- actor mailbox는 버퍼를 두지 않는다. actor가 현재 명령을 실행 중이면 다음
  호출자는 자신의 context로 admission을 기다리며, 서버 메모리에 무제한 command
  backlog를 만들지 않는다.
- 호출자 context는 actor가 명령을 수락하기 전까지만 admission 취소에 사용한다.
  actor가 수락한 뒤에는 `context.WithoutCancel`로 전달 값을 보존하되 transport 취소와
  deadline을 분리하고, 해당 실행과 멱등 결과 확정을 끝까지 완료한다. 저장소나 외부
  작업의 제한 시간은 room application이 별도 내부 context로 정한다.
- 방 폐쇄 flag를 admission의 선형화 지점으로 사용한다. 폐쇄 상태를 먼저 확정한 뒤
  actor가 아직 수락하지 않은 대기 제출과 새 제출을 `ErrRoomActorClosed`로 거부한다.
  이미 mailbox에서 수락한 현재 한 건만 취소하지 않고 완료한다.
- 수락된 실행이 끝나면 actor를 종료하고 room-lifetime cleanup을 정확히 한 번
  호출한다. cleanup은 향후 room registry 제거와 현재 `Processor.ForgetClosedRoom`,
  `RoomEventSequences.ForgetClosedRoom` 같은 자원 해제를 조합하는 경계다.
- `Close`를 기다리는 호출자의 context가 만료되어도 actor 종료를 취소하지 않는다.
  후속 `Close` 호출은 같은 완료 경계를 다시 기다릴 수 있다.
- 잘못된 `room_id`로 전달된 명령은 executor에 도달하기 전에 거부한다. 실제 방
  없음·폐쇄의 공개 오류 변환은 후속 room registry/router가 소유한다.
- actor가 호출한 executor 또는 cleanup이 panic하면 actor goroutine 경계에서 이를
  복구하고 `ErrRoomActorPanicked`로 기록한다. 해당 방은 즉시 폐쇄하고 cleanup을 한 번
  시도하며, panic 뒤에는 부분 적용 가능성이 있는 방에서 다음 명령을 실행하지 않는다.
  executor panic은 수락된 명령과 `Close` 양쪽에 terminal error로 전달하고, cleanup
  panic은 `Close`에 전달한다. 한 방의 panic이 서버 프로세스나 다른 방 actor를
  종료시키지 않는다.

## 대안

- 공유 room 구조체에 mutex만 두면 한 시점의 데이터 경쟁은 막을 수 있지만 command,
  timer와 event 저장·방송의 전체 순서를 하나의 실행 경계로 만들기 어렵다.
- 서버 전체 actor 하나는 한 방의 느린 저장이나 명령이 다른 모든 방을 막는다.
- 버퍼형 mailbox는 순간 처리량을 높일 수 있지만 폐쇄 시 아직 실행하지 않은 명령의
  취소·응답·멱등 처리 정책을 추가로 요구한다. v1 목표 규모에서는 명시적
  backpressure를 우선한다.

## 결과

- 같은 방의 수락된 명령은 동시에 실행되지 않고 actor admission 순서로 처리된다.
- transport 연결이 끊겨도 이미 수락된 명령의 authoritative 결과가 중간 취소되지
  않는다.
- 방 폐쇄와 room-lifetime 자원 정리가 하나의 관찰 가능한 완료 경계를 가진다.
- room-owned callback panic은 해당 방의 terminal failure로 격리되고 프로세스로
  전파되지 않는다.
- 실제 방 생성·입장 상태, registry routing, timer와 SQLite event store는 후속
  기능에서 이 actor 내부 executor와 cleanup에 연결해야 한다.
