import '@xterm/xterm/css/xterm.css';
import './style.css';

import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {WebLinksAddon} from '@xterm/addon-web-links';
import {loadUnicodeWidths, NARROW, WIDE} from './cjkwidth';
import {helpOpen, toggleHelp} from './help';
import {confirmPaste, pasteConfirmOpen} from './paste';
import {openPrefs, prefsOpen} from './prefs';
import {expandMacro, macroForKey, macrosOpen, openMacros} from './macros';
import {forwardsOpen, forwardsTabClosed, openForwards} from './forwards';
import {askOpen} from './dialog';
import {attachSearch, closeSearch, openSearch, switchSearch} from './search';
import {loadSettings, saveSettings, settings} from './settings';
import {closeFiles, filesOpen, focusFiles, forgetFiles, openFiles, setActiveTabProvider, showFilesFor} from './files';

import {
    CloseTab, Connect, Disconnect, GetHistory, GetVersion, Resize, Send, SetEncoding, ShowLogs, StartLog, StopLog,
} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {BrowserOpenURL, ClipboardGetText, ClipboardSetText, EventsOn, WindowSetTitle} from '../wailsjs/runtime/runtime';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

let appName = 'choboterm';
let version = '';

// ---- Tabs ----

// idle: not connected, on: terminal session, ftp: FTP (file window only)
type TabState = 'idle' | 'on' | 'ftp';

interface Tab {
    id: number;
    term: Terminal;
    fit: FitAddon;
    search: ReturnType<typeof attachSearch>;
    pane: HTMLDivElement;
    el: HTMLDivElement;
    label: HTMLSpanElement;
    state: TabState;
    proto: string; // "ssh" / "telnet" / "ftp" as reported by Connect
    encoding: string;
    // Last connection, kept in memory only, for reconnect / duplicate.
    req?: main.ConnectRequest;
    // This tab's Connect dialog, while it is open.
    dialog?: DialogState;
    // Created just for a Connect dialog: cancelling the dialog closes it.
    temp?: boolean;
    // Automatic reconnect after the connection broke: the pending attempt.
    retry?: {attempt: number; timer: number};
    reconnecting?: boolean;
    // Session log file while logging is on.
    log?: string;
}

// Connect dialog contents saved per tab, so switching tabs keeps them.
interface DialogState {
    host: string;
    port: string;
    login: string;
    pass: string;
    encoding: string;
    error: string;
    connecting: boolean;
}

const tabs = new Map<number, Tab>();
const tabsEl = $<HTMLDivElement>('tabs');
const termsEl = $<HTMLDivElement>('terms');
let active: Tab | undefined;
let nextId = 1;
let dragging: Tab | undefined;

function notice(t: Tab, msg: string) {
    t.term.write(`\r\n\x1b[33m[${msg.replace(/\n/g, '\r\n')}]\x1b[0m\r\n`);
}

function welcome(t: Tab) {
    t.term.write(`choboterm V${version}\r\n`);
    t.term.write('\x1b[90mEnter: 접속 창 · Ctrl+Shift+T: 새 탭 · Ctrl+Tab: 탭 전환 · Ctrl+Shift+W: 탭 닫기\r\n');
    t.term.write('Ctrl+Shift+E: UTF-8 ↔ EUC-KR · Ctrl+Shift+F: 파일 전송 · Ctrl+Shift+D: 연결 끊기\x1b[0m\r\n');
}

// EUC-KR screens assume ambiguous-width symbols (─│■○ etc.) take 2 columns.
function applyEncoding(t: Tab, name: string) {
    t.encoding = name;
    const wanted = name === 'EUC-KR' ? WIDE : NARROW;
    // Never let a missing width provider break connecting.
    if (t.term.unicode.versions.includes(wanted)) t.term.unicode.activeVersion = wanted;
}

