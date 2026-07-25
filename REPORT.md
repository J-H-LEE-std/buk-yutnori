# 명세 검증 보고서

작성일: 2026-07-22  
상태: 정합성 수정 전 검토 기록 — 처리 결과는 `REPORT_RESOLUTION.md` 참조

## 1. 검토 범위와 원칙

다음 자료를 모두 읽고 교차 검토했다.

- `README.md`
- `AGENTS.md`
- `CODEX_START_HERE.md`
- `docs/`의 모든 문서
- `spec/`의 모든 문서
- `schemas/`의 모든 문서
- `assets/board_reference/yut_board_reference.png`

검토 시 `README.md`에 정의된 정본 우선순위를 적용했다.

1. `docs/`의 정제된 명세
2. `spec/`의 기계 판독용 정책
3. `schemas/`의 메시지 및 설정 스키마
4. `docs/reference/decision_addendum.md`
5. `docs/reference/requirements_qa_finished.md`

게임 규칙의 빈칸을 일반적인 윷놀이 규칙으로 보완하지 않았다. 이 보고서는 검토 자료이며, 발견 사항을 아직 정본 명세에 반영하지 않았다.

## 2. 요약 결론

전체 제품 구현 전에 해결해야 할 명세 충돌과 규칙 모델 공백이 있다. 특히 다음 네 영역은 구현 차단 항목으로 취급하는 것이 안전하다.

1. 보드의 북 후보 태그 불일치
2. 백도 경로 이력의 표현 및 갱신 규칙 누락
3. 분기 그래프에서의 `완주까지 남은 칸 수` 정의 누락
4. WebSocket 계약 및 재접속 스냅샷 스키마 부족

보드 그래프의 기본 연결 자체는 대체로 완전하다. 읽기 전용 무결성 검사 결과는 다음과 같다.

- 노드: 29개, ID 중복 없음
- 전진 간선: 32개, 중복 및 잘못된 노드 참조 없음
- `참먹이`에서 모든 노드로 전진 도달 가능
- 모든 노드에 렌더 좌표 존재
- 명시된 경로 선택과 실제 간선이 일치
- 북 후보 목록: 10개
- `buk_candidate` 태그 노드: 11개
- 북 후보 불일치: `mo_do` 1개

## 3. 확인된 명세 충돌

### P0 — 북 후보 태그가 정본 내부에서 충돌

`모도(mo_do)`는 노드 정의에서 `buk_candidate` 태그를 가지지만, 명시적인 북 후보 목록과 게임 규칙 문서에는 포함되지 않는다.

- 노드 태그: `spec/board_graph.yaml:46`
- 실제 후보 목록: `spec/board_graph.yaml:137`
- 게임 규칙 후보 목록: `docs/02_game_rules.md`의 `북 목적지`

현재 문서와 명시적 `buk.random_candidates`가 서로 일치하므로 `mo_do`의 태그가 잘못되었을 가능성이 높다. 그러나 `spec/board_graph.yaml`이 기계 판독 가능한 정본이므로 결정 없이 임의 수정해서는 안 된다.

결정 필요:

- `mo_do`에서 `buk_candidate` 태그를 제거한다.
- 또는 북 후보 목록과 게임 규칙에 `모도`를 추가한다.

### P0 — 전체 보드의 완성 상태가 문서 간 충돌

- `spec/board_graph.yaml`은 `status: canonical`이며 전체 그래프를 제공한다.
- `docs/00_authority_and_status.md`는 `윷판 전체 노드와 전체 연결`을 미결정으로 표시한다.
- `spec/board_graph_constraints.yaml`은 `status: partial_requires_board_reference`로 표시한다.
- `docs/12_open_items.md`는 논리 규칙과 전체 보드에 알려진 구현 차단 항목이 없다고 선언한다.

`docs/`가 `spec/`보다 우선이므로 상태 문서를 기계적으로 적용하면 전체 보드 구현을 시작할 수 없다.

결정 필요:

- `docs/00_authority_and_status.md`에서 보드 미결정 항목을 제거한다.
- `spec/board_graph_constraints.yaml`을 폐기하거나 `board_graph.yaml`로부터 파생되는 검증 제약 문서로 갱신한다.
- `docs/12_open_items.md`의 구현 차단 여부를 이번 검토 결과에 맞게 수정한다.

