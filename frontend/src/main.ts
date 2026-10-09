import {dbg} from './debug';
import '@xterm/xterm/css/xterm.css';
import './style.css';

import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {WebLinksAddon} from '@xterm/addon-web-links';
import {WebglAddon} from '@xterm/addon-webgl';
import {ambiguousFontReady, fontFamily, loadUnicodeWidths, NARROW, WIDE} from './cjkwidth';
import {helpOpen, toggleHelp} from './help';
import {confirmPaste, pasteConfirmOpen} from './paste';
import {initTranslucencyPreview, openPrefs, prefsOpen} from './prefs';
import {expandMacro, macroForKey, macrosOpen, openMacros} from './macros';
import {forwardsOpen, forwardsTabClosed, openForwards} from './forwards';
import {containersOpen, containersTabClosed, DockerMode, DockerSource, openContainers} from './containers';
import {askOpen} from './dialog';
import {openSaveSession, openSessionManager, sessionManagerOpen, sessionSaveOpen} from './sessions';
import {authPromptOpen, initAuthPrompt} from './authprompt';
import {viewerOpen} from './viewer';
import {attachHighlight, Highlighter, loadRules} from './highlight';
import {Lang, startI18n, tr} from './i18n';
import {imageOpen} from './imageview';
import {attachSearch, closeSearch, openSearch, switchSearch} from './search';
import {loadSettings, saveSettings, settings} from './settings';
import {themeByName, THEMES} from './themes';
import {closeFiles, filesOpen, focusFiles, forgetFiles, openFiles, setActiveTabProvider, showFilesFor} from './files';

import {
    CloseTab, Connect, Disconnect, DockerOpen, GetLanguage, ConsoleEval, ConsoleOpen, ConsoleSave, ConsoleInterrupt, RunScript, RunScriptFile, ScriptScreen, StartConsole, ShowScripts, StopScript, GetHistory, GetLocalShells, GetSessions, GetSSHConfigHosts, GetVersion, LookupSSH, Resize, Send,
    SetEncoding, SetTabTheme, ShowLogs, StartLog, StopLog, TabTheme,
} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {BrowserOpenURL, ClipboardGetText, ClipboardSetText, Environment, EventsOn, WindowSetTitle} from '../wailsjs/runtime/runtime';
import {isLinux, setPlatform} from './platform';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

let appName = 'choboterm';
let version = '';

// ---- Tabs ----

// idle: not connected, on: terminal session, ftp: FTP (file window only)
type TabState = 'idle' | 'on' | 'ftp';

// Host field texts that run a program on this PC instead of connecting (see localshell.go).
const LOCAL_SHELL = /^(cmd|powershell|pwsh|wsl)(\.exe)?(\s|$)/i;

function isLocal(host: string): boolean {
    return LOCAL_SHELL.test(host.trim());
}

// A tab bar entry. It shows one or more panes (split with Ctrl+Shift+\ / -);
// each pane is a Tab with its own connection and its own entry in the chip.
interface Group {
    box: HTMLDivElement;  // the panes' layout in #terms
    chip: HTMLDivElement; // the panes' tab bar entries
}

type SplitDir = 'row' | 'col'; // row: side by side, col: one above the other

interface Tab {
    id: number;
    group: Group;
    term: Terminal;
    fit: FitAddon;
    search: ReturnType<typeof attachSearch>;
    hl: Highlighter;
    pane: HTMLDivElement;
    el: HTMLDivElement;
    label: HTMLSpanElement;
    state: TabState;
    proto: string; // "ssh" / "telnet" / "ftp" / "local" as reported by Connect
    encoding: string;
    // Last connection, kept in memory only, for reconnect / duplicate.
    req?: main.ConnectRequest;
    // A docker exec / logs tab (no req): what to open again on reconnect.
    docker?: {src: DockerSource; id: string; name: string; mode: DockerMode};
    // The saved session req came from: its name is the tab label.
    session?: main.SavedSession;
    // This tab's Connect dialog, while it is open.
    dialog?: DialogState;
    // Created just for a Connect dialog: cancelling the dialog closes it.
    temp?: boolean;
    // Automatic reconnect after the connection broke: the pending attempt.
    retry?: {attempt: number; timer: number};
    reconnecting?: boolean;
    // Session log file while logging is on.
    log?: string;
    // The Lua script running in the tab, the badge showing its time, and the
    // box with its print() output and how it ended (shown over the pane).
    script?: {name: string; started: number; console: boolean};
    run: HTMLSpanElement;
    scriptBox?: ScriptBox;
    // Input waiting for the Send call in flight (see sendInput).
    outbox: string;
    sending: boolean;
    // The shell's folder as reported by OSC 7, and the window title it set.
    osc7?: string;
    title?: string;
    // The GPU (WebGL) renderer, while it draws this tab instead of the DOM renderer.
    webgl?: WebglAddon;
    // Color theme chosen for this tab's server; undefined: the one in Settings.
    theme?: string;
}

// Connect dialog contents saved per tab, so switching tabs keeps them.
interface DialogState {
    host: string;
    port: string;
    login: string;
    pass: string;
    encoding: string;
    jump: string;
    error: string;
    connecting: boolean;
    resolved: string; // Host text whose ~/.ssh/config values were already filled in
    session?: main.SavedSession; // the saved session picked from the list
}

const tabs = new Map<number, Tab>();
const tabsEl = $<HTMLDivElement>('tabs');
const termsEl = $<HTMLDivElement>('terms');
let active: Tab | undefined;
let nextId = 1;
let dragging: Tab | undefined;

function notice(t: Tab, msg: string) {
    t.term.write(`\r\n\x1b[33m[${tr(msg).replace(/\n/g, '\r\n')}]\x1b[0m\r\n`);
}

/** "Enter: 다시 접속", in yellow after a bracketed reason. */
function reconnectHint(t: Tab, why: string) {
    t.term.write(`\x1b[33m[${tr(why)}] ${tr('Enter: 다시 접속')}\x1b[0m\r\n`);
}

function welcome(t: Tab) {
    t.term.write(`choboterm V${version}\r\n`);
    t.term.write(`\x1b[90m${tr('Enter: 접속 창 · Ctrl+Shift+T: 새 탭 · Ctrl+Tab: 탭 전환 · Ctrl+Shift+W: 탭 닫기')}\r\n`);
    t.term.write(`${tr('Ctrl+Shift+E: UTF-8 ↔ EUC-KR · Ctrl+Shift+F: 파일 전송 · Ctrl+Shift+D: 연결 끊기')}\x1b[0m\r\n`);
}

function applyFontFamily(t: Tab) {
    const wide = t.encoding === 'EUC-KR';
    const font = fontFamily(wide ? settings.fontEucKr : settings.fontUtf8, wide);
    if (t.term.options.fontFamily !== font) t.term.options.fontFamily = font;
}

// EUC-KR screens assume ambiguous-width symbols (─│■○ etc.) take 2 columns.
function applyEncoding(t: Tab, name: string) {
    t.encoding = name;
    const wide = name === 'EUC-KR';
    const wanted = wide ? WIDE : NARROW;
    applyFontFamily(t);
    // Never let a missing width provider break connecting.
    if (t.term.unicode.versions.includes(wanted)) t.term.unicode.activeVersion = wanted;
}

// Software WebGL (SwiftShader, when the GPU is blocked) is slower than the DOM renderer.
const hardwareWebgl = (() => {
    const gl = document.createElement('canvas').getContext('webgl2');
    if (!gl) return false;
    const info = gl.getExtension('WEBGL_debug_renderer_info');
    const renderer = info ? String(gl.getParameter(info.UNMASKED_RENDERER_WEBGL)) : '';
    gl.getExtension('WEBGL_lose_context')?.loseContext();
    dbg(`webgl renderer: ${renderer || '(unknown)'}`);
    return !/swiftshader|software|llvmpipe/i.test(renderer);
})();

// The default DOM renderer rebuilds a span per styled run on every frame, which makes
// full-screen TUIs with many colors (sc-im, htop...) stutter; draw with WebGL instead.
// Returns the WebGL renderer if it is in use; onLost runs if it is dropped later.
function useWebgl(term: Terminal, onLost: () => void): WebglAddon | undefined {
    if (!hardwareWebgl) {
        dbg('webgl skipped: no hardware WebGL');
        return undefined;
    }
    try {
        const webgl = new WebglAddon();
        // GPU reset or too many contexts: drop back to the DOM renderer.
        webgl.onContextLoss(() => {
            dbg('webgl context lost; back to DOM renderer');
            webgl.dispose();
            onLost();
        });
        term.loadAddon(webgl);
        dbg('webgl renderer loaded');
        return webgl;
    } catch (e) {
        console.warn('WebGL renderer unavailable; using the DOM renderer', e);
        return undefined;
    }
}

/**
 * Draws t with the GPU, except on a see-through background, where the GPU
 * renderer draws text without proper anti-aliasing and the browser draws it
 * instead, unless the GPU was asked for there too (Settings).
 */
function chooseRenderer(t: Tab) {
    const want = !glassOn() || translucency.gpu;
    if (want && !t.webgl) {
        t.webgl = useWebgl(t.term, () => {
            t.webgl = undefined;
            if (t === active) updateTitle();
        });
    } else if (!want && t.webgl) {
        t.webgl.dispose();
        t.webgl = undefined;
    }
    if (t === active) updateTitle();
}

