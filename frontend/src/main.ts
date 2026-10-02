import '@xterm/xterm/css/xterm.css';
import './style.css';

import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {Unicode11Addon} from '@xterm/addon-unicode11';

import {Connect, Disconnect, GetHistory, Resize, Send} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {EventsOn} from '../wailsjs/runtime/runtime';

// ---- Terminal ----

const term = new Terminal({
    fontFamily: '"D2Coding", "Consolas", "Malgun Gothic", monospace',
    fontSize: 15,
    cursorBlink: true,
    scrollback: 5000,
    allowProposedApi: true,
    theme: {background: '#000000'},
});
const fit = new FitAddon();
term.loadAddon(fit);
term.loadAddon(new Unicode11Addon());
term.unicode.activeVersion = '11';
term.open(document.getElementById('terminal')!);
fit.fit();

let connected = false;

term.onData(data => {
    if (connected) {
        Send(data);
    } else if (data === '\r') {
        openDialog();
    }
});
term.onResize(({cols, rows}) => {
    if (connected) Resize(cols, rows);
});
new ResizeObserver(() => fit.fit()).observe(document.getElementById('terminal')!);

EventsOn('term:data', (b64: string) => {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    term.write(bytes);
});

EventsOn('term:closed', (msg: string) => {
    connected = false;
    term.write(`\r\n\x1b[33m[${msg}] Enter: 다시 접속\x1b[0m\r\n`);
});

// ---- Connect dialog ----

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const overlay = $<HTMLDivElement>('overlay');
const form = $<HTMLFormElement>('connect');
const host = $<HTMLInputElement>('host');
const port = $<HTMLInputElement>('port');
const login = $<HTMLInputElement>('login');
const pass = $<HTMLInputElement>('pass');
const proto = $<HTMLSpanElement>('proto');
const error = $<HTMLDivElement>('error');
const ok = $<HTMLButtonElement>('ok');
const cancel = $<HTMLButtonElement>('cancel');
const hostDrop = $<HTMLButtonElement>('hostDrop');
const hostList = $<HTMLUListElement>('hostList');

let history: main.HostEntry[] = [];
let activeIndex = -1;

function updateProto() {
    proto.textContent = Number(port.value) === 22 ? 'SSH' : 'Telnet';
}

async function openDialog() {
    error.textContent = '';
    pass.value = '';
    history = (await GetHistory()) ?? [];
    if (!host.value && history.length > 0) applyEntry(history[0]);
    updateProto();
    overlay.hidden = false;
    host.focus();
    host.select();
}

function closeDialog() {
    hideList();
    overlay.hidden = true;
    term.focus();
}

function applyEntry(e: main.HostEntry) {
    host.value = e.host;
    port.value = String(e.port);
    login.value = e.login;
    updateProto();
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
    error.textContent = '';
    ok.disabled = cancel.disabled = true;
    ok.textContent = '접속 중...';
    try {
        await Connect(main.ConnectRequest.createFrom({
            host: host.value.trim(),
            port: Number(port.value),
            login: login.value,
            pass: pass.value,
            cols: term.cols,
            rows: term.rows,
        }));
        connected = true;
        pass.value = '';
        term.reset();
        closeDialog();
    } catch (e) {
        error.textContent = String(e);
    } finally {
        ok.disabled = cancel.disabled = false;
        ok.innerHTML = '<u>C</u>onnect';
    }
}

form.addEventListener('submit', ev => {
    ev.preventDefault();
    if (!ok.disabled) doConnect();
});
cancel.addEventListener('click', closeDialog);
$<HTMLButtonElement>('close').addEventListener('click', closeDialog);

form.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.preventDefault();
        if (!hostList.hidden) hideList();
        else closeDialog();
    } else if (ev.altKey && (ev.key === 'c' || ev.key === 'C')) {
        ev.preventDefault();
        if (!ok.disabled) doConnect();
    } else if (ev.altKey && (ev.key === 'a' || ev.key === 'A')) {
        ev.preventDefault();
        closeDialog();
    }
});

// Ctrl+Shift+N: open the connect dialog at any time (disconnects on connect).
window.addEventListener('keydown', ev => {
    if (ev.ctrlKey && ev.shiftKey && (ev.key === 'N' || ev.key === 'n')) {
        ev.preventDefault();
        openDialog();
    }
}, true);

// Ctrl+Shift+D: disconnect.
window.addEventListener('keydown', ev => {
    if (ev.ctrlKey && ev.shiftKey && (ev.key === 'D' || ev.key === 'd') && connected) {
        ev.preventDefault();
        connected = false;
        Disconnect();
        term.write('\r\n\x1b[33m[연결을 끊었습니다] Enter: 다시 접속\x1b[0m\r\n');
    }
}, true);

term.write('choboterm\r\n\x1b[90mEnter 또는 Ctrl+Shift+N: 접속 창 열기\x1b[0m\r\n');
openDialog();
