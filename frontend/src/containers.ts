// Docker container window (Ctrl+Shift+J): the containers of the active tab's
// SSH server or of this PC (Docker Desktop). A shell or a log opens in a new
// tab; on a server it is an extra session on that tab's connection.

import {DockerAction, DockerList, ForwardAdd, ForwardList, ForwardRemove} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {BrowserOpenURL} from '../wailsjs/runtime/runtime';
import {ask} from './dialog';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('dkOverlay');
const panel = $<HTMLDivElement>('dk');
const source = $<HTMLSelectElement>('dkSrc');
const all = $<HTMLInputElement>('dkAll');
const body = $<HTMLTableSectionElement>('dkBody');
const portSel = $<HTMLSelectElement>('dkPort');
const hint = $<HTMLDivElement>('dkHint');
const error = $<HTMLDivElement>('dkError');
const btn = (id: string) => $<HTMLButtonElement>(id);
const shellBtn = btn('dkShell');
const logsBtn = btn('dkLogs');
const startBtn = btn('dkStart');
const stopBtn = btn('dkStop');
const restartBtn = btn('dkRestart');
const portBtn = btn('dkOpenPort');

/** Where containers are: an SSH tab's server, or this PC (tab 0). */
export interface DockerSource {
    tab: number;
    label: string;
    // The server (as in the tab's connection), to find another tab to it.
    host?: string;
    port?: number;
}

export type DockerMode = 'shell' | 'logs';

let sources: DockerSource[] = [];
let list: main.Container[] = [];
let selected = -1;
let loading = 0; // the latest DockerList call; older answers are dropped
let onOpen: ((src: DockerSource, c: main.Container, mode: DockerMode) => void) | undefined;
let onClose: (() => void) | undefined;

export function containersOpen() {
    return !overlay.hidden;
}

function current(): DockerSource {
    return sources[source.selectedIndex] ?? {tab: 0, label: '이 PC'};
}

function paint() {
    body.replaceChildren();
    list.forEach((c, i) => {
        const tr = body.insertRow();
        if (i === selected) tr.className = 'sel';
        tr.title = `${c.id}\n${c.image}\n만든 지: ${c.created}`;
        tr.insertCell().textContent = c.name;
        tr.insertCell().textContent = c.image;
        const st = tr.insertCell();
        st.textContent = c.status;
        st.className = c.running ? 'ok' : 'off';
        const ports = tr.insertCell();
        ports.textContent = c.ports;
        ports.title = c.ports;
        tr.addEventListener('mousedown', () => {
            selected = i;
            paint();
        });
        tr.addEventListener('dblclick', () => open('shell'));
    });
    if (list.length === 0 && !error.textContent) {
        const td = body.insertRow().insertCell();
        td.colSpan = 4;
        td.className = 'empty';
        td.textContent = all.checked ? '컨테이너가 없습니다' : '실행 중인 컨테이너가 없습니다';
    }
    const c = list[selected];
    shellBtn.disabled = !c?.running;
    logsBtn.disabled = !c;
    startBtn.disabled = !c || c.running;
    stopBtn.disabled = !c?.running;
    restartBtn.disabled = !c;
    portSel.replaceChildren();
    for (const p of c?.published ?? []) {
        const o = document.createElement('option');
        o.value = String(p.port);
        o.textContent = `${p.port} → ${p.to}`;
        portSel.appendChild(o);
    }
    portSel.disabled = portBtn.disabled = !c?.running || portSel.options.length === 0;
}

async function refresh() {
    const id = ++loading;
    const keep = list[selected]?.id;
    error.textContent = '';
    body.replaceChildren();
    const td = body.insertRow().insertCell();
    td.colSpan = 4;
    td.className = 'empty';
    td.textContent = '불러오는 중...';
    let got: main.Container[];
    try {
        got = (await DockerList(current().tab, all.checked)) ?? [];
    } catch (e) {
        got = [];
        if (id === loading) error.textContent = String(e);
    }
    if (id !== loading || overlay.hidden) return;
    list = got;
    selected = list.findIndex(c => c.id === keep);
    if (selected < 0 && list.length > 0) selected = 0;
    paint();
}

function paintHint() {
    hint.textContent = current().tab
        ? '셸 · 로그는 이 접속을 함께 쓰는 새 탭으로 엽니다(접속을 끊으면 함께 닫힘). 포트 열기: 서버의 포트를 이 PC의 같은 포트로 포워딩(-L)하고 브라우저로 엽니다.'
        : '이 PC의 Docker Desktop(또는 podman)입니다. 포트 열기: 브라우저로 http://localhost:포트 를 엽니다.';
}

function open(mode: DockerMode) {
    const c = list[selected];
    if (!c || (mode === 'shell' && !c.running)) return;
    const src = current();
    const cb = onOpen;
    closeContainers(false);
    cb?.(src, c, mode);
}

