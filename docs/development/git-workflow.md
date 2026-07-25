# Git 운영 정책

## 1. 상태와 범위

- 상태: 채택
- 채택일: 2026-07-24
- workflow: GitHub Flow
- 기본 브랜치: `main`

이 문서는 브랜치, 커밋, Pull Request, 리뷰, 병합과 릴리스의 정본이다.
게임 및 제품 요구사항의 권위는 기존 `docs/`, `spec/`, `schemas/` 우선순위를
따른다.

## 2. Bootstrap

bootstrap은 다음 네 조건이 모두 완료된 시점에 종료된다.

1. 최초 파일 import
2. 기본 CI 활성화
3. GitHub 원격 저장소 연결
4. `main` 보호 ruleset 적용

bootstrap 종료 전에는 위 작업을 위해 최소한의 직접 커밋을 허용한다.
종료 사실은 Issue 또는 bootstrap PR에 기록한다. 종료 뒤에는 관리자도 일상적인
변경을 `main`에 직접 push하지 않고 PR을 사용한다.

## 3. 브랜치

모든 작업 브랜치는 `main`에서 만든다.

```text
<type>/<issue-or-noissue>-<description>
```

허용 type:

- `feat`: 기능
- `fix`: 버그 수정
- `docs`: 문서
- `test`: 테스트
- `refactor`: 의미를 유지하는 구조 개선
- `perf`: 성능 개선
- `build`: 빌드·의존성
- `ci`: CI
- `chore`: 기타 유지보수
- `spike`: 폐기 가능한 기술 검증

`issue-or-noissue`:

- 기능, 버그, 테스트, 리팩터링, 스파이크 및 모든 고위험 변경은 숫자 Issue가 필수다.
- 의미를 바꾸지 않는 단순 오탈자·링크·표현 수정만 `noissue`를 사용할 수 있다.
- `noissue` 브랜치는 `docs` type만 허용한다.

`description`은 소문자 영문, 숫자와 하이픈을 사용한다.

## 4. 고위험 변경

다음 영역은 고위험이다.

- 게임 규칙, 보드, RNG, 턴, 승패
- 백도, 북, CPU
- 멱등성, sequence, WebSocket, 재접속
- 인증, 세션, 관리자 권한
- DB 트랜잭션, 감사 이벤트, 마이그레이션
- Docker, HTTPS/WSS, 비밀값

경로는 분류를 돕는 신호일 뿐이다. 실제 동작이나 정본 의미가 바뀌면 파일 확장자와
관계없이 고위험으로 분류한다.

## 5. 커밋

커밋 메시지는 Conventional Commits 형식을 따른다.

```text
<type>(<optional-scope>): <description>
```

- 제목은 명령형으로 간결하게 쓴다.
- 파괴적 변경은 `!`와 본문의 `BREAKING CHANGE:`를 사용한다.
- 작업 브랜치 안의 `fixup!` 또는 임시 커밋은 허용하지만 `main`에는 squash 결과만 남긴다.
- 비밀값, 생성된 개인 설정과 불필요한 바이너리를 커밋하지 않는다.

## 6. Pull Request

- 한 PR에는 하나의 기능 또는 위험 영역만 포함한다.
- PR 제목은 Conventional Commits 형식이며 squash 뒤 `main` 커밋 제목이 된다.
- 숫자 Issue 브랜치는 PR 본문에서 같은 Issue를 연결한다.
- 자기 검토 체크리스트, Codex 자체 리뷰와 모든 적용 가능한 CI가 필수다.
- 피드백 반영 뒤 영향을 받는 검사를 다시 실행한다.
- unresolved review conversation이 있으면 병합하지 않는다.
- PR 템플릿의 변경 분류는 최소 하나를 선택해야 한다.
- 의미를 바꾸지 않는 단순 문서 수정과 정본 의미 변경·고위험 변경·게임 규칙 의미
  변경은 동시에 선택할 수 없다.

## 7. 리뷰 단계

### Milestone 0~1

- GitHub 필수 외부 승인: 0명
- 자기 검토 체크리스트: 필수
- Codex 자체 리뷰: 필수
- 적용 가능한 모든 CI: 필수
- PR의 `외부 승인 필요` 체크박스는 비워 둔다.

### Milestone 2 이후

고위험 변경은 독립 리뷰 1회를 추가로 요구한다. 독립 리뷰는 다음 중 하나다.

- 사람의 GitHub review
- 원 작성 세션과 분리된 에이전트 세션
- 외부 모델 리뷰

GitHub 외부 리뷰는 PR 본문에 리뷰 주체, 검토 범위, 발견 사항과 처리 결과를
기록한다. 작성자가 같은 맥락에서 수행한 자기 검토는 독립 리뷰로 계산하지 않는다.

### 게임 규칙 의미 변경

게임 규칙의 의미 변경은 Milestone과 관계없이 항상 사용자 승인이 필요하다.
PR에는 승인된 결정이나 대화의 추적 가능한 근거를 남긴다. 독립 리뷰는 사용자
승인을 대체하지 않는다.

## 8. Docs-only 판정

docs-only 여부는 경로가 아니라 의미 변화로 판단한다.

축소 검사가 가능한 변경:

- 의미를 바꾸지 않는 오탈자
- 깨진 링크 수정
- 표현과 서식 개선

전체 관련 검사가 필요한 변경:

- 정본 게임 규칙
- 보드 그래프와 이동 의미
- RNG, 턴, 승패, 백도, 북, CPU 정책
- 프로토콜과 스키마 계약
- 인증·저장·배포의 보안 또는 동작 의미

축소 검사 여부가 불분명하면 전체 관련 검사를 실행한다.

## 9. 병합

- squash merge만 허용한다.
- merge commit과 rebase merge는 사용하지 않는다.
- 병합 뒤 원격 작업 브랜치를 삭제한다.
- bootstrap 종료 뒤 `main` 직접 push와 force push를 금지한다.
- 작업 브랜치의 force push는 본인 소유 브랜치에서 `--force-with-lease`만 허용하며,
  리뷰가 시작된 뒤에는 리뷰어에게 알린다.

## 10. CI와 병합 조건

- 모든 적용 가능한 CI가 성공해야 한다.
- branch protection에는 안정적인 집계 체크 `policy / required`와
  `ci / required`를 필수로 등록한다.
- 세부 job은 변경 종류에 따라 실행 또는 생략할 수 있지만 집계 체크는 항상 실행한다.
- 필수 체크를 우회하거나 이름을 임의로 바꾸지 않는다.
- CI 예외가 필요하면 별도 Issue와 PR로 정책 자체를 변경한다.

## 11. 릴리스

- 태그 형식은 `vMAJOR.MINOR.PATCH`다.
- annotated tag만 사용한다.
- `main`의 마일스톤 완료 커밋에만 태그한다.
- v1 이전 마일스톤은 원칙적으로 `v0.MINOR.0`을 사용한다.
- 게시한 태그를 재사용하거나 다른 커밋으로 이동하지 않는다.
- 태그 생성과 릴리스 노트는 해당 마일스톤 완료 PR 또는 Issue에서 추적한다.
- `release / tag policy`는 태그 형식, annotated tag 여부와 `main` 도달 가능성을
  검사하는 초기 안전망이다.
