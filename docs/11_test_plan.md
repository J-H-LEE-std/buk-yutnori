# 테스트 계획

## 테스트 계층

1. 순수 규칙 단위 테스트
2. 상태 머신 속성 테스트
3. 프로토콜 계약 테스트
4. 서버 통합 테스트
5. 브라우저 다중 클라이언트 테스트
6. Docker 배포 스모크 테스트

## PR 필수 검사

현재 문서·명세 단계에서는 다음 집계 체크를 모든 PR에서 실행한다.

- `policy / required`
  - 브랜치 이름
  - Conventional Commits 형식의 PR 제목
  - Issue 또는 허용된 `noissue`
  - 자기 검토와 Codex 리뷰 증빙
  - Milestone 2 이후 고위험 변경의 독립 리뷰 증빙
  - 게임 규칙 의미 변경의 사용자 승인 증빙
- `ci / required`
  - 모든 YAML·JSON 파싱
  - 모든 JSON Schema 메타 검증과 예제 검증
  - 보드 그래프 무결성
  - 문서 기본 구조와 내부 링크

`ci / required`는 적용 가능한 Go format/vet/test, C 경고 없는 네이티브 단위 테스트,
WASM build를 실행한다. WASM 브라우저 검사는 실제 Backspace 키 입력이 DOM 값을
삭제하고 변경된 UTF-8 값이 C/WASM 상태와 다시 일치하는지도 확인한다.
SQLite·WebSocket 통합 검사와 Docker smoke 검사는 해당 서버·배포 코드가 추가될 때
단계적으로 연결한다.

인증 코드가 있는 PR은 Google verifier의 audience·issuer·subject 거부, 내부 user ID
분리, 세션 원문 미저장, 정확한 30일 만료, 만료·폐기 세션 거부,
`Secure`/`HttpOnly`/`SameSite` 쿠키와 로그인·세션 조회·로그아웃 HTTP 계약을
검사한다. 실제 Google 서명과 키 회전은 공식 검증 라이브러리에 맡기고 저장소 및
HTTP 테스트에서는 검증기 경계를 대역으로 주입한다.

WebSocket 전송 코드가 있는 PR은 세션 인증이 upgrade 전에 수행되는지, 쿠키 원문이
application session으로 내려가지 않는지, same-host Origin만 허용하는지 검사한다.
또한 UTF-8 텍스트 JSON과 모든 v1 client command payload를 엄격히 decode하고,
binary·16 KiB 초과·unknown/duplicate/trailing JSON을 각각 정해진 close code로
fail closed 처리하는지 실제 HTTP/WebSocket 통합 테스트로 확인한다.

의미를 바꾸지 않는 단순 문서 수정은 제품 빌드 검사를 생략할 수 있다. 정본 규칙,
보드, RNG, 턴, 승패, 백도, 북, CPU, 프로토콜, 인증, DB 또는 배포 의미를 바꾸면
문서 파일만 수정했더라도 전체 관련 검사를 실행한다.

## 보드 그래프 무결성

- 모든 `board.ForwardPlanner` 구현은 정본 `board.Graph`를 기준으로
  `internal/domain/board/boardtest.CheckForwardPlanner` 계약을 통과해야 함
- 계약 검사는 모든 정본 말 위치, 선택·강제 지름길 정책과 최장 완주 거리 초과 이동까지
  destination, `actual_previous_space`, traversed 노드·간선, route 순서 및
  홈 체크포인트·완주 전이를 비교함
- 모든 노드 ID가 유일함
- 참먹이에서 외곽 모든 노드에 도달 가능
- 외곽 경로가 참먹이로 돌아옴
- 모와 뒷모의 일반·지름길 경로가 모두 유효함
- 모개와 뒷모개가 방으로 합류함
- 방에서 속윷·방수기 두 경로가 유효함
- 속모가 찌모로 합류함
- 안찌가 참먹이로 합류함
- 북 후보 10개가 모두 존재함
- `buk_candidate` 태그 집합과 명시적 북 후보 목록이 완전히 일치함
- 북 후보가 방 또는 경로 선택 노드가 아님
- 모든 판 위 노드에서 역행 판정이 가능함
- 실제 직전 칸이 방이 아닌 경우 백도로 방 신규 진입 불가