### P1 — 구현 순서가 문서 간 충돌

- `CODEX_START_HERE.md`: 수직 기술 검증 → 순수 규칙 엔진 → 전체 보드
- `docs/13_implementation_plan.md`: 규칙 코어와 전체 보드 → 수직 프로토타입
- `AGENTS.md`: 규칙·큐·보드·특수 규칙 이후 WebSocket/raylib

권고안은 폐기 가능한 짧은 수직 기술 스파이크를 먼저 수행하되, 제품 코드 PR 순서는 `AGENTS.md`와 `docs/13_implementation_plan.md`를 따르는 것이다. 기술 스파이크와 제품 구현의 차이를 문서에 명시해야 한다.

## 4. 규칙 누락 및 구현 위험

### P0 — 백도 경로 이력의 갱신 규칙 부족

현재 명세는 `actual_previous_space` 또는 동등한 경로 이력을 요구한다.

- `docs/04_board_graph_and_movement.md:77`
- `spec/board_graph.yaml:121`

그러나 다음 상태 전이는 정의되지 않았다.

- 연속 백도 후 경로 이력을 어떻게 갱신하는가
- 서로 다른 경로로 `방`에 들어온 말들이 업힐 때 어느 이력을 묶음이 승계하는가
- 업기 불가 상태에서 이력이 다른 동일 칸 말들을 북이 함께 이동시킨 뒤 각 말의 이력을 어떻게 설정하는가
- 북 이동 후 백도가 북 이전 위치로 돌아가는지, 북 목적지의 일반 선행 칸으로 가는지
- 홈 체크포인트 상태에서 백도를 적용할 수 있는지와 목적지
- 잡힘, 출발 대기 복귀, 완주 시 경로 이력을 언제 초기화하는가

단일 `actual_previous_space`만으로 일부 상태를 표현하지 못할 가능성이 있다. 경로 스택, 이동 이력 커서 또는 규칙에 특화된 명시적 역행 상태 중 하나가 필요하다.

### P0 — `완주까지 남은 칸 수`의 계산식 누락

북 대상 선택과 CPU 정책은 모두 이 값을 사용한다.

- `docs/02_game_rules.md:82`
- `spec/cpu_policy.yaml:10`

분기 그래프에서는 다음 중 어느 정의를 사용할지 불명확하다.

- 현재 방 설정의 강제 경로 거리
- 선택 가능한 경로 중 최단 거리
- 일반 경로만 사용한 거리
- 실제 진입 경로 및 경로 이력을 반영한 거리
- `참먹이` 도착까지의 거리
- 홈 체크포인트 이후 필요한 다음 전진까지 포함한 거리

북 후보 우선순위와 CPU 선택 결과가 이 계산에 직접 의존하므로 구현 전에 수식과 예제를 확정해야 한다.

### P0 — WebSocket envelope가 문서 요구를 강제하지 못함

`docs/06_network_reconnection.md`는 모든 메시지에 다음 필드가 있다고 규정한다.

- `version`
- `type`
- `request_id`
- `command_id` 또는 이벤트 `sequence`
- `room_id`
- `match_id`
- `payload`

하지만 `schemas/protocol_envelope.schema.json`은 `version`, `type`, `payload`만 필수로 규정하고 나머지는 모두 생략하거나 `null`로 둘 수 있게 한다.

추가 누락:

- 명령과 이벤트의 `type` 목록
- 타입별 payload 스키마
- 클라이언트 명령과 서버 이벤트의 방향 구분
- `command_id`와 `sequence`의 조건부 필수 규칙
- 오류 응답과 원래 요청의 상관관계

권고안:

- 공통 envelope는 재사용 가능한 기본 형식으로 유지한다.
- 명령 및 이벤트별 `oneOf` 스키마를 추가한다.
- 메시지 방향과 type에 따라 필수 필드를 조건부 검증한다.
- HTTP API는 WebSocket envelope 적용 대상이 아님을 명시한다.

### P0 — 재접속 스냅샷이 요구 상태를 표현하지 못함

`docs/06_network_reconnection.md`는 방, 팀, 플레이어 순서, 경기 상태, 턴, 타이머, 결과 큐, 말 위치, 최근 채팅, 플레이어/관전자 권한 복구를 요구한다.

현재 `schemas/game_snapshot.schema.json`에는 다음이 빠져 있거나 구체적인 구조가 없다.