async function act(action: 'start' | 'stop' | 'restart') {
    const c = list[selected];
    if (!c) return;
    if (action !== 'start') {
        const ok = action === 'stop'
            ? await ask('컨테이너 중지', `"${c.name}" 컨테이너를 중지할까요?`, '중지')
            : await ask('컨테이너 재시작', `"${c.name}" 컨테이너를 재시작할까요?`, '재시작');
        panel.focus();
        if (!ok) return;
    }
    error.textContent = '';
    for (const b of [startBtn, stopBtn, restartBtn]) b.disabled = true;
    try {
        await DockerAction(current().tab, c.id, action);
    } catch (e) {
        error.textContent = String(e);
        paint();
        return;
    }
    await refresh();
}

/** Opens the chosen published port: through a local forward on a server. */
async function openPort() {
    const c = list[selected];
    const p = c?.published.find(p => String(p.port) === portSel.value);
    if (!p) return;
    error.textContent = '';
    const src = current();
    if (src.tab) {
        try {
            const rules = (await ForwardList(src.tab)) ?? [];
            const same = rules.find(r => r.type !== 'R' && r.bindPort === p.port);
            if (same && (same.type !== 'L' || same.port !== p.port || same.error)) {
                error.textContent = `이 PC의 포트 ${p.port}는 이미 다른 포워딩 규칙이 씁니다. 포트 포워딩 창(Ctrl+Shift+P)에서 확인하세요.`;
                return;
            }
            if (!same) {
                const host = p.ip === '0.0.0.0' ? '127.0.0.1' : p.ip;
                const st = await ForwardAdd(src.tab, main.Forward.createFrom({type: 'L', bindAddr: '', bindPort: p.port, host, port: p.port}));
                if (st.error) {
                    await ForwardRemove(src.tab, st.id);
                    error.textContent = `이 PC의 포트 ${p.port}를 열 수 없습니다: ${st.error}. 포트 포워딩 창(Ctrl+Shift+P)에서 다른 포트로 추가하세요.`;
                    return;
                }
            }
        } catch (e) {
            error.textContent = String(e);
            return;
        }
    }
    BrowserOpenURL(`http://localhost:${p.port}/`);
}

/**
 * Opens the window. srcs: the active SSH tab's server first if there is one;
 * "this PC" is added at the end. open makes the tab for a shell or a log.
 */
export async function openContainers(srcs: DockerSource[], openTab: typeof onOpen, close: () => void) {
    fillSources([...srcs, {tab: 0, label: '이 PC (Docker Desktop)'}]);
    onOpen = openTab;
    onClose = close;
    list = [];
    selected = -1;
    error.textContent = '';
    overlay.hidden = false;
    panel.focus();
    await refresh();
}

function fillSources(srcs: DockerSource[]) {
    sources = srcs;
    source.replaceChildren(...sources.map(s => {
        const o = document.createElement('option');
        o.textContent = s.tab ? `서버: ${s.label}` : s.label;
        return o;
    }));
    source.selectedIndex = 0;
    paintHint();
}

export function closeContainers(focusBack = true) {
    if (overlay.hidden) return;
    overlay.hidden = true;
    loading++;
    const cb = onClose;
    onClose = onOpen = undefined;
    if (focusBack) cb?.();
}

/** A tab whose server is listed ended: list this PC instead. */
export function containersTabClosed(id: number) {
    if (overlay.hidden || !sources.some(s => s.tab === id)) return;
    fillSources(sources.filter(s => s.tab !== id));
    refresh();
}

source.addEventListener('change', () => {
    paintHint();
    refresh();
});
all.addEventListener('change', refresh);
btn('dkRefresh').addEventListener('click', refresh);
shellBtn.addEventListener('click', () => open('shell'));
logsBtn.addEventListener('click', () => open('logs'));
startBtn.addEventListener('click', () => act('start'));
stopBtn.addEventListener('click', () => act('stop'));
restartBtn.addEventListener('click', () => act('restart'));
portBtn.addEventListener('click', openPort);
btn('dkClose').addEventListener('click', () => closeContainers());
btn('dkX').addEventListener('click', () => closeContainers());

panel.addEventListener('keydown', ev => {
    const inField = ev.target instanceof HTMLSelectElement || ev.target instanceof HTMLButtonElement;
    if (ev.key === 'Escape') {
        ev.preventDefault();
        closeContainers();
    } else if (ev.key === 'F5') {
        ev.preventDefault();
        refresh();
    } else if (ev.key === 'Enter' && !inField) {
        ev.preventDefault();
        open('shell');
    } else if ((ev.key === 'ArrowDown' || ev.key === 'ArrowUp') && !(ev.target instanceof HTMLSelectElement) && list.length) {
        ev.preventDefault();
        selected = Math.max(0, Math.min(list.length - 1, selected + (ev.key === 'ArrowDown' ? 1 : -1)));
        paint();
        body.rows[selected]?.scrollIntoView({block: 'nearest'});
    }
    ev.stopPropagation();
});
