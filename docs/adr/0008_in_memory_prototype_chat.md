# ADR-0008: 메모리형 프로토타입 채팅 허브

- 상태: 채택
- 결정일: 2026-08-16

## 맥락

Milestone 2는 인증된 두 브라우저가 한국어 채팅을 WebSocket으로 주고받는 수직
프로토타입이 필요하다. 현재 정식 방 생성·입장·권한 생명주기, 닉네임 설정, SQLite
이벤트 저장과 재접속 replay는 아직 없다. 이 기능들을 채팅 PR에 함께 도입하면
Milestone 3 방 서버와 Milestone 5 영구 채팅 범위를 선행 구현하게 된다.

한 WebSocket에서 command response와 비동기 server event를 함께 보내야 하므로
동시 write를 피할 세션 구조와 느린 구독자 정책도 필요하다. `docs/12_open_items.md`의
정식 방 actor/goroutine 모델은 아직 결정하지 않으며, 이번 결정은 프로토타입 채팅에
한정한다.

## 결정

- Milestone 2 서버는 로그인한 WebSocket 연결을 고정 `prototype-room`에 자동
  구독시킨다. 이 ID 외의 `SEND_CHAT`은 retriable `ROOM_NOT_FOUND`로 거부한다. 존재한
  적 없는 방에는 room-lifetime 멱등 범위가 없으므로 이 결과를 registry에 보존하지
  않는다.
- 메모리형 `PrototypeChatRoom`이 채팅 제한, room-scoped sequence와 활성 구독자를
  소유한다. 정식 방 membership이나 경기 상태를 가장하지 않는다.
- `RealtimeSession`은 reader goroutine 하나에서 command를 처리하고, session loop
  하나에서 `COMMAND_RESULT`와 `CHAT_MESSAGE`를 직렬 write한다.
- 각 구독자는 16개 이벤트의 bounded queue를 가진다. queue가 가득 차면 이벤트를
  조용히 버리지 않고 해당 연결의 명령 적용 context를 취소한 뒤 별도 WebSocket
  `1013 event_backpressure` close handshake를 시작한다. 진행 중인 read/write context를
  먼저 취소해 close frame 전송 기회를 없애지 않는다.
- `CHAT_MESSAGE`는 stable `sender_user_id`만 포함한다. 정식 클라이언트는 향후 방의
  participant 상태에서 닉네임을 해석한다. 현재 프로토타입 UI는 내부 ID를 표시한다.
- 브라우저는 메시지를 `textContent`로만 렌더링하며 최근 100개 DOM 항목만 유지한다.
  이 제한은 재접속 recent-chat snapshot 개수 정책이 아니다.
- 인증과 채팅 상태는 모두 메모리형이며 서버 재시작 시 사라진다. 채팅 영구 저장과
  replay는 SQLite event store 및 정식 방 생명주기와 함께 후속 구현한다.

## 채팅 제한

- 메시지는 Unicode code point 기준 1~200자다.
- 같은 사용자의 승인 메시지는 sliding 1초 구간에 최대 3개다.
- 프로토콜상 유효하고 `prototype-room`을 대상으로 한 모든 `SEND_CHAT` 시도를
  sliding 5초 구간에 기록한다. 16번째 시도는 거부하고 그 시점부터 1분 차단한다.
- 같은 사용자가 마지막으로 승인받은 텍스트와 byte-for-byte 동일한 텍스트를 5초
  미만에 다시 보내면 `CHAT_DUPLICATE`로 거부한다. 정확히 5초 뒤에는 허용한다.
- 위 일시적 거부는 `retriable=true`이며 멱등 processor에 영구 보존하지 않는다.

## 대안

- WebSocket 연결마다 command를 읽은 goroutine이 직접 write하면 채팅 event와
  response의 동시 write 및 순서 경쟁이 생기므로 선택하지 않는다.
- 느린 구독자의 queue가 찼을 때 event를 drop하면 클라이언트에 설명되지 않는
  sequence 공백이 생기므로 선택하지 않는다.
- 이번 단계에서 정식 방 actor와 SQLite 로그를 함께 구현하면 하나의 PR이 여러 위험
  영역을 소유하게 되므로 선택하지 않는다.

## 결과

- 두 인증 브라우저가 서버에서 한 번 확정된 같은 채팅 이벤트를 받는다.
- 같은 `command_id` 재전송은 최초 `COMMAND_RESULT`만 재전송하고 새 채팅 event나
  sequence를 만들지 않는다.
- 고정 방, 내부 ID 표시, 메모리 저장은 프로토타입 한계이며 운영 배포 대상이 아니다.
- 이 ADR은 정식 방 actor/goroutine 모델 미결정 항목을 해소하지 않는다.
