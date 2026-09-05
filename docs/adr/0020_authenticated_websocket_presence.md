# ADR-0020: 인증 WebSocket presence와 경기 이탈 수명주기

## 상태

Accepted — 2026-09-05, Issue #157

## 배경

경기 스냅샷은 참가자의 `connected`를 노출하고, 경기 중 연결 이탈은 해당 턴 CPU
대체, 방장 일시정지 자동 해제, 전원 이탈 30초 무효화를 요구한다. 그러나 기존
런타임은 인간 참가자를 항상 연결된 것으로 표시했다. 브라우저 한 사용자가 여러 탭을
열 수 있으므로 개별 socket 종료를 사용자 이탈로 간주해서도 안 된다.

## 결정

- 하나의 `RoomRegistry`가 인증 사용자별 활성 WebSocket 수를 소유한다. 첫 연결은
  disconnected→connected, 마지막 연결 종료는 connected→disconnected 전이이며 중간
  증감은 경기 이벤트를 만들지 않는다.
- WebSocket session은 구독 전에 연결을 등록하고, 정상 종료·read/write 오류·느린
  구독자 종료·상위 context 취소를 포함한 모든 반환 경로에서 정확히 한 번 해제한다.
- presence 전이는 기존 방 mutex 아래에서 직렬화한다. 사용자가 속한 진행 경기마다
  `PLAYER_DISCONNECTED` 또는 `PLAYER_RECONNECTED`를 정본 이벤트로 저장한 뒤 방송한다.
  관전자의 presence는 snapshot에 반영하되 player 전용 두 이벤트는 만들지 않는다.
- 인간 플레이어가 마지막 연결을 잃고 그 플레이어의 행동 창이 열려 있으며 다른 인간
  플레이어가 하나 이상 연결 중이면, 현재 창을 취소하고
  `CPU_CONTROL_STARTED(reason=disconnected)` 뒤 기존 CPU 정책으로 그 턴만 끝낸다.
  다음 턴 시작 시에도 담당 인간이 disconnected이면 같은 정책을 적용한다. 복귀는 이미
  확정된 CPU 행동을 롤백하지 않으며 `PLAYER_RECONNECTED.control_restored`는 다음으로
  열리는 자기 행동 창을 인간이 제어할 수 있는지를 뜻한다.
- 양 팀의 인간 플레이어가 모두 disconnected가 되면 CPU 진행과 활성 턴 timer를
  멈추고 남은 시간을 보존한 채 단조 시계 30초 watchdog을 시작한다. CPU 좌석과
  관전자는 이 판정에 포함하지 않는다. 한 인간이라도 복귀하면 watchdog을 취소하고,
  현재 플레이어가 연결됐으면 보존한 창을 복구하며 아니면 해당 턴을 CPU가 끝낸다.
- 30초 동안 복귀가 없으면 `GAME_ENDED(status=invalid,
  reason=all_players_disconnected)`와 `ROOM_UPDATED(status=closed)`를 순서대로 저장·방송한
  뒤 방을 제거한다. 사용자 전적은 갱신하지 않는다.
- host-request 일시정지 중 방장의 마지막 연결이 끊기면
  `GAME_RESUMED(reason=host_disconnected)`를 먼저 확정한다. 그 결과 전원이 이탈한
  상태라면 복구된 행동 창은 즉시 다시 보존되고 30초 watchdog만 계속된다.
- 사용자 일시정지와 저장 장애 정지는 watchdog을 취소하지 않는다. watchdog 만료의
  terminal 기록도 저장을 우선 시도하지만, 저장소가 계속 실패하면 ADR-0017의 비상
  종료와 같이 메모리 종료·구독자 통지·오류 로그를 우선하고 방을 제거한다. 전원 이탈
  중 저장 재시도 횟수가 먼저 소진되더라도 30초 복귀 유예보다 앞서 저장 장애 사유로
  경기를 끝내지 않는다. 이때 pending batch를 유지하고, 30초 전에 인간 플레이어가
  복귀하면 새 저장 재시도 주기를 시작한다.
- 연결 등록 중 `PLAYER_RECONNECTED` 저장이 실패해 진행 경기가 저장 장애 정지로
  전환됐더라도 이미 인증·등록된 WebSocket은 끊지 않는다. 해당 연결을 유지해 복구
  이벤트와 재동기화를 받을 수 있게 하며, 실제 session 종료 때 정상적으로 refcount를
  한 번 감소시킨다.
- 서버 shutdown에서 active connection을 어떤 close frame과 제한 시간으로 drain할지는
  Milestone 6 운영 정책으로 남긴다. session context 종료 자체는 마지막 연결 해제에
  포함한다.

## 결과

- 다중 탭 하나를 닫는 행동은 경기 제어를 CPU로 넘기지 않는다.
- snapshot과 live 이벤트가 같은 서버 presence를 표현한다.
- 전원 이탈 상태에서 CPU가 경기를 즉시 끝내지 않아 30초 복귀 유예가 실제 의미를
  갖는다.
- presence 이벤트도 다른 경기 이벤트와 같은 persist→sequence commit→broadcast 순서를
  따른다.
