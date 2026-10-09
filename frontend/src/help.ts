// F1 help window, switchable between Korean and English.

import {currentLang} from './i18n';
import {BrowserOpenURL} from '../wailsjs/runtime/runtime';

const REPO = 'https://github.com/chobocho/choboterm';

type Lang = 'ko' | 'en';

interface Section {
    title: string;
    rows: [string, string][];
}

interface HelpText {
    title: string;
    other: string;
    close: string;
    sections: Section[];
    notes: string[];
    links: {title: string; rows: [string, string][]}; // [label, url]
}

const TEXT: Record<Lang, HelpText> = {
    ko: {
        title: '도움말',
        other: 'English',
        close: '닫기',
        sections: [
            {
                title: '탭',
                rows: [
                    ['Ctrl+Shift+T / Ctrl+Shift+N', '새 탭으로 접속'],
                    ['Ctrl+Tab / Ctrl+Shift+Tab', '다음 / 이전 탭 (Ctrl+PgDn / Ctrl+PgUp)'],
                    ['Ctrl+Shift+W', '탭 닫기 (분할된 탭에서는 지금 창만)'],
                    ['Ctrl+Shift+\\ / Ctrl+Shift+-', '오른쪽 / 아래로 창 분할 (tmux처럼 | 와 -). 새 창마다 따로 접속 (지금 서버가 미리 채워짐)'],
                    ['Alt+←→↑↓', '분할된 탭에서 옆 창으로 이동 (클릭해도 됨). 경계선을 끌어 크기 조절, 더블클릭으로 반반'],
                    ['Enter (연결이 없는 탭)', '그 탭에서 접속 창 열기'],
                    ['마우스', '+ 또는 탭 바 더블클릭: 새 탭 · 가운데 클릭: 닫기 · 끌기: 순서 바꾸기 · 우클릭: 메뉴'],
                ],
            },
            {
                title: '복사 / 붙여넣기',
                rows: [
                    ['마우스로 선택', '선택하면 바로 복사'],
                    ['우클릭', '붙여넣기 (마우스를 쓰는 프로그램에서는 Shift+우클릭)'],
                    ['Ctrl+Shift+C / Ctrl+Shift+V', '복사 / 붙여넣기'],
                    ['여러 줄 붙여넣기', '실행 전에 확인 창 표시 (다시 묻지 않기 선택 가능)'],
                ],
            },
            {
                title: '화면',
                rows: [
                    ['Ctrl+휠 / Ctrl+= / Ctrl+-', '글꼴 크게 / 작게 (모든 탭, 크기 기억)'],
                    ['Ctrl+0', '글꼴 크기 기본값(15)'],
                    ['Ctrl+클릭', '화면의 http(s) 주소를 브라우저에서 열기'],
                    ['키워드 강조', 'error · fail · 오류는 빨강, warning · 경고는 노랑, success · 성공은 초록 배경으로 표시. 단어 목록은 설정(Ctrl+Shift+O)에서 바꾸거나 끔'],
                    ['Ctrl+Shift+S', '스크롤백 검색 (Enter: 위로, Shift+Enter: 아래로, Alt+C: 대소문자, Alt+R: 정규식, Esc: 닫기)'],
                ],
            },
            {
                title: '세션',
                rows: [
                    ['Ctrl+Shift+H / ★', '세션 관리 창: 저장한 세션을 그룹별로 보고 고치기·복제·삭제·찾기. Enter: 접속 · "그룹 전체 열기": 그룹의 세션을 모두 탭으로'],
                    ['접속 창의 ☆ Save (Alt+S)', '입력한 접속에 이름과 그룹을 붙여 저장. 비밀번호도 저장 가능 (Windows 사용자 계정으로 암호화)'],
                    ['탭 우클릭 → 세션으로 저장', '지금 탭의 접속을 세션으로 저장. 세션으로 연 탭에는 세션 이름이 붙음'],
                    ['Host 칸에 입력', '▼ 목록을 이름·주소·그룹으로 걸러 보여 줌. 저장한 세션은 그룹별로 맨 위에'],
                    ['가져오기 · 내보내기', '세션 관리 창에서 PuTTY 세션 가져오기, 파일로 내보내기·가져오기 (비밀번호는 내보내지 않음)'],
                ],
            },
            {
                title: '접속',
                rows: [
                    ['Alt+C / Alt+A / Alt+S / Esc', '접속 창에서 Connect / Cancel / 세션으로 저장 / 닫기'],
                    ['Ctrl+Shift+D', '연결 끊기 (탭은 유지)'],
                    ['Ctrl+Shift+E', 'UTF-8 ↔ EUC-KR 전환'],
                    ['자동 재접속', '네트워크가 끊기면 3·5·10·20·30초 간격으로 최대 10번 다시 연결 (Enter: 지금 · Esc: 취소)'],
                    ['Ctrl+Shift+O / ⚙', '설정: 글꼴, 색 테마, 커서 모양, 스크롤백 줄 수, 반투명(창 전체 / 배경만), 붙여넣기 확인, 연결 유지 간격, 자동 재접속, 세션 로그'],
                    ['탭 우클릭 → 색 테마', '이 서버만 다른 색 테마로. 서버(호스트+포트)별로 저장해 다음 접속 때도 적용'],
                    ['Ctrl+Shift+L', '세션 로그 기록 시작 / 중지: 받은 출력을 파일에 계속 저장 (탭에 빨간 점 표시). 설정에서 자동 기록·폴더·일반 텍스트·줄마다 시각 기록 선택'],
                    ['Ctrl+Shift+M', '매크로 창: 자주 쓰는 명령을 저장해 두고 보내기 (Enter / 더블클릭)'],
                    ['Ctrl+Shift+P', 'SSH 포트 포워딩: 로컬(-L) / 원격(-R) / 동적(-D, SOCKS5). 호스트별로 저장해 다음 접속 때 자동 적용'],
                    ['Ctrl+Shift+J', '도커 컨테이너: SSH 서버 또는 이 PC(Docker Desktop)의 컨테이너 목록. 셸 열기(docker exec, 같은 SSH 접속을 함께 씀) · 로그 보기 · 시작/중지/재시작 · 포트 열기(서버 포트를 이 PC로 포워딩해 브라우저로). docker가 없으면 podman'],
                    ['F2~F12, Shift/Ctrl+F1~F12', '매크로에 지정한 키를 누르면 바로 보내기 (지정하지 않은 키는 프로그램으로 전달)'],
                    ['Lua 스크립트', '매크로 종류를 "Lua 스크립트"로 하거나 탭 우클릭 → "Lua 스크립트 실행..." (문서\\choboterm\\scripts). send · expect · sleep · screen · print. 탭에 ▶ 걸린 시간, 오른쪽 아래에 출력과 결과. 별도 프로세스(메모리 256MB 제한)에서 돌아 스크립트가 잘못돼도 choboterm은 그대로'],
                    ['Ctrl+Shift+K', 'Lua 콘솔(REPL): 한 줄씩 바로 실행, 변수 유지. 식을 입력하면 값 표시. Shift+Enter 줄바꿈 · ↑↓ 이전 입력 · Ctrl+C 실행 중지 · Ctrl+O(또는 "열기") Lua 파일을 입력창으로 불러오기 · Ctrl+S(또는 "저장") 입력·출력을 파일로 저장 · Esc 터미널로 · 위쪽 테두리를 끌어 높이 조절 · ✕ 또는 Ctrl+Shift+K 한 번 더 누르면 닫기(실행 중인 코드도 멈춤)'],
                    ['포트', '22 = SSH, 21 = FTP, 23 = Telnet, 그 밖의 포트는 자동 판별'],
                    ['Host에 cmd · powershell · wsl', '이 PC의 셸(cmd, PowerShell, pwsh, WSL)을 탭에서 열기. "wsl -d Ubuntu"처럼 인자도 가능. ▼ 목록에 "로컬 셸"로 표시'],
                    ['Host에 COM3 · /dev/ttyUSB0', '시리얼 포트 열기. Port 칸이 속도(baud, 기본 115200). "COM3 9600 7E1 rtscts"처럼 데이터 비트·패리티(N/E/O)·정지 비트와 흐름 제어(rtscts · xonxoff)도 지정. ▼ 목록에 연결된 포트 표시. 탭 우클릭 → "Break 신호 보내기". USB 어댑터를 뺐다 꽂으면 자동 재접속'],
                    ['SSH 로그인 질문', 'OTP·인증 코드 같은 서버 질문, 암호 걸린 개인키의 암호는 창으로 물어봄. Pass를 비우면 비밀번호도 물어봄. 에이전트(OpenSSH·Pageant) 키는 자동 사용'],
                    ['Host에 ssh config 이름', '~/.ssh/config의 Host 이름(예: busan)이나 "ssh busan", "ssh -p 2222 user@busan"을 쓰면 HostName·Port·User·IdentityFile을 읽어 접속. ▼ 목록에도 표시'],
                    ['Jump (점프 호스트)', '배스천을 거쳐 접속(ProxyJump). 접속 창의 Jump 칸에 user@bastion, 여러 개는 쉼표로. 비우면 ~/.ssh/config의 ProxyJump, "ssh -J bastion web"도 됨, none이면 쓰지 않음'],
                ],
            },
            {
                title: '파일 전송',
                rows: [
                    ['Ctrl+Shift+F', '파일 전송 창 (SFTP / SCP / FTP)'],
                    ['↑ ↓ Home End', '선택 (Shift: 범위 선택)'],
                    ['Ctrl+클릭 / Shift+클릭 / Ctrl+A', '여러 개 선택 / 범위 선택 / 모두 선택'],
                    ['Enter / 더블클릭', '폴더 열기 · 다운로드 (여러 개나 폴더는 저장할 폴더를 골라 하위까지 받기)'],
                    ['Delete / F2 / F7', '삭제 (폴더는 안의 내용까지) / 이름 바꾸기 / 새 폴더'],
                    ['Shift+F4', '빈 파일 만들기 (같은 이름이 있으면 만들지 않음)'],
                    ['F3 / F4', '텍스트 보기 / 편집 (UTF-8 / EUC-KR 자동 판별, 인코딩 직접 선택, Ctrl+F 찾기, Ctrl+S 서버에 저장)'],
                    ['F3 (이미지)', '이미지 보기 (JPG·PNG·GIF·BMP·WebP·SVG, 30MB까지): ←→ 이전/다음, 0 맞춤, 1 원본, +/−·Ctrl+휠 확대·축소, 끌어서 이동, Esc 닫기'],
                    ['끌어 놓기', 'Windows 탐색기에서 파일·폴더를 목록으로 끌어 놓으면 업로드 · 우클릭: 메뉴'],
                    ['Backspace · Alt+↑ · ..', '상위 폴더 (목록 맨 위 .. 를 Enter / 더블클릭)'],
                    ['이어받기', '끊긴 다운로드는 .part로 남고, 다시 받을 때 이어받기 / 처음부터를 물음. 끊긴 업로드도 같음 (SFTP · FTP)'],
                    ['F5 / Esc', '새로 고침 / 닫기'],
                    ['sz 파일 / rz', '터미널에서 Zmodem 전송 (Ctrl+C / Ctrl+X로 취소)'],
                ],
            },
            {
                title: '도움말',
                rows: [
                    ['F1', '도움말 열기 / 닫기'],
                    ['언어 (Language)', '설정(Ctrl+Shift+O)에서 자동 / 한국어 / English. 다시 시작하면 적용'],
                    ['Alt+L', '한국어 ↔ English 전환'],
                ],
            },
        ],
        notes: [
            '다운로드와 Zmodem 수신 파일은 기본으로 ~/Downloads에 저장됩니다.',
            '접속 기록: %AppData%\\choboterm\\hosts.json',
            '세션 로그: 기본으로 문서\\choboterm\\logs에 "호스트_포트_날짜_시간.log"로 저장됩니다.',
        ],
        links: {
            title: '링크',
            rows: [
                ['릴리스 페이지', `${REPO}/releases/latest`],
                ['라이선스 (MIT)', `${REPO}/blob/main/LICENSE`],
                ['서드파티 라이선스', `${REPO}/blob/main/THIRD_PARTY_NOTICES.txt`],
            ],
        },
    },
    en: {
        title: 'Help',
        other: '한국어',
        close: 'Close',
        sections: [
            {
                title: 'Tabs',
                rows: [
                    ['Ctrl+Shift+T / Ctrl+Shift+N', 'Connect in a new tab'],
                    ['Ctrl+Tab / Ctrl+Shift+Tab', 'Next / previous tab (Ctrl+PgDn / Ctrl+PgUp)'],
                    ['Ctrl+Shift+W', 'Close tab (only the current pane of a split tab)'],
                    ['Ctrl+Shift+\\ / Ctrl+Shift+-', 'Split right / down (| and - as in tmux). Each pane has its own connection (current server filled in)'],
                    ['Alt+←→↑↓', 'Move to the next pane in a split tab (or click it). Drag the divider to resize, double-click to even out'],
                    ['Enter (disconnected tab)', 'Open the Connect dialog in that tab'],
                    ['Mouse', '+ or double-click the tab bar: new tab · Middle-click: close · Drag: reorder · Right-click: menu'],
                ],
            },
            {
                title: 'Copy / Paste',
                rows: [
                    ['Select with mouse', 'Copies right away'],
                    ['Right-click', 'Paste (Shift+right-click in programs that use the mouse)'],
                    ['Ctrl+Shift+C / Ctrl+Shift+V', 'Copy / Paste'],
                    ['Multi-line paste', 'Asks before pasting (can be turned off)'],
                ],
            },
            {
                title: 'View',
                rows: [
                    ['Ctrl+Wheel / Ctrl+= / Ctrl+-', 'Larger / smaller font (all tabs, remembered)'],
                    ['Ctrl+0', 'Default font size (15)'],
                    ['Ctrl+Click', 'Open an http(s) address in the browser'],
                    ['Keyword highlight', 'error · fail on red, warning on yellow, success on green. Edit the words or turn it off in Settings (Ctrl+Shift+O)'],
                    ['Ctrl+Shift+S', 'Search scrollback (Enter: up, Shift+Enter: down, Alt+C: match case, Alt+R: regex, Esc: close)'],
                ],
            },
            {
                title: 'Sessions',
                rows: [
                    ['Ctrl+Shift+H / ★', 'Session manager: saved sessions by group; edit, duplicate, delete, search. Enter: connect · "Open group": every session of the group in tabs'],
                    ['☆ Save in the Connect dialog (Alt+S)', 'Saves what you typed with a name and group. The password can be saved too (encrypted for your Windows account)'],
                    ['Tab right-click → Save as session', "Saves the tab's connection. A tab opened from a session shows the session's name"],
                    ['Typing in Host', 'Narrows the ▼ list by name, address or group. Saved sessions come first, by group'],
                    ['Import · Export', 'In the session manager: import PuTTY sessions, export to / import from a file (passwords are never exported)'],
                ],
            },
            {
                title: 'Connection',
                rows: [
                    ['Alt+C / Alt+A / Alt+S / Esc', 'Connect / Cancel / Save as session / Close in the Connect dialog'],
                    ['Ctrl+Shift+D', 'Disconnect (keeps the tab)'],
                    ['Ctrl+Shift+E', 'Toggle UTF-8 ↔ EUC-KR'],
                    ['Auto reconnect', 'After a network drop, retries at 3/5/10/20/30 s, up to 10 times (Enter: now · Esc: cancel)'],
                    ['Ctrl+Shift+O / ⚙', 'Settings: font, color theme, cursor shape, scrollback lines, translucency (whole window / background only), paste confirmation, keepalive interval, auto reconnect, session log'],
                    ['Tab right-click → Color theme', 'A different color theme for this server. Saved per host and port, used on the next connection too'],
                    ['Ctrl+Shift+L', 'Start / stop the session log: keeps writing the output to a file (red dot on the tab). Auto start, folder, plain text and a timestamp per line in Settings'],
                    ['Ctrl+Shift+M', 'Macros: save frequent commands and send them (Enter / double-click)'],
                    ['Ctrl+Shift+P', 'SSH port forwarding: local (-L) / remote (-R) / dynamic (-D, SOCKS5). Saved per host and started on the next connection'],
                    ['Ctrl+Shift+J', 'Docker containers on the SSH server or on this PC (Docker Desktop). Open a shell (docker exec, sharing the SSH connection) · view the log · start/stop/restart · open a port (forwards the server port to this PC and opens the browser). Uses podman when docker is missing'],
                    ['F2-F12, Shift/Ctrl+F1-F12', 'Sends the macro bound to the key (unbound keys reach the program)'],
                    ['Lua scripts', 'Set a macro\'s kind to "Lua script", or tab right-click → "Lua 스크립트 실행..." (Documents\\choboterm\\scripts). send · expect · sleep · screen · print. The tab shows ▶ time so far; output and result appear bottom right. Runs in its own process (256MB limit), so a broken script can\'t take choboterm down'],
                    ['Ctrl+Shift+K', 'Lua console (REPL): runs each input at once and keeps variables. An expression shows its value. Shift+Enter new line · ↑↓ earlier input · Ctrl+C stop it · Ctrl+O (or "열기") load a Lua file into the input · Ctrl+S (or "저장") save input and output to a file · Esc back to the terminal · drag the top edge to resize · ✕ or Ctrl+Shift+K again closes it (stopping code that runs)'],
                    ['Port', '22 = SSH, 21 = FTP, 23 = Telnet, others are auto-detected'],
                    ['cmd · powershell · wsl in Host', 'Opens a shell on this PC (cmd, PowerShell, pwsh, WSL) in the tab. Arguments work too ("wsl -d Ubuntu"). Listed as "local shell" under ▼'],
                    ['COM3 · /dev/ttyUSB0 in Host', 'Opens a serial port. The Port field is the speed (baud, 115200 by default). Data bits, parity (N/E/O), stop bits and flow control (rtscts · xonxoff) go after the name: "COM3 9600 7E1 rtscts". Connected ports are listed under ▼. Tab right-click → "Send break". Unplugging and replugging a USB adapter reconnects'],
                    ['SSH login questions', 'One-time codes and other server questions, and passphrases of encrypted keys, are asked in a window. With Pass empty the password is asked too. Agent keys (OpenSSH, Pageant) are used automatically'],
                    ['ssh config name in Host', 'Type a Host name from ~/.ssh/config (e.g. busan), "ssh busan" or "ssh -p 2222 user@busan" to use its HostName, Port, User and IdentityFile. Also listed under ▼'],
                    ['Jump (jump host)', 'Connect through a bastion (ProxyJump): user@bastion in the Jump field, several separated by commas. Empty = ProxyJump of ~/.ssh/config; "ssh -J bastion web" works too; none = no jump'],
                ],
            },
            {
                title: 'File transfer',
                rows: [
                    ['Ctrl+Shift+F', 'File window (SFTP / SCP / FTP)'],
                    ['↑ ↓ Home End', 'Select (Shift: extend)'],
                    ['Ctrl+Click / Shift+Click / Ctrl+A', 'Multi-select / Range / Select all'],
                    ['Enter / Double-click', 'Open folder · Download (several items or folders go into a folder you pick, with all contents)'],
                    ['Delete / F2 / F7', 'Delete (folders with contents) / Rename / New folder'],
                    ['Shift+F4', 'New empty file (never replaces one with the same name)'],
                    ['F3 / F4', 'View / edit text (UTF-8 / EUC-KR auto-detected, encoding can be forced, Ctrl+F find, Ctrl+S save to server)'],
                    ['F3 (image)', 'Image viewer (JPG·PNG·GIF·BMP·WebP·SVG, up to 30MB): ←→ previous/next, 0 fit, 1 actual size, +/−·Ctrl+wheel zoom, drag to move, Esc close'],
                    ['Drag and drop', 'Drop files or folders from Explorer on the list to upload · Right-click: menu'],
                    ['Backspace · Alt+↑ · ..', 'Parent folder (Enter / double-click the .. row at the top)'],
                    ['Resume', 'A broken download stays as .part; downloading it again asks to resume or start over. Broken uploads too (SFTP · FTP)'],
                    ['F5 / Esc', 'Refresh / Close'],
                    ['sz file / rz', 'Zmodem transfer from the terminal (Ctrl+C / Ctrl+X to cancel)'],
                ],
            },
            {
                title: 'Help',
                rows: [
                    ['F1', 'Open / close this help'],
                    ['Language', 'Auto / 한국어 / English in Settings (Ctrl+Shift+O). Applies after a restart'],
                    ['Alt+L', 'Switch English ↔ 한국어'],
                ],
            },
        ],
        notes: [
            'Downloads and Zmodem received files are saved to ~/Downloads by default.',
            'Connection history: %AppData%\\choboterm\\hosts.json',
            'Session logs: saved as "host_port_date_time.log" in Documents\\choboterm\\logs by default.',
        ],
        links: {
            title: 'Links',
            rows: [
                ['Releases', `${REPO}/releases/latest`],
                ['License (MIT)', `${REPO}/blob/main/LICENSE`],
                ['Third-party licenses', `${REPO}/blob/main/THIRD_PARTY_NOTICES.txt`],
            ],
        },
    },
};

