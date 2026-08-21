# Issue #51 독립 테스트·리뷰 지침

이 문서는 `feat(room): add authoritative room actor foundation` 변경을 AGY와 Claude가 서로 독립적으로 검증하기 위한 인계 문서다. 두 리뷰어는 서로의 결론을 참고하지 않고 테스트와 코드 검토를 완료한 뒤 결과를 합친다.

## 1. 검토 기준과 범위

- 기준 브랜치와 커밋: `origin/main` (`248ce15`)
- 대상 브랜치: `feat/51-room-actor-foundation`
- 추적 Issue: `#51`
- 구현 범위: 방별 command 직렬 실행, 종료 경계, caller context 소유권, cleanup 호출
- 제외 범위: 방 registry, HTTP room API, SQLite event store, 경기 복구, Issue `#48`의 WebSocket 재접속 연결

우선 아래 파일을 모두 읽는다.

- `internal/application/room_actor.go`
- `internal/application/room_actor_test.go`
- `internal/application/processor.go`
- `docs/adr/0012_room_actor_execution_boundary.md`
- `docs/05_room_match_lifecycle.md`
- `docs/11_test_plan.md`
- `docs/12_open_items.md`

검토 시 `git diff`만 신뢰하지 않는다. 아직 커밋되지 않은 인계 상태에서는 새 파일이 untracked일 수 있으므로 `git status --short`의 파일도 명시적으로 확인한다. 리뷰어는 독립성을 유지하기 위해 코드나 테스트를 직접 수정하지 않고, 재현 절차와 finding을 보고한다. 수정 뒤에는 같은 리뷰어가 최종 상태를 다시 검증한다.

## 2. 공통 자동 검증

두 리뷰어 모두 저장소 루트에서 다음을 실행하고 실제 결과를 기록한다.

```sh
git status --short --branch
git diff --check
git diff --stat origin/main
env GOCACHE=/tmp/buk-yutnori-go-cache go test -race -count=100 ./internal/application
env GOCACHE=/tmp/buk-yutnori-go-cache go test -race -count=1 ./...
env GOCACHE=/tmp/buk-yutnori-go-cache go vet ./...
python3 tools/validate_specs.py all
make -B -C client test
```

현재 작업 디렉터리에는 검토 범위 밖의 ignored build artifact가 있을 수 있다. 문서, 포맷, module 정합성은 tracked 및 untracked 소스만 복사한 임시 디렉터리에서 검사한다.

```sh
REVIEW_COPY=$(mktemp -d /tmp/buk-yutnori-review.XXXXXX)
git ls-files --cached --others --exclude-standard -z | tar --null -T - -cf - | tar -xf - -C "$REVIEW_COPY"
cd "$REVIEW_COPY"
python3 tools/validate_docs.py
python3 tools/validate_specs.py all
env GOCACHE=/tmp/buk-yutnori-go-cache go mod tidy -diff
test -z "$(gofmt -l .)"
```

성공 기준은 race detector와 전체 Go 테스트에 실패가 없고, `go vet`, client 테스트, 문서·명세 검증이 모두 통과하며, `go mod tidy -diff`, `gofmt -l`, `git diff --check`가 차이를 출력하지 않는 것이다.

## 3. AGY 지침: 동시성·선형화 검토

AGY는 scheduling을 바꾸어 actor의 명령 수락과 종료가 하나의 설명 가능한 순서를 갖는지 집중 검증한다.

```sh
env GOCACHE=/tmp/buk-yutnori-go-cache GOMAXPROCS=1 go test -race -count=100 -run '^TestRoomActor' ./internal/application
env GOCACHE=/tmp/buk-yutnori-go-cache GOMAXPROCS=8 go test -race -count=100 -run '^TestRoomActor' ./internal/application
env GOCACHE=/tmp/buk-yutnori-go-cache go test -race -count=500 -run '^TestRoomActorCloseRejectsNewCommandsAndCleansUpAfterAcceptedExecution$' ./internal/application
```

다음 불변조건을 테스트 이름, 실행 결과, 코드 위치와 함께 확인한다.