/** Creates a tab, or with split a new pane next to split.from in its tab. */
function createTab(split?: {from: Tab; dir: SplitDir}): Tab {
    const id = nextId++;

    const pane = document.createElement('div');
    pane.className = 'term';
    let group: Group;
    if (split) {
        group = split.from.group;
        splitPane(split.from.pane, pane, split.dir);
    } else {
        const box = document.createElement('div');
        box.className = 'group';
        box.hidden = true;
        box.appendChild(pane);
        termsEl.appendChild(box);
        const chip = document.createElement('div');
        chip.className = 'tabgroup';
        tabsEl.appendChild(chip);
        group = {box, chip};
    }

    const term = new Terminal({
        fontFamily: fontFamily(settings.fontUtf8, false),
        fontSize: settings.fontSize,
        allowProposedApi: true,
        ...lookOptions(),
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    const search = attachSearch(term);
    // Ctrl+click opens a URL; a plain click stays free for selecting.
    term.loadAddon(new WebLinksAddon((ev, uri) => {
        if (ev.ctrlKey) BrowserOpenURL(uri);
    }, {
        hover: () => (pane.title = 'Ctrl+클릭: 브라우저에서 열기'),
        leave: () => (pane.title = ''),
    }));
    loadUnicodeWidths(term);
    term.unicode.activeVersion = NARROW;
    term.attachCustomKeyEventHandler(ev => !isAppShortcut(ev));
    dbg(`tab ${id} open`);
    term.open(pane);

    term.onData(d => dbg(`tab ${id} onData ${JSON.stringify(d)}`));
    term.onRender(({start, end}) => dbg(`tab ${id} render rows ${start}-${end}`));

    const el = document.createElement('div');
    el.className = 'tab';
    el.draggable = true;
    el.dataset.id = String(id);
    const dot = document.createElement('span');
    dot.className = 'dot';
    const label = document.createElement('span');
    label.className = 'label';
    label.textContent = '새 탭';
    const x = document.createElement('button');
    x.className = 'x';
    x.type = 'button';
    x.title = '탭 닫기 (Ctrl+Shift+W)';
    x.textContent = '✕';
    const run = document.createElement('span');
    run.className = 'run';
    run.hidden = true;
    el.append(dot, label, run, x);
    if (split) split.from.el.after(el);
    else group.chip.appendChild(el);

    const hl = attachHighlight(term);
    const t: Tab = {id, group, term, fit, search, hl, pane, el, label, run, state: 'idle', proto: '', encoding: 'UTF-8', outbox: '', sending: false};
    tabs.set(id, t);
    chooseRenderer(t);
    applyLook(t);
    paintGroup(group);
    // Window size, splits and dragged dividers all change the pane's size.
    new ResizeObserver(() => fitPane(t)).observe(pane);

    // Shells can report their folder as OSC 7 file://host/path (e.g. with vte.sh).
    term.parser.registerOscHandler(7, data => {
        const m = /^file:\/\/[^/]*(\/.*)$/.exec(data);
        if (m) {
            try {
                t.osc7 = decodeURIComponent(m[1]);
            } catch {
                t.osc7 = m[1];
            }
        }
        return true;
    });
    term.onTitleChange(title => (t.title = title));

    term.onData(data => {
        if (t.state === 'on') sendInput(t, data);
        else if (t.reconnecting) return;
        else if (t.retry) {
            if (data === '\r') retryNow(t);
            else if (data === '\x1b') {
                cancelRetry(t);
                reconnectHint(t, '자동 재접속을 취소했습니다');
            }
        } else if (data === '\r') t.docker ? reconnect(t) : openDialog(t);
    });
    term.onResize(({cols, rows}) => {
        if (t.state === 'on') Resize(t.id, cols, rows);
    });

    // Clicking (or tabbing) into a pane of a split tab makes it the active one.
    pane.addEventListener('focusin', () => {
        if (active !== t && tabs.has(t.id)) activate(t);
    });

    // Like PuTTY: selecting with the mouse copies, right-click pastes.
    pane.addEventListener('mouseup', ev => {
        if (ev.button !== 0) return;
        dbg(`tab ${t.id} (${t.proto}) mouseup: selection ${t.term.getSelection().length} chars, mouse mode ${term.modes.mouseTrackingMode}`);
        copySelection(t);
    });
    term.onSelectionChange(() => dbg(`tab ${t.id} selection ${term.getSelection().length} chars`));
    pane.addEventListener('contextmenu', ev => {
        ev.preventDefault();
        // Programs using the mouse (mc, htop...) get the click; Shift+right-click still pastes.
        if (term.modes.mouseTrackingMode !== 'none' && !ev.shiftKey) return;
        pasteClipboard(t);
    });
    // Ctrl+V / Shift+Insert go through the same multi-line check.
    pane.addEventListener('paste', ev => {
        if (t.scriptBox?.el.contains(ev.target as Node)) return; // the Lua console's input pastes as text
        ev.preventDefault();
        ev.stopPropagation();
        pasteText(t, ev.clipboardData?.getData('text/plain') ?? '');
    }, true);

    el.addEventListener('mousedown', ev => {
        if (ev.button === 0) activate(t);
    });
    el.addEventListener('auxclick', ev => {
        if (ev.button === 1) { // middle click closes, like browsers and MobaXterm
            ev.preventDefault();
            closeTab(t);
        }
    });
    el.addEventListener('contextmenu', ev => {
        ev.preventDefault();
        activate(t);
        showMenu(t, ev.clientX, ev.clientY);
    });
    x.addEventListener('mousedown', ev => ev.stopPropagation());
    x.addEventListener('click', () => closeTab(t));

    // Drag to reorder; a split tab moves as a whole.
    el.addEventListener('dragstart', ev => {
        dragging = t;
        t.group.chip.classList.add('dragging');
        ev.dataTransfer?.setData('text/plain', String(id));
    });
    el.addEventListener('dragend', () => {
        dragging = undefined;
        t.group.chip.classList.remove('dragging');
    });
    el.addEventListener('dragover', ev => {
        if (!dragging || dragging.group === t.group) return;
        ev.preventDefault();
        const chip = t.group.chip;
        const r = chip.getBoundingClientRect();
        tabsEl.insertBefore(dragging.group.chip, ev.clientX < r.left + r.width / 2 ? chip : chip.nextSibling);
    });

    welcome(t);
    return t;
}

// ---- Split panes ----

/** The panes of group g, in tab bar order. */
function panesOf(g: Group): Tab[] {
    return orderedTabs().filter(t => t.group === g);
}

/** Fits a pane to its size; panes of hidden tabs are fitted when they are shown. */
function fitPane(t: Tab) {
    if (t.pane.clientWidth > 0 && t.pane.clientHeight > 0) t.fit.fit();
}

function paintGroup(g: Group) {
    const multi = g.box.querySelectorAll('.term').length > 1;
    g.chip.classList.toggle('multi', multi);
    g.box.classList.toggle('multi', multi);
}

/** Puts pane next to old (right of it for row, below it for col), halving old's space. */
function splitPane(old: HTMLElement, pane: HTMLElement, dir: SplitDir) {
    const split = document.createElement('div');
    split.className = `split ${dir}`;
    split.style.flex = old.style.flex; // take old's place in its parent
    old.replaceWith(split);
    old.style.flex = '';
    const gutter = document.createElement('div');
    gutter.className = 'gutter';
    gutter.title = '끌어서 크기 조절 · 더블클릭: 반반';
    gutter.addEventListener('mousedown', ev => dragGutter(gutter, ev));
    gutter.addEventListener('dblclick', () => {
        (gutter.previousElementSibling as HTMLElement).style.flex = '';
        (gutter.nextElementSibling as HTMLElement).style.flex = '';
    });
    split.append(old, gutter, pane);
}

/** Takes pane out of its split; the part left over takes the split's place. */
function unsplitPane(pane: HTMLElement) {
    const split = pane.parentElement;
    pane.remove();
    if (!split?.classList.contains('split')) return;
    const rest = Array.from(split.children).find(el => !el.classList.contains('gutter')) as HTMLElement;
    rest.style.flex = split.style.flex;
    split.replaceWith(rest);
}

function dragGutter(gutter: HTMLElement, ev: MouseEvent) {
    if (ev.button !== 0) return;
    ev.preventDefault();
    const split = gutter.parentElement!;
    const a = gutter.previousElementSibling as HTMLElement;
    const b = gutter.nextElementSibling as HTMLElement;
    const row = split.classList.contains('row');
    document.body.classList.add(row ? 'resizing-row' : 'resizing-col');
    const move = (e: MouseEvent) => {
        const r = split.getBoundingClientRect();
        const pos = row ? (e.clientX - r.left) / r.width : (e.clientY - r.top) / r.height;
        const f = Math.max(0.1, Math.min(0.9, pos));
        a.style.flex = `${f} 1 0`;
        b.style.flex = `${1 - f} 1 0`;
    };
    const up = () => {
        window.removeEventListener('mousemove', move);
        window.removeEventListener('mouseup', up);
        document.body.classList.remove('resizing-row', 'resizing-col');
        focusActive();
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
}

/** Splits t's pane and opens the Connect dialog in the new one, filled in with t's server. */
function splitTab(t: Tab, dir: SplitDir) {
    if (t.dialog) return toast('접속 창을 먼저 닫으세요');
    const n = createTab({from: t, dir});
    n.temp = true; // cancelling its dialog closes the pane again
    openDialog(n, t);
}

/** Moves to the nearest pane in a direction within the active tab. */
function focusPane(dx: number, dy: number) {
    const t = active;
    if (!t) return;
    const c = t.pane.getBoundingClientRect();
    const cx = (c.left + c.right) / 2;
    const cy = (c.top + c.bottom) / 2;
    let best: Tab | undefined;
    let bestDist = Infinity;
    for (const o of panesOf(t.group)) {
        if (o === t) continue;
        const r = o.pane.getBoundingClientRect();
        const beyond = dx < 0 ? r.right <= c.left + 1 : dx > 0 ? r.left >= c.right - 1 :
            dy < 0 ? r.bottom <= c.top + 1 : r.top >= c.bottom - 1;
        if (!beyond) continue;
        const dist = Math.hypot((r.left + r.right) / 2 - cx, (r.top + r.bottom) / 2 - cy);
        if (dist < bestDist) {
            best = o;
            bestDist = dist;
        }
    }
    if (best) activate(best);
}

const ARROWS: Record<string, [number, number]> = {ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1]};

/**
 * Ctrl+Shift+\ (a "|": side by side) and Ctrl+Shift+- (one above the other) split
 * the pane, as in tmux. By physical key, so ₩ on Korean keyboards works too.
 */
function splitKey(ev: KeyboardEvent): SplitDir | undefined {
    if (!ev.ctrlKey || !ev.shiftKey || ev.altKey) return undefined;
    if (ev.code === 'Backslash' || ev.code === 'IntlYen') return 'row';
    if (ev.code === 'Minus') return 'col';
    return undefined;
}

/** Alt+arrow moves between panes, but only in a split tab; otherwise the program gets it. */
function isPaneKey(ev: KeyboardEvent): boolean {
    return ev.altKey && !ev.ctrlKey && !ev.shiftKey && ev.key in ARROWS &&
        !!active && panesOf(active.group).length > 1;
}

// ---- Colors and cursor ----

type CursorStyle = 'block' | 'underline' | 'bar';

// ---- Translucency ----

// The window is always see-through where the page is (main.go), so both kinds
// are drawn here. What is shown now: the saved setting, or the one being tried in Settings.
let translucency = {mode: 'off', opacity: 100, gpu: false};

function seeThroughOn() {
    return translucency.mode !== 'off' && translucency.opacity < 100;
}

/** "배경만": terminal backgrounds are see-through, text stays solid. */
function glassOn() {
    return seeThroughOn() && translucency.mode === 'background';
}

/**
 * "창 전체" fades the tab bar and the terminals, text included; dialogs and
 * menus are outside them and stay opaque. "배경만" paints each pane's
 * background see-through.
 */
function applyTranslucency(mode: string, opacity: number, gpu: boolean) {
    if (isLinux) mode = 'off'; // the window can't be see-through there
    translucency = {mode, opacity, gpu};
    const body = document.body.classList;
    body.toggle('seethrough', seeThroughOn());
    body.toggle('glass', glassOn());
    body.toggle('fade', seeThroughOn() && mode === 'window');
    document.documentElement.style.setProperty('--fade', String(opacity / 100));
    for (const t of tabs.values()) {
        chooseRenderer(t);
        applyLook(t);
    }
    paintMargin();
    dbg(`translucency ${mode} ${opacity}% gpu=${gpu} seethrough=${seeThroughOn()} glass=${glassOn()}`);
}

/** #rrggbb with the background opacity, for see-through panes. */
function seeThrough(color: string): string {
    const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})/i.exec(color);
    if (!m) return color;
    const [r, g, b] = m.slice(1).map(h => parseInt(h, 16));
    return `rgba(${r}, ${g}, ${b}, ${translucency.opacity / 100})`;
}

