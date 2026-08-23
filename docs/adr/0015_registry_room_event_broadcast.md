# ADR-0015: 레지스트리 방 이벤트 방송과 시작 확인 알림

- 상태: 채택
- 결정일: 2026-08-23

## 맥락

레지스트리 방(#69~#73)은 방 입장, 팀·준비 변경, 시작 확인이라는 확정 전이를 이미
수행하지만 구독자에게 어떤 신호도 보내지 않는다. 그 결과 두 가지가 막혀 있다.

- `CONFIRM_GAME_START`는 `match_id`를 필수로 요구하지만 클라이언트가 활성 창의
  `match_id`와 마감을 학습할 경로가 없어 시작 확인이 외부 도달 불가다(docs/06 기록).
- 준비 화면에서 다른 플레이어의 팀·준비 변화가 실시간으로 보이지 않는다.

v1 이벤트 스키마에는 이미 `GAME_STARTING`(room_id, match_id,
confirmation_deadline_at)과 `ROOM_UPDATED`(revision, status)가 정의되어 있고 미사용
상태다. 채팅 프로토타입만 구독 경로(버퍼 채널, 비차단 발행, 느린 구독자 드롭)를
갖는다. v1은 재시작 복구가 없고 event store(ADR-0014)도 아직 구현 전이다.

## 평가

방 상세(팀·준비 목록)를 클라이언트에 전달하는 방식을 세 가지로 비교했다.

A. `ROOM_UPDATED`를 신호로만 쓰고 상세는 HTTP 조회(pull-on-notify)
B. `ROOM_UPDATED` payload에 팀·준비 목록을 확장해 push
C. 변화별 세부 이벤트(준비 변경, 팀 변경 등)를 스키마에 신설

B와 C는 스키마 변경과 함께 동일한 상태를 이벤트 payload라는 두 번째 표현으로
복제한다. 상태의 정본은 서버 메모리(향후 DB)이므로 복제 표현은 드리프트 검증
부담만 늘린다. C는 이벤트 종류 난립으로 클라이언트 재구성 로직도 늘린다. v1 규모
(동시 50명, ADR-0001)에서 pull 비용은 무시할 수 있는 수준이다.

## 결정

대안 A를 채택하고 다음 계약을 정의한다.

### 방송 원본

- 레지스트리 방의 모든 확정 상태 전이는 typed server event 하나를 소비해 같은 방
  sequence 공간(ADR-0007, 채팅과 공유)에서 방송된다.
- sequence 소비와 구독자 큐잉은 registry mutex 안에서 원자적으로 수행된다. 이벤트
  순서는 sequence 순서와 일치한다.
- 방송은 live 전달 only다. 저장·재생은 ADR-0014 구현 및 별도 미결 항목에 남는다.

### 시작 확인 알림

- `RequestStart`가 수락되면 `GAME_STARTING`을 방송한다. payload는 기존 스키마 그대로
  match_id와 confirmation_deadline_at(RFC3339 벽시계 문자열)이다.
- confirmation_deadline_at은 표시용이다. 마감 판정은 여전히 서버 단조 시계 instant
  이며(ADR-0003), 클라이언트가 이 문자열로 authoritative 판정을 대체하지 않는다.
- 이 이벤트로 CONFIRM_GAME_START의 도달성이 회복된다.

### ROOM_UPDATED 발행 시점과 의미

- 발행 시점: 멤버십 변화(입장, 만료 제재에 의한 제외 포함), 팀·준비 변경 성공,
  시작 창 개시, 시작 확정(started) 각각이다.
- revision := 해당 ROOM_UPDATED 이벤트가 소비한 방 sequence다. 별도 카운터를 두지
  않는다. 단일 단조 값이며 docs/06의 "경계 0은 이벤트 없음" 관례와 일치한다.
- status 매핑: 대기 idle=`lobby`, 시작 창 진행=`starting`, 전원 확인 완료
  (`started`)=`in_match`, 만료 제재 적용 후=`lobby`, 폐쇄=`closed`. post_match는
  경기 런타임이 종료 복귀를 구현할 때 사용한다.
- ROOM_UPDATED는 "상태가 바뀌었다"는 신호이며 내용을 담지 않는다.

### 구독 모델

- application 계층에 방별 이벤트 허브를 둔다. 구독은 해당 방의 멤버십 보유자
  (플레이어와 관전자 모두, docs/08 관전 요구)에게만 허용된다.
- wsapi 세션은 입장 성공 후 해당 room_id를 구독하고 퇴장·연결 종료 시 해지한다.
- 버퍼링과 느린 구독자 정책은 채팅 선례를 재사용한다: 유한 버퍼, 비차단 발행,
  버퍼가 찬 구독자는 즉시 드롭(fail-closed). 드롭된 세션은 연결을 닫고 브라우저
  재연결 후 재구독한다.
- 구독 성공 시 허브가 보관 중인 최신 ROOM_UPDATED 한 건을 즉시 전달해 초기 상태를
  확보한다. 최신 상태와 상세 조회를 조합하면 새 연결도 현재 화면을 구성할 수 있다.

### 상세 조회

- `GET /api/v1/rooms/{room_id}`(멤버십별 role·team·ready 목록)를 HTTP에 추가한다.
  클라이언트는 ROOM_UPDATED 수신 시 이 엔드포인트로 상세를 당겨온다(pull-on-notify).
  payload 스키마는 `schemas/http_rooms.schema.json`에 확장한다.

## 결과

- 시작 확인 흐름이 외부에 도달 가능해진다. GAME_STARTING → CONFIRM_GAME_START →
  전원 확인 started 확정이 끝까지 연결된다.
- 준비 화면이 실시간 동기화된다: 방송 신호 + 상세 조회 조합.
- 스키마 변경이 0이다. 기존 v1 이벤트 계약을 소비한다.
- 남는 미결: 레지스트리 방 이벤트의 저장·RECONNECT replay(ADR-0009/0013 연계),
  GAME_STARTED 이후 경기 이벤트, 오프라인 누락 구간 복구. v1 재시작 복구 없음
  원칙상 허브도 인메모리다.
- 구현은 두 슬라이스로 나눈다: (a) 허브+이벤트 발행+wsapi 구독 배선,
  (b) 방 상세 조회 API. (a)가 선행이다.
