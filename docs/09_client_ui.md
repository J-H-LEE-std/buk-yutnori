# 클라이언트 UI 요구사항

## 목표

- 데스크톱과 모바일 브라우저 모두 지원
- 반응형 UI
- 현대 브라우저 호환 목표
- 정확한 최소 지원 버전은 기술 검증 후 확정

## 구현 언어와 실행 경계

- raylib/WASM 클라이언트는 C11로 구현한다.
- 브라우저 프레임 콜백으로 raylib 갱신·렌더링을 실행하며 기본 빌드에서
  ASYNCIFY를 사용하지 않는다.
- HTML/JavaScript와 C/WASM은 명시적인 C ABI로 연결하고 문자열은 UTF-8로
  전달한다.
- 세션 원문과 Google 토큰은 WASM에 전달하지 않는다.
- 상세 근거와 최초 검증 도구 버전은
  `docs/adr/0004_c_raylib_wasm_client.md`를 따른다.

## HTML/JavaScript 셸 권장 역할

- Google 로그인
- 세션 및 초기 부트스트랩
- 인증된 WebSocket 생성과 연결 상태 표시
- 한글 IME 입력
- 채팅 텍스트 입력
- 클립보드
- 접근성용 DOM
- 브라우저 오류 표시
- 캔버스 크기·회전·가상 키보드 대응

DOM 텍스트 입력은 브라우저의 기본 편집 동작을 유지한다. Emscripten GLFW의 전역 키
캡처가 입력창의 Backspace·Tab을 막거나 게임 입력으로 중복 전달하지 않도록 DOM 입력
이벤트와 캔버스 키 이벤트의 경계를 분리한다.

Google Identity Services callback의 ID 토큰은 HTML/JavaScript가 같은 origin의
HTTP 인증 API로 직접 전달한다. 서버가 발급한 세션은 HttpOnly 쿠키이므로
JavaScript와 C/WASM이 읽지 않는다. 인증 서버가 없는 정적 WASM 실행에서는 로그인
영역만 비활성 안내를 표시하고 raylib 초기화와 IME 왕복은 계속 동작한다.

유효한 세션이 확인되면 HTML/JavaScript 셸이 현재 origin의 `/api/v1/ws`에 연결하고
연결 중·연결됨·끊김·실패 상태를 접근성 DOM에 표시한다. 로그아웃 시 브라우저
WebSocket을 먼저 닫는다. application command와 JSON 전송은 셸이 소유하고, 별도
C 프로토콜 상태 모듈이 event sequence와 재접속 snapshot의 원자적 적용 경계를
검증한다. 재동기화가 완료되기 전에는 상태 변경 command를 보내지 않는다.

예상하지 못한 연결 종료에서는 셸이 제한된 backoff로 새 WebSocket을 만들며,
로그아웃에서는 재연결을 예약하지 않는다. JavaScript는 room/match routing과 bundle
sequence를 먼저 검사한 뒤 10진 문자열 C ABI로 snapshot/event sequence를 staging한다.
실패하면 기존 확정 표시 상태를 유지하고 상태 변경 UI를 잠근다.
재접속 bundle에 event tail이 있으면 셸 reducer가 snapshot 복사본에 sequence 순서대로
표시용 변경만 적용한 뒤 C/WASM에 한 번에 staging한다. 지원하지 않는 이벤트나
payload 불일치는 전체 bundle을 거부하고 기존 확정 상태를 유지한다.

## raylib/WASM 역할

- 윷판 렌더링
- 말 렌더링과 애니메이션
- 윷 던지기 애니메이션
- 현재 턴, 결과 큐, 타이머
- 게임 내 선택 UI
- 관전 화면
- 동일 팀 말의 `stacks`/`position_groups` 정합성을 확인한 뒤 같은 칸의 말 겹침과
  업기 개수 배지를 코드 드로잉으로 표시한다. 정합성이 깨진 snapshot은 원자 적용하지 않는다.

지름길 선택 UI는 `game_snapshot.current_turn.move_request` 또는 같은 의미의
`MOVE_REQUIRED`가 제공한 `routes`만 표시한다. C/raylib는 경로 선택 intent만 만들고,
HTML/JavaScript 셸이 확정 snapshot의 opaque `token_id`·`piece_id`와 실제
room/match scope를 결합해 `SELECT_ROUTE`를 전송한다. 관전자, 상대 턴, CPU 제어,
재동기화 중과 응답 대기 중에는 선택을 잠근다.