const LANG_KEY = 'choboterm.helpLang';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('helpOverlay');
const panel = $<HTMLDivElement>('help');
const title = $<HTMLSpanElement>('helpTitle');
const content = $<HTMLDivElement>('helpContent');
const langBtn = $<HTMLButtonElement>('helpLang');
const closeBtn = $<HTMLButtonElement>('helpClose');

let lang: Lang | undefined; // read when first shown: the app's language is known by then
let onClose: (() => void) | undefined;

function loadLang(): Lang {
    try {
        const v = localStorage.getItem(LANG_KEY);
        if (v === 'ko' || v === 'en') return v;
    } catch {
        // Storage unavailable: fall back to the default.
    }
    return currentLang();
}

function saveLang() {
    try {
        localStorage.setItem(LANG_KEY, lang!);
    } catch {
        // Not remembered; harmless.
    }
}

function render() {
    lang ??= loadLang();
    const t = TEXT[lang];
    title.textContent = `${t.title} (F1)`;
    langBtn.textContent = t.other;
    closeBtn.textContent = t.close;
    content.replaceChildren();
    for (const s of t.sections) {
        const h = document.createElement('h3');
        h.textContent = s.title;
        const table = document.createElement('table');
        for (const [key, desc] of s.rows) {
            const tr = table.insertRow();
            const k = tr.insertCell();
            k.className = 'key';
            k.textContent = key;
            tr.insertCell().textContent = desc;
        }
        content.append(h, table);
    }
    const ul = document.createElement('ul');
    for (const n of t.notes) {
        const li = document.createElement('li');
        li.textContent = n;
        ul.append(li);
    }
    content.append(ul);

    // Links open in the default browser.
    const h = document.createElement('h3');
    h.textContent = t.links.title;
    const table = document.createElement('table');
    for (const [label, url] of t.links.rows) {
        const tr = table.insertRow();
        const k = tr.insertCell();
        k.className = 'key';
        k.textContent = label;
        const a = document.createElement('a');
        a.href = url;
        a.textContent = url.replace('https://', '');
        a.addEventListener('click', ev => {
            ev.preventDefault();
            BrowserOpenURL(url);
        });
        tr.insertCell().append(a);
    }
    content.append(h, table);
}

export function helpOpen() {
    return !overlay.hidden;
}

/** Opens the help window. close is called after it is closed, to restore focus. */
export function openHelp(close: () => void) {
    onClose = close;
    render();
    overlay.hidden = false;
    content.scrollTop = 0;
    panel.focus();
}

export function closeHelp() {
    if (overlay.hidden) return;
    overlay.hidden = true;
    const cb = onClose;
    onClose = undefined;
    cb?.();
}

export function toggleHelp(close: () => void) {
    if (helpOpen()) closeHelp();
    else openHelp(close);
}

langBtn.addEventListener('click', () => {
    lang = lang === 'ko' ? 'en' : 'ko';
    saveLang();
    render();
    panel.focus();
});
closeBtn.addEventListener('click', closeHelp);
$<HTMLButtonElement>('helpX').addEventListener('click', closeHelp);
overlay.addEventListener('mousedown', ev => {
    if (ev.target === overlay) closeHelp();
});

panel.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.preventDefault();
        closeHelp();
    } else if (ev.altKey && (ev.key === 'l' || ev.key === 'L')) {
        ev.preventDefault();
        langBtn.click();
    }
    // Keep keys away from the terminal and other windows underneath.
    ev.stopPropagation();
});
