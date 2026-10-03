# choboterm V0.1.0

옛 ZTerm처럼 간결하게 쓸 수 있는 Windows용 SSH / Telnet / FTP 터미널입니다.
Go + [Wails v2](https://wails.io) + [xterm.js](https://xtermjs.org)로 만들었습니다.

**[⬇ 최신 버전 다운로드 (choboterm.exe)](https://github.com/chobocho/choboterm/releases/latest/download/choboterm.exe)** · [소개 페이지](https://chobocho.github.io/choboterm/) · [모든 버전](https://github.com/chobocho/choboterm/releases)

설치 없이 내려받아 실행하면 됩니다. 코드 서명이 없어 처음 실행할 때 "Windows의 PC 보호" 창이 뜨면 **추가 정보 → 실행**을 누르세요.

![choboterm 스크린샷](./docs/screen_shot.png)

## 기능

- **탭**: MobaXterm처럼 여러 접속을 탭으로 띄웁니다. 탭마다 접속, 인코딩, 파일 전송이 따로 동작합니다.
  - 탭 상태 점: 초록 = 접속 중, 회색 = 끊김, 주황 = 파일 전송 중. 보고 있지 않은 탭에 새 출력이 오면 탭 이름이 강조됩니다.
  - `+` 버튼이나 탭 바 빈 곳 더블클릭으로 새 탭, 가운데 클릭으로 닫기, 끌어서 순서 바꾸기
  - 탭 우클릭 메뉴: 다시 연결, 복제(같은 서버로 새 탭), 파일 전송, 연결 끊기, 탭 닫기
  - 접속 창과 파일 전송 창은 탭마다 따로 열립니다. 창이 떠 있어도 탭을 바꿀 수 있고, 돌아오면 입력값·폴더·전송 상태가 그대로 남아 있습니다. 파일 전송은 다른 탭을 보는 동안에도 계속됩니다.
- **프로토콜 선택**: 22 = SSH, 21 = FTP, 23 = Telnet. 그 밖의 포트(예: Termux의 8022)는 접속하자마자 서버의 첫 인사말로 자동 판별합니다(`SSH-` → SSH, `220` → FTP, 그 외 → Telnet). 먼저 말을 걸지 않는 Telnet 서버는 판별에 2초가 걸립니다.
- **SSH**: 비밀번호, keyboard-interactive, `~/.ssh` 개인키(id_ed25519 / id_ecdsa / id_rsa, 암호 없는 키) 인증
  - `~/.ssh/known_hosts`로 호스트 키 확인. 처음 접속하는 호스트는 지문을 보여 주고 신뢰할지 묻고, 키가 바뀐 호스트는 차단합니다.
- **Telnet**: NAWS / TTYPE / ECHO / SGA / BINARY 협상, `login:` / `password:` 프롬프트 자동 로그인
  - 비밀번호를 기억해 두었다가 Host를 고르면 자동으로 채웁니다. Windows DPAPI로 암호화해 저장하므로 현재 Windows 사용자만 풀 수 있습니다. Pass를 비우고 접속하면 저장된 비밀번호를 지웁니다.
- **FTP**: 접속하면 파일 전송 창이 바로 열립니다. 창을 닫아도 탭을 닫기 전까지 연결이 유지되며 `Ctrl+Shift+F`로 다시 엽니다. Login을 비워 두면 anonymous로 로그인합니다.
- **SFTP / SCP**: SSH 접속 중 `Ctrl+Shift+F`로 파일 전송 창을 엽니다. 다시 로그인할 필요가 없습니다.
  - 서버에 SFTP가 없으면(Dropbear, 공유기, 임베디드 장비 등) 자동으로 SCP로 바꿔 씁니다. 이때 폴더 목록은 `ls`로 가져옵니다.
- **Zmodem**: 터미널에서 `sz 파일` / `rz`를 실행하면 자동으로 전송합니다. 원격 서버에 lrzsz가 설치되어 있어야 합니다.
- **EUC-KR (CP949)**: 접속 창의 Code에서 선택하거나 접속 중 `Ctrl+Shift+E`로 전환합니다.
  - EUC-KR 모드에서는 `─ │ ■ ○ ※` 같은 특수문자를 옛 터미널처럼 2칸 폭으로 그립니다.
  - FTP 파일 이름과 Zmodem 파일 이름에도 같은 인코딩을 적용합니다.
- **복사 / 붙여넣기**: PuTTY처럼 마우스로 선택하면 바로 복사되고, 우클릭하면 붙여넣습니다. `Ctrl+Shift+C` / `Ctrl+Shift+V`도 됩니다.
  - 여러 줄을 붙여넣을 때는 줄마다 명령이 실행될 수 있으므로 먼저 내용을 보여 주고 확인을 받습니다. "다시 묻지 않기"로 끌 수 있습니다.
  - mc, htop처럼 마우스를 쓰는 프로그램에서는 Shift+우클릭으로 붙여넣습니다.
- **글꼴 크기**: `Ctrl+휠`, `Ctrl+=` / `Ctrl+-`로 바꾸고 `Ctrl+0`으로 되돌립니다. 모든 탭에 적용되고 다음 실행 때도 유지됩니다.
- **스크롤백 검색**: `Ctrl+Shift+S`로 검색 막대를 엽니다. 입력하는 대로 가장 최근 출력부터 찾고, 모든 결과를 강조합니다. `Enter`는 위로(이전 출력), `Shift+Enter`는 아래로, `Alt+C`는 대소문자 구분, `Alt+R`은 정규식입니다.
- **링크 열기**: 화면에 나온 `http://` / `https://` 주소를 `Ctrl+클릭`하면 기본 브라우저로 엽니다. 그냥 클릭은 선택용으로 남겨 둡니다.
- **연결 유지 / 자동 재접속**: SSH는 `keepalive@openssh.com`, Telnet은 NOP를 60초마다 보내 공유기·방화벽이 쉬는 연결을 끊지 않게 하고, 3번 연속 응답이 없으면 끊긴 것으로 봅니다.
  - 네트워크가 끊기면 3·5·10·20·30초 간격으로 최대 10번 다시 연결합니다. 화면 내용은 그대로 둡니다. `Enter`로 바로 연결, `Esc`로 취소합니다.
  - `exit`처럼 서버가 정상적으로 끝낸 연결은 다시 연결하지 않습니다.
- **설정 창**: `Ctrl+Shift+O` 또는 탭 바 오른쪽 ⚙ 버튼. 글꼴 크기, 여러 줄 붙여넣기 확인, 연결 유지 간격(0 = 끄기), 자동 재접속을 바꿉니다.
- **창 위치 기억**: 창 크기·위치·최대화 상태를 닫을 때 저장해 다음 실행 때 그대로 엽니다. 모니터를 뺀 경우처럼 저장된 위치가 화면 밖이면 보이는 곳으로 옮깁니다.
- **최근 접속 기록**: Host 목록에 최근 20개를 저장합니다. 비밀번호는 Telnet만, 암호화해서 저장합니다.

X11 포워딩은 지원하지 않습니다.

## 단축키

| 키 | 동작 |
|---|---|
| `Enter` (연결이 없는 탭에서) | 그 탭에서 접속 창 열기 |
| `Ctrl+Shift+T` / `Ctrl+Shift+N` | 새 탭으로 접속 |
| `Ctrl+Tab` / `Ctrl+Shift+Tab` (`Ctrl+PgDn` / `Ctrl+PgUp`) | 다음 / 이전 탭 |
| `Ctrl+Shift+W` | 탭 닫기 |
| `Alt+C` / `Alt+A` / `Esc` | 접속 창에서 Connect / Cancel / 닫기 |
| `Ctrl+Shift+D` | 연결 끊기 (탭은 유지) |
| `Ctrl+Shift+E` | UTF-8 ↔ EUC-KR 전환 |
| `Ctrl+Shift+F` | 파일 전송 창 (SFTP / SCP) |
| `Ctrl+휠` / `Ctrl+=` / `Ctrl+-` / `Ctrl+0` | 글꼴 크게 / 작게 / 기본값 |
| `Ctrl+Shift+S` | 스크롤백 검색 |
| `Ctrl+Shift+O` | 설정 창 |
| `Ctrl+클릭` | 화면의 URL을 브라우저에서 열기 |
| `Ctrl+Shift+C` / `Ctrl+Shift+V` | 복사 / 붙여넣기 (마우스 선택 = 복사, 우클릭 = 붙여넣기) |
| `Ctrl+C` / `Ctrl+X` (Zmodem 전송 중) | 전송 취소 |
| `F1` | 도움말 열기 / 닫기 (도움말 창에서 `Alt+L`로 한국어 ↔ English 전환) |

파일 전송 창: `↑` `↓` `Home` `End`로 선택, `Enter`/더블클릭으로 폴더 열기·다운로드, `Backspace`로 상위 폴더, `F5`로 새로 고침, `Esc`로 닫기

## 파일 저장 위치

- 다운로드(SFTP / SCP / FTP): 저장 창에서 선택합니다. 기본 위치는 `~/Downloads`입니다.
- Zmodem 수신: `~/Downloads`에 저장합니다. 같은 이름이 있으면 `이름 (1).확장자`로 저장합니다.
- 접속 기록: `%AppData%\choboterm\hosts.json` (Telnet 비밀번호는 DPAPI로 암호화된 값만 저장)
- 설정: `%AppData%\choboterm\settings.json`

## 빌드

필요한 것: Go 1.26 이상, Node.js, Wails CLI v2, WebView2 런타임(Windows 11에는 기본 포함)

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@latest

wails dev     # 개발 모드 (핫 리로드)
wails build   # build/bin/choboterm.exe 생성
```

`build.bat`을 실행하면 테스트 → `wails build` → `release\choboterm.exe` 복사까지 한 번에 합니다. (`release` 폴더는 git에 올리지 않습니다.)

## 릴리스

1. `main.go`의 `AppVersion`과 `wails.json`의 `productVersion`을 올립니다.
2. `build.bat`으로 `release\choboterm.exe`를 만듭니다. (실행 중인 choboterm은 먼저 종료)
3. 커밋·푸시 후 GitHub Release를 만듭니다.

   ```sh
   gh release create v0.2.0 "release/choboterm.exe#choboterm.exe (Windows x64)" --title "choboterm V0.2.0" --notes "..."
   ```

소개 페이지(`docs/`, GitHub Pages 기준: `main` 브랜치 `/docs`)의 다운로드 버튼은 항상 최신 릴리스의 `choboterm.exe`를 가리키므로 따로 고칠 필요가 없습니다.

## 테스트

```sh
go test ./...
```

실제 서버가 필요한 테스트는 환경변수를 설정했을 때만 실행됩니다.

- FTP: 루트에 `한글파일.txt`(내용 `hello`)와 빈 `sub` 폴더가 있고 `user` / `pass`로 로그인되는 서버

  ```sh
  CHOBOTERM_FTP_TEST=127.0.0.1:2121 CHOBOTERM_FTP_ENCODING=EUC-KR go test -run FTP -v
  ```

- Zmodem: WSL에 있는 lrzsz `sz` / `rz`와 실제로 주고받습니다. 설치 없이 패키지만 풀어서 쓸 수 있습니다.

  ```sh
  wsl -- sh -c 'mkdir -p /tmp/lrzsz && cd /tmp/lrzsz && apt-get download lrzsz && dpkg -x lrzsz_*.deb x'
  CHOBOTERM_LRZSZ=/tmp/lrzsz/x/usr/bin go test -run Zmodem -v
  ```

  Git Bash에서는 `/tmp` 경로가 바뀌지 않도록 `MSYS2_ENV_CONV_EXCL=CHOBOTERM_LRZSZ`도 함께 설정하세요.

- 실제 SSH 서버(아무 포트) 접속: 셸 명령, 창 크기 변경, 파일 목록까지 확인합니다. 호스트 키는 임시 known_hosts에 저장됩니다.

  ```sh
  CHOBOTERM_SSH_TEST=host:8022 CHOBOTERM_SSH_USER=user CHOBOTERM_SSH_PASS=secret go test -run LiveSSH -v
  ```

- SCP 자동 전환: SFTP 없이 명령만 실행하는 테스트 SSH 서버를 띄우고, 명령은 WSL에서 실행합니다(WSL에 `scp`, `ls` 필요).

  ```sh
  CHOBOTERM_WSL=1 go test -run SCPFallback -v
  ```

## 소스 구성

| 파일 | 내용 |
|---|---|
| `app.go` | 탭별 접속, 키 입력 전달, 출력 묶음 전송, Zmodem 감지 |
| `ssh.go` / `telnet.go` / `ftp.go` / `sftp.go` / `scp.go` | 프로토콜 |
| `detect.go` | 표준이 아닌 포트의 프로토콜 자동 판별 |
| `filexfer.go` | 파일 전송 공통 계층(RemoteFS), 진행률, 취소 |
| `zmodem.go` / `zmodem_app.go` | Zmodem 프로토콜과 앱 연결 |
| `codec.go` | UTF-8 ↔ CP949 변환 |
| `history_store.go` | 최근 접속 기록 |
| `settings.go` | 사용자 설정 (`settings.json`) |
| `window_windows.go` | 창 크기·위치 저장과 복원 (Win32 WINDOWPLACEMENT) |
| `secret_windows.go` | 비밀번호 암호화 (Windows DPAPI) |
| `frontend/src/main.ts` | 탭, 터미널, 접속 창, 단축키 |
| `frontend/src/files.ts` | 파일 전송 창, 진행률 상자 |
| `frontend/src/help.ts` | F1 도움말 창 (한국어 / English) |
| `frontend/src/search.ts` | 스크롤백 검색 막대 |
| `frontend/src/prefs.ts` | 설정 창 |
| `frontend/src/paste.ts` | 여러 줄 붙여넣기 확인 창 |
| `frontend/src/settings.ts` | 설정 읽기 / 저장 |
| `frontend/src/cjkwidth.ts` | EUC-KR 모드의 2칸 폭 문자 처리 |
| `tools/make_icon.py` | 앱 아이콘 생성 (`build/appicon.png`, `build/windows/icon.ico`) |

## 아직 지원하지 않는 기능

- X11 포워딩, 포트 포워딩
- FTPS(TLS), 이어받기, 원격 파일 삭제와 폴더 만들기

## 라이선스

[MIT](LICENSE)
