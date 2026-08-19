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

## raylib/WASM 역할

- 윷판 렌더링
- 말 렌더링과 애니메이션
- 윷 던지기 애니메이션
- 현재 턴, 결과 큐, 타이머
- 게임 내 선택 UI
- 관전 화면

## 서버 판정과 애니메이션

- 서버는 애니메이션과 무관하게 결과를 즉시 확정
- 클라이언트는 확정 이벤트를 순서대로 재생
- 애니메이션 중 새 이벤트는 버퍼링할 수 있음
- 애니메이션이 게임 판정을 바꾸지 않음
- 재접속 스냅샷은 애니메이션보다 우선

## 준비 상태

준비 완료한 플레이어는 다음 UI를 잠근다.

- 팀 변경
- 플레이어/관전자 전환
- 방 상세 설정에 영향을 주는 조작

방 설정이나 팀 구성이 변경되면 준비 상태를 초기화한다.