## 서버 판정과 애니메이션

- 서버는 애니메이션과 무관하게 결과를 즉시 확정
- 클라이언트는 확정 이벤트를 순서대로 재생
- `PIECE_MOVED.movement_kind=backdo|buk`와 `BUK_RESOLVED`는 match scope와
  단조 sequence를 확인한 뒤 최대 900ms의 표시 cue만 만든다. cue는 판정을
  재계산하지 않으며 재접속 snapshot 적용 시 즉시 제거한다.
- 애니메이션 중 새 이벤트는 버퍼링할 수 있음
- 애니메이션이 게임 판정을 바꾸지 않음
- 재접속 스냅샷은 애니메이션보다 우선

## 화면 구성과 접근 순서

1. 로그인 스크린 — 비회원이 도달할 수 있는 유일한 화면이다.
2. 메인 로비 — 방 목록(`GET /api/v1/rooms`)과 전체 채팅을 표시한다. 방 목록은
   인증 세션에서만 조회하며 loading/empty/error 상태를 DOM에 표시하고, 서버가
   반환한 방 요약 필드를 `textContent`로 렌더링한다. 새로고침은 명시적 버튼으로
   재조회한다. 방 생성·플레이어 참여·관전은 같은 HTTP 세로 흐름으로 연결하며,
   성공하면 서버가 반환한 방 상세의 멤버 nickname·역할·팀·준비·시작 확인 상태를 표시한다.
   방 상세에 진입한 뒤에는 인증된 WebSocket으로 ROOM_UPDATED/GAME_STARTING을 수신해
   상세를 다시 조회하고, 플레이어의 팀 선택·준비 및 방장의 게임 시작 요청을 서버에
   전달한다. 명령 결과가 거부되면 서버 오류를 표시하며 클라이언트는 상태를 추론하지 않는다.
   전체 채팅은 모든 인증 연결이 자동 구독하는 `room_id=lobby` scope다. 셸은
   `SEND_CHAT`과 새 연결 이후의 `CHAT_MESSAGE`만 처리하며, 재접속 때 과거 목록을
   복구하지 않는다(ADR-0018). 표시명은 서버가 넣은 `sender_nickname`만 사용하고,
   브라우저는 이를 `textContent`로 렌더링한다.
3. 방 로비 — 입장 성공 후 도달한다. 방 상세 조회(멤버 전용), ROOM_UPDATED/GAME_STARTING
   구독과 팀·준비·시작 확인 명령이 데이터 소스다. 전체 채팅은 계속 `lobby` scope를
   사용하며 방 membership를 요구하지 않는다. 멤버 표시는 서버 `nickname`을
   `textContent`로 렌더링하고, profile 부재·읽기 실패 시 서버가 넣은 `user_id` fallback을
   그대로 사용한다.
4. 게임 화면 — 경기 런타임 확정 후 접근하며 snapshot과 경기 이벤트를 소비한다.
   참가자 표시는 서버가 넣은 `game_snapshot.participants[].nickname`을 사용하며,
   profile 부재·읽기 실패 시 서버가 넣은 `user_id` fallback을 그대로 표시한다.

방 상세 화면은 `GET /api/v1/rooms/{room_id}/game-logs`의 종료된 경기 로그를 별도의
스크롤 가능한 텍스트 영역으로 표시한다. 항목은 서버가 만든 `notation`을
`textContent`로만 넣으며, 빈 목록·권한 오류·서버 오류를 구분해 표시한다. 클라이언트는
경기 이벤트를 조합하거나 기보를 추론하지 않는다.

모든 화면 데이터는 로그인 세션을 전제로 하고, 방 스코프 화면(3·4)은 멤버십을
전제로 한다.

## 준비 상태

준비 완료한 플레이어는 다음 UI를 잠근다.

- 팀 변경
- 플레이어/관전자 전환

방장은 경기 시작 요청 전까지 방 상세 설정을 변경할 수 있다. 방 설정 변경,
미준비 플레이어의 팀 변경과 플레이어 입·퇴장은 기존 준비 상태를 유지하며,
새로 입장한 플레이어만 미준비 상태로 표시한다. 클라이언트는 이 상태를 자체 추론하지
않고 서버가 확정한 방 상태를 표시한다.
