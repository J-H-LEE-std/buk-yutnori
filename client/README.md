# C raylib/WASM 클라이언트

이 디렉터리는 서버가 확정한 상태를 표시하는 C 클라이언트의 시작점이다. 브라우저
전용 기능인 Google 로그인, 세션 부트스트랩, WebSocket 전송과 한글 IME 입력은
HTML/JavaScript 셸이 소유한다. C/WASM은 표시 상태, 렌더링과 애니메이션을
소유한다.

## 네이티브 계약 테스트

```sh
make -C client test
```

이 테스트에는 raylib나 Emscripten이 필요하지 않다.

## WebAssembly 빌드

검증된 도구 버전은 다음과 같다.

- raylib 6.0 (`dbc56a87da87d973a9c5baa4e7438a9d20121d28`)
- Emscripten 5.0.4

raylib 소스와 활성화된 Emscripten SDK가 준비된 셸에서 실행한다.

```sh
make -C client wasm RAYLIB_PATH=/absolute/path/to/raylib
python3 -m http.server --directory build/client/web 8080
```

그 뒤 `http://localhost:8080/`을 연다. 브라우저 보안 정책 때문에 생성된 HTML을
`file://`로 직접 열지 않는다.

HTML 입력은 조합 중인 IME 값을 C에 보내지 않고 `compositionend` 뒤 UTF-8로
전달한다. C가 보존한 값을 다시 HTML에 표시하여 양방향 경계를 확인한다. DOM 입력
중에는 Emscripten GLFW의 전역 키 캡처를 차단하므로 Backspace와 Tab 같은 브라우저
기본 편집 키가 정상 동작한다.

## Google 로그인 수직 프로토타입

Google Cloud에서 Web application client ID를 만들고 authorized JavaScript origin에
`http://localhost:8080`을 등록한다. WASM build 뒤 저장소 루트에서 실행한다.

```sh
BUK_GOOGLE_CLIENT_ID="<web-client-id>.apps.googleusercontent.com" \
  go run ./cmd/server
```

`<web-client-id>` 부분은 Google Cloud에서 발급받은 실제 Web application client ID로
교체한다. 예시 문자열을 그대로 실행하면 Google이 `invalid_client`로 거부한다.
다운로드한 client secret JSON은 이 로그인 흐름에서 사용하지 않는다.

그 뒤 `http://localhost:8080/`을 연다. 브라우저 셸은 공개 client ID를 서버에서
조회하고 Google popup callback의 ID 토큰을 같은 origin JSON API로 전달한다.
서버 세션은 HttpOnly 쿠키라 JavaScript나 C/WASM에서 읽을 수 없다.

유효한 세션이 확인되면 셸은 같은 origin의 `/api/v1/ws`에 연결하고 로그인 영역에
실시간 연결 상태를 표시한다. 현재 단계는 인증된 전송 기반만 제공하므로 브라우저가
application command를 보내면 서버는 상태를 변경하지 않고 `1013`으로 연결을 닫는다.
명령 처리와 자동 재접속은 후속 단계에서 추가한다.

현재 서버는 메모리 인증 저장소를 사용하는 기술 프로토타입이다. 쿠키 만료는
30일이지만 서버를 재시작하면 다시 로그인해야 한다. 운영 전에 SQLite 세션 저장소로
교체해야 한다.
