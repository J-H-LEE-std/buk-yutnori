# ADR-0006: 인증된 WebSocket 전송 경계

- 상태: 채택
- 결정일: 2026-08-15

## 맥락

Milestone 2는 브라우저가 30일 자체 세션으로 인증된 WebSocket 연결을 만들고,
후속 방·경기 명령과 서버 이벤트를 JSON envelope로 교환할 기반이 필요하다. 이
연결은 쿠키 원문이나 Google 식별자를 application 계층에 노출하지 않아야 하며,
브라우저의 cross-site WebSocket 요청과 과대·비정상 메시지를 fail closed로
거부해야 한다.

`docs/12_open_items.md`에는 Go WebSocket 라이브러리 결정이 구현 전 ADR 항목으로
남아 있다. 표준 `net/http`에는 WebSocket 구현이 없으므로 별도 라이브러리를 좁은
전송 어댑터 뒤에 둔다.

## 결정

- 서버 WebSocket 구현은 `github.com/coder/websocket` v1.8.15를 사용한다.
- 라이브러리의 context 기반 read/write, same-host origin 기본 검증, 명시적
  `SetReadLimit`과 기본 압축 비활성 동작을 사용한다.
- 브라우저 전용 endpoint는 `GET /api/v1/ws`다. `Origin`이 없거나 Origin host가
  요청 Host와 다르면 세션 조회와 upgrade 전에 거부한다. 라이브러리의 origin
  검증도 이중 방어로 유지한다.
- `__Host-buk_session` 원문은 인증 서비스에만 전달한다. upgrade 뒤 application
  session handler에는 검증된 내부 `user_id`와 프로젝트 소유 연결 어댑터만
  전달한다.
- 연결은 텍스트 JSON만 허용하고 수신 메시지를 16 KiB로 제한한다. binary는
  WebSocket `1003`, 과대 메시지는 `1009`, 비정상 v1 client command는 `1008`로
  연결을 종료한다.
- 프로젝트 연결 어댑터는 현재 스키마의 모든 client command envelope와 payload를
  엄격히 decode한다. 알 수 없는 필드, 중복 object key, trailing JSON, 잘못된
  command scope를 거부한다.
- 최초 transport PR의 기본 session handler는 유효한 application command에 상태를
  바꾸지 않고 `1013`으로 닫는 기반만 제공했다. 후속 Issue #42 application PR은
  인증된 command processor와 `COMMAND_RESULT` loop를 실제 서버에 연결한다. 방·경기
  executor가 없는 동안에는 상태를 적용하지 않고 retriable
  `APPLICATION_UNAVAILABLE` 결과를 반환한다.
- 전송 계층은 방, 경기, RNG, 턴, sequence 또는 승패 상태를 소유하지 않는다.

## 대안

- `github.com/gorilla/websocket`은 널리 쓰이고 API가 안정적이지만, 이 프로젝트는
  context 기반 취소와 좁은 JSON 어댑터를 우선한다.
- `golang.org/x/net/websocket`은 공식적으로 deprecated 상태이므로 선택하지 않는다.
- WebSocket을 직접 구현하면 프레임, close handshake, origin과 크기 제한의 보안
  위험이 커지므로 선택하지 않는다.

## 결과

- 인증과 origin 검증이 HTTP upgrade 전에 완료되고, raw 세션 토큰은 transport
  아래로 내려가지 않는다.
- 제3자 라이브러리는 `internal/wsapi` 내부에 격리되어 방·경기 application 코드는
  프로젝트 연결 계약에만 의존한다.
- 압축은 벤치마크 근거가 생기기 전까지 활성화하지 않는다.
- reverse proxy는 외부 Host와 Origin host를 보존해야 한다. 운영 HTTPS/WSS와
  trusted proxy 세부 설정은 Milestone 6 배포 ADR에서 확정한다.
- graceful shutdown 중 hijacked 연결 정리와 heartbeat 정책은 실제 connection
  registry를 도입할 때 별도로 결정한다.

## 근거 자료

- [coder/websocket repository](https://github.com/coder/websocket)
- [coder/websocket v1.8.15 package documentation](https://pkg.go.dev/github.com/coder/websocket@v1.8.15)
- [gorilla/websocket repository](https://github.com/gorilla/websocket)
