// The Connect dialog. Each tab keeps its own contents (DialogState), shown
// while the tab is the active one.

import {openSaveSession} from './sessions';
import {GetHistory, GetLocalShells, GetSerialPorts, GetSessions, GetSSHConfigHosts, LookupSSH} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {isLinux} from './platform';
import {active, DialogState, focusActive, Tab, tabs} from './tabstate';
import {toast} from './notify';
import {cancelRetry, connectTab, openFilesFor, setState} from './connection';
import {activate, closeTab, createTab} from './tabs';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

// Host field texts that run a program on this PC instead of connecting (see localshell.go).
const LOCAL_SHELL = /^(cmd|powershell|pwsh|wsl)(\.exe)?(\s|$)/i;
const TERMUX_LOCAL_SHELL = /^(bash|zsh|fish|sh)(\s|$)/i;

function isLocal(host: string): boolean {
    const name = host.trim();
    return LOCAL_SHELL.test(name) || (import.meta.env.VITE_TERMUX === '1' && TERMUX_LOCAL_SHELL.test(name));
}

// Host field texts that open a serial port; the Port field is then the speed (see serial.go).
const SERIAL_PORT = /^(com[1-9]\d*|\/dev\/\S+)(\s|$)/i;
const DEFAULT_BAUD = '115200';
const BAUDS = [300, 600, 1200, 2400, 4800, 9600, 14400, 19200, 38400, 57600, 115200, 230400, 460800, 921600];
const SERIAL_SUPPORTED = import.meta.env.VITE_TERMUX !== '1';

function isSerial(host: string): boolean {
    return SERIAL_SUPPORTED && SERIAL_PORT.test(host.trim());
}

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
const portLabel = $<HTMLLabelElement>('portLabel');
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
let serialPorts: main.SerialPort[] = [];
let serverPort = ''; // the Port field from before a serial port was typed, put back after
let entries: ListEntry[] = []; // saved sessions, history, then ~/.ssh/config names, local shells and serial ports not in it
let visible: ListEntry[] = []; // entries matching what is typed in the Host field
let filter = '';
let activeIndex = -1;
let chosen: main.SavedSession | undefined; // saved session the fields came from
let resolved = '';

/** Marks whether the Port field now holds a serial port's speed, without changing it. */
function syncPortMode() {
    if (isSerial(host.value) && !isLocal(host.value)) {
        port.dataset.baud = '1';
    } else {
        delete port.dataset.baud;
    }
}

function updateProto() {
    // A local shell needs no port, login or password, and is always UTF-8.
    const local = isLocal(host.value);
    // A serial port takes its speed in the Port field, and no login or password.
    const serial = !local && isSerial(host.value);
    port.disabled = encoding.disabled = local;
    login.disabled = pass.disabled = local || serial;
    portLabel.textContent = serial ? 'Baud' : 'Port';
    if (serial && !port.dataset.baud) {
        serverPort = port.value;
        port.dataset.baud = '1';
        if (!BAUDS.includes(Number(port.value))) port.value = DEFAULT_BAUD; // a server's port
    } else if (!serial && port.dataset.baud) {
        delete port.dataset.baud;
        port.value = serverPort || '22';
    }
    const p = Number(port.value);
    jump.disabled = local || serial || p === 21 || p === 23; // jump hosts are for SSH only
    if (local) {
        proto.textContent = '로컬 셸';
        return;
    }
    if (serial) {
        proto.textContent = '시리얼';
        return;
    }
    // Other ports are detected from the server greeting when connecting.
    proto.textContent = p === 22 ? 'SSH' : p === 21 ? 'FTP' : p === 23 ? 'Telnet' : '자동 감지';
}