- 팀 및 플레이어 필드
- 말 상태, 위치, 업기 묶음, 위치 그룹
- 경로 이력 및 홈 체크포인트
- 현재 턴 단계와 요구되는 의사결정
- 결과 토큰별 안정적인 ID와 생성 원인
- 북 목적지
- 플레이어 순서와 CPU 제어 상태
- 일시 정지 소진 여부와 종료 시각
- 방 및 관전자 권한
- 최근 채팅과 복구 범위
- 스냅샷 `sequence` 이후 누락 이벤트의 원자적 기준

최상위 `additionalProperties: true`로 인해 서로 호환되지 않는 구현도 같은 스키마를 통과할 수 있다.

### P1 — 멱등성과 이벤트 순서의 범위 부족

다음 항목을 명시해야 한다.

- `command_id`의 고유성 범위: 사용자, 연결, 방, 경기 또는 전역
- 중복 명령 수신 시 이전 응답을 재전송할지 단순 무시할지
- 처리한 `command_id`의 보존 기간
- 경기 전 방 이벤트와 채팅의 sequence 범위
- 스냅샷 생성 중 새 이벤트가 발생할 때의 경계
- 클라이언트가 sequence 공백을 발견했을 때 사용할 동기화 요청
- 경기 종료 이후 지연 도착한 명령의 처리 방식

### P1 — 시작 확인 제한 시간 누락

`docs/05_room_match_lifecycle.md`는 제한 시간 내 미응답자를 제외하도록 하지만 정확한 시간, 재전송 및 지연 응답 처리 정책이 없다.

### P1 — 준비 상태 초기화 범위 모호

`관련 플레이어의 준비 상태를 초기화`와 `준비 상태를 초기화`가 혼용된다. 다음 변경별로 전원 초기화인지 특정 사용자만 초기화인지 확정해야 한다.

- 팀 이동
- 플레이어/관전자 전환
- 최대 인원 변경
- 말 개수 및 게임 규칙 변경
- 타이머 변경
- 방장 변경

### P1 — 일시 정지 상태 모델 부족

다음이 정의되지 않았다.

- 1~30분의 단위와 허용 값
- 방장이 일시 정지 시점마다 기간을 선택하는지
- 방장 연결 끊김과 정지 요청이 동시에 발생할 때의 순서
- 자동 재개와 수동 재개의 원자성
- 정지 중 양 팀 전원 이탈 30초 타이머의 적용 여부
- 정지 전 던지기/이동 타이머 복원 방식
- 이미 일시 정지를 사용한 사실을 스냅샷에서 표현하는 방식

### P1 — 감사 로그 저장 실패 정책 누락

`docs/07_auth_profiles_data.md`는 모든 경기 이벤트와 채팅 기록을 영구 저장 대상으로 규정한다. 그러나 DB 쓰기 실패 시 경기를 계속할지 중단할지 정하지 않았다.

다음 순서를 ADR로 고정해야 한다.

1. 명령 검증
2. 이벤트 계산
3. 감사 이벤트 DB 저장
4. 서버 인메모리 상태 반영
5. 클라이언트 브로드캐스트

또는 이와 다른 순서를 선택한다면 유실, 중복, 확정 이벤트 롤백 금지 원칙에 미치는 영향을 기록해야 한다.

### P2 — 추가로 구체화할 정책

- 동일 채팅 반복 제한의 정확한 횟수와 문자열 정규화
- 채팅 `200자`의 단위: grapheme, Unicode scalar 또는 UTF-8 byte
- 시작 확인, 비밀번호 입력, 관리자 API의 rate limit
- 운영자 강제 종료 시 전적과 무효 사유
- 북 뒤에 토큰이 생길 수 없는 정상 생성 흐름의 불변조건과 테스트
- 승리와 동시에 발생 가능한 잡기 및 추가 던지기의 폐기 순서
- 방 제목의 존재 여부와 방 목록 표시 필드
- 차단된 사용자의 기존 세션과 WebSocket 종료 정책

## 5. `docs/12_open_items.md` 추가 제안

승인 후 다음 내용을 정본 문서에 반영하는 것을 권한다.

