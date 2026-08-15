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
전달한다. C가 보존한 값을 다시 HTML에 표시하여 양방향 경계를 확인한다.
