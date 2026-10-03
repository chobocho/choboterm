// F1 help window, switchable between Korean and English.

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
                    ['Ctrl+Shift+W', '탭 닫기'],
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
                ],
            },
            {
                title: '접속',
                rows: [
                    ['Alt+C / Alt+A / Esc', '접속 창에서 Connect / Cancel / 닫기'],
                    ['Ctrl+Shift+D', '연결 끊기 (탭은 유지)'],
                    ['Ctrl+Shift+E', 'UTF-8 ↔ EUC-KR 전환'],
                    ['포트', '22 = SSH, 21 = FTP, 23 = Telnet, 그 밖의 포트는 자동 판별'],
                ],
            },
            {
                title: '파일 전송',
                rows: [
                    ['Ctrl+Shift+F', '파일 전송 창 (SFTP / SCP / FTP)'],
                    ['↑ ↓ Home End', '선택'],
                    ['Enter / 더블클릭', '폴더 열기 · 다운로드'],
                    ['Backspace / F5 / Esc', '상위 폴더 / 새로 고침 / 닫기'],
                    ['sz 파일 / rz', '터미널에서 Zmodem 전송 (Ctrl+C / Ctrl+X로 취소)'],
                ],
            },
            {
                title: '도움말',
                rows: [
                    ['F1', '도움말 열기 / 닫기'],
                    ['Alt+L', '한국어 ↔ English 전환'],
                ],
            },
        ],
        notes: [
            '다운로드와 Zmodem 수신 파일은 기본으로 ~/Downloads에 저장됩니다.',
            '접속 기록: %AppData%\\choboterm\\hosts.json',
        ],
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
                    ['Ctrl+Shift+W', 'Close tab'],
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
                ],
            },
            {
                title: 'Connection',
                rows: [
                    ['Alt+C / Alt+A / Esc', 'Connect / Cancel / Close in the Connect dialog'],
                    ['Ctrl+Shift+D', 'Disconnect (keeps the tab)'],
                    ['Ctrl+Shift+E', 'Toggle UTF-8 ↔ EUC-KR'],
                    ['Port', '22 = SSH, 21 = FTP, 23 = Telnet, others are auto-detected'],
                ],
            },
            {
                title: 'File transfer',
                rows: [
                    ['Ctrl+Shift+F', 'File window (SFTP / SCP / FTP)'],
                    ['↑ ↓ Home End', 'Select'],
                    ['Enter / Double-click', 'Open folder · Download'],
                    ['Backspace / F5 / Esc', 'Parent folder / Refresh / Close'],
                    ['sz file / rz', 'Zmodem transfer from the terminal (Ctrl+C / Ctrl+X to cancel)'],
                ],
            },
            {
                title: 'Help',
                rows: [
                    ['F1', 'Open / close this help'],
                    ['Alt+L', 'Switch English ↔ 한국어'],
                ],
            },
        ],
        notes: [
            'Downloads and Zmodem received files are saved to ~/Downloads by default.',
            'Connection history: %AppData%\\choboterm\\hosts.json',
        ],
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

let lang: Lang = loadLang();
let onClose: (() => void) | undefined;

function loadLang(): Lang {
    try {
        const v = localStorage.getItem(LANG_KEY);
        if (v === 'ko' || v === 'en') return v;
    } catch {
        // Storage unavailable: fall back to the default.
    }
    return 'ko';
}

function saveLang() {
    try {
        localStorage.setItem(LANG_KEY, lang);
    } catch {
        // Not remembered; harmless.
    }
}

function render() {
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