```markdown
## 구현 차단 항목

- `spec/board_graph.yaml`에서 `mo_do`는 `buk_candidate` 태그를 가지지만
  명시적 북 후보 목록과 게임 규칙 문서에는 포함되지 않는다.
- 백도 경로 이력의 갱신 규칙을 확정해야 한다:
  연속 백도, 방에서의 업기, 북 이동, 동일 위치 그룹 이동, 홈 체크포인트.
- 분기 그래프에서 “완주까지 남은 칸 수”의 계산식을 확정해야 한다.
- WebSocket 명령/이벤트별 필수 필드와 payload 계약이 없다.
- 재접속 스냅샷 스키마가 명세의 복구 항목과 경로 이력을 표현하지 못한다.

## 명세 상태 충돌

- `docs/00_authority_and_status.md`와
  `spec/board_graph_constraints.yaml`은 전체 판이 미완성이라고 표시하지만,
  `spec/board_graph.yaml`은 canonical 전체 그래프를 제공한다.
- `CODEX_START_HERE.md`와 `docs/13_implementation_plan.md`의
  수직 기술 검증/규칙 엔진 구현 순서가 다르다.

## 프로토콜 및 동시성 결정 필요

- command_id 고유성 범위, 보존 기간, 중복 응답 정책
- room/match/chat 이벤트 sequence 범위
- 스냅샷과 누락 이벤트의 원자적 경계
- 결과 토큰 ID와 선택 명령의 안정적인 참조 방식
- 시작 확인 제한 시간
- 준비 상태 초기화 대상
- 전원 이탈, 자동 재개, 방 폐쇄의 경쟁 조건
- 감사 로그 DB 쓰기 실패 시 경기 처리 정책

## 규칙 세부 결정 필요

- 북 이동 후 백도 경로 상태
- 서로 다른 경로 이력을 가진 말이 업힐 때 묶음 경로 상태
- 홈 체크포인트에서 백도 가능 여부와 목적지
- 북/CPU에서 사용하는 완주 거리 계산식
- 승리 확정 시 남은 큐 및 추가 던지기 폐기 순서
```

## 6. ADR 초안

### ADR-001 — 서버 디렉터리 및 의존성 구조

상태: Proposed

결정:

- `internal/domain`: 순수 규칙, 보드, 턴, RNG 인터페이스
- `internal/application`: 방/경기 유스케이스와 actor
- `internal/adapters`: HTTP, WebSocket, PostgreSQL, Google 인증
- `internal/protocol`: JSON DTO와 도메인 변환
- 도메인 패키지는 네트워크, DB 및 UI 패키지를 import하지 않는다.

영향:

- 서버 실행이나 DB 없이 규칙 단위 테스트가 가능하다.
- 프로토콜 DTO 변경이 순수 규칙 모델에 직접 전파되지 않는다.
- 일부 타입 변환 코드가 추가된다.

### ADR-002 — raylib 클라이언트 언어

상태: Proposed

결정: C++17과 raylib C API를 사용한다.

근거:

- raylib는 C99로 작성되었고 HTML5 빌드를 지원한다.
- 이벤트 버퍼, 문자열, 컨테이너 및 자원 수명 관리에는 C++ RAII가 유리하다.
- JavaScript와의 ABI는 `extern "C"` 함수로 제한할 수 있다.

영향:

- C보다 런타임 상태와 메모리 수명 관리가 단순해진다.
- 예외와 RTTI는 WASM 빌드에서 기본적으로 사용하지 않는 방향을 권장한다.
- 표준 라이브러리 사용량과 WASM 산출물 크기를 측정해야 한다.

참고: <https://github.com/raysan5/raylib>

### ADR-003 — 데이터베이스

상태: Proposed

결정: PostgreSQL을 사용한다.

근거:

- 세션, 고유 닉네임, 전적, 채팅, 감사 이벤트, 관리자 조치를 하나의 DB에서 처리할 수 있다.
- 경기 결과와 팀원 전적 갱신을 트랜잭션으로 묶을 수 있다.
- append-only 이벤트 조회를 위한 인덱스를 제공한다.
- 단일 VPS Docker 운영과 부합한다.

세부안:

- 이벤트 payload는 `jsonb`로 저장한다.
- `match_id`, `sequence`, `event_type`, `created_at`은 일반 컬럼으로 둔다.
- `(match_id, sequence)`에 고유 제약을 둔다.

참고: <https://www.postgresql.org/docs/current/datatype-json.html>

