# Contributing

이 저장소는 GitHub Flow를 사용한다. 상세하고 우선하는 정책은
`docs/development/git-workflow.md`에 있다.

## 작업 시작

1. 관련 `docs/`, `spec/`, `schemas/`와 `AGENTS.md`를 읽는다.
2. 기능·버그·테스트·리팩터링·스파이크 또는 고위험 변경이면 GitHub Issue를 만든다.
3. `main`에서 최신 상태를 받아 작업 브랜치를 만든다.

브랜치 이름:

```text
<type>/<issue-or-noissue>-<description>
```

예:

```text
feat/12-result-queue
fix/27-backdo-history
docs/noissue-fix-broken-link
```

허용 type은 `feat`, `fix`, `docs`, `test`, `refactor`, `perf`, `build`,
`ci`, `chore`, `spike`다. `noissue`는 의미를 바꾸지 않는 단순 오탈자,
링크 또는 표현 수정에만 사용할 수 있다.

## 커밋과 PR

- 커밋과 PR 제목은 Conventional Commits 형식을 사용한다.
- 한 PR에는 하나의 기능 또는 위험 영역만 포함한다.
- PR 템플릿의 자기 검토와 Codex 자체 리뷰를 완료한다.
- 모든 적용 가능한 CI가 통과해야 한다.
- 정본 규칙, 보드 또는 프로토콜 문서는 경로가 문서라는 이유로 축소 검사를 적용하지 않는다.
- 게임 규칙 의미 변경은 항상 사용자 승인을 기록한다.

예:

```text
feat(rules): add result queue state machine
fix(backdo): preserve actual previous space
docs(workflow): define GitHub Flow policy
```

## 리뷰

- Milestone 0~1: 필수 외부 승인 0명
- Milestone 2 이후: 고위험 변경은 독립 리뷰 1회
- 독립 리뷰어는 사람, 별도 에이전트 세션 또는 외부 모델일 수 있다.

독립 리뷰가 GitHub 승인으로 표시되지 않는 경우 PR 본문에 리뷰어, 검토 범위,
발견 사항 및 처리 결과를 남긴다.

## 병합과 릴리스

- bootstrap 종료 뒤 `main` 직접 push 금지
- squash merge만 허용
- 병합 뒤 브랜치 삭제
- 마일스톤 릴리스만 `main`에 annotated SemVer 태그 생성
- 게시한 태그의 재사용·이동 금지

이 문서의 절차는 Git 저장소가 초기화되고 GitHub 설정이 실제 적용된 뒤 효력을
갖는다. bootstrap 정의와 설정값은 `docs/development/github-settings.md`를 따른다.