1. 같은 방에서 executor가 동시에 두 번 실행되지 않는다.
2. actor가 수락한 command는 caller context 취소나 `Close` 시작 뒤에도 한 번 완료된다.
3. 실행 대기 중 아직 수락되지 않은 command는 `Close`와 경합해도 `ErrRoomActorClosed`를 받고 executor에 진입하지 않는다.
4. `Close` 이후 새 command는 즉시 거부되며, `Close`는 여러 번 호출해도 안전하다.
5. 짧은 context로 호출한 `Close`가 먼저 반환되어도 실제 종료와 cleanup은 계속 진행된다.
6. cleanup은 진행 중 실행이 끝난 뒤 정확히 한 번 호출되고, `Close`의 정상 반환보다 앞선다.
7. 서로 다른 방의 actor는 한 방의 block 때문에 함께 멈추지 않는다.
8. command의 `room_id`가 actor의 방과 다르면 executor에 도달하지 않는다.
9. executor와 cleanup panic은 프로세스로 전파되지 않고 해당 방의 terminal error가 된다.

특히 `closed` 확인과 mailbox 수신 사이의 `select` 경합을 검토한다. 종료가 시작된 뒤 대기 command가 우연히 선택되더라도 executor 호출 전에 거부되는지 확인한다. goroutine leak, 응답 channel 영구 대기, send-on-closed-channel 가능성이 보이면 재현 가능 여부와 무관하게 finding으로 남긴다.

## 4. Claude 지침: 계약·아키텍처 검토

Claude는 구현, 테스트, ADR, 정본 문서가 같은 실행 경계를 설명하는지 집중 검증한다.

다음 질문에 각각 `예/아니오`와 근거를 남긴다.

1. `RoomActor`가 UI, WebSocket, DB 세부사항 없이 순수 application 경계로 유지되는가?
2. `Processor`의 `command_id` 멱등 결과 보존과 actor cleanup이 경합해도 이미 확정된 결과를 롤백하지 않는가?
3. caller context는 mailbox 수락 전까지만 admission을 통제하고, 수락 뒤에는 value를 보존하면서 cancellation/deadline 전파를 끊으며 이를 테스트하는가?
4. malformed command, typed-nil command, 다른 `room_id`가 panic 없이 거부되는가?
5. cleanup이 room 소유 상태를 제거하기에 충분히 명시적인 한 번의 hook이고, callback panic도 해당 방에 격리되는가?
6. ADR의 대안, 선택, 결과가 실제 코드와 테스트에 의해 뒷받침되는가?
7. room registry와 event store가 구현된 것처럼 문서가 과장하지 않으며, 남은 결정은 `docs/12_open_items.md`에 유지되는가?
8. 이번 변경이 Issue `#51` 경계를 넘어서 게임 규칙이나 프로토콜 의미를 바꾸지 않는가?

누락 테스트도 finding이다. context value 보존, executor panic, cleanup panic은 ADR-0012의 계약과 테스트가 일치하는지 확인한다. 그 밖에 현재 계약이 명시하지 않은 상황은 임의로 요구사항을 만들지 말고 `명세 공백` 또는 `잔여 위험`으로 구분한다. 게임 규칙 의미 변경이 필요하다고 판단되면 구현안을 적용하지 말고 사용자 승인이 필요한 항목으로 보고한다.

## 5. 결과 보고 형식

각 리뷰어는 아래 형식으로 별도 결과를 남긴다. `PASS WITH FINDINGS`는 merge를 막지 않는 개선점만 있을 때 사용한다. 정확성, race, deadlock, 계약 위반은 `FAIL`이다.

```text
Reviewer: AGY | Claude
Verdict: PASS | PASS WITH FINDINGS | FAIL
Base: origin/main@248ce15
Reviewed head: <commit SHA 또는 working tree 상태>

Commands:
- <명령>: PASS | FAIL (<핵심 출력>)

Findings:
- [BLOCKER|HIGH|MEDIUM|LOW] <file:line> <현상과 근거>
  Reproduction: <명령 또는 scheduling 절차>
  Required handling: <수정, 문서화, 또는 잔여 위험 수용>

Residual risks:
- <검증하지 못한 항목과 이유>

Independent review statement:
- 다른 리뷰어의 결론을 보기 전에 이 검토를 완료했는가: yes | no
```

두 결과가 모두 `PASS` 또는 merge를 막지 않는 `PASS WITH FINDINGS`여야 독립 리뷰 요건을 충족한 것으로 기록한다. 수정이 발생하면 이전 결과를 그대로 재사용하지 않고 변경된 head에서 공통 검증과 담당 집중 검토를 다시 실행한다.
