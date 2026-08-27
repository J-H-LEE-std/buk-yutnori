# GUI 리소스와 애니메이션 가이드라인

## 목적과 범위

이 문서는 Milestone 4(전체 보드 UI 통합)에서 클라이언트가 사용하는 GUI 리소스의
분류·배치·포맷·명명과 애니메이션 정의 방식을 규정한다. 화면 구성·역할 분리·재생
모델의 정본은 `docs/09_client_ui.md`다.

- **정하는 것**: 4단계 화면별 리소스 소유, 디렉터리 구조와 명명, 포맷·크기 기준,
  보드 좌표 매핑 규칙, 렌더 레이어 순서, 말·윷 상태 대응표, v1 필수 애니메이션
  카탈로그, 초기 로딩 예산.
- **여기서 확정하지 않는 것**: 아트 스타일·색상표, 사운드 포맷, 텍스처 atlas,
  경로 따라가기 이동 연출, 접근성 테마. 구현 과정에서 필요해지면 본 문서에
  개정으로 추가하고 `docs/12_open_items.md`에 미결 항목을 남긴다.
- 게임 판정은 전부 서버 소유다. 리소스와 애니메이션은 표현 계층일 뿐이며 어떤
  경우에도 판정을 바꾸지 않는다(docs/09 서버 권위 재생 모델).

## 화면 4단계와 리소스 소유

`docs/09`의 화면 순서와 와이어프레임(`assets/wireframe/`, gitignore된 로컬 참고
자료 — 저장소 정본이 아님)를 근거로 각 단계의 리소스 소유를 나눈다.

| 단계 | 화면 | 주 데이터 소스 | 리소스 소요 |
|---|---|---|---|
| 1 | 로그인 스크린 | HTML/JS 셸(DOM) | 캔버스 배경 1장 또는 투명. DOM이 주체라 raylib 리소스 최소 |
| 2 | 메인 로비 | `GET /api/v1/rooms` + 전체 채팅(계약 미결) | 공통 위젯 세트(패널·버튼·목록 행), 방 상태 아이콘 |
| 3 | 방 로비 | ROOM_UPDATED + 방 상세 조회 + 팀/준비/시작 명령 | 플레이어 슬롯 프레임, 팀 A/B 마커, 준비 상태 아이콘, 관전자 마커 |
| 4 | 게임 화면 | snapshot + 경기 이벤트 | 윷판 아트, 칸 마커, 말 스프라이트, 윷가락 결과 프레임, 이펙트, HUD |

공통 위젯은 단계별로 복제하지 않고 `gui/common/` 한 벌만 정의한다.

## 디렉터리 구조와 명명

원본 소스는 `client/assets/`에 두고, `client/Makefile`이 `build/client/web/assets/`
로 복사한다. 서버는 변경 없이 기존 webRoot 정적 서빙으로 `/assets/**` 같은 origin
경로를 제공하며 새 API는 없다. 네이티브(desktop) 빌드도 동일한 상대 경로
`assets/`를 실행 시점 기준으로 읽는다.

```text
client/assets/
  gui/common/    panel.png button_normal.png button_hover.png button_pressed.png
                 button_disabled.png slot_frame.png badge_ready.png badge_watch.png
                 marker_team_a.png marker_team_b.png stack_count.png
  screen/game/   hud_frame.png result_queue_panel.png turn_banner.png
  board/         board_main.png node_marker.png node_marker_last.png path_highlight.png
  piece/         a_on_board.png a_home_checkpoint.png a_finished.png a_waiting.png
                 b_on_board.png b_home_checkpoint.png b_finished.png b_waiting.png
                 movable_outline.png
  yut/           result_do.png result_gae.png result_geol.png result_yut.png
                 result_mo.png result_backdo.png result_buk.png
                 toss_00.png ... toss_07.png
  fx/            capture_flash.png stack_pop.png
  font/          notosans_kr_regular.ttf notosans_kr_bold.ttf
```