### ADR-004 — HTML/JavaScript 셸과 WASM 연결

상태: Proposed

결정:

- JS 셸이 Google 로그인, HTTP, WebSocket, 쿠키 세션, IME/채팅 DOM 및 재접속을 담당한다.
- WASM이 윷판 렌더링, 애니메이션, 표시 상태 및 게임 선택 UI를 담당한다.
- 두 영역은 좁은 C ABI와 JSON 메시지로 연결한다.
- JS → WASM은 확정 서버 이벤트를 전달한다.
- WASM → JS는 사용자 명령을 전달한다.
- 양방향 bounded queue와 최대 메시지 크기를 둔다.

영향:

- IME와 접근성 DOM을 WASM 내부에서 직접 다루지 않아도 된다.
- 네트워크와 렌더링 상태의 소유권이 명확해진다.
- ABI 호출 중 WASM 메모리의 포인터 수명 규칙을 문서화해야 한다.

참고: <https://emscripten.org/docs/porting/connecting_cpp_and_javascript/Interacting-with-code.html>

### ADR-005 — Go HTTP 및 WebSocket

상태: Proposed

결정: `net/http`와 `github.com/coder/websocket`을 사용한다.

근거:

- 초기 10~50명 규모에서는 별도 HTTP 프레임워크가 필요하지 않다.
- `coder/websocket`은 `context.Context`, 동시 write, close handshake 및 JSON helper를 제공한다.
- 라우팅과 미들웨어를 작은 명시적 계층으로 유지할 수 있다.

참고: <https://github.com/coder/websocket>

### ADR-006 — 방 및 경기 동시성 모델

상태: Proposed

결정: 방당 단일 actor goroutine을 사용한다.

- 모든 방 및 경기 명령을 한 채널로 직렬화한다.
- 타이머 만료, 재접속, 전원 이탈 및 일시 정지도 actor 명령으로 변환한다.
- WebSocket 연결마다 단일 writer를 둔다.
- actor 외부에서 방이나 경기 도메인 상태를 직접 변경하지 않는다.

영향:

- 전원 이탈 30초와 방 폐쇄의 원자성을 구현하기 쉽다.
- 느린 DB나 네트워크 작업을 actor 내부에서 직접 기다리지 않도록 별도 정책이 필요하다.
- actor 종료 및 goroutine 누수 검사를 테스트해야 한다.

### ADR-007 — 경기 이벤트 저장

상태: Proposed

결정 초안:

- 경기 이벤트는 `(match_id, sequence)` 고유키를 가진 append-only 행으로 저장한다.
- 클라이언트에 공개하기 전에 감사 이벤트 저장 성공을 확인한다.
- DB 실패 시 새 게임 명령 확정을 중단하고 운영 오류 상태로 전환한다.
- 정상 종료 시 경기 결과와 전적을 하나의 트랜잭션으로 갱신한다.
- 저장된 이벤트는 v1 경기 복구 입력으로 사용하지 않는다.

주의: DB 장애 시 경기를 즉시 무효 처리할지, 일시 정지 후 재시도할지는 제품 결정이 필요하다.

### ADR-008 — 타이머와 일시 정지

상태: Proposed

결정:

- 서버의 monotonic deadline을 정본으로 사용한다.
- 클라이언트 `remaining_ms`는 표시용이다.
- 정지 시 남은 시간을 저장하고 기존 timer generation을 무효화한다.
- 재개 시 새 generation과 deadline을 발급한다.
- 만료도 방 actor 명령으로 전달하며 오래된 generation의 만료는 무시한다.

## 7. 제안 저장소 구조

```text
buk-yutnori/
├─ cmd/
│  └─ server/
├─ internal/
│  ├─ domain/
│  │  ├─ board/
│  │  ├─ game/
│  │  ├─ turn/
│  │  ├─ room/
│  │  └─ rng/
│  ├─ application/
│  │  ├─ matchactor/
│  │  └─ roomservice/
│  ├─ protocol/
│  └─ adapters/
│     ├─ httpapi/
│     ├─ websocket/
│     ├─ postgres/
│     └─ googleauth/
├─ client/
│  ├─ include/
│  ├─ src/
│  ├─ web/
│  └─ tests/
├─ schemas/
│  ├─ commands/
│  └─ events/
├─ spec/
├─ docs/
│  └─ adr/
├─ migrations/
├─ tools/
│  └─ speccheck/
├─ tests/
│  ├─ contract/
│  ├─ integration/
│  └─ browser/
└─ deploy/
```

