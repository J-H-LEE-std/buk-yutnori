# C raylib/WASM 클라이언트

이 디렉터리는 서버가 확정한 상태를 표시하는 C 클라이언트의 시작점이다. 브라우저
전용 기능인 Google 로그인, 세션 부트스트랩, WebSocket 전송과 한글 IME 입력은
HTML/JavaScript 셸이 소유한다. C/WASM은 표시 상태, 렌더링과 애니메이션을
소유한다.

## 네이티브 계약 테스트

```sh
make -C client test
```

이 테스트에는 raylib나 Emscripten이 필요하지 않다. UTF-8 표시 상태와 함께 재접속
snapshot/event sequence를 staging한 뒤 원자적으로 확정하는 프로토콜 상태와 10진
문자열 `uint64` JavaScript/C bridge를 검사한다. 표시 상태 테스트는 snapshot
metadata·말·결과 큐를 동적 staging하고, 잘못된 enum·칸·말 상태 조합이나 불완전한
bundle이 이전 확정 화면을 바꾸지 않는지도 검사한다.

Milestone 4 보드 레이아웃 테스트는 1280×720 논리 화면의 letterbox 계산과
`spec/board_graph.yaml`의 29개 노드 좌표 매핑을 검사한다. 컴파일되는 노드·간선
테이블은 `tools/validate_specs.py board`가 정본 YAML과 정확히 같은지도 검사한다.

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

현재 게임 캔버스는 최종 이미지가 없어도 raylib 프리미티브로 정본 판의 32개 간선과
29개 노드를 렌더링한다. HTML 셸은 1280×720 논리 캔버스의 왼쪽 정사각형 판 영역을
표시하고 오른쪽 상태·입력은 한글 DOM 패널로 배치한다. 네이티브 창은 같은 논리
화면을 유지하면서 창 크기에 맞춰 letterbox한다. 유효한 authoritative
`game_snapshot`이 확정되면 실제 말 위치, 대기·완주 수, 경기·턴·입력·타이머 상태와
결과 큐를 같은 캔버스에 표시한다. 말 ID 원문은 JavaScript가 snapshot 순번과 함께
보관하고 C/WASM에는 길이 제한이 없는 순번 매핑만 전달한다. 지름길 선택 입력과
업기·겹침 전용 표현은 서버 snapshot을 따른다. 말 선택 시 서버가 제공한 이동 경로를
판 위에 표시하며 결과 선택 후 필요한 지름길 선택을 별도로 확정한다.

로그인·메인 로비·방 로비·게임은 하나씩만 보인다. IME 진단 패널은 URL에
`?diagnostics`를 붙였을 때만 표시한다.

## 실제 브라우저 회귀 테스트

WASM 빌드 후 별도 터미널에서 격리된 테스트 서버를 실행한다.

```sh
BUK_BROWSER_HARNESS=1 go test ./cmd/server -run '^TestBrowserHarness$' -v -timeout 12m
```

이 서버는 테스트 바이너리 전용 인증 검증기와 임시 SQLite를 사용하며 `google.yaml`과
개발 DB를 읽지 않는다. 10분 후 자동 종료된다. 실제 Google 로그인 검증을 대신하지는 않는다.
별도 테스트 프로필의 Chrome을 remote debugging 포트 9231로 실행하여
`http://localhost:8766/`을 연 다음 Node.js 18 이상에서 실행한다.

```sh
node client/tests/playable_browser_test.mjs http://localhost:9231 http://localhost:8766/ build/evidence-161
```

모바일 인증 부트스트랩·재시도 경로도 CI에서 headless Chrome으로 검사한다. 동일한
격리 서버와 브라우저를 직접 실행한 경우 아래 테스트로 세션 본문 지연, 설정/로그인
오류, 재시도 버튼 상태와 Google Identity Services의 중복 로드 방지를 검증할 수 있다.

```sh
node client/tests/auth_bootstrap_browser_test.mjs http://localhost:9231 http://localhost:8766/
```

테스트는 해당 브라우저의 쿠키를 초기화하므로 개인 브라우저 프로필을 사용하지 않는다.
로그인·프로필·방 생성·CPU 추가·준비·시작·던지기 결과·서버 경로·내 말 이동·모바일
폭·새로고침 복구를 실제 HTTP/WebSocket과 브라우저 클릭으로 검증한다.

## Google 로그인 수직 프로토타입

Google Cloud에서 Web application client ID를 만들고 authorized JavaScript origin에
`http://localhost:8080`을 등록한다. 저장소의 `google.yaml.example`을
`google.yaml`로 복사한 뒤 `web_client_id`를 실제 값으로 교체하거나, 아래처럼 환경
변수를 지정한다. WASM build 뒤 저장소 루트에서 실행한다.

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
실시간 연결 상태를 표시한다. 로그인한 연결은 정식 `lobby` 전체 채팅 scope에 자동
구독하며, HTML 채팅 입력의 `SEND_CHAT`을 멱등 application processor로 전달한다.
서버가 확정한 `CHAT_MESSAGE`는 모든 활성 인증 연결에 전달되고 셸은 내용을 DOM
`textContent`로 렌더링한다. 승인 메시지는 비차단 최선노력으로 SQLite 로그에 남지만
새로고침·재접속 시 과거 목록을 replay하지 않는다. 발신자 표시는 서버가 넣은
`sender_nickname`을 사용하며, profile이 아직 없거나 서버 조회가 실패한 경우에는
안정적인 내부 `user_id`가 그 표시 필드에 들어온다.

브라우저는 로그인 시 재접속 scope를 임의로 만들지 않는다(ADR-0013 은퇴, #82).
재접속 machinery(`setStateReconnectScope` → `RECONNECT` → bundle staging)는 유지되며,
정식 방 라비 화면이 시작된 방의 실제 `match_id`(GAME_STARTING 방송)로 scope를 설정하면
새 연결에서 마지막 확정 sequence로 `RECONNECT`를 보내고 서버가 조립한 실데이터
snapshot을 적용한다. 적용이 완료되기 전 state-changing command gate는 닫혀 있다.
셸은 `game_snapshot.schema.json`의 표시 관련 구조와 모든 canonical enum·보드 칸을
검증하고, C/WASM이 snapshot body와 sequence를 모두 staging한 뒤에만 둘을 함께
확정한다. 현재 서버는 최신 sequence 경계에서 snapshot을 만들기 때문에 replay event
tail이 비어 있다. payload reducer가 구현되기 전 비어 있지 않은 tail은 부분 적용하지
않고 기존 화면을 유지한 채 fail-closed한다.
예상하지 못한 WebSocket 종료는 최대 5회의 제한된 backoff로 새 연결을 만든다.

`SEND_CHAT` 이외의 현재 셸 경로 명령 중 방·경기 command는 정식 레지스트리 실행기로
전달된다. `THROW_YUT`, `SELECT_*`, `RECONNECT`는 started 방의 경기 런타임과 실데이터
snapshot을 대상으로 하며, 멱등 처리와 거부 코드는 docs/06을 따른다. 이전 채팅은
재접속 snapshot에 포함하지 않고 새 연결 뒤 메시지만 표시한다.

현재 서버는 SQLite 인증 저장소를 사용한다. 저장소 루트의 ignored `google.yaml`에
`web_client_id`만 두거나 `BUK_GOOGLE_CLIENT_ID`를 지정해 실행할 수 있으며, 유효한
30일 세션은 서버 재시작 뒤에도 유지된다. 방과 진행 중 경기 런타임은 v1에서 메모리
전용이므로 서버 재시작 시 복구하지 않는다.