/** Terminal options set in the Settings window, besides the font; t's own theme if it has one. */
function lookOptions(t?: Tab) {
    const style = (['block', 'underline', 'bar'] as const).find(s => s === settings.cursorStyle) ?? 'block';
    const theme = themeByName(t?.theme || settings.theme).theme;
    return {
        // Before theme: xterm works out its colors when the theme is set.
        allowTransparency: glassOn(),
        // With see-through panes the pane draws the background, once.
        theme: glassOn() ? {...theme, background: '#00000000'} : theme,
        cursorStyle: style as CursorStyle,
        cursorBlink: settings.cursorBlink,
        scrollback: settings.scrollback > 0 ? settings.scrollback : 5000,
    };
}

/** Applies the settings to every tab (after the Settings window's OK). */
function applySettings() {
    applyTranslucency(settings.translucency || 'off', settings.opacity || 85, settings.glassGpu);
    applyFontSize();
    loadRules();
    for (const t of tabs.values()) t.hl.reset();
}

function applyLook(t: Tab) {
    Object.assign(t.term.options, lookOptions(t));
    const bg = themeByName(t.theme || settings.theme).theme.background ?? '#000';
    t.pane.style.background = glassOn() ? seeThrough(bg) : bg;
    if (t === active) paintMargin();
}

/** The margin around the terminals takes the active tab's background (inside the panes when see-through). */
function paintMargin() {
    const bg = themeByName(active?.theme || settings.theme).theme.background ?? '#000';
    document.documentElement.style.setProperty('--term-bg', seeThroughOn() ? 'transparent' : bg);
}

/** Picks a color theme for tab t ("" = Settings) and remembers it for its server. */
async function chooseTheme(t: Tab, name: string) {
    t.theme = name || undefined;
    applyLook(t);
    const label = name ? themeByName(name).label : '기본값 (설정 따름)';
    let saved = false;
    try {
        saved = await SetTabTheme(t.id, name);
    } catch (e) {
        notice(t, String(e));
    }
    if (!saved) return toast(`색 테마: ${label} (이 탭만)`);
    // Other tabs on the same server follow.
    for (const o of tabs.values()) {
        if (o === t || !o.req) continue;
        const theme = (await TabTheme(o.id)) || undefined;
        if (theme !== o.theme) {
            o.theme = theme;
            applyLook(o);
        }
    }
    toast(`색 테마: ${label} (${t.req?.host ?? '이 서버'}에 저장)`);
}

// ---- Font size ----

const MIN_FONT = 8;
const MAX_FONT = 40;
const DEFAULT_FONT = 15;

function setFontSize(n: number) {
    n = Math.max(MIN_FONT, Math.min(MAX_FONT, Math.round(n)));
    if (n !== settings.fontSize) {
        saveSettings(s => (s.fontSize = n));
        applyFontSize();
    }
    toast(`글꼴 크기 ${n}`);
}

function applyFontSize() {
    // Hidden tabs are fitted again when they are shown.
    for (const t of tabs.values()) {
        t.term.options.fontSize = settings.fontSize;
        applyFontFamily(t);
        fitPane(t);
    }
}

const toastEl = $<HTMLDivElement>('toast');
let toastTimer = 0;

function toast(msg: string) {
    toastEl.textContent = msg;
    toastEl.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = window.setTimeout(() => (toastEl.hidden = true), 1200);
}

termsEl.addEventListener('wheel', ev => {
    if (!ev.ctrlKey) return;
    ev.preventDefault();
    ev.stopPropagation(); // don't scroll the terminal as well
    setFontSize(settings.fontSize + (ev.deltaY < 0 ? 1 : -1));
}, {capture: true, passive: false});

// ---- Clipboard ----

function copySelection(t: Tab) {
    const text = t.term.getSelection();
    if (text) {
        ClipboardSetText(text)
            .then(ok => dbg(`clipboard set ${text.length} chars: ${ok}`))
            .catch(e => dbg(`clipboard set failed: ${e}`));
    }
}

async function pasteText(t: Tab, text: string) {
    if (!text || t.state !== 'on') return;
    const ok = await confirmPaste(text);
    if (ok && t.state === 'on') t.term.paste(text);
    if (t === active) focusActive();
}

async function pasteClipboard(t: Tab) {
    pasteText(t, await ClipboardGetText());
}

/** All panes in tab bar order (the panes of a split tab are next to each other). */
function orderedTabs(): Tab[] {
    return Array.from(tabsEl.querySelectorAll<HTMLElement>('.tab'))
        .map(el => tabs.get(Number(el.dataset.id)))
        .filter((t): t is Tab => !!t);
}

function activate(t: Tab) {
    const changed = active !== t;
    if (changed) {
        const old = active;
        if (old) {
            if (old.dialog) saveDialog(old);
            old.el.classList.remove('active');
            old.pane.classList.remove('focused');
        }
        if (old?.group !== t.group) {
            if (old) {
                old.group.box.hidden = true;
                old.group.chip.classList.remove('current');
            }
            t.group.box.hidden = false;
            t.group.chip.classList.add('current');
            for (const p of panesOf(t.group)) p.el.classList.remove('activity');
        }
        active = t;
        t.el.classList.add('active');
        t.el.classList.remove('activity');
        t.pane.classList.add('focused');
        t.el.scrollIntoView({block: 'nearest', inline: 'nearest'});
        paintMargin();
        // Each tab keeps its own Connect dialog and file window.
        showFilesFor(t.id);
        switchSearch(t.search);
        if (t.dialog) loadDialog(t);
        else hideDialog();
    }
    for (const p of panesOf(t.group)) fitPane(p);
    updateTitle();
    if (changed) focusActive();
    else if (!t.dialog && !filesOpen()) t.term.focus();
}

function focusActive() {
    const t = active;
    if (!t) return;
    if (t.dialog) host.focus();
    else if (filesOpen()) focusFiles();
    else t.term.focus();
}

function cycleTab(step: number) {
    const list = orderedTabs();
    if (list.length < 2 || !active) return;
    const i = list.indexOf(active);
    activate(list[(i + step + list.length) % list.length]);
}

function closeTab(t: Tab) {
    cancelRetry(t);
    forwardsTabClosed(t.id);
    forgetFiles(t.id);
    if (t === active) closeSearch();
    if (t.dialog && t === active) hideDialog();
    t.dialog = undefined;
    const list = orderedTabs();
    const i = list.indexOf(t);
    CloseTab(t.id);
    t.term.dispose();
    unsplitPane(t.pane);
    t.el.remove();
    tabs.delete(t.id);
    const g = t.group;
    if (g.box.querySelector('.term')) {
        paintGroup(g);
    } else {
        g.box.remove();
        g.chip.remove();
    }

    if (active === t) {
        active = undefined;
        // Closing one pane of a split tab stays in that tab.
        const near = [list[i - 1], list[i + 1]].find(n => n?.group === g);
        const next = near ?? list[i + 1] ?? list[i - 1];
        if (next) activate(next);
    }
    if (tabs.size === 0) {
        const n = createTab();
        activate(n);
        openDialog(n);
    }
}

function setState(t: Tab, state: TabState) {
    t.state = state;
    t.el.classList.toggle('on', state !== 'idle');
    if (t.docker) {
        const d = t.docker;
        t.label.textContent = d.mode === 'logs' ? `📜 ${d.name}` : `🐳 ${d.name}`;
        t.el.title = `${d.mode === 'logs' ? 'docker logs' : 'docker exec'}: ${d.name} @ ${d.src.label}`;
        if (t.log) t.el.title += `
로그 기록 중: ${t.log}`;
    } else if (t.req && t.proto === 'local') {
        t.label.textContent = t.session?.name ?? t.req.host;
        t.el.title = `로컬 셸: ${t.req.host}`;
        if (t.log) t.el.title += `
로그 기록 중: ${t.log}`;
    } else if (t.req) {
        const scheme = t.proto || 'telnet';
        // Show the port only when it isn't the protocol's usual one.
        const usual = [21, 22, 23].includes(Number(t.req.port));
        t.label.textContent = t.session?.name ?? (usual ? t.req.host : `${t.req.host}:${t.req.port}`);
        t.el.title = `${scheme}://${t.req.login ? t.req.login + '@' : ''}${t.req.host}:${t.req.port}`;
        if (t.log) t.el.title += `
로그 기록 중: ${t.log}`;
    }
    if (t === active) updateTitle();
}

function updateTitle() {
    let title = appName;
    const t = active;
    if (t?.docker && t.state !== 'idle') {
        title += ` - ${t.docker.name} @ ${t.docker.src.label} (docker)`;
    } else if (t?.req && t.state !== 'idle' && t.proto === 'local') {
        title += ` - ${t.req.host} (로컬)`;
    } else if (t?.req && t.state !== 'idle') {
        const where = `${t.req.host}:${t.req.port}`;
        title += t.state === 'ftp' ? ` - ftp://${where}` : ` - ${where}`;
        title += ` [${t.encoding}]`;
    }
    // At the right end: whether this tab is drawn with GPU acceleration.
    if (t) title += t.webgl ? '  ⚡GPU' : '  🐢CPU';
    WindowSetTitle(title);
}

/**
 * Connects tab t with req; returns the protocol. Throws on failure.
 * keepScreen keeps the old output (automatic reconnect) instead of clearing it.
 */
async function connectTab(t: Tab, req: main.ConnectRequest, keepScreen = false): Promise<string> {
    cancelRetry(t);
    req.cols = t.term.cols;
    req.rows = t.term.rows;
    forgetFiles(t.id); // a reconnect replaces the old connection's file window
    const protocol = await Connect(t.id, req);
    if (!tabs.has(t.id)) {
        CloseTab(t.id); // the tab was closed while connecting
        throw new Error('탭이 닫혔습니다');
    }
    t.req = req;
    t.proto = protocol;
    if (protocol === 'local') req.encoding = 'UTF-8'; // the pseudo console speaks UTF-8 only
    if (!keepScreen) t.term.reset();
    applyEncoding(t, req.encoding || 'UTF-8');
    if (protocol === 'ftp') {
        setState(t, 'ftp');
        t.term.write(`FTP ${req.host}:${req.port}\r\n\x1b[90m${tr('Ctrl+Shift+F: 파일 전송 창 열기 · 탭을 닫으면 연결이 끊어집니다')}\x1b[0m\r\n`);
    } else {
        setState(t, 'on');
        // The theme saved for this server, if any.
        t.theme = (await TabTheme(t.id)) || undefined;
        applyLook(t);
        // The screen was cleared after an automatic log start was announced.
        if (t.log && !keepScreen) notice(t, `로그 기록 중: ${t.log}`);
    }
    return protocol;
}

