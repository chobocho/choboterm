// SSH port forwarding window (Ctrl+Shift+P). Rules are saved per host by the
// Go side and start again on the next connection.

import {ForwardAdd, ForwardList, ForwardRemove} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {EventsOn} from '../wailsjs/runtime/runtime';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('pfOverlay');
const panel = $<HTMLDivElement>('pf');
const title = $<HTMLSpanElement>('pfTitle');
const body = $<HTMLTableSectionElement>('pfBody');
const type = $<HTMLSelectElement>('pfType');
const bindAddr = $<HTMLInputElement>('pfBindAddr');
const bindPort = $<HTMLInputElement>('pfBindPort');
const host = $<HTMLInputElement>('pfHost');
const port = $<HTMLInputElement>('pfPort');
const target = $<HTMLSpanElement>('pfTarget');
const hint = $<HTMLDivElement>('pfHint');
const error = $<HTMLDivElement>('pfError');
const delBtn = $<HTMLButtonElement>('pfDel');
const cmd = $<HTMLInputElement>('pfCmd');

const HINTS: Record<string, string> = {
    L: '로컬(-L): 이 PC의 수신 포트로 들어온 연결을 서버를 거쳐 대상으로 보냅니다. 대상은 서버에서 본 주소입니다 (예: localhost:5432).',
    R: '원격(-R): 서버의 수신 포트로 들어온 연결을 이 PC를 거쳐 대상으로 보냅니다. 대상은 이 PC에서 본 주소입니다.',
    D: '동적(-D): 이 PC의 수신 포트가 SOCKS5 프록시가 됩니다. 브라우저 등에서 프록시로 지정하면 서버를 거쳐 접속합니다.',
};

let tabId = 0;
let conn: main.ConnectRequest | undefined;
let rules: main.ForwardStatus[] = [];
let selected = -1;
let onClose: (() => void) | undefined;

export function forwardsOpen() {
    return !overlay.hidden;
}

function bindText(f: main.ForwardStatus) {
    return `${f.bindAddr}:${f.bindPort}`;
}

// An IPv6 address needs brackets inside ssh's colon-separated specs.
function sshAddr(a: string) {
    return a.includes(':') ? `[${a}]` : a;
}

/** The rule as an ssh option, e.g. "-L 127.0.0.1:8888:localhost:8888". */
function sshOption(f: main.Forward) {
    const bind = `${sshAddr(f.bindAddr)}:${f.bindPort}`;
    return f.type === 'D' ? `-D ${bind}` : `-${f.type} ${bind}:${sshAddr(f.host)}:${f.port}`;
}

/** The ssh command line that opens the same connection with all rules. */
function sshCommand() {
    if (!conn) return '';
    const parts = ['ssh', ...rules.map(sshOption)];
    if (conn.jump) parts.push('-J', conn.jump);
    if (conn.port && conn.port !== 22) parts.push('-p', String(conn.port));
    parts.push(conn.login ? `${conn.login}@${conn.host}` : conn.host);
    return parts.join(' ');
}

function paint() {
    body.replaceChildren();
    rules.forEach((f, i) => {
        const tr = body.insertRow();
        if (i === selected) tr.className = 'sel';
        tr.title = sshOption(f);
        tr.insertCell().textContent = f.type;
        tr.insertCell().textContent = bindText(f);
        tr.insertCell().textContent = f.type === 'D' ? 'SOCKS5' : `${f.host}:${f.port}`;
        const st = tr.insertCell();
        if (f.error) {
            st.className = 'bad';
            st.textContent = `오류: ${f.error}`;
            st.title = f.error;
        } else if (f.standby) {
            st.className = 'wait';
            st.textContent = '대기 (다른 탭에서 수신 중)';
            st.title = '같은 서버에 접속한 다른 탭이 이 포워딩을 맡고 있습니다. 그 탭의 접속이 끊기면 이 탭이 이어받습니다.';
        } else {
            st.className = 'ok';
            st.textContent = f.conns > 0 ? `수신 중 · 연결 ${f.conns}` : '수신 중';
            st.title = f.conns > 0
                ? `이 규칙을 통해 열려 있는 연결 ${f.conns}개`
                : '열린 연결 없음 (새 연결을 기다리는 중)';
        }
        tr.addEventListener('mousedown', () => {
            selected = i;
            paint();
        });
    });
    if (rules.length === 0) {
        const td = body.insertRow().insertCell();
        td.colSpan = 4;
        td.className = 'empty';
        td.textContent = '규칙이 없습니다. 아래에서 추가하세요.';
    }
    delBtn.disabled = selected < 0;
    cmd.value = sshCommand();
}

