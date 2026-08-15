# ADR-0004: C raylib/WASM 클라이언트와 브라우저 경계

- 상태: 채택
- 결정일: 2026-08-15

## 맥락

Milestone 2를 시작하려면 raylib 클라이언트 언어와 HTML/JavaScript 셸이 WASM과
상호 작용하는 경계를 먼저 확정해야 한다. raylib와 공식 Web 예제는 C를 기준으로
하며, 프로젝트의 정본 게임 판정은 Go 서버에 있다. 브라우저의 Google 로그인,
HttpOnly 세션 쿠키, WebSocket과 한글 IME는 DOM 및 브라우저 API와 직접 맞닿는다.

`docs/11_test_plan.md`는 C++ 검사를 예시로 들었지만
`docs/12_open_items.md`는 C와 C++ 중 선택을 미결정으로 두고 있었다. C++을 이미
채택한 것으로 해석하지 않고 이번 결정으로 모호함을 해소한다.

## 결정

- raylib/WASM 클라이언트는 C11로 작성한다.
- raylib는 C API를 직접 사용하고 C++ 래퍼나 Embind를 기본 경계로 도입하지 않는다.
- HTML/JavaScript 셸은 Google 로그인, 세션 부트스트랩, WebSocket 전송, 한글 IME,
  접근성 DOM과 브라우저 오류 표시를 소유한다.
- C/WASM은 서버가 확정한 표시 상태, 이벤트 재생 큐, 렌더링과 애니메이션을
  소유한다. 이 상태는 서버 정본을 대체하지 않는다.
- JavaScript와 C 사이는 명시적이고 좁은 C ABI로 연결한다. 문자열은 UTF-8로
  전달하며 세션 원문이나 Google 토큰을 WASM에 노출하지 않는다.
- 서버 WebSocket JSON envelope는 변경하지 않는다. 셸은 전송을 담당하고,
  프로토콜 의미와 sequence 적용은 후속 클라이언트 상태 모듈에서 검증한다.
- Web 빌드는 Emscripten을 사용한다. 브라우저가 프레임 루프를 소유하도록
  `emscripten_set_main_loop`를 사용하고 기본 빌드에서 ASYNCIFY를 사용하지 않는다.
- 최초 검증 버전은 raylib 6.0과 Emscripten 5.0.4로 고정한다. 의존성 갱신은 빌드와
  브라우저 검사를 통과한 별도 변경으로 수행한다.

## 결과

- 공식 raylib C/Web 구조와 프로젝트 클라이언트의 구현 언어가 일치한다.
- 한글 조합 입력과 인증 같은 브라우저 기능을 캔버스 입력 처리에 억지로 넣지 않는다.
- C 상태 코드는 raylib와 분리하여 네이티브 단위 테스트를 실행할 수 있다.
- C의 수동 메모리 관리 위험은 경계별 고정 용량, 입력 거부와 단위 테스트로 제한한다.
- 복잡한 C++ 전용 요구가 실제 측정으로 확인되기 전에는 C++로 전환하지 않는다.

## 참고

- [raylib Working for Web (HTML5)](https://github.com/raysan5/raylib/wiki/Working-for-Web-%28HTML5%29)
- [raylib 6.0 Web window example](https://github.com/raysan5/raylib/blob/dbc56a87da87d973a9c5baa4e7438a9d20121d28/examples/core/core_window_web.c)
- [Emscripten C/C++ and JavaScript interaction](https://emscripten.org/docs/porting/connecting_cpp_and_javascript/Interacting-with-code.html)