// ---- Automatic reconnect ----

const RETRY_DELAYS = [3, 5, 10, 20, 30]; // seconds; the last one repeats
const MAX_RETRIES = 10;

function scheduleRetry(t: Tab, attempt: number, why: string) {
    cancelRetry(t);
    const reason = `\r\n\x1b[33m[${tr(why).replace(/\n/g, '\r\n')}]\r\n`;
    if (attempt > MAX_RETRIES) {
        t.term.write(`${reason}[${tr(`${MAX_RETRIES}번 시도했지만 연결하지 못했습니다`)}] ${tr('Enter: 다시 접속')}\x1b[0m\r\n`);
        return;
    }
    const secs = RETRY_DELAYS[Math.min(attempt - 1, RETRY_DELAYS.length - 1)];
    t.term.write(`${reason}[${tr(`${secs}초 후 다시 연결합니다 (${attempt}/${MAX_RETRIES})`)}] ${tr('Enter: 지금 연결 · Esc: 취소')}\x1b[0m\r\n`);
    t.el.classList.add('retry');
    t.retry = {attempt, timer: window.setTimeout(() => retryNow(t), secs * 1000)};
}

function cancelRetry(t: Tab) {
    if (!t.retry) return;
    clearTimeout(t.retry.timer);
    t.retry = undefined;
    t.el.classList.remove('retry');
}

async function retryNow(t: Tab) {
    const attempt = t.retry?.attempt ?? 1;
    cancelRetry(t);
    if (!t.req || !tabs.has(t.id) || t.state !== 'idle') return;
    t.reconnecting = true;
    t.el.classList.add('retry');
    t.term.write(`\x1b[90m${tr(`다시 연결하는 중... ${t.req.host}:${t.req.port}`)}\x1b[0m\r\n`);
    try {
        await connectTab(t, t.req, true);
        notice(t, '다시 연결했습니다');
    } catch (e) {
        if (tabs.has(t.id) && t.state === 'idle') scheduleRetry(t, attempt + 1, String(e));
    } finally {
        t.reconnecting = false;
        if (!t.retry) t.el.classList.remove('retry');
    }
}

async function reconnect(t: Tab) {
    if (t.docker) return startDocker(t).catch(e => notice(t, String(e)));
    if (!t.req) return openDialog(t);
    try {
        if (await connectTab(t, t.req) === 'ftp') await openFilesFor(t);
    } catch (e) {
        notice(t, String(e));
    }
}

async function duplicate(t: Tab) {
    if (t.docker) return openDockerTab(t.docker.src, t.docker.id, t.docker.name, t.docker.mode);
    if (!t.req) return;
    const n = createTab();
    n.session = t.session;
    activate(n);
    try {
        if (await connectTab(n, main.ConnectRequest.createFrom({...t.req})) === 'ftp') await openFilesFor(n);
    } catch (e) {
        notice(n, String(e));
    }
}

/** Tab menu "세션으로 저장": saves the tab's connection; the tab then shows the session's name. */
async function saveTabSession(t: Tab) {
    if (!t.req) return;
    const r = t.req;
    const s = await openSaveSession({
        host: r.host, port: r.port, login: r.login, encoding: r.encoding, pass: r.pass, jump: r.jump, session: t.session,
    });
    if (s) {
        for (const o of tabs.values()) {
            if (o === t || (o.session && o.session.id === s.id)) {
                o.session = s;
                setState(o, o.state);
            }
        }
        toast(`세션 저장: ${s.group ? s.group + ' / ' : ''}${s.name}`);
    }
    focusActive();
}

function disconnect(t: Tab) {
    forwardsTabClosed(t.id);
    if (t.retry) {
        cancelRetry(t);
        reconnectHint(t, '자동 재접속을 취소했습니다');
    }
    if (t.state === 'idle') return;
    closeFiles(t.id);
    Disconnect(t.id);
    setState(t, 'idle');
    t.term.write('\r\n');
    reconnectHint(t, '연결을 끊었습니다');
}