- 명명: snake_case, 팀 접두사 `a_`/`b_`, 상태 접미사는 도메인 enum 그대로
  (`waiting`/`on_board`/`home_checkpoint`/`finished`). 공백·대문자 금지.
- 이미지는 PNG(RGBA8)만 사용한다. raylib `LoadTexture`가 그대로 읽는다.
- 폰트는 TTF(`LoadFontEx`)만 사용하며 한글 글리프(U+AC00–D7A3)와 ASCII를 반드시
  포함한다. raylab 기본 폰트는 한글이 없으므로 폰트 파일은 필수 리소스다. 웨이트
  하나당 1MB 이하를 권장하고 서브셋을 권장한다.
- **코드 드로잉 우선**: 진행 링·타이머 바·테두리·구분선·단색 버튼처럼 도형과
  텍스트로 그릴 수 있는 UI는 이미지 대신 raylib 프리미티브로 그린다. 해상도
  독립성과 번들 크기를 위해 이미지는 아트가 필요한 요소에만 쓴다.

## 크기와 로딩 예산

- 기준 논리 해상도는 **1280×720**(16:9)이며 canvas 실제 크기에 맞춰 letterbox
  스케일한다. 모든 px 수치는 이 기준 해상도 기준값이다.
- 초기 로딩(게임 화면 플레이에 필수) 합계 **≤ 4MB**, 전체 자산 합계 **≤ 16MB**.
  초과 시 atlas·압축·서브셋으로 줄인다. 수치는 경향치이며 개정 가능하다.
- 개별 PNG는 가급적 2의 거듭제곱 폭·높이를 권장한다(mipmap 비활성 조건에서 NPOT
  허용). 텍스처 atlas는 v1에서 도입하지 않으며, 도입 시 프레임 메타는 C 헤더
  상수로 정의한다.
- 로딩 전략: 게임 화면 진입에 필요한 리소스는 연결 직후 백그라운드 프리페치하고,
  실패해도 텍스트/프리미티브 폴백으로 화면이 동작해야 한다(리소스 누락은 판정과
  무관).

## 보드 좌표계와 렌더 레이어

- 정본은 `spec/board_graph.yaml`의 `render_reference.coordinates`(정규화
  0..1, 참고 그림 추정치)다. 게임 화면의 보드 뷰포트 사각형에 매핑하며
  `node = viewport.origin + coord * viewport.size`.
- 좌표 조정은 docs/12에 기록된 대로 허용되며 논리 그래프(노드·연결)는 절대
  변경하지 않는다. 아트 정합 오차 기준: 마커 중심 ±보드 폭 1% 이내.
- 페인터 순서(아래가 먼저): 배경 → 윷판 아트 → 칸 마커 → 경로 하이라이트 → 말
  (업기 오프셋) → 이펙트 → HUD(턴 배너·타이머·결과 큐·버튼).
- 말 앵커는 칸 중심 하단, 업기는 같은 앵커에서 위로 겹치기+개수 배지. 현재 차례의
  이동 가능 말은 `movable_outline` 오버레이로 표시한다.

## 말·윷 상태 대응표

말은 도메인 상태 enum 그대로 4종 × 2팀이다.

| 상태 | 의미 | 표현 |
|---|---|---|
| waiting | 판 밖 대기 | 화면 하단 대기 슬롯 아이콘 |
| on_board | 판 위 | 노드 위 말 스프라이트 |
| home_checkpoint | 참먹이 도착 | chammeogi 위치 스택 |
| finished | 완주 | 완주 슬롯 처리(판 밖) |

윷 결과는 서버가 확정한 값 7종(do/gae/geol/yut/mo/backdo/buk)에 1:1 대응하는
결과 프레임을 쓴다. 윷가락 4개 개별 앞뒤 조합 애니메이션은 v1에서 생략하고
던지기 모션 프레임(toss_00~07) 후 결과 프레임으로 착지한다.

## 애니메이션 원칙과 v1 카탈로그

원칙(docs/09 재확인): 서버 확정 이벤트를 순서대로 재생, 재생 중 새 이벤트 버퍼링
허용, 재접속 snapshot이 애니메이션보다 우선, 지속 시간 상한 준수, 스킵 가능.