Go와 C++ 사이에 공용 도메인 코드를 만들지 않고 JSON 계약과 고정 fixture만 공유하는 구성을 권장한다.

## 8. 단계별 구현 계획

### 단계 0 — 명세 정리

- 이 보고서의 P0 항목을 결정한다.
- `docs/12_open_items.md`를 갱신한다.
- ADR을 검토하고 채택 또는 수정한다.
- 전체 보드의 상태가 충돌하는 문서를 정리한다.
- 규칙 결정은 코드보다 명세와 테스트 시나리오에 먼저 반영한다.

완료 조건:

- 북 후보가 모든 정본에서 일치한다.
- 백도 경로 상태와 완주 거리 계산식이 예제로 정의된다.
- 최소 프로토콜 명령 및 이벤트 집합이 스키마로 검증 가능하다.
- 재접속 스냅샷이 요구 상태를 표현한다.

### 단계 1 — 명세 검사 기반

- YAML 및 JSON 정적 검사
- 보드 그래프 로더와 무결성 테스트
- 북 후보 태그와 목록 일치 검사
- 좌표, 경로 선택, 역행 제약 검사
- JSON Schema 계약 테스트
- CI에서 모든 명세 검사를 실행

### 단계 2 — 제한된 수직 기술 스파이크

- 브라우저에서 raylib WASM 실행
- JS ↔ WASM ABI 검증
- 한글 IME 입력 검증
- Google 로그인 및 자체 세션 쿠키 검증
- 인증된 WebSocket 연결
- 서버 결과 한 건을 두 브라우저에 동일 sequence로 전달
- 새로고침 후 연결 및 자리 복구 검증

이 단계는 기술 위험 검증용이며 전체 제품 규칙을 구현하지 않는다.

### 단계 3 — 순수 규칙 엔진

- 공용 도메인 타입
- 방 설정 검증
- RNG와 테스트 시드
- 결과 토큰과 FIFO/자유 큐
- 턴 상태 머신
- 전체 보드 이동과 완주
- 업기와 잡기
- 백도
- 북
- CPU 정책
- 모든 설정 조합에 대한 속성 테스트

각 기능은 불변조건 테스트를 먼저 작성한 뒤 최소 구현을 추가한다.

### 단계 4 — 방/경기 actor와 프로토콜

- 명령 멱등성
- 이벤트 sequence
- 방 생성, 입장, 팀, 준비
- 시작 확인
- 턴 및 제한 시간
- 이탈과 CPU 대체
- 일시 정지
- 경기 종료와 재대결

### 단계 5 — 인증, DB 및 재접속

- Google ID 토큰 검증
- 30일 자체 세션
- 프로필 및 고유 닉네임
- 경기 감사 이벤트 저장
- 전적 트랜잭션
- 원자적 스냅샷과 누락 이벤트 동기화
- 다중 탭 및 새 연결의 제어권 이전

### 단계 6 — 클라이언트 통합

- 전체 윷판 렌더링
- 말, 업기 묶음 및 위치 그룹 표현
- 결과 큐와 북 장벽 UI
- 지름길 선택
- 서버 확정 이벤트 애니메이션
- 재접속 스냅샷 우선 적용
- 데스크톱 및 모바일 반응형 UI

### 단계 7 — 부가 기능과 운영

- 관전
- 전체 채팅
- 프로필과 전적
- 관리자 기능과 감사 로그
- Docker 배포
- HTTPS/WSS
- 모니터링과 부하 테스트
- 지원 브라우저 검증

## 9. 검토 권고

다음 순서로 사용자 결정을 받는 것이 효율적이다.

1. `mo_do`가 북 후보인지 확인
2. 백도 경로 이력과 북 이동 후 백도의 의미 확정
3. `완주까지 남은 칸 수` 계산식 확정
4. 프로토콜 및 스냅샷의 최소 계약 범위 승인
5. 기술 ADR 승인 또는 대안 지정
6. 승인된 내용만 `docs/12_open_items.md`, 관련 명세 및 테스트 계획에 반영

이 결정 전에는 전체 규칙 엔진이나 네트워크 제품 구현을 시작하지 않는 것을 권고한다.
