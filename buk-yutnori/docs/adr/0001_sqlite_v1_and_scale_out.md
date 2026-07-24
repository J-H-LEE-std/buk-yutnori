# ADR-0001: v1 SQLite와 확장 전환 기준

- 상태: 채택
- 결정일: 2026-07-23

## 맥락

v1은 단일 VPS와 단일 Go 서버를 목표로 하는 컴팩트한 서비스다. 진행 중 경기 이벤트는 감사 목적으로 저장하지만 서버 재시작 뒤 경기를 복구하지 않는다. 현재 예상 규모는 최대 50명 동시 접속이며, 다중 서버 인스턴스나 별도 DB 호스트가 필수라는 근거가 아직 없다.

## 결정

v1은 SQLite를 사용한다.

- DB 파일은 Go 서버와 같은 호스트의 로컬 영구 볼륨에 둔다.
- WAL 모드와 내구성 우선 동기화 설정을 사용한다.
- 경기 상태 변경 쓰기는 하나의 직렬화된 고우선순위 쓰기 경로로 처리한다.
- 채팅 로그는 경기 쓰기를 막지 않는 별도 저우선순위 경로로 처리한다.
- DB 드라이버와 쿼리 계층은 도메인 규칙에서 분리한다.
- 배포 시점에는 SQLite WAL 복구 관련 알려진 결함이 수정된 유지보수 버전을 고정하고 회귀 테스트한다.
- PostgreSQL 호환을 위해 SQL의 최소공배수만 강제하지는 않는다. 대신 저장소 인터페이스, 명시적 트랜잭션 경계와 데이터 내보내기 절차를 유지한다.

## PostgreSQL 전환 조건

다음 중 하나라도 사실이 되면 구현을 계속 확장하기 전에 PostgreSQL 전환 ADR을 작성한다.

- 둘 이상의 Go 서버 인스턴스가 같은 DB에 동시에 써야 한다.
- DB를 애플리케이션과 다른 호스트 또는 네트워크 파일 시스템에 둬야 한다.
- 목표 부하 시험에서 단일 쓰기 큐가 지속적으로 증가하거나 DB 쓰기 지연 때문에 명령·타이머 예산을 지키지 못한다.
- 쓰기 경합 또는 체크포인트 지연이 정상 경기를 자동 정지·무효 처리하게 만든다.
- 운영 조회·백업 작업이 경기 쓰기에 허용할 수 없는 영향을 준다.
- 고가용성, 온라인 복제 또는 서버 재시작 후 경기 복구가 v1 이후 필수 요구사항이 된다.

전환 여부는 “접속자 수가 커 보인다”는 추측이 아니라 부하·장애 주입 시험 결과로 결정한다.

## MariaDB를 기본 전환 대상으로 두지 않은 이유

MariaDB/InnoDB도 트랜잭션, 동시 접근 및 복제를 지원하는 충분히 가능한 선택이다. 배제 이유는 기능 부족이 아니다. 다만 이 프로젝트가 SQLite 한계를 넘어설 때 필요할 가능성이 높은 다중 writer, 풍부한 이벤트 payload 조회, JSON 인덱싱과 운영 생태계를 한 기본안으로 묶기 위해 PostgreSQL을 우선 전환 후보로 둔다.

MariaDB 운영 경험이나 호스팅 이점이 더 크다면 같은 전환 시점에 MariaDB도 벤치마크 후보로 비교할 수 있다. MariaDB의 `JSON`은 `LONGTEXT` 별칭이라는 차이처럼 데이터 타입과 인덱싱 의미가 PostgreSQL과 다르므로, 이름만 보고 동등하다고 가정하지 않고 실제 쿼리와 운영 요구로 판단한다.

## 결과

- 초기 운영 구성과 개발 환경이 단순해진다.
- 별도 DB 서버의 메모리·운영 비용을 피한다.
- 한 시점에 하나의 writer라는 SQLite 제약을 설계와 시험에서 명시적으로 관리해야 한다.
- WAL 파일을 포함한 백업·복원 절차와 고정 버전 검증이 필요하다.
- 전환이 필요해지면 데이터 이전과 저장소 구현 교체 비용이 발생한다.

## 근거 자료

- SQLite, Appropriate Uses For SQLite: https://sqlite.org/whentouse.html
- SQLite, Isolation In SQLite: https://sqlite.org/isolation.html
- SQLite, Write-Ahead Logging: https://sqlite.org/wal.html
- PostgreSQL, Concurrency Control: https://www.postgresql.org/docs/current/mvcc-intro.html
- MariaDB, InnoDB: https://mariadb.com/docs/server/server-usage/storage-engines/innodb
- MariaDB, Replication Overview: https://mariadb.com/docs/server/ha-and-performance/standard-replication/replication-overview
- MariaDB, JSON Data Type: https://mariadb.com/docs/server/reference/data-types/string-data-types/json
