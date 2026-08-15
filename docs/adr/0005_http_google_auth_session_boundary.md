# ADR-0005: Go HTTP Google 인증과 자체 세션 경계

- 상태: 채택
- 결정일: 2026-08-15

## 맥락

Milestone 2의 웹 수직 프로토타입은 Google 로그인 결과를 서버에서 검증한 뒤 내부
사용자 ID와 30일 자체 세션으로 교환해야 한다. Google ID 토큰과 브라우저 세션은
서로 목적과 수명이 다르며, 어느 쪽도 내부 사용자 ID나 WASM 표시 상태가 되어서는
안 된다. `docs/12_open_items.md`에는 Go HTTP/WebSocket 프레임워크 결정이 구현 전
ADR 항목으로 남아 있었다.

## 결정

- 인증 HTTP API는 Go 표준 `net/http`로 구현한다. 현재 필요한 라우팅과 미들웨어
  범위에는 별도 HTTP 프레임워크를 도입하지 않는다. WebSocket 라이브러리는 후속
  ADR에서 별도로 결정한다.
- 브라우저는 Google Identity Services JavaScript callback으로 받은 `credential`을
  같은 origin의 JSON `POST /api/v1/auth/google`로 전달한다. 상태 변경 요청은
  `X-Buk-Request: 1` 사용자 정의 헤더와 JSON content type을 요구하며 CORS를 열지
  않는다. Google이 로그인 URI에 직접 form POST하는 모드는 이 API에서 사용하지
  않는다.
- 서버는 `cloud.google.com/go/auth/credentials/idtoken` v0.20.0으로 서명, 만료와
  audience를 검증한다. 그 뒤 issuer가 `accounts.google.com` 또는
  `https://accounts.google.com`인지 다시 확인하고, 검증된 `sub`만 외부 계정
  식별자로 전달한다.
- 내부 사용자 ID는 Google `sub`와 별도의 128-bit 서버 난수 ID로 생성한다.
- 세션 원문은 256-bit 서버 난수이며 브라우저 쿠키에만 둔다. 저장소 경계에는
  SHA-256 digest만 전달한다.
- 쿠키 이름은 `__Host-buk_session`이며 `Secure`, `HttpOnly`, `Path=/`, Domain 없음,
  `SameSite=Strict`를 사용한다. 만료는 생성 시점부터 절대 30일이고 마지막 사용으로
  연장하지 않는다.
- `GET /api/v1/auth/session`은 유효한 세션의 내부 user ID만 반환한다.
  `DELETE /api/v1/auth/session`은 세션을 폐기하고 쿠키를 제거하며 반복 호출에 대해
  멱등이다.
- 현재 `MemoryStore`는 수직 프로토타입과 테스트 어댑터다. 서버 재시작 뒤 세션을
  보존하지 않으므로 운영용이 아니다. 운영 배포 전 `docs/07_auth_profiles_data.md`에
  정의된 SQLite 영구 세션 저장소가 같은 인터페이스를 구현해야 한다.

## 결과

- Google 토큰 검증, 세션 수명과 저장소가 HTTP·UI에서 분리되어 테스트 가능하다.
- Google credential과 세션 원문이 C/WASM이나 저장소 레코드로 흘러가지 않는다.
- `SameSite=Strict` 때문에 다른 사이트에서 진입한 첫 요청에는 쿠키가 전송되지 않을
  수 있다. 현재 동일 origin 앱과 Google popup callback에는 맞으며, UX 요구가 바뀌면
  보안 검토와 함께 재평가한다.
- Google 검증 라이브러리와 그 전이 의존성이 Go 모듈에 추가된다.
- 메모리 어댑터만으로는 “30일 로그인 유지”의 재시작 내구성을 충족하지 못한다.
  SQLite 어댑터가 완료되기 전에는 프로토타입 서버를 운영 환경에 배포하지 않는다.

## 근거 자료

- [Google: Verify the Google ID token on your server side](https://developers.google.com/identity/gsi/web/guides/verify-google-id-token)
- [Google Cloud Go auth idtoken package](https://pkg.go.dev/cloud.google.com/go/auth/credentials/idtoken)
- [MDN: Set-Cookie](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Set-Cookie)