## 필수 규칙 시나리오

- 도·개·걸·윷·모
- 연속 윷·모
- 윷 후 북: `[윷, 북]`
- 북 앞 일반 결과 우선 처리
- 자유 순서 모드에서 북 장벽
- 사용할 수 없는 토큰만 소멸
- 잡기 즉시 추가 던지기
- 잡기와 윷/모 추가 던지기 독립 누적
- 업기
- 업힌 묶음 잡기
- 업기 불가 동일 칸 공존
- 업기 불가 칸의 상대 말 전부 잡기
- 지름길 선택
- 지름길 강제
- 중앙 방의 두 진입·두 진출
- 방에서 백도
- 백도로 방 신규 진입 불가
- 첫 칸 백도 후 홈 체크포인트
- 홈 체크포인트 다음 전진으로 완주
- 완주 초과 이동
- 선택 지름길에서 합법적 최단 완주 거리
- 강제 지름길에서 강제 경로만 사용한 완주 거리
- 내부 경로와 홈 체크포인트의 완주 거리
- 북과 CPU가 동일한 완주 거리 함수를 사용
- 연속 백도마다 실제 직전 칸 갱신
- 서로 다른 경로 이력을 가진 말의 업기 시 마지막 도착 경로 승계
- 업기 불가 동일 칸 말의 독립 경로 이력
- 북 이동 후 백도로 북 이동 전 칸 복귀
- 홈 체크포인트 백도와 `도 → 참먹이 → 도` 백도
- 잡힘·출발 대기·완주 시 경로 상태 초기화
- 북 후보 가중치
- 북 활성 경기의 서버 난수원 필수 주입과 범위 위반 시 상태 무변경
- 같은 테스트 시드에서 북 목적지와 동률 대상 선택 재현
- 북 목적지에 이미 위치
- 북 후보 없음
- 북 도착 업기/잡기
- 북 목적지 공개

## 시간·CPU

- CPU는 방의 자유 순서 설정과 무관하게 결과 큐 선두를 FIFO로 선택
- CPU는 선택 가능한 지름길이 있으면 항상 지름길 계획을 선택
- CPU 후보는 즉시 완주, 상대 잡기, 북, 아군 업기, 지름길 진입,
  완주에 가까운 판 위 말, 출발 대기 말, 동률 난수 순으로 결정
- 업힌 묶음은 CPU 후보 하나로, 업기 불가 동일 칸 아군은 독립 후보로 처리
- 합법적인 일반·백도 후보가 없으면 선두 토큰만 폐기
- 던지기 시간 초과
- 이동 시간 초과
- 현재 턴 CPU 완료
- 다음 턴 인간 복귀
- CPU 동률 난수
- 테스트 시드 재현
- CPU 행동 중 재접속
- 확정 이벤트 롤백 금지

## 방·네트워크

- 방 제목은 Unicode extended grapheme cluster 기준 1자와 25자를 허용하고 빈 제목과
  26자를 거부
- 조합된 한글 음절과 분해된 한글 자모열을 각각 사용자에게 보이는 한 글자로 계산
- 방 비밀번호 미설정을 허용하고, 설정 시 4자와 16자의 영문 대·소문자·숫자를 허용
- 방 비밀번호 3자·17자, 한글·기호·공백 포함 문자열을 거부
- 같은 방 actor가 수락한 command를 동시에 실행하지 않고 admission 순서로 실행
- 같은 방의 client command와 서버 소유 내부 상태 전이가 같은 mailbox에서 직렬 실행
- 한 방의 실행이 막혀 있어도 다른 방 actor의 command는 독립적으로 완료
- caller context가 admission 전에 취소되면 command를 실행하지 않음
- actor가 수락한 command는 caller context 취소나 방 폐쇄로 중간 취소하지 않음
- actor가 수락한 command context가 caller value를 보존하면서 취소와 deadline은 분리
- actor가 수락한 내부 상태 전이는 제출자 context의 값을 보존하면서 취소와 deadline을
  분리하고, 폐쇄 전 수락되지 않은 내부 상태 전이는 거부