/** The folder in the prompt at the cursor: "user@host:~/src$", "~/src $", "[/etc]#"... */
function promptDir(term: Terminal): string {
    const buf = term.buffer.active;
    const line = buf.getLine(buf.baseY + buf.cursorY)?.translateToString(true, 0, buf.cursorX) ?? '';
    // The first path followed by a prompt sign; later ones belong to the typed command.
    const m = /(~[^\s$#%>:]*|\/[^\s$#%>:\])]*)[\])]?\s?[$#%>](?:\s|$)/.exec(line);
    return m ? m[1] : '';
}

/** The folder in a window title like "user@host: ~/src". */
function titleDir(title = ''): string {
    const m = /(?:^|[:\s])(~[^\s]*|\/[^\s]*)\s*$/.exec(title);
    return m ? m[1] : '';
}

/** Where the shell is, as far as the terminal can tell ("" if unknown). */
function shellDir(t: Tab): string {
    if (t.proto !== 'ssh') return '';
    return promptDir(t.term) || t.osc7 || titleDir(t.title);
}

async function openFilesFor(t: Tab) {
    if (t.state === 'idle' || !t.req || t.dialog) return;
    if (t.proto === 'local') return toast('로컬 셸에서는 파일 전송 창을 쓰지 않습니다');
    try {
        await openFiles(t.id, t.req.host, {startDir: shellDir(t), onClose: () => focusActive()});
    } catch (e) {
        notice(t, String(e));
    }
}

async function toggleEncoding(t: Tab) {
    if (t.proto === 'local' && t.state !== 'idle') return toast('로컬 셸은 UTF-8만 씁니다');
    const name = await SetEncoding(t.id, t.encoding === 'EUC-KR' ? 'UTF-8' : 'EUC-KR');
    applyEncoding(t, name);
    if (t.req) t.req.encoding = name;
    notice(t, `인코딩: ${name}`);
    updateTitle();
}



// WebGL caches glyphs, so redraw them once the ambiguous-width face has arrived.
// (Not on every 'loadingdone': redrawing can load fonts again and loop forever.)
ambiguousFontReady.then(() => {
    dbg(`ambiguous font ready; clearing texture atlas of ${tabs.size} tab(s)`);
    tabs.forEach(t => t.term.clearTextureAtlas());
});

EventsOn('term:data', (id: number, b64: string) => {
    const t = tabs.get(id);
    if (!t) return;
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    const t0 = performance.now();
    t.term.write(bytes, () => {
        dbg(`tab ${id} wrote ${bytes.length} bytes in ${Math.round(performance.now() - t0)}ms`);
        t.hl.schedule();
    });
    if (t.group !== active?.group) t.el.classList.add('activity');
});

// lost: the connection broke (not ended by the server), so it may be reconnected.
EventsOn('term:closed', (id: number, msg: string, lost: boolean) => {
    const t = tabs.get(id);
    if (!t) return;
    dbg(`tab ${id} term:closed "${msg}" lost=${lost} autoReconnect=${settings.autoReconnect}`);
    forgetFiles(t.id);
    forwardsTabClosed(t.id);
    containersTabClosed(t.id);
    setState(t, 'idle');
    if (lost && settings.autoReconnect && t.req) scheduleRetry(t, 1, msg);
    else {
        t.term.write('\r\n');
        reconnectHint(t, msg);
    }
});

// path: the new log file, or "" when logging stopped.
EventsOn('log:changed', (id: number, path: string) => {
    const t = tabs.get(id);
    if (!t) return;
    t.log = path || undefined;
    t.el.classList.toggle('log', !!path);
    setState(t, t.state); // refresh the tooltip
    // While connecting, connectTab announces it once the screen is reset.
    if (t.state === 'on' || !path) notice(t, path ? `로그 기록 시작: ${path}` : '로그 기록을 멈췄습니다');
});

EventsOn('xfer:progress', (id: number) => tabs.get(id)?.el.classList.add('busy'));
EventsOn('xfer:end', (id: number) => tabs.get(id)?.el.classList.remove('busy'));

// ---- Tab context menu ----

const menu = $<HTMLUListElement>('tabMenu');
let menuTab: Tab | undefined;

function showMenu(t: Tab, x: number, y: number) {
    menuTab = t;
    const enable = (act: string, on: boolean) =>
        menu.querySelector(`[data-act="${act}"]`)!.classList.toggle('disabled', !on);
    enable('reconnect', true);
    enable('duplicate', !!t.req || !!t.docker);
    enable('files', t.state !== 'idle' && !!t.req && t.proto !== 'local');
    enable('forwards', t.state === 'on' && t.proto === 'ssh');
    enable('disconnect', t.state !== 'idle');
    enable('saveSession', !!t.req);
    enable('log', !!t.log || t.state === 'on');
    enable('script', t.state === 'on' && !t.script);
    enable('console', t.state === 'on' && (!t.script || t.script.console));
    enable('stopScript', !!t.script);
    menu.querySelector('[data-act="log"]')!.textContent = t.log ? '로그 기록 중지' : '로그 기록 시작';
    menu.querySelector('[data-act="close"]')!.textContent = panesOf(t.group).length > 1 ? '분할 창 닫기' : '탭 닫기';
    hideThemeMenu();
    menu.hidden = false;
    const r = menu.getBoundingClientRect();
    menu.style.left = `${Math.min(x, window.innerWidth - r.width - 4)}px`;
    menu.style.top = `${Math.min(y, window.innerHeight - r.height - 4)}px`;
}

function hideMenu() {
    menu.hidden = true;
    menuTab = undefined;
    hideThemeMenu();
}

// The "색 테마 ▸" submenu: Settings' theme, then every theme, the current one checked.
const themeMenu = $<HTMLUListElement>('themeMenu');
let themeMenuTab: Tab | undefined;

function showThemeMenu(t: Tab, li: HTMLElement) {
    themeMenuTab = t;
    themeMenu.innerHTML = '';
    const item = (name: string, label: string, bg: string, fg: string) => {
        const el = document.createElement('li');
        el.dataset.theme = name;
        if ((t.theme ?? '') === name) el.classList.add('checked');
        const sw = document.createElement('span');
        sw.className = 'swatch';
        sw.style.background = bg;
        sw.style.color = fg;
        sw.textContent = 'Aa';
        el.append(sw, label);
        themeMenu.appendChild(el);
    };
    const def = themeByName(settings.theme);
    item('', `기본값 (설정: ${def.label})`, def.theme.background!, def.theme.foreground!);
    const sep = document.createElement('li');
    sep.className = 'sep';
    themeMenu.appendChild(sep);
    for (const th of THEMES) item(th.name, th.label, th.theme.background!, th.theme.foreground!);
    themeMenu.hidden = false;
    const r = li.getBoundingClientRect();
    const m = themeMenu.getBoundingClientRect();
    const right = r.right + m.width <= window.innerWidth;
    themeMenu.style.left = `${right ? r.right - 2 : r.left - m.width + 2}px`;
    themeMenu.style.top = `${Math.max(4, Math.min(r.top - 4, window.innerHeight - m.height - 4))}px`;
}

function hideThemeMenu() {
    themeMenu.hidden = true;
    themeMenuTab = undefined;
}

themeMenu.addEventListener('mousedown', ev => ev.stopPropagation());
themeMenu.addEventListener('click', ev => {
    const li = (ev.target as HTMLElement).closest('li');
    const t = themeMenuTab;
    if (!li || !t || li.classList.contains('sep')) return;
    hideMenu();
    chooseTheme(t, li.dataset.theme ?? '');
    if (t === active) focusActive();
});

menu.addEventListener('mousedown', ev => ev.stopPropagation());
// Pointing at "색 테마 ▸" opens its submenu; pointing at another item closes it.
menu.addEventListener('mouseover', ev => {
    const li = (ev.target as HTMLElement).closest('li');
    if (!li || !menuTab || li.classList.contains('sep')) return;
    if (li.dataset.act === 'theme') {
        if (themeMenu.hidden) showThemeMenu(menuTab, li);
    } else {
        hideThemeMenu();
    }
});
menu.addEventListener('click', ev => {
    const li = (ev.target as HTMLElement).closest('li');
    const t = menuTab;
    if (!li || !t || li.classList.contains('disabled') || li.classList.contains('sep')) return;
    if (li.dataset.act === 'theme') return showThemeMenu(t, li);
    hideMenu();
    switch (li.dataset.act) {
        case 'reconnect':
            reconnect(t);
            break;
        case 'duplicate':
            duplicate(t);
            break;
        case 'containers':
            showContainers();
            break;
        case 'splitRight':
            splitTab(t, 'row');
            break;
        case 'splitDown':
            splitTab(t, 'col');
            break;
        case 'files':
            openFilesFor(t);
            break;
        case 'forwards':
            showForwards(t);
            break;
        case 'script':
            runScript(t, async () => void await RunScriptFile(t.id));
            break;
        case 'console':
            showConsole(t);
            break;
        case 'stopScript':
            StopScript(t.id);
            break;
        case 'scripts':
            ShowScripts().catch(e => toast(String(e)));
            break;
        case 'log':
            toggleLog(t);
            break;
        case 'logs':
            ShowLogs(t.id).catch(e => notice(t, String(e)));
            break;
        case 'saveSession':
            saveTabSession(t);
            break;
        case 'disconnect':
            disconnect(t);
            break;
        case 'close':
            closeTab(t);
            break;
    }
});
window.addEventListener('mousedown', hideMenu);
window.addEventListener('blur', hideMenu);

$<HTMLButtonElement>('newTab').addEventListener('click', () => openDialog(null));
tabsEl.addEventListener('dblclick', ev => {
    if (ev.target === tabsEl) openDialog(null); // double-click empty tab bar: new tab
});

// ---- Connect dialog ----

const overlay = $<HTMLDivElement>('overlay');
const form = $<HTMLFormElement>('connect');
const host = $<HTMLInputElement>('host');
const port = $<HTMLInputElement>('port');
const login = $<HTMLInputElement>('login');
const pass = $<HTMLInputElement>('pass');
const encoding = $<HTMLSelectElement>('encoding');
const jump = $<HTMLInputElement>('jump');
const JUMP_HINT = jump.placeholder;
const proto = $<HTMLSpanElement>('proto');
const error = $<HTMLDivElement>('error');
const ok = $<HTMLButtonElement>('ok');
const cancel = $<HTMLButtonElement>('cancel');
const hostDrop = $<HTMLButtonElement>('hostDrop');
const hostList = $<HTMLUListElement>('hostList');
const hostTip = $<HTMLDivElement>('hostTip');

type ListEntry = Pick<main.HostEntry, 'host' | 'port' | 'login' | 'encoding' | 'pass'> & {
    jump?: string;
    fromConfig?: boolean;
    label?: string;
    session?: main.SavedSession;
};

let history: main.HostEntry[] = [];
let sessions: main.SavedSession[] = [];
let configHosts: main.SSHConfigHost[] = [];
let localShells: main.LocalShell[] = [];
let entries: ListEntry[] = []; // saved sessions, history, then ~/.ssh/config names and local shells not in it
let visible: ListEntry[] = []; // entries matching what is typed in the Host field
let filter = '';
let activeIndex = -1;
let chosen: main.SavedSession | undefined; // saved session the fields came from
let resolved = '';

function updateProto() {
    // A local shell needs no port, login or password, and is always UTF-8.
    const local = isLocal(host.value);
    for (const el of [port, login, pass, encoding]) el.disabled = local;
    const p = Number(port.value);
    jump.disabled = local || p === 21 || p === 23; // jump hosts are for SSH only
    if (local) {
        proto.textContent = '로컬 셸';
        return;
    }
    // Other ports are detected from the server greeting when connecting.
    proto.textContent = p === 22 ? 'SSH' : p === 21 ? 'FTP' : p === 23 ? 'Telnet' : '자동 감지';
}

/** Reads what the Host list offers: saved sessions, history, ~/.ssh/config names, local shells. */
async function loadEntries() {
    [history, sessions, configHosts, localShells] = await Promise.all([
        GetHistory(), GetSessions(), GetSSHConfigHosts(), GetLocalShells(),
    ]).then(r => r.map(x => x ?? []) as [main.HostEntry[], main.SavedSession[], main.SSHConfigHost[], main.LocalShell[]]);
    const seen = new Set(history.map(h => h.host));
    const saved = new Set(sessions.map(s => `${s.host}:${s.port}`));
    entries = [...sessions.map(s => ({
        host: s.host, port: s.port, login: s.login, encoding: s.encoding, pass: s.pass, jump: s.jump, session: s,
    })), ...history.filter(h => !saved.has(`${h.host}:${h.port}`)), ...configHosts.filter(c => !seen.has(c.host)).map(c => ({
        host: c.host, port: c.port, login: c.login, encoding: 'UTF-8', pass: '', fromConfig: true,
    })), ...localShells.filter(s => !seen.has(s.name)).map(s => ({
        host: s.name, port: 0, login: '', encoding: 'UTF-8', pass: '', label: s.label,
    }))];
}

/**
 * Opens the Connect dialog. t: the tab to connect in, null: a new tab,
 * undefined: the active tab if it's empty, otherwise a new tab.
 */
async function openDialog(t?: Tab | null, from?: Tab) {
    if (active?.dialog) saveDialog(active);
    let tab: Tab;
    if (t === null || (t === undefined && !(active && active.state === 'idle'))) {
        tab = createTab();
        tab.temp = true;
    } else {
        tab = t ?? active!;
    }
    if (tab.dialog) return activate(tab);
    cancelRetry(tab);

    await loadEntries();
    const src = tab.req ? tab : from; // a new split pane starts with its neighbour's server
    const r = src?.req;
    if (r) {
        applyEntry({host: r.host, port: r.port, login: r.login, encoding: r.encoding, pass: r.pass, jump: r.jump, session: src.session});
    } else if (!host.value && history.length > 0) {
        applyEntry(history[0]);
    } else {
        fillSavedPass();
    }
    tab.dialog = {
        host: host.value, port: port.value, login: login.value, pass: pass.value,
        encoding: encoding.value, jump: jump.value, error: '', connecting: false, resolved, session: chosen,
    };
    if (active === tab) {
        loadDialog(tab);
        focusActive();
    } else {
        activate(tab);
    }
    host.select();
}

function saveDialog(t: Tab) {
    const d = t.dialog;
    if (!d) return;
    d.host = host.value;
    d.port = port.value;
    d.login = login.value;
    d.pass = pass.value;
    d.encoding = encoding.value;
    d.jump = jump.value;
    d.error = error.textContent ?? '';
    d.resolved = resolved;
    d.session = chosen;
    hideList();
}

function loadDialog(t: Tab) {
    const d = t.dialog!;
    host.value = d.host;
    port.value = d.port;
    login.value = d.login;
    pass.value = d.pass;
    encoding.value = d.encoding;
    jump.value = d.jump;
    resolved = d.resolved;
    chosen = d.session;
    error.textContent = d.error;
    paintConnecting(d);
    updateProto();
    overlay.hidden = false;
}

function hideDialog() {
    hideList();
    hideTip();
    overlay.hidden = true;
}

function paintConnecting(d: DialogState) {
    ok.disabled = cancel.disabled = d.connecting;
    ok.innerHTML = d.connecting ? '접속 중...' : '<u>C</u>onnect';
}

/** Closes tab t's dialog. A tab opened just for a cancelled dialog is closed too. */
function dismissDialog(t: Tab, cancelled: boolean) {
    t.dialog = undefined;
    if (t === active) hideDialog();
    if (cancelled && t.temp && t.state === 'idle' && tabs.size > 1) return closeTab(t);
    t.temp = false;
    if (t === active) focusActive();
}

function cancelDialog() {
    if (active?.dialog && !active.dialog.connecting) dismissDialog(active, true);
}

function applyEntry(e: ListEntry) {
    chosen = e.session;
    host.value = e.host;
    if (e.port > 0) port.value = String(e.port); // a local shell (port 0) keeps the port for later
    login.value = e.login;
    encoding.value = e.encoding || 'UTF-8';
    pass.value = e.pass ?? '';
    jump.value = e.jump ?? '';
    jump.placeholder = JUMP_HINT;
    resolved = host.value.trim();
    updateProto();
}

/**
 * Fills Port/Login from ~/.ssh/config when the Host field holds a config
 * name ("busan") or an ssh command line ("ssh -p 2222 me@busan"); the field
 * keeps just the name. Values the user set after that are left alone.
 */
async function resolveHost() {
    const text = host.value.trim();
    if (!text || text === resolved || isLocal(text)) return;
    const r = await LookupSSH(text);
    if (host.value.trim() !== text) return; // typed on meanwhile
    host.value = r.host;
    if (r.port > 0) port.value = String(r.port);
    if (r.login) login.value = r.login;
    if (r.jump) jump.value = r.jump; // ssh -J
    jump.placeholder = r.configJump ? `ssh config: ${r.configJump}` : JUMP_HINT;
    resolved = host.value.trim();
    updateProto();
    fillSavedPass();
    if (document.activeElement === host) showTip(r);
}

function hideTip() {
    hostTip.hidden = true;
}

function tipLine(text: string, cls = '') {
    const div = document.createElement('div');
    if (cls) div.className = cls;
    div.textContent = text;
    hostTip.appendChild(div);
}

/** Explains what the Host field accepts, or what the typed config name stands for. */
function showTip(r?: main.SSHTarget) {
    if (!hostList.hidden) return hideTip();
    hostTip.innerHTML = '';
    if (r?.fromConfig) {
        const user = r.login ? r.login + '@' : '';
        tipLine(`~/.ssh/config의 ${r.host}`, 'head');
        tipLine(`→ ${user}${r.hostName || r.host}:${r.port || 22}`);
    } else {
        tipLine('주소 또는 ~/.ssh/config의 Host 이름', 'head');
        tipLine('예) busan · ssh busan');
        tipLine('     ssh -p 2222 user@busan', 'pre');
        tipLine(isLinux ? '이 PC의 셸: bash · zsh · fish · sh' : '이 PC의 셸: cmd · powershell · wsl (wsl -d Ubuntu)');
        if (configHosts.length > 0) {
            const names = configHosts.slice(0, 6).map(c => c.host).join(', ');
            tipLine(`등록된 이름: ${names}${configHosts.length > 6 ? ' …' : ''} (▼ 목록)`, 'names');
        } else {
            tipLine('~/.ssh/config가 없거나 등록된 Host가 없습니다.', 'names');
        }
    }
    hostTip.hidden = false;
}

let tipSeq = 0;
async function refreshTip() {
    const text = host.value.trim();
    const seq = ++tipSeq;
    const r = text ? await LookupSSH(text) : undefined;
    if (seq === tipSeq && document.activeElement === host) showTip(r);
}

// Fills the remembered password for the host/port currently typed in:
// the picked session's, another session's for that server, or the history's (Telnet).
function fillSavedPass() {
    const h = host.value.trim(), p = Number(port.value);
    const same = (e: {host: string; port: number}) => e.host === h && e.port === p;
    if (chosen && !same(chosen)) chosen = undefined;
    pass.value = (chosen ?? sessions.find(s => same(s) && s.pass) ?? history.find(same))?.pass ?? '';
}

/** Entries that contain every word typed in the Host field (name, address, group or login). */
function matching(): ListEntry[] {
    const words = filter.toLowerCase().split(/\s+/).filter(Boolean);
    if (words.length === 0) return entries;
    return entries.filter(e => {
        const text = [e.host, e.login, e.label, e.session?.name, e.session?.group].join(' ').toLowerCase();
        return words.every(w => text.includes(w));
    });
}

/** The heading an entry goes under; only saved sessions get headings. */
function section(e: ListEntry): string {
    if (e.session) return '★ ' + (e.session.group || '저장된 세션');
    return sessions.length > 0 ? '최근 · ssh config · 로컬 셸' : '';
}

function showList() {
    hostList.innerHTML = '';
    visible = matching();
    if (visible.length === 0) return hideList();
    hideTip();
    let head = '';
    visible.forEach((e, i) => {
        const sec = section(e);
        if (sec !== head) {
            head = sec;
            const h = document.createElement('li');
            h.className = 'head';
            h.textContent = sec;
            h.addEventListener('mousedown', ev => ev.preventDefault());
            hostList.appendChild(h);
        }
        const li = document.createElement('li');
        li.textContent = e.session?.name ?? e.host;
        const meta = document.createElement('span');
        meta.className = 'meta';
        const where = e.session && e.session.name !== e.host ? `${e.host} · ` : '';
        meta.textContent = isLocal(e.host) ? `로컬 셸${e.label ? ' · ' + e.label : ''}` :
            `${where}${e.port}${e.login ? ' · ' + e.login : ''}${e.fromConfig ? ' · ssh config' : ''}`;
        li.appendChild(meta);
        li.addEventListener('mousedown', ev => {
            ev.preventDefault();
            applyEntry(e);
            hideList();
            focusAfterHost();
        });
        if (i === activeIndex) {
            li.className = 'active';
            requestAnimationFrame(() => li.scrollIntoView({block: 'nearest'}));
        }
        hostList.appendChild(li);
    });
    hostList.hidden = false;
}

function hideList() {
    hostList.hidden = true;
    activeIndex = -1;
    filter = '';
}

hostDrop.addEventListener('click', () => {
    if (hostList.hidden) {
        showList();
        host.focus();
    } else {
        hideList();
    }
});

host.addEventListener('blur', () => {
    hideList();
    hideTip();
});
host.addEventListener('focus', () => refreshTip());
host.addEventListener('input', () => {
    chosen = undefined;
    updateProto(); // "cmd", "wsl"... are local shells
    // Typing narrows the list down; with nothing typed the tooltip explains the field.
    filter = host.value.trim();
    activeIndex = -1;
    if (filter) showList();
    else hideList();
    if (hostList.hidden) refreshTip();
});
host.addEventListener('change', () => {
    fillSavedPass();
    resolveHost();
});
port.addEventListener('change', fillSavedPass);
host.addEventListener('keydown', ev => {
    if (ev.key === 'ArrowDown' || ev.key === 'ArrowUp') {
        ev.preventDefault();
        if (hostList.hidden) visible = matching();
        if (visible.length === 0) return;
        const step = ev.key === 'ArrowDown' ? 1 : -1;
        activeIndex = (activeIndex + step + visible.length) % visible.length;
        applyEntry(visible[activeIndex]);
        showList();
    } else if (ev.key === 'Enter' && !hostList.hidden) {
        // Nothing picked with the arrows: the list only opened because of typing,
        // so Enter connects to what was typed, as it does without the list.
        if (activeIndex < 0) return hideList();
        ev.preventDefault();
        hideList();
        focusAfterHost();
    }
});

/** After choosing a host: the password, or Connect for a local shell. */
function focusAfterHost() {
    (pass.disabled ? ok : pass).focus();
}

port.addEventListener('input', () => {
    port.value = port.value.replace(/\D/g, '');
    updateProto();
});

async function doConnect() {
    const t = active;
    if (!t?.dialog || t.dialog.connecting) return;
    await resolveHost();
    if (t !== active || !t.dialog || t.dialog.connecting) return;
    saveDialog(t);
    await connectDialog(t);
}

/** Connects tab t with what its Connect dialog holds; the dialog closes on success or shows the error. */
async function connectDialog(t: Tab) {
    const d = t.dialog;
    if (!d) return;
    d.error = '';
    d.connecting = true;
    error.textContent = '';
    paintConnecting(d);
    // The user may switch tabs while connecting; results go to tab t.
    try {
        const protocol = await connectTab(t, main.ConnectRequest.createFrom({
            host: d.host.trim(),
            port: Number(d.port),
            login: d.login,
            pass: d.pass,
            encoding: d.encoding,
            jump: d.jump.trim(),
        }));
        d.connecting = false;
        t.session = d.session;
        setState(t, t.state); // the label is the session's name
        if (t.dialog === d) dismissDialog(t, false);
        if (protocol === 'ftp') await openFilesFor(t);
    } catch (e) {
        d.connecting = false;
        d.error = String(e);
        if (t.dialog === d && t === active) {
            error.textContent = d.error;
            paintConnecting(d);
        }
    }
}

form.addEventListener('submit', ev => {
    ev.preventDefault();
    doConnect();
});
cancel.addEventListener('click', cancelDialog);
$<HTMLButtonElement>('close').addEventListener('click', cancelDialog);
$<HTMLButtonElement>('saveSess').addEventListener('click', saveFromDialog);

/** ☆ Save: saves what is typed in the Connect dialog as a session (or updates the picked one). */
async function saveFromDialog() {
    const t = active;
    if (!t?.dialog || t.dialog.connecting) return;
    hideList();
    await resolveHost();
    if (!host.value.trim()) {
        error.textContent = 'Host를 입력하세요';
        return host.focus();
    }
    const local = isLocal(host.value);
    const s = await openSaveSession({
        host: host.value.trim(),
        port: local ? 0 : Number(port.value),
        login: local ? '' : login.value,
        encoding: local ? 'UTF-8' : encoding.value,
        pass: local ? '' : pass.value,
        jump: local ? '' : jump.value.trim(),
        session: chosen,
    });
    if (s) {
        chosen = s;
        await loadEntries();
        toast(`세션 저장: ${s.group ? s.group + ' / ' : ''}${s.name}`);
    }
    if (t === active && t.dialog) ok.focus();
}

form.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.preventDefault();
        if (!hostList.hidden) hideList();
        else cancelDialog();
    } else if (ev.altKey && (ev.key === 'c' || ev.key === 'C')) {
        ev.preventDefault();
        doConnect();
    } else if (ev.altKey && (ev.key === 'a' || ev.key === 'A')) {
        ev.preventDefault();
        cancelDialog();
    } else if (ev.altKey && (ev.key === 's' || ev.key === 'S')) {
        ev.preventDefault();
        saveFromDialog();
    }
});