function createTab(): Tab {
    const id = nextId++;

    const pane = document.createElement('div');
    pane.className = 'term';
    pane.hidden = true;
    termsEl.appendChild(pane);

    const term = new Terminal({
        fontFamily: '"D2Coding", "Consolas", "Malgun Gothic", monospace',
        fontSize: settings.fontSize,
        cursorBlink: true,
        scrollback: 5000,
        allowProposedApi: true,
        theme: {background: '#000000'},
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
    term.open(pane);

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
    el.append(dot, label, x);
    tabsEl.appendChild(el);

    const t: Tab = {id, term, fit, search, pane, el, label, state: 'idle', proto: '', encoding: 'UTF-8'};
    tabs.set(id, t);

    term.onData(data => {
        if (t.state === 'on') Send(t.id, data);
        else if (t.reconnecting) return;
        else if (t.retry) {
            if (data === '\r') retryNow(t);
            else if (data === '\x1b') {
                cancelRetry(t);
                t.term.write('\x1b[33m[자동 재접속을 취소했습니다] Enter: 다시 접속\x1b[0m\r\n');
            }
        } else if (data === '\r') openDialog(t);
    });
    term.onResize(({cols, rows}) => {
        if (t.state === 'on') Resize(t.id, cols, rows);
    });

    // Like PuTTY: selecting with the mouse copies, right-click pastes.
    pane.addEventListener('mouseup', ev => {
        if (ev.button === 0) copySelection(t);
    });
    pane.addEventListener('contextmenu', ev => {
        ev.preventDefault();
        // Programs using the mouse (mc, htop...) get the click; Shift+right-click still pastes.
        if (term.modes.mouseTrackingMode !== 'none' && !ev.shiftKey) return;
        pasteClipboard(t);
    });
    // Ctrl+V / Shift+Insert go through the same multi-line check.
    pane.addEventListener('paste', ev => {
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

    // Drag to reorder.
    el.addEventListener('dragstart', ev => {
        dragging = t;
        el.classList.add('dragging');
        ev.dataTransfer?.setData('text/plain', String(id));
    });
    el.addEventListener('dragend', () => {
        dragging = undefined;
        el.classList.remove('dragging');
    });
    el.addEventListener('dragover', ev => {
        if (!dragging || dragging === t) return;
        ev.preventDefault();
        const r = el.getBoundingClientRect();
        tabsEl.insertBefore(dragging.el, ev.clientX < r.left + r.width / 2 ? el : el.nextSibling);
    });

    welcome(t);
    return t;
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
    // Hidden tabs are fitted again when they are activated.
    for (const t of tabs.values()) t.term.options.fontSize = settings.fontSize;
    active?.fit.fit();
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
    if (text) ClipboardSetText(text);
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

function orderedTabs(): Tab[] {
    return Array.from(tabsEl.children)
        .map(el => tabs.get(Number((el as HTMLElement).dataset.id)))
        .filter((t): t is Tab => !!t);
}

function activate(t: Tab) {
    const changed = active !== t;
    if (changed) {
        if (active) {
            if (active.dialog) saveDialog(active);
            active.el.classList.remove('active');
            active.pane.hidden = true;
        }
        active = t;
        t.el.classList.add('active');
        t.el.classList.remove('activity');
        t.pane.hidden = false;
        t.el.scrollIntoView({block: 'nearest', inline: 'nearest'});
        // Each tab keeps its own Connect dialog and file window.
        showFilesFor(t.id);
        switchSearch(t.search);
        if (t.dialog) loadDialog(t);
        else hideDialog();
    }
    t.fit.fit();
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
    t.pane.remove();
    t.el.remove();
    tabs.delete(t.id);

    if (active === t) {
        active = undefined;
        const next = list[i + 1] ?? list[i - 1];
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
    if (t.req) {
        const scheme = t.proto || 'telnet';
        // Show the port only when it isn't the protocol's usual one.
        const usual = [21, 22, 23].includes(Number(t.req.port));
        t.label.textContent = usual ? t.req.host : `${t.req.host}:${t.req.port}`;
        t.el.title = `${scheme}://${t.req.login ? t.req.login + '@' : ''}${t.req.host}:${t.req.port}`;
        if (t.log) t.el.title += `
로그 기록 중: ${t.log}`;
    }
    if (t === active) updateTitle();
}

function updateTitle() {
    let title = appName;
    const t = active;
    if (t?.req && t.state !== 'idle') {
        const where = `${t.req.host}:${t.req.port}`;
        title += t.state === 'ftp' ? ` - ftp://${where}` : ` - ${where}`;
        title += ` [${t.encoding}]`;
    }
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
    if (!keepScreen) t.term.reset();
    applyEncoding(t, req.encoding || 'UTF-8');
    if (protocol === 'ftp') {
        setState(t, 'ftp');
        t.term.write(`FTP ${req.host}:${req.port}\r\n\x1b[90mCtrl+Shift+F: 파일 전송 창 열기 · 탭을 닫으면 연결이 끊어집니다\x1b[0m\r\n`);
    } else {
        setState(t, 'on');
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
    const reason = `\r\n\x1b[33m[${why.replace(/\n/g, '\r\n')}]\r\n`;
    if (attempt > MAX_RETRIES) {
        t.term.write(`${reason}[${MAX_RETRIES}번 시도했지만 연결하지 못했습니다] Enter: 다시 접속\x1b[0m\r\n`);
        return;
    }
    const secs = RETRY_DELAYS[Math.min(attempt - 1, RETRY_DELAYS.length - 1)];
    t.term.write(`${reason}[${secs}초 후 다시 연결합니다 (${attempt}/${MAX_RETRIES})] Enter: 지금 연결 · Esc: 취소\x1b[0m\r\n`);
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
    t.term.write(`\x1b[90m다시 연결하는 중... ${t.req.host}:${t.req.port}\x1b[0m\r\n`);
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
    if (!t.req) return openDialog(t);
    try {
        if (await connectTab(t, t.req) === 'ftp') await openFilesFor(t);
    } catch (e) {
        notice(t, String(e));
    }
}

async function duplicate(t: Tab) {
    if (!t.req) return;
    const n = createTab();
    activate(n);
    try {
        if (await connectTab(n, main.ConnectRequest.createFrom({...t.req})) === 'ftp') await openFilesFor(n);
    } catch (e) {
        notice(n, String(e));
    }
}

function disconnect(t: Tab) {
    forwardsTabClosed(t.id);
    if (t.retry) {
        cancelRetry(t);
        t.term.write('\x1b[33m[자동 재접속을 취소했습니다] Enter: 다시 접속\x1b[0m\r\n');
    }
    if (t.state === 'idle') return;
    closeFiles(t.id);
    Disconnect(t.id);
    setState(t, 'idle');
    t.term.write('\r\n\x1b[33m[연결을 끊었습니다] Enter: 다시 접속\x1b[0m\r\n');
}

async function openFilesFor(t: Tab) {
    if (t.state === 'idle' || !t.req || t.dialog) return;
    try {
        await openFiles(t.id, t.req.host, {onClose: () => focusActive()});
    } catch (e) {
        notice(t, String(e));
    }
}

async function toggleEncoding(t: Tab) {
    const name = await SetEncoding(t.id, t.encoding === 'EUC-KR' ? 'UTF-8' : 'EUC-KR');
    applyEncoding(t, name);
    if (t.req) t.req.encoding = name;
    notice(t, `인코딩: ${name}`);
    updateTitle();
}

new ResizeObserver(() => active?.fit.fit()).observe(termsEl);

EventsOn('term:data', (id: number, b64: string) => {
    const t = tabs.get(id);
    if (!t) return;
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    t.term.write(bytes);
    if (t !== active) t.el.classList.add('activity');
});

// lost: the connection broke (not ended by the server), so it may be reconnected.
EventsOn('term:closed', (id: number, msg: string, lost: boolean) => {
    const t = tabs.get(id);
    if (!t) return;
    forgetFiles(t.id);
    forwardsTabClosed(t.id);
    setState(t, 'idle');
    if (lost && settings.autoReconnect && t.req) scheduleRetry(t, 1, msg);
    else t.term.write(`\r\n\x1b[33m[${msg}] Enter: 다시 접속\x1b[0m\r\n`);
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
    enable('duplicate', !!t.req);
    enable('files', t.state !== 'idle');
    enable('forwards', t.state === 'on' && t.proto === 'ssh');
    enable('disconnect', t.state !== 'idle');
    enable('log', !!t.log || t.state === 'on');
    menu.querySelector('[data-act="log"]')!.textContent = t.log ? '로그 기록 중지' : '로그 기록 시작';
    menu.hidden = false;
    const r = menu.getBoundingClientRect();
    menu.style.left = `${Math.min(x, window.innerWidth - r.width - 4)}px`;
    menu.style.top = `${Math.min(y, window.innerHeight - r.height - 4)}px`;
}

function hideMenu() {
    menu.hidden = true;
    menuTab = undefined;
}

menu.addEventListener('mousedown', ev => ev.stopPropagation());
menu.addEventListener('click', ev => {
    const li = (ev.target as HTMLElement).closest('li');
    const t = menuTab;
    if (!li || !t || li.classList.contains('disabled') || li.classList.contains('sep')) return;
    hideMenu();
    switch (li.dataset.act) {
        case 'reconnect':
            reconnect(t);
            break;
        case 'duplicate':
            duplicate(t);
            break;
        case 'files':
            openFilesFor(t);
            break;
        case 'forwards':
            showForwards(t);
            break;
        case 'log':
            toggleLog(t);
            break;
        case 'logs':
            ShowLogs(t.id).catch(e => notice(t, String(e)));
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
const proto = $<HTMLSpanElement>('proto');
const error = $<HTMLDivElement>('error');
const ok = $<HTMLButtonElement>('ok');
const cancel = $<HTMLButtonElement>('cancel');
const hostDrop = $<HTMLButtonElement>('hostDrop');
const hostList = $<HTMLUListElement>('hostList');

let history: main.HostEntry[] = [];
let activeIndex = -1;

function updateProto() {
    const p = Number(port.value);
    // Other ports are detected from the server greeting when connecting.
    proto.textContent = p === 22 ? 'SSH' : p === 21 ? 'FTP' : p === 23 ? 'Telnet' : '자동 감지';
}

/**
 * Opens the Connect dialog. t: the tab to connect in, null: a new tab,
 * undefined: the active tab if it's empty, otherwise a new tab.
 */
async function openDialog(t?: Tab | null) {
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

    history = (await GetHistory()) ?? [];
    if (tab.req) {
        const r = tab.req;
        applyEntry({host: r.host, port: r.port, login: r.login, encoding: r.encoding, pass: r.pass});
    } else if (!host.value && history.length > 0) {
        applyEntry(history[0]);
    } else {
        fillSavedPass();
    }
    tab.dialog = {
        host: host.value, port: port.value, login: login.value, pass: pass.value,
        encoding: encoding.value, error: '', connecting: false,
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
    d.error = error.textContent ?? '';
    hideList();
}

function loadDialog(t: Tab) {
    const d = t.dialog!;
    host.value = d.host;
    port.value = d.port;
    login.value = d.login;
    pass.value = d.pass;
    encoding.value = d.encoding;
    error.textContent = d.error;
    paintConnecting(d);
    updateProto();
    overlay.hidden = false;
}

function hideDialog() {
    hideList();
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

function applyEntry(e: Pick<main.HostEntry, 'host' | 'port' | 'login' | 'encoding' | 'pass'>) {
    host.value = e.host;
    port.value = String(e.port);
    login.value = e.login;
    encoding.value = e.encoding || 'UTF-8';
    pass.value = e.pass ?? '';
    updateProto();
}

// Fills the remembered (Telnet) password for the host/port currently typed in.
function fillSavedPass() {
    const e = history.find(h => h.host === host.value.trim() && h.port === Number(port.value));
    pass.value = e?.pass ?? '';
}

function showList() {
    hostList.innerHTML = '';
    if (history.length === 0) return;
    history.forEach((e, i) => {
        const li = document.createElement('li');
        li.textContent = e.host;
        const meta = document.createElement('span');
        meta.className = 'meta';
        meta.textContent = `${e.port}${e.login ? ' · ' + e.login : ''}`;
        li.appendChild(meta);
        li.addEventListener('mousedown', ev => {
            ev.preventDefault();
            applyEntry(e);
            hideList();
            pass.focus();
        });
        if (i === activeIndex) li.className = 'active';
        hostList.appendChild(li);
    });
    hostList.hidden = false;
}

function hideList() {
    hostList.hidden = true;
    activeIndex = -1;
}

hostDrop.addEventListener('click', () => {
    if (hostList.hidden) {
        showList();
        host.focus();
    } else {
        hideList();
    }
});

host.addEventListener('blur', hideList);
host.addEventListener('change', fillSavedPass);
port.addEventListener('change', fillSavedPass);
host.addEventListener('keydown', ev => {
    if (ev.key === 'ArrowDown' || ev.key === 'ArrowUp') {
        ev.preventDefault();
        if (history.length === 0) return;
        const step = ev.key === 'ArrowDown' ? 1 : -1;
        activeIndex = (activeIndex + step + history.length) % history.length;
        applyEntry(history[activeIndex]);
        showList();
    } else if (ev.key === 'Enter' && !hostList.hidden) {
        ev.preventDefault();
        hideList();
        pass.focus();
    }
});

port.addEventListener('input', () => {
    port.value = port.value.replace(/\D/g, '');
    updateProto();
});

async function doConnect() {
    const t = active;
    if (!t?.dialog || t.dialog.connecting) return;
    saveDialog(t);
    const d = t.dialog;
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
        }));
        d.connecting = false;
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
    }
});

// ---- Settings window ----

function showPrefs() {
    if (!modalOpen()) openPrefs(applyFontSize, focusActive);
}

$<HTMLButtonElement>('prefsBtn').addEventListener('click', showPrefs);

// ---- Macros ----

function sendMacro(t: Tab, m: main.Macro) {
    if (t.state !== 'on') return toast('연결되어 있지 않습니다');
    const text = expandMacro(m.text);
    if (text) Send(t.id, text);
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
    openForwards(t.id, t.req.host, focusActive);
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
    return pasteConfirmOpen() || prefsOpen() || macrosOpen() || forwardsOpen() || askOpen();
}

function isAppShortcut(ev: KeyboardEvent): boolean {
    if (ev.type !== 'keydown') return false;
    if (ev.key === 'F1' && !ev.ctrlKey && !ev.altKey && !ev.shiftKey) return true;
    if (macroForKey(ev)) return true;
    if (ev.ctrlKey && !ev.altKey && ['=', '+', '-', '0'].includes(ev.key)) return true; // font size
    if (ev.ctrlKey && (ev.key === 'Tab' || ev.key === 'PageUp' || ev.key === 'PageDown')) return true;
    return ev.ctrlKey && ev.shiftKey && !ev.altKey && /^[TNWDEFCVSOMPL]$/i.test(ev.key);
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
        case 'P':
            if (t) showForwards(t);
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

Promise.all([GetVersion(), loadSettings()]).then(([v]) => {
    version = v;
    appName = `choboterm V${v}`;
    const first = createTab();
    activate(first);
    openDialog(first);
});
