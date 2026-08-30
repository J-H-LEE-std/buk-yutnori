# 남은 항목

## 구현 차단 항목

이번 명세 정합성 수정으로 전체 보드, 북 후보, 완주 거리, 백도 경로 상태의 알려진 구현 차단 항목은 해소되었다. 전체 보드 그래프는 `spec/board_graph.yaml`이 정본이며 추가 보드 자료를 기다리지 않는다.

최소 WebSocket 명령·이벤트와 재접속 스냅샷 스키마는 초안이 마련되었다. 시작 확인, 멱등 처리, 방 생명주기 단위 sequence, 일시 정지 및 저장 장애 정책은 확정되었다. 실제 네트워크 제품 구현 전에 아래 나머지 프로토콜 세부 정책을 결정해야 한다.

## 프로토콜 세부 정책 미결정

- `ERROR` 이벤트의 표준 오류 코드 목록
- WebSocket heartbeat/idle timeout과 graceful shutdown 시 active connection 종료 정책
- 대기실·시작 확인 구독자 알림 계약은 ADR-0015로 확정되었다. GAME_STARTING 방송으로
  시작 확인 도달성이 회복되고, ROOM_UPDATED 신호 + HTTP 방 상세 조회(pull-on-notify)
  조합이 준비 화면 동기화를 담당한다.
- 레지스트리 방 이벤트의 저장과 RECONNECT replay는 여전히 미결이다. ADR-0015 방송은
  live 전달 only며 누락 구간 복구는 ADR-0009/0013/0014 연계 후속 과제다.
- Milestone 4 클라이언트의 비어 있지 않은 replay tail은 #102에서 지원 이벤트를
  검증·표시 상태에 원자 적용한다. 지원하지 않는 이벤트와 payload 불일치는 여전히
  fail-closed하며, 서버 판정을 재계산하지 않는다. 남는 후속은 새 이벤트 타입이
  추가될 때 reducer 표와 계약 테스트를 함께 확장하는 것이다. 특히
  `RESULT_QUEUE_UPDATED`는 이벤트 토큰에 `generated_by_player_id`가 없어
  `game_snapshot` 토큰으로 완전하게 투영할 수 없으므로, 비어 있지 않은 큐는
  현재 fail-closed 처리하고 서버·스냅샷 스키마 통합 시 재검토한다.
- Milestone 4 지름길 UI 구현에서 `MOVE_REQUIRED`에 허용 경로가 없고 snapshot에는
  선택 중인 토큰·말 ID가 없어 재접속 클라이언트가 후보를 복원할 수 없는 빈칸이
  확인되었다. #96에서 `MOVE_REQUIRED.payload`와
  `game_snapshot.current_turn.move_request`를 같은 서버 권위형 선택 요청으로 맞추고,
  `select_route`의 `normal`/`shortcut` 후보를 명시해 이 빈칸을 닫는다.
- 방 퇴장·강퇴의 전송 계약. v1 WebSocket command에 leave/kick 타입이 없고 마지막
  사용자 퇴장 시 빈 방 즉시 삭제(docs/05)와 강퇴(docs/05)가 이를 필요로 한다.

## 방·운영 흐름 미결정

- 운영자 강제 종료 시 전적과 무효 사유
- 경기 런타임 연결은 #82로 해소되었다. 시작 확인 전원 동의 시 레지스트리가 실제
  경기 런타임을 조립하고(ADR-0016), THROW_YUT·SELECT_* 명령과 던지기·이동 제한
  시간 CPU 대체, GAME_ENDED 뒤 post_match 대기실 복귀와 started 해제가 동작한다.
  조립 실패 시 보상 전이로 방이 고착하지 않고, 팀 교대 순서와 post_match 유지
  규칙도 ADR-0016에 명시되었다. 남는 후속 과제는 방장 위임(퇴장 시)을 퇴장
  구현과 함께 연결하는 것이다.
- 사용자 일시 정지·재개는 #86으로 해소되었다(방장 1회·1~30분, 타이머 종류와
  남은 밀리초 보존, 만료 자동 재개, 스냅샷 표현). 남는 것: 방장 연결 끊김 자동
  재개와 정지 중 전원 이탈 30초 감시는 presence 추적 선행이며, 저장 장애 자동
  일시 정지·재시도(1s→2s→5s)·경기 무효 전이는 #87에서 ADR-0017 차단 위에
  구현한다.
- 선택 불가 결과의 서버 자동 폐기. v1 WebSocket command에는 클라이언트 폐기
  타입이 없으므로 이동 가능한 말이 없는 일반 토큰을 런타임이 자동 폐기하고
  RESULT_QUEUE_UPDATED로 알린다(docs/03 discard_only_that_token과 결과 동일).
  클라이언트 명시 폐기 계약이 필요해지면 퇴장·강퇴 전송 계약과 함께 정의한다.
- game_snapshot 참가자의 `nickname`은 #139부터 profile nickname(없거나 읽기 실패·손상
  record면 `user_id`)으로 해석한다. `connected=true`는 실제 presence 추적 전의 임시값으로
  남는다.

## 구현 전 ADR 필요

- 이벤트 저장 방식과 인덱스는 ADR-0014로 확정되고 #84에서 구현되었다. 방
  sequence별 typed event 행이 유일한 정본 저장이고 기보는 조회 시 파생 표현이며,
  확정 순서와 저장 장애 차단은 ADR-0017에 기록했다. 남는 후속: 체크포인트 기반의
  비어 있지 않은 replay 스냅샷, 채팅 행의 저우선순위 영구 저장(비복구 유지),
  저장 장애 자동 일시 정지·재시도·무효 전이(일시 정지 슬라이스). 종료 시점 기보
  복사본은 Milestone 5의 조회 비용 근거가 확인되면 별도 ADR로 다시 판단한다.

방 actor/goroutine 모델은 ADR-0012로 확정되었다.

## 제품 세부 정책 미결정

- 실제 presence 연결 상태. #137은 전체 채팅, #139는 `game_snapshot`, #141은 member-only
  room detail의 nickname을 profile nickname(없거나 읽기 실패 시 `user_id`)으로 정했다.
  연결 여부의 정식 추적과 nickname 변경 realtime push는 별도 구현에서 확정한다.
- 방 비밀번호 실패 시도 제한
- 채팅·게임 로그 보존 기간
- 사용자 계정 삭제 정책
- 브라우저 최소 지원 버전
- 관리자 역할과 인증 방식

## UI 조정 가능 항목

- `spec/board_graph.yaml`의 렌더링 좌표는 참고 그림에서 얻은 근사값이다.
- 화면 비율, 모바일 배치, 말 겹침 오프셋은 실제 클라이언트 구현 중 조정한다.
- 좌표 조정은 논리 그래프의 노드와 연결을 바꾸지 않는다.
- GUI 리소스·애니메이션 계약은 docs/15로 확정했다(M4 착수 전 최소 계약). 미결:
  아트 스타일 가이드, 사운드 포맷, atlas, 경로 추적 이동 연출·백도·북·승리
  연출, 접근성 테마, 최종 asset 교체는
  구현 중이며 실행 계약은 docs/15에 기록한다.

## 비차단 권장값

- 프로토콜 JSON
- v1 DB SQLite. 단일 서버·로컬 디스크 전제가 깨지거나 부하 시험에서 전환 조건이 확인되면 PostgreSQL 재검토
