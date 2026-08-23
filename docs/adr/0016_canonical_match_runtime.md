# ADR-0016: 정식 경기 런타임과 실데이터 재접속

- 상태: 채택
- 결정일: 2026-08-23
- 대상: Issue #82

## 맥락

ADR-0015까지 레지스트리 방은 시작 확인 전원 동의로 started 상태가 되지만 이를
소비하는 경기 실행이 없었다. docs/12는 "경기 런타임 연결"을 미결로 기록했고,
THROW_YUT·SELECT_* command는 `APPLICATION_UNAVAILABLE`을 반환했다. ADR-0013의
고정 `prototype-match` scope는 재접속 배선 검증 전용 임시 장치였으며, 정식
room/match registry·참가자 상태·게임 snapshot 조립으로 대체해야 한다고 명시했다.

도메인 계층은 이미 준비되어 있었다: `spec/board_graph.yaml` 로더와 이동 planner,
턴 머신과 결과 큐(#14~#31), 시드 고정 가능한 윷 RNG, CPU 결정 정책(#33). 남은
것은 이들을 started 방의 생명주기에 연결하는 application 계층 수직 슬라이스다.

## 결정

### started 소비와 런타임 조립

- 마지막 CONFIRM_GAME_START가 확인되면 레지스트리가 같은 임계구역에서 (1) 확인된
  로스터로부터 A/B 팀 로스터와 말 setup을 만들고, (2) 팀 내 순서와 선공을 주입된
  RNG로 추첨하며(docs/05 재대결 추첨), (3) `match.Game`·`turn.Machine`·`yut.Sampler`
  ·`cpu.Policy`를 하나의 matchRuntime으로 조립한다. 조립 실패 시 started 전환은
  일어나지 않는다.
- 조립 직후 ROOM_UPDATED(in_match) → GAME_STARTED → TURN_STARTED(first)를 순서대로
  방송한다. 모든 경기 이벤트는 ROOM_UPDATED·GAME_STARTING과 같은 방 sequence
  공간과 ADR-0015 허브를 소비하며 live 전달 only다(저장은 ADR-0014 후속).
- 난수는 서버가 소유한다. 운영은 crypto/rand에서 두 uint64 시드를 뽑아 PCG 소스에
  공급하고(AGENTS.md 테스트 모드 시드 고정 원칙), 테스트는 고정 시드를 주입해
  전체 경기를 재현한다. 같은 PCG 스트림이 던지기 가중치, 북 그룹 가중치, CPU
  동률 선택에 공유되지만 직렬화 경계 안에서만 사용된다.
- **턴 교대 방식(v1 정본)**: 각 팀 내부 순서를 셔플한 뒤 두 팀을 번갈아 배치하고
  시작 팀을 추첨해 전역 순서 `A1,B1,A2,B2,…`(또는 B 먼저)를 만든다. docs/05의
  "팀 내 순서와 선공을 다시 추첨"이 팀 간 배치까지 규정하지 않는 빈칸이므로 그
  구체화를 본 ADR에 기록한다. 실제 윷놀이 관례(상대 팀과 번갈아 진행)와 일치하며
  이동·판정 규칙에는 영향이 없다.
- **조립 실패의 보상**: 도메인 확인 전이는 비가역이므로, 조립 실패 시 started
  플립 이전이라도 확인 창을 만료 제재와 동등한 보상으로 되돌린다(확인 기록 제거,
  타이머 정지, 잔여 준비 해제, ROOM_UPDATED(lobby)). 방은 재시도 가능한 대기실
  상태로 복귀하고 고착하지 않는다.

### 턴 진행과 제한 시간

- THROW_YUT는 도메인 RNG 결과를 토큰으로 큐에 추가하고 YUT_RESULT,
  RESULT_QUEUE_UPDATED를 방송한다. 윷/모 추가 던지기는 새 THROW_YUT 입력을 요구하며
  던지기 타이머를 초기화한다.
- 던지기 연쇄가 끝나면 이동 처리 시간 창 하나가 열리고 결과·말·지름길 선택을 모두
  포괄한다(docs/03). 북 선두는 서버가 자동 해결하고(BUK_RESOLVED), 이동 가능한 말이
  없는 일반 토큰은 v1 프로토콜에 클라이언트 폐기 타입이 없으므로 서버가 자동 폐기
  한다. 이는 docs/03 discard_only_that_token과 동일한 결과다.
- 던지기·이동 제한 시간 만료 시 서버는 현재 플레이어의 해당 턴 전체를 CPU 정책으로
  완료하고 CPU_CONTROL_STARTED(reason=timeout)를 방송한다. 다음 플레이어 턴부터는
  인간 제어로 복귀한다(턴별 generation 카운터로 오래된 타이머를 무시한다).
- 모든 말이 완주하면 GAME_ENDED(finished)를 방송하고, 같은 전이에서 started를
  해제하며 확인 기록을 지우고 잔여 플레이어 준비 상태를 초기화한 뒤
  ROOM_UPDATED(post_match)로 대기실 복귀를 알린다(docs/05). 팀·설정은 유지된다.
- **post_match 유지 규칙**: post_match는 "종료 후 복귀한 대기실"의 상태이며 다음
  START_GAME 창(starting)이 열릴 때까지 유지된다. 그 기간의 입장·팀·준비 방송도
  현재 생명주기 상태인 post_match를 그대로 실는다. lobby 라벨은 다음 시작 시도
  전에는 나타나지 않으며, 클라이언트는 ROOM_UPDATED 신호 + HTTP 방 상세 조회
  (ADR-0015 pull-on-notify)로 실제 멤버십을 확인한다.

### RECONNECT 실데이터화

- 승인된 RECONNECT는 현재 방 sequence 경계에서 game_snapshot 스키마 전체를
  채우는 실제 스냅샷을 조립해 반환하고 sequence를 소비하지 않는다. 저장 구현 전까지
  replay 배열은 항상 빈 것이다(ADR-0009 번들 계약, ADR-0014 후속).
- 거부 매핑: 멤버 아님=`ROOM_NOT_MEMBER`(재시도 불가), 방 없음=`ROOM_NOT_FOUND`
  (재시도 가능), 스코프 불일치·진행 중 경기 없음·last_sequence 초과=
  retriable `RESYNC_REQUIRED`. 승인 결과만 멱등 보존한다.
- 참가자 표현의 connected=true와 nickname=user_id는 presence·프로필 구현 전
  임시값이다(docs/12 기록).

### 직렬화 경계

- 경기 런타임 실행은 레지스트리 전역 mutex 안에서 수행된다. 이는 ADR-0015가
  기록한 현행 구조를 따른 것으로, 방별 actor(ADR-0012)로의 이관은 여전히 미결
  후속 과제다. 타이머 콜백은 actor mailbox 대신 mutex 재진입 + generation 검증으로
  만료를 직렬화한다(시작 확인 창의 time.AfterFunc 선례와 동일).

## 결과

- started 방이 실제 규칙 엔진으로 플레이되고, 시드 고정 전체 경기·CPU 대체·큐
  순서·북 장벽·업기 불가 독립·전체 잡기·백도 회귀 테스트로 고정된다.
- ADR-0013의 고정 scope, bootstrap sequence 1, 빈 로스터 스냅샷과 예제 파일은
  제거되었고 브라우저 셸은 로그인 시 scope를 위조하지 않는다. 재접속 machinery는
  정식 화면이 실제 match_id로 scope를 설정할 때 동일하게 동작한다.
- 남는 미결: 경기 이벤트 저장·replay(ADR-0014), 일시 정지·재개, 방장 위임·퇴장
  전송 계약, ERROR 코드 목록 표준화, presence·닉네임.