// ---- Settings window ----

function showPrefs() {
    if (!modalOpen()) openPrefs(applySettings, focusActive);
}

$<HTMLButtonElement>('prefsBtn').addEventListener('click', showPrefs);

// ---- Session manager ----

function showSessions() {
    if (!modalOpen()) openSessionManager(openSessions, focusActive);
}

$<HTMLButtonElement>('sessBtn').addEventListener('click', showSessions);

/**
 * Connects saved sessions: the first in the active tab if it is empty, the
 * others in new tabs. Each goes through its tab's Connect dialog, which stays
 * open with the error if the connection fails.
 */
async function openSessions(list: main.SavedSession[]) {
    for (const s of list) {
        await openDialog(list.length > 1 && s !== list[0] ? null : undefined);
        const t = active;
        if (!t?.dialog || t.dialog.connecting) continue;
        applyEntry({host: s.host, port: s.port, login: s.login, encoding: s.encoding, pass: s.pass, jump: s.jump, session: s});
        saveDialog(t);
        void connectDialog(t);
    }
}

// Wails runs each Go call on its own goroutine, so calls made back to back can
// reach the server out of order (e.g. replies to a burst of terminal queries).
// One Send at a time keeps the order; input typed meanwhile goes in the next one.
async function sendInput(t: Tab, data: string) {
    t.outbox += data;
    if (t.sending) return;
    t.sending = true;
    try {
        while (t.outbox) {
            const chunk = t.outbox;
            t.outbox = '';
            await Send(t.id, chunk);
        }
    } catch (e) {
        t.outbox = '';
        dbg(`tab ${t.id} send failed: ${e}`);
    } finally {
        t.sending = false;
    }
}

// ---- Lua scripts ----

/** Starts a script in t (start calls Go); errors such as "already running" become a toast. */
async function runScript(t: Tab, start: () => Promise<void>) {
    if (t.state !== 'on') return toast('연결되어 있지 않습니다');
    try {
        await start();
    } catch (e) {
        toast(String(e));
    }
}