- 방 폐쇄 시작 뒤 새 command를 거부하고 현재 실행 완료 뒤 cleanup을 정확히 한 번 수행
- `Close` 대기 context 만료가 actor 종료를 취소하지 않고 후속 `Close`가 같은 완료를 관찰
- executor 또는 cleanup panic을 해당 방 terminal failure로 격리하고 다른 방과 서버
  프로세스에 전파하지 않음
- 내부 상태 전이 panic은 해당 방 terminal failure로 격리하되 보통 error는 방을
  자동 폐쇄하지 않고 제출자에게 반환
- 서버 절대 deadline과 현재 단조 시각의 차이를 정확한 timer duration으로 사용하고
  아직 timer가 발화하지 않았으면 내부 상태 전이를 실행하지 않음
- 이미 지난 deadline을 0 duration으로 제한해 즉시 만료 대상으로 처리
- deadline 또는 actor admission 대기 중 취소는 operation 실행을 막지만 actor가 이미
  수락한 operation은 취소하거나 롤백하지 않음
- deadline `Wait` context 취소는 예약 자체를 취소하지 않고, 다중 `Wait`가 같은 terminal
  결과를 관찰
- 방 폐쇄와 내부 operation의 보통 error·panic 결과가 deadline owner에게 전달되고 모든
  terminal 경로에서 timer 자원이 정리되며, deadline 전 방 폐쇄는 timer를 즉시 종료
- 새 플레이어의 준비 상태가 `false`로 시작
- 방 설정 변경 뒤 기존 플레이어의 준비 상태 유지
- 플레이어 입·퇴장과 미준비 플레이어의 팀 변경 뒤 남은 플레이어의 준비 상태 유지
- 준비 완료 플레이어의 팀 변경 거부와 상태 무변경
- 시작 요청 시 최소 인원, 팀 균형과 전원 준비 상태 재검증
- 시작 확인 10초 안에 전원 응답 시 시작
- 시작 확인 10초 만료 시 미응답자 퇴장, 시작 취소, 남은 전원 준비 해제의 단일 상태 전이
- 시작 확인 마감 뒤 지연 응답이 취소된 시작을 되살리지 않음
- 시작 전 연결 끊김
- 게임 중 단일 이탈
- 한 팀 전원 이탈
- 양 팀 전원 이탈 후 29초 복귀
- 양 팀 전원 이탈 30초 무효·폐쇄
- 중복 명령
- 같은 `(user_id, command_id)`의 동시·순차 중복이 한 번만 상태를 변경
- 중복 명령에 최초 `COMMAND_RESULT`와 최초 이벤트 sequence 범위를 재전송
- 같은 사용자가 같은 `command_id`를 다른 payload에 재사용하면 거부
- 서로 다른 사용자의 같은 `command_id`는 서로 독립
- 이벤트 순서
- 새로고침 재접속
- 스냅샷 sequence와 이후 누락 이벤트의 원자적 경계
- snapshot보다 오래된 클라이언트 sequence, event 공백·중복·역전 시
  `RESYNC_REQUIRED`와 기존 확정 상태 보존