async function refresh() {
    try {
        rules = (await ForwardList(tabId)) ?? [];
    } catch (e) {
        rules = [];
        error.textContent = String(e);
    }
    if (selected >= rules.length) selected = rules.length - 1;
    paint();
}

function paintType() {
    const dynamic = type.value === 'D';
    target.hidden = dynamic;
    bindAddr.placeholder = type.value === 'R' ? 'localhost' : '127.0.0.1';
    hint.textContent = HINTS[type.value];
}

async function add() {
    error.textContent = '';
    try {
        const st = await ForwardAdd(tabId, main.Forward.createFrom({
            type: type.value,
            bindAddr: bindAddr.value.trim(),
            bindPort: Number(bindPort.value),
            host: host.value.trim(),
            port: Number(port.value),
        }));
        if (st.error) error.textContent = `저장했지만 수신하지 못했습니다: ${st.error}`;
        bindPort.value = '';
        await refresh();
        selected = rules.findIndex(r => r.id === st.id);
        paint();
        bindPort.focus();
    } catch (e) {
        error.textContent = String(e);
    }
}

async function remove() {
    const r = rules[selected];
    if (!r) return;
    error.textContent = '';
    await ForwardRemove(tabId, r.id);
    await refresh();
}

/** Opens the window for a connected SSH tab. */
export async function openForwards(id: number, req: main.ConnectRequest, close: () => void) {
    tabId = id;
    conn = req;
    onClose = close;
    title.textContent = `포트 포워딩 - ${req.host}`;
    error.textContent = '';
    selected = -1;
    paintType();
    overlay.hidden = false;
    await refresh();
    bindPort.focus();
}

export function closeForwards() {
    if (overlay.hidden) return;
    overlay.hidden = true;
    tabId = 0;
    const cb = onClose;
    onClose = undefined;
    cb?.();
}

/** The tab's connection ended: its rules are gone. */
export function forwardsTabClosed(id: number) {
    if (id === tabId) closeForwards();
}

EventsOn('fwd:changed', (id: number) => {
    if (id === tabId && !overlay.hidden) refresh();
});

type.addEventListener('change', paintType);
$<HTMLButtonElement>('pfAdd').addEventListener('click', add);
delBtn.addEventListener('click', remove);
$<HTMLButtonElement>('pfClose').addEventListener('click', closeForwards);
$<HTMLButtonElement>('pfCopy').addEventListener('click', () => {
    if (cmd.value) navigator.clipboard.writeText(cmd.value).catch(e => (error.textContent = String(e)));
});
$<HTMLButtonElement>('pfX').addEventListener('click', closeForwards);
for (const el of [bindPort, port]) {
    el.addEventListener('input', () => (el.value = el.value.replace(/\D/g, '')));
}

panel.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.preventDefault();
        closeForwards();
    } else if (ev.key === 'Enter' && ev.target instanceof HTMLInputElement) {
        ev.preventDefault();
        add();
    } else if (ev.key === 'Delete' && !(ev.target instanceof HTMLInputElement)) {
        ev.preventDefault();
        remove();
    }
    ev.stopPropagation();
});