function elapsedText(ms: number): string {
    const s = Math.floor(ms / 1000);
    const h = Math.floor(s / 3600), m = Math.floor(s / 60) % 60, sec = s % 60;
    const mmss = `${m}:${String(sec).padStart(2, '0')}`;
    return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : mmss;
}

interface ScriptBox {
    el: HTMLDivElement;
    head: HTMLSpanElement;
    body: HTMLDivElement;
    timer: number;
    // Lua console only: the input line, earlier inputs, and the input running now.
    input?: HTMLTextAreaElement;
    stop?: HTMLButtonElement;
    open?: HTMLButtonElement;
    history: string[];
    hpos: number;
    log: string[]; // every line shown, for saving: the box itself keeps only the last 200
    busy?: {id: number; started: number};
}

/** The ▶ badge on the tab and the box's heading: the running script's (or console input's) time so far. */
function paintScript(t: Tab) {
    const s = t.script;
    t.run.hidden = !s;
    if (!s) return;
    const box = t.scriptBox;
    if (s.console) {
        const busy = box?.busy;
        const time = busy ? elapsedText(Date.now() - busy.started) : '';
        t.run.textContent = busy ? `▶ ${time}` : 'Lua';
        t.run.title = busy ? 'Lua 콘솔: 실행 중 (Ctrl+C: 중지)' : 'Lua 콘솔 (Ctrl+Shift+K)';
        if (box) {
            box.head.textContent = busy ? `▶ Lua 콘솔 · 실행 중 ${time}` : 'Lua 콘솔';
            box.stop!.hidden = !busy;
        }
        return;
    }
    const time = elapsedText(Date.now() - s.started);
    t.run.textContent = `▶ ${time}`;
    t.run.title = `Lua 스크립트 실행 중: ${s.name}\n탭 우클릭 → 스크립트 중지`;
    if (box) box.head.textContent = `▶ ${s.name} · ${time}`;
}

/**
 * The script box in the corner of the pane. Script messages go here, not
 * into the terminal, where they would mix with the program's own screen.
 */
function scriptBox(t: Tab) {
    if (t.scriptBox) return t.scriptBox;
    const el = document.createElement('div');
    el.className = 'script-box';
    const top = document.createElement('div');
    top.className = 'top';
    const head = document.createElement('span');
    const x = document.createElement('button');
    x.type = 'button';
    x.textContent = '✕';
    x.title = '닫기';
    x.addEventListener('click', () => {
        if (t.script?.console) StopScript(t.id); // closing the console ends it
        closeScriptBox(t);
    });
    top.append(head, x);
    const body = document.createElement('div');
    body.className = 'lines';
    el.append(top, body);
    el.addEventListener('mousedown', ev => ev.stopPropagation()); // keep the terminal's selection
    t.pane.appendChild(el);
    t.scriptBox = {el, head, body, timer: 0, history: [], hpos: 0, log: []};
    return t.scriptBox;
}

// ---- Lua console ----

let evalSeq = 0;

/** Ctrl+Shift+K / tab menu: opens the tab's Lua console, or goes to it if it is open. */
function showConsole(t: Tab) {
    if (t.script?.console) return t.scriptBox?.input?.focus();
    runScript(t, () => StartConsole(t.id));
}

/** Turns the script box into a console: an input line under the output. */
function consoleBox(t: Tab) {
    const box = scriptBox(t);
    if (box.input) return box;
    box.el.classList.add('console');
    const grip = document.createElement('div');
    grip.className = 'grip';
    grip.title = '끌어서 높이 조절';
    grip.addEventListener('mousedown', ev => resizeConsole(box.el, ev));
    box.el.prepend(grip);
    if (settings.consoleHeight) box.el.style.height = `${settings.consoleHeight}px`;
    const stop = document.createElement('button');
    stop.type = 'button';
    stop.className = 'stop';
    stop.textContent = '중지';
    stop.title = 'Ctrl+C';
    stop.hidden = true;
    stop.addEventListener('click', () => ConsoleInterrupt(t.id));
    const save = document.createElement('button');
    save.type = 'button';
    save.className = 'stop';
    save.textContent = '저장';
    save.title = '입력·출력을 파일로 저장 (Ctrl+S)';
    save.addEventListener('click', () => consoleSave(box));
    const open = document.createElement('button');
    open.type = 'button';
    open.className = 'stop';
    open.textContent = '열기';
    open.title = 'Lua 파일을 입력창으로 불러오기 (Ctrl+O)';
    open.addEventListener('click', () => consoleOpen(box));
    box.head.after(stop, open, save);
    const input = document.createElement('textarea');
    input.className = 'input';
    input.rows = 1;
    input.spellcheck = false;
    input.placeholder = 'Lua · Enter 실행 · Shift+Enter 줄바꿈 · ↑↓ 이전 입력 · Ctrl+O 열기 · Ctrl+S 저장 · Esc 터미널로';
    input.addEventListener('input', () => fitInput(input));
    input.addEventListener('keydown', ev => consoleKey(t, box, ev));
    box.el.appendChild(input);
    box.input = input;
    requestAnimationFrame(() => consoleMinHeight(box.el));
    box.stop = stop;
    box.open = open;
    return box;
}