/** Reads what the Host list offers: saved sessions, history, ~/.ssh/config names, local shells. */
async function loadEntries() {
    [history, sessions, configHosts, localShells, serialPorts] = await Promise.all([
        GetHistory(), GetSessions(), GetSSHConfigHosts(), GetLocalShells(), GetSerialPorts(),
    ]).then(r => r.map(x => x ?? []) as [main.HostEntry[], main.SavedSession[], main.SSHConfigHost[], main.LocalShell[], main.SerialPort[]]);
    const seen = new Set(history.map(h => h.host));
    const saved = new Set(sessions.map(s => `${s.host}:${s.port}`));
    entries = [...sessions.map(s => ({
        host: s.host, port: s.port, login: s.login, encoding: s.encoding, pass: s.pass, jump: s.jump, session: s,
    })), ...history.filter(h => !saved.has(`${h.host}:${h.port}`)), ...configHosts.filter(c => !seen.has(c.host)).map(c => ({
        host: c.host, port: c.port, login: c.login, encoding: 'UTF-8', pass: '', fromConfig: true,
    })), ...localShells.filter(s => !seen.has(s.name)).map(s => ({
        host: s.name, port: 0, login: '', encoding: 'UTF-8', pass: '', label: s.label,
    })), ...serialPorts.filter(s => !seen.has(s.name)).map(s => ({
        host: s.name, port: Number(DEFAULT_BAUD), login: '', encoding: 'UTF-8', pass: '', label: s.label,
    }))];
}

/**
 * Opens the Connect dialog. t: the tab to connect in, null: a new tab,
 * undefined: the active tab if it's empty, otherwise a new tab.
 */
export async function openDialog(t?: Tab | null, from?: Tab) {
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

export function saveDialog(t: Tab) {
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

export function loadDialog(t: Tab) {
    const d = t.dialog!;
    host.value = d.host;
    port.value = d.port;
    login.value = d.login;
    pass.value = d.pass;
    encoding.value = d.encoding;
    jump.value = d.jump;
    syncPortMode();
    resolved = d.resolved;
    chosen = d.session;
    error.textContent = d.error;
    paintConnecting(d);
    updateProto();
    overlay.hidden = false;
}

export function hideDialog() {
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
    // A local shell (port 0) keeps the port for later; a serial port's speed
    // doesn't replace the server port that comes back after it.
    if (isSerial(e.host) && !port.dataset.baud) serverPort = port.value;
    if (e.port > 0) port.value = String(e.port);
    syncPortMode();
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
    if (!text || text === resolved || isLocal(text) || isSerial(text)) return;
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
        tipLine(isLinux ? '시리얼 포트: /dev/ttyUSB0 · /dev/ttyUSB0 9600 7E1 (Port 칸 = 속도)' : '시리얼 포트: COM3 · COM3 9600 7E1 rtscts (Port 칸 = 속도)');
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
    return sessions.length > 0 ? '최근 · ssh config · 로컬 셸 · 시리얼' : '';
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
            isSerial(e.host) ? `${where}시리얼 · ${e.port} baud${e.label ? ' · ' + e.label : ''}` :
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

// The Host field keeps the focus: losing it would close the list just before the click that closes it.
hostDrop.addEventListener('mousedown', ev => ev.preventDefault());
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
    const serial = isSerial(d.host);
    try {
        const protocol = await connectTab(t, main.ConnectRequest.createFrom({
            host: d.host.trim(),
            port: Number(d.port),
            login: serial ? '' : d.login,
            pass: serial ? '' : d.pass,
            encoding: d.encoding,
            jump: serial ? '' : d.jump.trim(),
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
    const bare = local || isSerial(host.value); // no login, password or jump host
    const s = await openSaveSession({
        host: host.value.trim(),
        port: local ? 0 : Number(port.value),
        login: bare ? '' : login.value,
        encoding: local ? 'UTF-8' : encoding.value,
        pass: bare ? '' : pass.value,
        jump: bare ? '' : jump.value.trim(),
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

/**
 * Connects saved sessions: the first in the active tab if it is empty, the
 * others in new tabs. Each goes through its tab's Connect dialog, which stays
 * open with the error if the connection fails.
 */
export async function openSessions(list: main.SavedSession[]) {
    for (const s of list) {
        await openDialog(list.length > 1 && s !== list[0] ? null : undefined);
        const t = active;
        if (!t?.dialog || t.dialog.connecting) continue;
        applyEntry({host: s.host, port: s.port, login: s.login, encoding: s.encoding, pass: s.pass, jump: s.jump, session: s});
        saveDialog(t);
        void connectDialog(t);
    }
}
