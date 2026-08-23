# ADR-0017: 경기 이벤트 정본 저장의 확정 순서와 저장 장애 차단

- 상태: 채택
- 결정일: 2026-08-24
- 대상: Issue #84 (ADR-0014 구현)

## 맥락

ADR-0014는 방 sequence별 typed event 행을 유일한 정본 저장으로 확정했지만
구현은 없었다. #82까지 모든 이벤트는 메모리에서 즉시 확정·방송되고 재접속
번들의 replay 배열은 항상 비어 있었다. `spec/turn_state_machine.yaml`의
`game_state_event_commit_order`(validate_and_compute → persist_event →
commit_in_memory_state_and_sequence → broadcast)를 적용하려면 기존
`emitLocked`의 "커밋과 방송이 같은 임계구역" 구조를 바꿔야 했다.

## 결정

### 방출 트랜잭션(eventTx)

- 하나의 직렬화 연산이 생산하는 모든 이벤트를 스테이징한다. 빌더는 시퀀스를
  flush 시점에 받지만 payload 값은 스테이징 시점에 캡처하므로 이후 상태 변화를
  관측하지 않는다.
- flush는 (1) 현재 boundary 뒤의 연속 sequence로 행을 만들고, (2) 단일
  트랜잭션으로 전부 영속화한 뒤, (3) sequence 카운터를 소비하고 허브 캐시와
  구독자에게 전달한다. 영속화가 실패하면 (3)은 일어나지 않는다.
- 저장소는 단일 writer SQLite WAL(synchronous=FULL)에 `(room_id, sequence)`
  복합 기본키 WITHOUT ROWID 테이블 한 개로 구성된다(보조 인덱스 없음,
  ADR-0014). 드라이버는 CGO 없는 `modernc.org/sqlite`다. 배치는 원자적이며
  중복 키는 `ErrDuplicateEvent`로 실패한다. 스키마 버전은 `PRAGMA user_version`
  으로 기록한다.
- 저장소가 부착되지 않은 레지스트리는 테스트용 메모리 동작을 유지한다. 운영
  main 배선은 항상 부착한다(`BUK_DB_PATH`, 기본 `buk.db`).

### 저장 장애의 fail-closed 차단

- flush 중 영속화 실패 시 그 연산의 이벤트는 커밋·방송되지 않고 해당 방은
  차단(poisoned)된다: 이후 경기 명령, 대기실 변경, 시작 절차는 retriable
  `EVENT_STORE_UNAVAILABLE`로 거부되고 RECONNECT도 동일 코드로 거부된다.
  타이머 만료에 의한 CPU 대체도 차단된 방에서는 발화하지 않는다.
- 이는 spec의 자동 일시 정지(on_initial_failure → auto_pause)를 대체하는 최소
  안전장치다. 일시 정지 기능 자체가 아직 없으므로, 차단은 "숨은 발진 상태의
  추가 확정을 막고 클라이언트에게 회복 가능한(retriable) 신호를 주는" 역할만
  한다. 도메인 객체는 이미 계산 단계에서 진행됐을 수 있어 메모리가 저장소보다
  앞설 수 있지만, 차단된 방은 더 이상 관측 가능한 확정을 만들지 않으며 v1
  재시작 무효 원칙(docs/06 서버 재시작)과 정리된다. 완전한 persist-선행-
  상태확정(2단계 도메인 적용)과 자동 일시 정지는 일시 정지·재개 슬라이스에서
  다룬다.
- 오류 코드 `EVENT_STORE_UNAVAILABLE`(`retriable=true`)은 COMMAND_RESULT 거부
  경로(경기·대기실·RECONNECT 공통)에서 사용한다.

### replay 읽기 경로

- RECONNECT는 스냅샷 경계 뒤의 저장 이벤트를 조회해 연속성을 검증한 뒤 번들에
  담는다. 스냅샷은 항상 현재 경계에서 조립되므로 오늘 replay 배열은 빈 값이며,
  체크포인트(과거 경계) 스냅샷이 도입되면 프로토콜 변경 없이 채워진다. 손상된
  저장소의 비연속 행은 번들로 서비스되지 않는다.

## 결과

- 모든 커밋된 방 이벤트가 방송과 동일한 내용으로 영속 저장되고 sequence 공간이
  무공백임이 테스트로 고정된다. 저장 장애 시 미확정·미방송·차단이 보장된다.
- 남는 미결: 체크포인트 스냅샷 기반의 비어 있지 않은 replay, 채팅 행의 저우선순위
  영구 저장(ADR-0010 비복구 유지), 자동 일시 정지·저장 재시도·경기 무효 전이.