/** Drags the console's top edge: up makes it taller, up to 80% of the pane (the CSS max-height). The height is kept for next time. */
function resizeConsole(el: HTMLElement, ev: MouseEvent) {
    if (ev.button !== 0) return;
    ev.preventDefault();
    const startY = ev.clientY;
    const startH = el.offsetHeight;
    const maxH = el.parentElement!.clientHeight * 0.8;
    const move = (e: MouseEvent) => {
        el.style.height = `${Math.min(maxH, startH + startY - e.clientY)}px`; // the CSS min-height keeps the input whole
    };
    const up = () => {
        window.removeEventListener('mousemove', move);
        window.removeEventListener('mouseup', up);
        if (el.isConnected) saveSettings(s => s.consoleHeight = el.offsetHeight); // the box may have closed mid-drag
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
}

function fitInput(input: HTMLTextAreaElement) {
    input.style.height = 'auto';
    input.style.height = `${Math.min(input.scrollHeight, 120)}px`;
    input.style.overflowY = input.scrollHeight > 120 ? 'auto' : 'hidden';
    consoleMinHeight(input.parentElement as HTMLElement);
}

/** The console can't get smaller than its heading, the input line and three lines of output. */
function consoleMinHeight(el: HTMLElement) {
    const part = (sel: string) => (el.querySelector(sel) as HTMLElement | null)?.offsetHeight ?? 0;
    const lines = el.querySelector('.lines') as HTMLElement;
    const linesMin = parseFloat(getComputedStyle(lines).minHeight) + 8; // + its padding
    el.style.minHeight = `${part('.top') + part('.input') + linesMin + 2}px`; // + the border
}

function consoleKey(t: Tab, box: ScriptBox, ev: KeyboardEvent) {
    const input = box.input!;
    const lines = input.value.split('\n');
    const caretLine = input.value.slice(0, input.selectionStart).split('\n').length - 1;
    if (ev.key === 'Enter' && !ev.shiftKey) {
        ev.preventDefault();
        consoleRun(t, box);
    } else if (ev.key === 'Escape') {
        ev.preventDefault();
        focusActive();
    } else if (ev.key.toLowerCase() === 's' && ev.ctrlKey && !ev.shiftKey && !ev.altKey) {
        ev.preventDefault();
        consoleSave(box);
    } else if (ev.key.toLowerCase() === 'o' && ev.ctrlKey && !ev.shiftKey && !ev.altKey) {
        ev.preventDefault();
        consoleOpen(box);
    } else if (ev.key === 'c' && ev.ctrlKey && !ev.shiftKey && box.busy && input.selectionStart === input.selectionEnd) {
        ev.preventDefault();
        ConsoleInterrupt(t.id);
    } else if (ev.key === 'ArrowUp' && caretLine === 0 && box.hpos > 0) {
        ev.preventDefault();
        box.hpos--;
        input.value = box.history[box.hpos];
        fitInput(input);
    } else if (ev.key === 'ArrowDown' && caretLine === lines.length - 1 && box.hpos < box.history.length) {
        ev.preventDefault();
        box.hpos++;
        input.value = box.history[box.hpos] ?? '';
        fitInput(input);
    }
}

/** Puts a Lua file's text in the input line, to look at or edit before Enter runs it. */
async function consoleOpen(box: ScriptBox) {
    if (!box.input) return;
    try {
        const f = await ConsoleOpen();
        const input = box.input; // the console may have ended while the dialog was open
        if (!f.name || !input) return box.input?.focus();
        input.value = f.text.replace(/\n+$/, '');
        box.hpos = box.history.length;
        fitInput(input);
        input.setSelectionRange(0, 0);
        input.scrollTop = 0;
        if (!input.value) toast(`빈 파일입니다: ${f.name}`);
    } catch (e) {
        toast(`불러오지 못했습니다: ${e}`);
    }
    box.input?.focus();
}

async function consoleSave(box: ScriptBox) {
    if (!box.log.length) return toast('저장할 내용이 없습니다');
    try {
        const path = await ConsoleSave(box.log.join('\n') + '\n');
        if (path) toast(`저장했습니다: ${path}`);
    } catch (e) {
        toast(`저장하지 못했습니다: ${e}`);
    }
    box.input?.focus();
}

async function consoleRun(t: Tab, box: ScriptBox) {
    const input = box.input!;
    const code = input.value;
    if (!code.trim()) return;
    if (box.busy) return toast('앞의 입력이 실행 중입니다 (Ctrl+C: 중지)');
    if (box.history[box.history.length - 1] !== code) box.history.push(code);
    if (box.history.length > 100) box.history.shift();
    box.hpos = box.history.length;
    input.value = '';
    fitInput(input);
    code.split('\n').forEach((line, i) => scriptLine(t, (i ? '  ' : '> ') + line, 'in'));
    const id = ++evalSeq;
    box.busy = {id, started: Date.now()};
    paintScript(t);
    try {
        await ConsoleEval(t.id, id, code);
    } catch (e) {
        box.busy = undefined;
        scriptLine(t, String(e), 'err');
        paintScript(t);
    }
}

EventsOn('script:result', (id: number, req: number, value: string, err: string) => {
    const t = tabs.get(id);
    const box = t?.scriptBox;
    if (!t || !box || box.busy?.id !== req) return;
    box.busy = undefined;
    if (err) scriptLine(t, err, 'err');
    else if (value) scriptLine(t, value, 'val');
    paintScript(t);
});

function closeScriptBox(t: Tab) {
    if (!t.scriptBox) return;
    clearTimeout(t.scriptBox.timer);
    t.scriptBox.el.remove();
    t.scriptBox = undefined;
    if (t === active) focusActive();
}

function scriptLine(t: Tab, text: string, cls = '') {
    const box = scriptBox(t);
    for (const line of text.split('\n')) {
        const div = document.createElement('div');
        if (cls) div.className = cls;
        div.textContent = line;
        box.body.appendChild(div);
        box.log.push(line);
    }
    if (box.log.length > 22000) box.log.splice(0, box.log.length - 20000); // trimmed in batches, not a line at a time
    while (box.body.childElementCount > 200) box.body.firstElementChild!.remove();
    box.body.scrollTop = box.body.scrollHeight;
}

window.setInterval(() => {
    for (const t of tabs.values()) if (t.script) paintScript(t);
}, 1000);

EventsOn('script:start', (id: number, s: {name: string; started: number; console: boolean}) => {
    const t = tabs.get(id);
    if (!t) return;
    t.script = {name: s.name, started: s.started, console: s.console};
    closeScriptBox(t); // a new box: the last script's may be a console, or the other way round
    const box = s.console ? consoleBox(t) : scriptBox(t);
    paintScript(t);
    if (s.console) {
        scriptLine(t, 'send · expect · sleep · screen · print 사용 가능. 식을 입력하면 값을 보여 줍니다 (Lua 5.1).', 'hint');
        box.input!.focus();
    }
});
EventsOn('script:end', (id: number, ms: number, msg: string, stopped: boolean) => {
    const t = tabs.get(id);
    if (!t) return;
    const name = t.script?.name ?? '스크립트';
    t.script = undefined;
    paintScript(t);
    const time = elapsedText(ms);
    const box = scriptBox(t);
    if (box.input) {
        // The console is over: no more input.
        // Save stays: what was typed and shown can still be kept.
        box.input.remove();
        box.stop?.remove();
        box.open?.remove();
        box.input = box.stop = box.open = box.busy = undefined;
        box.el.style.minHeight = '';
    }
    if (stopped) {
        box.el.className = 'script-box stopped';
        box.head.textContent = `■ ${name} · 멈춤 · ${time}`;
        scriptLine(t, msg);
        box.timer = window.setTimeout(() => closeScriptBox(t), 5000);
    } else if (msg) {
        // Errors stay until closed: the line number is needed to fix the script.
        box.el.className = 'script-box failed';
        box.head.textContent = `✖ ${name} · ${time}`;
        scriptLine(t, msg, 'err');
    } else {
        box.el.className = 'script-box done';
        box.head.textContent = `✔ ${name} · 끝 · ${time}`;
        box.timer = window.setTimeout(() => closeScriptBox(t), 5000);
    }
});
EventsOn('script:print', (id: number, text: string) => {
    const t = tabs.get(id);
    if (t) scriptLine(t, text);
});
// screen(): the text the tab shows now.
EventsOn('script:screen', (id: number, req: number) => {
    const t = tabs.get(id);
    if (!t) return;
    const b = t.term.buffer.active;
    const lines: string[] = [];
    for (let y = b.viewportY; y < b.viewportY + t.term.rows; y++) {
        lines.push(b.getLine(y)?.translateToString(true) ?? '');
    }
    ScriptScreen(id, req, lines.join('\n').replace(/\s+$/, ''));
});

// ---- Macros ----

function sendMacro(t: Tab, m: main.Macro) {
    if (t.state !== 'on') return toast('연결되어 있지 않습니다');
    if (m.kind === 'lua') return runScript(t, () => RunScript(t.id, m.name || '매크로', m.text));
    const text = expandMacro(m.text);
    if (text) sendInput(t, text);
}

function showMacros() {
    if (modalOpen()) return;
    openMacros(m => active && sendMacro(active, m), focusActive);
}

// ---- Port forwarding ----

function showForwards(t: Tab) {
    if (modalOpen()) return;
    if (t.state !== 'on' || t.proto !== 'ssh' || !t.req) return toast('포트 포워딩은 SSH 접속에서만 사용할 수 있습니다');
    if (t !== active) activate(t);
    openForwards(t.id, t.req, focusActive);
}

// ---- Docker containers ----

function showContainers() {
    if (modalOpen()) return;
    const t = active;
    const srcs: DockerSource[] = [];
    if (t && t.state === 'on' && t.proto === 'ssh' && t.req) {
        srcs.push({tab: t.id, label: `${t.req.login ? t.req.login + '@' : ''}${t.req.host}`, host: t.req.host, port: Number(t.req.port)});
    }
    openContainers(srcs, (src, c, mode) => openDockerTab(src, c.id, c.name, mode), focusActive);
}

async function openDockerTab(src: DockerSource, id: string, name: string, mode: DockerMode) {
    const n = createTab();
    n.docker = {src, id, name, mode};
    activate(n);
    try {
        await startDocker(n);
    } catch (e) {
        notice(n, String(e));
    }
}

/** Opens (again) tab t's docker exec / logs session. Throws on failure. */
async function startDocker(t: Tab) {
    const d = t.docker!;
    // The tab it was opened from is gone: another tab to the same server will do.
    const sshOn = (o?: Tab) => !!o && o.state === 'on' && o.proto === 'ssh' && !!o.req;
    if (d.src.tab && !sshOn(tabs.get(d.src.tab))) {
        const o = [...tabs.values()].find(o => sshOn(o) && o.req!.host === d.src.host && Number(o.req!.port) === d.src.port);
        if (o) d.src = {...d.src, tab: o.id};
    }
    cancelRetry(t);
    forgetFiles(t.id);
    await DockerOpen(t.id, d.src.tab, d.id, d.mode, `${d.name}@${d.src.label}`, t.term.cols, t.term.rows);
    if (!tabs.has(t.id)) {
        CloseTab(t.id); // the tab was closed while opening
        return;
    }
    t.proto = 'docker';
    t.term.reset();
    applyEncoding(t, 'UTF-8');
    setState(t, 'on');
    applyLook(t);
}

// ---- Session log ----

/** Starts or stops writing tab t's output to a file; log:changed reports the result. */
async function toggleLog(t: Tab) {
    if (t.log) return StopLog(t.id);
    if (t.state !== 'on') return toast('연결된 터미널에서만 로그를 기록할 수 있습니다');
    try {
        await StartLog(t.id);
    } catch (e) {
        notice(t, String(e));
    }
}

// ---- Shortcuts ----

/** A window that takes all keys until it is closed. */
function modalOpen() {
    return pasteConfirmOpen() || prefsOpen() || macrosOpen() || forwardsOpen() || containersOpen() || askOpen() || viewerOpen() || imageOpen() ||
        authPromptOpen() || sessionSaveOpen() || sessionManagerOpen();
}

function isAppShortcut(ev: KeyboardEvent): boolean {
    if (ev.type !== 'keydown') return false;
    if (ev.key === 'F1' && !ev.ctrlKey && !ev.altKey && !ev.shiftKey) return true;
    if (macroForKey(ev)) return true;
    if (ev.ctrlKey && !ev.altKey && ['=', '+', '-', '0'].includes(ev.key)) return true; // font size
    if (ev.ctrlKey && (ev.key === 'Tab' || ev.key === 'PageUp' || ev.key === 'PageDown')) return true;
    if (isPaneKey(ev) || splitKey(ev)) return true;
    return ev.ctrlKey && ev.shiftKey && !ev.altKey && /^[TNWDEFCVSOMPLHKJ]$/i.test(ev.key);
}

window.addEventListener('keydown', ev => {
    if (!isAppShortcut(ev)) return;
    const t = active;
    const key = ev.key.toUpperCase();
    const macro = macroForKey(ev);
    // In the file window F5 refreshes; dialogs keep their keys too.
    if (macro && (t?.dialog || filesOpen() || helpOpen() || modalOpen())) return;
    // Copy / paste belong to the terminal; in a text box they keep their usual meaning.
    const el = ev.target;
    const typing = el instanceof HTMLInputElement ||
        (el instanceof HTMLTextAreaElement && !el.classList.contains('xterm-helper-textarea'));
    if ((key === 'C' || key === 'V') && (typing || t?.dialog || filesOpen() || helpOpen() || modalOpen())) return;
    ev.preventDefault();
    ev.stopPropagation();
    if (modalOpen()) return;
    if (macro) return t && sendMacro(t, macro);
    if (ev.key === 'F1') return toggleHelp(focusActive);
    // Other shortcuts wait until the help window is closed.
    if (helpOpen()) return;
    // Tab shortcuts work even while a Connect dialog or file window is open.
    if (ev.key === 'Tab') return cycleTab(ev.shiftKey ? -1 : 1);
    if (ev.key === 'PageDown') return cycleTab(1);
    if (ev.key === 'PageUp') return cycleTab(-1);
    if (ev.key === '=' || ev.key === '+') return setFontSize(settings.fontSize + 1);
    if (ev.key === '-') return setFontSize(settings.fontSize - 1);
    if (ev.key === '0') return setFontSize(DEFAULT_FONT);
    const dir = splitKey(ev);
    if (dir) return t && splitTab(t, dir);
    if (isPaneKey(ev)) {
        if (!t?.dialog && !filesOpen()) focusPane(...ARROWS[ev.key]);
        return;
    }
    switch (key) {
        case 'C':
            if (t) copySelection(t);
            break;
        case 'V':
            if (t) pasteClipboard(t);
            break;
        case 'S':
            if (t && !t.dialog && !filesOpen()) openSearch(t.search, focusActive);
            break;
        case 'O':
            showPrefs();
            break;
        case 'M':
            showMacros();
            break;
        case 'H':
            showSessions();
            break;
        case 'K':
            if (t) showConsole(t);
            break;
        case 'P':
            if (t) showForwards(t);
            break;
        case 'J':
            showContainers();
            break;
        case 'L':
            if (t) toggleLog(t);
            break;
        case 'T':
        case 'N':
            openDialog(null);
            break;
        case 'W':
            if (t) closeTab(t);
            break;
        case 'D':
            if (t) disconnect(t);
            break;
        case 'E':
            if (t) toggleEncoding(t);
            break;

        case 'F':
            if (t) openFilesFor(t);
            break;
    }
}, true);

// ---- Start ----

setActiveTabProvider(() => active?.id ?? 0);

// Login prompts name the connection they are for: the Connect dialog's values while it is connecting.
initAuthPrompt(id => {
    const t = tabs.get(id);
    const d = t?.dialog;
    const r = d ? {login: d.login, host: d.host.trim(), port: d.port} : t?.req;
    if (!r) return '';
    return `${r.login ? r.login + '@' : ''}${r.host}:${r.port}`;
}, focusActive);

Promise.all([GetVersion(), loadSettings(), GetLanguage(), Environment()]).then(([v, , lang, env]) => {
    version = v;
    setPlatform(env.platform);
    startI18n(lang === 'en' ? 'en' : 'ko' as Lang);
    initTranslucencyPreview(applyTranslucency);
    appName = `choboterm V${v}`;
    applySettings();
    const first = createTab();
    activate(first);
    openDialog(first);
});
