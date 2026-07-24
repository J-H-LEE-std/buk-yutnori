# GitHub 설정 초안

이 문서는 GitHub 저장소 생성 뒤 적용할 설정의 선언적 초안이다. 파일을 추가하는
것만으로 GitHub 설정이 변경되지는 않는다.

## 저장소

- 기본 브랜치: `main`
- squash merge: 활성화
- merge commit: 비활성화
- rebase merge: 비활성화
- 병합 뒤 head branch 자동 삭제: 활성화

## `main` ruleset

bootstrap 완료 직후 다음 설정을 적용한다.

- Pull Request를 통한 변경 요구
- required approvals: Milestone 0~1에서는 0
- conversation resolution 요구
- required status checks:
  - `policy / required`
  - `ci / required`
- 태그 push workflow:
  - `release / tag policy`
- branch 최신화 요구 여부는 CI 시간과 충돌률을 측정한 뒤 결정
- force push 금지
- branch 삭제 금지
- 직접 push 금지
- 우회 권한은 복구 목적의 최소 관리자에게만 제한하고 일상 개발에는 사용하지 않음

Milestone 2부터 고위험 변경의 독립 리뷰 증빙을 `policy / required`가 검사한다.
GitHub의 승인 수는 사람 리뷰만 계산하므로 저장소 전체 required approvals를
무조건 1로 올리면 별도 에이전트나 외부 모델 리뷰를 인정할 수 없다. 사람 리뷰만
정책으로 바꾸기로 결정하기 전까지 GitHub 승인 수와 독립 리뷰 증빙을 구분한다.

## Actions 권한

- workflow 기본 토큰 권한은 contents read
- 각 workflow가 필요한 최소 권한만 개별 선언
- 외부 fork PR에서는 비밀값을 제공하지 않음
- action은 major tag 또는 검토한 commit SHA로 고정
- 비밀값을 로그, artifact 또는 PR 본문에 기록하지 않음

## Bootstrap 적용 순서

1. Git 저장소 초기화와 최초 import
2. GitHub 원격 생성 및 연결
3. 기본 CI가 `main`에서 성공하는지 확인
4. 저장소 merge 설정 적용
5. `main` ruleset 적용
6. bootstrap 종료를 Issue 또는 PR에 기록

현재 문서화 작업은 위 작업을 수행하지 않는다.
