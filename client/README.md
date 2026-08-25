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
29개 노드를 렌더링한다. HTML 셸이 1280×720 캔버스를 16:9로 축소하고, 네이티브
창은 같은 논리 화면을 유지하면서 창 크기에 맞춰 letterbox한다. 유효한 authoritative
`game_snapshot`이 확정되면 실제 말 위치, 대기·완주 수, 경기·턴·입력·타이머 상태와
결과 큐를 같은 캔버스에 표시한다. 말 ID 원문은 JavaScript가 snapshot 순번과 함께
보관하고 C/WASM에는 길이 제한이 없는 순번 매핑만 전달한다. 지름길 선택 입력과
업기·겹침 전용 표현, 애니메이션은 후속 Milestone 4 슬라이스다.

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
실시간 연결 상태를 표시한다. 로그인한 연결은 Milestone 2 전용
`prototype-room`에 자동 구독하며, HTML 채팅 입력의 `SEND_CHAT`을 멱등 application
processor로 전달한다. 서버가 확정한 `CHAT_MESSAGE`는 같은 방의 모든 활성 연결에
전달되고 셸은 내용을 DOM `textContent`로 렌더링한다. 닉네임 경계가 아직 없으므로
현재 발신자는 내부 `user_id`로 표시한다.

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

현재 서버는 메모리 인증 저장소를 사용하는 기술 프로토타입이다. 쿠키 만료는
30일이지만 서버를 재시작하면 다시 로그인해야 한다. 운영 전에 SQLite 세션 저장소로
교체해야 한다.