- 재동기화 staging 완료 전 command 전송 차단과 완료 시 sequence 원자 교체
- 예상하지 못한 WebSocket 종료 뒤 250ms, 500ms, 1초, 2초, 5초의 최대 5회 재연결
- 연결 성공 시 backoff 초기화, 로그아웃 시 예약 취소, 한도 소진 뒤 자동 재연결 중단
- JavaScript/C ABI의 10진 `uint64` sequence 경계와 invalid·overflow 문자열 거부
- 실제 고정 prototype scope의 WebSocket snapshot 왕복과 새로고침 `last_sequence=0` 회귀
- synchronization bundle의 routing·연속성 검증, invalid bundle에서 command gate 잠금
- `RESYNC_REQUIRED`에 새 `command_id`, `last_sequence=0` 전체 재동기화 1회 재요청
- room/match scope 변경 뒤 이전 pending response를 적용하지 않음
- 결과 토큰 ID와 생성 원인의 재접속 복구
- 최소 명령·이벤트 예제의 JSON Schema 검증
- 인증 WebSocket의 Origin 누락·cross-host·세션 누락·만료·저장 실패 거부
- binary `1003`, 과대 메시지 `1009`, 비정상 command `1008` close
- 유효한 한글 텍스트 command의 transport 왕복
- 고정 프로토타입 방의 `SEND_CHAT`이 한 sequence만 소비하고 모든 활성 연결에 같은
  `CHAT_MESSAGE`를 전달
- 동일 `command_id` 재전송이 채팅 event를 다시 발행하지 않고 최초 응답만 반환
- sliding 1초 3개, 5초 16번째 시도 1분 차단, 동일 텍스트 5초 제한
- bounded chat queue가 가득 찬 연결을 종료하고 다른 연결 전달은 유지
- backpressure 종료가 진행 중인 write context를 먼저 취소하지 않고 실제 WebSocket
  `1013 event_backpressure` close frame을 전달
- 채팅 DOM 렌더링이 HTML을 실행하지 않고 text로 보존
- 다중 탭
- 관전자 입퇴장
- 게임 종료 후 대기실
- 재대결 순서·선공 재추첨
- 서버 재시작 무효

## 일시 정지·저장 장애

- 사용자 일시 정지는 1분 단위의 1~30분만 허용
- 정지와 재개 전후로 활성 타이머의 남은 밀리초가 보존됨
- 정지 중 CPU가 행동하지 않음
- 사용자 정지 중에도 전원 이탈 30초 감시가 진행됨
- 정지 중 전원 이탈 유예 만료 시 경기 무효·방 폐쇄
- DB 자동 정지가 사용자 일시 정지 1회 사용량을 소비하지 않음
- 경기 이벤트 최초 저장 실패 시 자동 정지
- 저장 실패 후 1초, 2초, 5초 재시도 중 성공 시 한 번만 상태 확정·전송하고 타이머 재개
- 최초 실패와 3회 재시도가 모두 실패하면 경기 무효
- 무효 이벤트도 저장할 수 없는 장애에서 메모리 종료, 클라이언트 통지, 비상 로그가 수행됨
- 자동 정지 중 새 상태 변경 명령을 큐에 쌓지 않고 재시도 가능 오류로 거부
- 채팅 저장 실패가 경기와 채팅 전달을 중단하지 않음

## SQLite 적합성·전환 시험

- 실제 목표 부하에서 SQLite WAL과 단일 쓰기 경로로 50명 동시 접속 시나리오 수행
- 경기 이벤트 저장 지연, 쓰기 큐 증가, WAL 체크포인트 지연을 측정
- 의도적인 `SQLITE_BUSY`, 디스크 가득 참, I/O 오류 주입
- 일관된 백업 생성과 복원 시험
- SQLite 고정 버전이 알려진 WAL 복구 수정이 포함된 버전인지 확인
- `docs/adr/0001_sqlite_v1_and_scale_out.md`의 전환 조건 중 하나라도 충족하면 PostgreSQL 전환 ADR을 작성

## 속성 테스트

가능한 모든 방 설정 조합을 생성해 다음 불변조건을 검사한다.

- 한 말은 동시에 두 상태에 존재하지 않음
- 완주한 말은 이동하지 않음
- 상대 팀 말이 같은 칸에 정상 상태로 공존하지 않음
- 결과 토큰은 한 번만 소비됨
- 북 장벽을 건너뛸 수 없음
- 턴 소유자가 아닌 사용자의 명령은 거부됨
- 같은 방의 대기실·채팅·경기·재대결 서버 이벤트 sequence가 `1`부터 중복·공백·
  초기화 없이 증가하고, 서로 다른 방의 sequence는 독립적임
- 승리 후 추가 이동이 적용되지 않음