| id | 트리거 이벤트 | 리소스 | 길이(상한) | 비고 |
|---|---|---|---|---|
| yut_toss | THROW_YUT 수락(YUT_RESULT 수신) | toss_00~07 + result_* | 900ms | 결과 프레임 착지 후 유지 |
| piece_move | PIECE_MOVED | 말 스프라이트 | 칸당 ≤150ms, 총 ≤900ms | v1은 from→to 직선 보간, 경로 추적은 미결 |
| capture_flash | PIECES_CAPTURED | fx/capture_flash.png | ≤400ms | 피격 말 waiting 슬롯 퇴장과 병행 |
| stack_pop | PIECES_STACKED | fx/stack_pop.png + stack_count | ≤250ms | 배지 갱신 |
| buk_resolve | BUK_RESOLVED | result_buk + piece_move 재사용 | piece_move 한도 내 | 전용 연출은 미결 |
| turn_pulse | TURN_STARTED / MOVE_REQUIRED | movable_outline | ≤300ms | 이동 가능 말 강조 |
| pause_veil | GAME_PAUSED / GAME_RESUMED | 코드 드로잉 반투명 veil | 즉시 | 타이머 표시는 보존값 |

버퍼링 규칙: 큐에 이벤트가 밀리면 같은 종류 연출을 병합하거나 스킵해 총 지연이
1.5초를 넘지 않게 한다. 초과 시 즉시 최신 확정 상태로 스냅샷 렌더링한다.

## v1 필수 리소스 체크리스트

| id | 경로 | 크기(px) | 비고 |
|---|---|---|---|
| board_main | board/board_main.png | 1024×1024 | render_reference [0..1]² 전체 커버 |
| node_marker | board/node_marker.png | 48×48 | 29노드 공통 |
| node_marker_last | board/node_marker_last.png | 48×48 | from→to 강조 |
| path_highlight | board/path_highlight.png | 48×48 | 선택 가능 경로 |
| piece_a/b × 4상태 | piece/*.png | 96×96 | 앵커 하단 중앙 |
| movable_outline | piece/movable_outline.png | 112×112 | 말보다 한 라운드 큼 |
| yut result ×7 | yut/result_*.png | 256×256 | |
| yut toss ×8 | yut/toss_00~07.png | 256×256 | 던지기 모션 |
| fx ×2 | fx/*.png | 256×256 | capture/stack |
| gui/common 일괄 | gui/common/*.png | 32~512 | 버튼 4상태 등 |
| font ×2 | font/notosans_kr_{regular,bold}.ttf | — | 한글 서브셋 권장 |
| hud | screen/game/hud_frame.png 등 | 1280×720 영역 | 게임 HUD 프레임 |

위 수치는 초기 기준값이다. 아트 작업에서 조정이 필요하면 본 문서를 개정으로
갱신한다. 검증 자동화는 `tools/check_assets.py`로 수행한다. 로컬에서는
`python3 tools/check_assets.py --root client/assets`를 실행하며, 실제 리소스
fixture가 추가되면 동일 명령을 CI 필수 단계로 연결한다. 현재 CI는
`tools/check_assets_test.py`로 검사기 자체의 정상·실패 경로를 검증한다.

## 미결 항목

아래는 확정을 유보하고 구현 중 추가하기로 한다. 추가 시 본 문서 개정과
`docs/12_open_items.md` 갱신을 함께 한다.

- 아트 스타일 가이드(색상표, 라인 웨이트, 승부 연출 톤)
- 사운드/BGM 포맷과 트리거(WAV/OGG, 루프 정책)
- 텍스처 atlas 도입과 프레임 메타 형식
- 경로 따라가기 이동 연출(traversed 경로 기반), 백도·북 전용 연출, 승리 연출
- 고대비/접근성 테마와 UI 스케일 설정
- 실제 asset fixture 추가 뒤 `tools/check_assets.py`를 CI 필수 단계로 연결
