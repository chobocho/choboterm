# choboterm: 작업 규칙

Windows · Linux용 SSH / Telnet / FTP / 시리얼 터미널. Go + Wails v2(창) + TypeScript/xterm.js(화면).
창 없이 같은 화면을 브라우저에 주는 웹 서버(headless)도 있다. 기능과 파일별 역할은 `README.ko.md`의
"아키텍처"와 "소스 구성"을 먼저 본다.

## 구조 요약

- `main.go`: 진입점일 뿐, 본체는 `internal/app/`. 연결 종류·전송·저장 등은 `internal/<패키지>/`.
- `frontend/src/`: 화면(TypeScript). `frontend/dist`가 Go 프로그램에 임베드된다(`frontend/assets*.go`).
- 웹 서버: `internal/app/web*.go` + `web_shim.js`. 창과 브라우저가 같은 `frontend/dist`를 쓴다.
- 빌드 태그 `headless`: Wails 창 코드가 빠진 웹 서버 전용 빌드(Linux는 `CGO_ENABLED=0` 정적 바이너리).

## 검사 (푸시 전에)

```sh
go vet ./... && go test ./...      # Go
tools/vet_all.sh                   # Go 파일을 옮기거나 OS·빌드 태그별 코드를 고쳤을 때: 모든 플랫폼 타입 검사
(cd frontend && npm run build)     # tsc + vite: 화면을 고쳤을 때
```

- `internal/app/wire_test.go`가 화면·저장 파일이 보는 Go 쪽(바인딩 메서드, JSON 형식, 기본 설정, 이벤트 이름)을
  `testdata/wire.golden`으로 고정한다. 의도한 변경이면 `UPDATE_GOLDEN=1 go test -run TestWire ./internal/app/`로
  갱신하고 diff를 확인한다. 바인딩 메서드를 추가·변경하면 `frontend/wailsjs/`도 다시 만든다.
- 화면 E2E(`frontend/e2e/`, Playwright)는 headless choboterm을 띄워 브라우저로 시험한다. 셸 명령이 `cmd`라
  스위트는 Windows 기준이지만, Telnet 대역 서버(`FakeServer`)만 쓰는 스펙(`remote`, `ime` 등)은 Linux에서도 돈다.
  설치된 브라우저를 쓰며 다운로드하지 않는다(`E2E_BROWSER`, `frontend/e2e/README.md` 참고).
- `TestSerialSessionPty`는 root로 돌리면 실패한다(TIOCEXCL). 일반 사용자로는 통과한다.
- CI(`.github/workflows/ci.yml`): PR과 main 푸시마다 Windows · Ubuntu 22.04에서 vet + `go test ./...`.

## 버그 수정

- 고치기 전에 재현한다. 재현 → 원인의 증거(이벤트 기록, 로그, 실패하는 테스트) → 수정 → 같은 재현으로 확인 →
  회귀 테스트 순서. 추측으로 넣은 우회 코드는 다른 입력을 망가뜨리기 쉽다.
  예: Linux 창(WebKitGTK)의 한글 입력은 v0.2.24/25에서 추측으로 고쳤다가 Enter · Backspace를 잃었고,
  v0.2.26에서 Xvfb + ibus-hangul로 실제 이벤트 순서를 기록해 원인(조합마다 `compositionend`가 두 번)을 찾았다
  (`frontend/src/tabs.ts`의 `isWebKitGTK` 부분, `frontend/e2e/ime.spec.ts`).
- Linux 창 문제는 WebKitGTK에서, 브라우저 문제는 그 브라우저에서 확인한다. 둘의 이벤트 순서는 다르다.

## 코드 · 문구

- 주석은 영어 문장으로, 무엇보다 "왜"를 쓴다. 주변 코드의 밀도와 말투를 따른다.
- 화면 문구는 소스에 한국어로 쓰고, 영어는 `frontend/src/i18n_en.json`(한국어 원문 → 영어)에 추가한다.
  화면과 Go(`internal/i18n/`)가 이 사전 하나를 함께 쓴다.
- 커밋 메시지는 영어, 첫 줄은 명령형 요약("Fix ...", "Add ..."). 본문에 원인과 확인 방법을 적는다.

## 버전과 릴리스

- 버전을 올릴 때 바꾸는 곳: `internal/version/version.go`(`Version`), `wails.json`(`productVersion`),
  `README.md` · `README.ko.md`(제목, "릴리스" 절의 예시), `.github/workflows/release.yml`(주석의 예시),
  `docs/architecture.svg` · `docs/architecture_en.svg`(오른쪽 위 버전 표기와 이번 버전의 변경 한 줄).
- 릴리스는 자동이다: `version.go`가 바뀐 커밋이 `main`에 머지되면 `release` 워크플로가 4개 파일을 빌드하고
  `v<Version>` 태그와 GitHub Release를 만든다(태그가 이미 있으면 건너뜀). 따로 실행할 것은 없다.
  손으로 할 때는 Actions > release > Run workflow에서 Tag와 Ref `main`.
- `release` 브랜치는 v0.2.23에서 멈춘 옛 브랜치다. 빌드·릴리스에 쓰지 않는다.
- `history*.html`은 고치지 않는다.
