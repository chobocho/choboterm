import {FileCancel, FileDownload, FileList, FileOpen, FileUpload} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {EventsOn} from '../wailsjs/runtime/runtime';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('filesOverlay');
const panel = $<HTMLDivElement>('files');
const title = $<HTMLSpanElement>('filesTitle');
const pathInput = $<HTMLInputElement>('fPath');
const body = $<HTMLTableSectionElement>('fBody');
const error = $<HTMLDivElement>('fError');
const upBtn = $<HTMLButtonElement>('fUp');
const refreshBtn = $<HTMLButtonElement>('fRefresh');
const uploadBtn = $<HTMLButtonElement>('fUpload');
const downloadBtn = $<HTMLButtonElement>('fDownload');

// One file window per tab. Only the active tab's window is shown; the others
// keep their folder, selection and running transfer in the background.
interface View {
    tabId: number;
    heading: string;
    cwd: string;
    entries: main.FileEntry[];
    selected: number;
    busy: boolean;
    status: string;
    statusOk: boolean;
    onClose?: () => void;
}

const views = new Map<number, View>();
let current: View | undefined;
// Returns the active tab id (set by main.ts), so late results land in the right place.
let activeTab: () => number = () => 0;

export function setActiveTabProvider(fn: () => number) {
    activeTab = fn;
}

/** True while a file window is visible (for the active tab). */
export function filesOpen() {
    return !!current;
}

export function hasFiles(tabId: number) {
    return views.has(tabId);
}

export function formatSize(n: number): string {
    if (n < 1024) return `${n} B`;
    const units = ['KB', 'MB', 'GB', 'TB'];
    let v = n / 1024;
    let i = 0;
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v.toFixed(v < 10 ? 1 : 0)} ${units[i]}`;
}

function joinPath(dir: string, name: string) {
    return dir.endsWith('/') ? dir + name : `${dir}/${name}`;
}

function parentPath(dir: string) {
    const trimmed = dir.replace(/\/+$/, '');
    const i = trimmed.lastIndexOf('/');
    return i <= 0 ? '/' : trimmed.slice(0, i);
}

// ---- rendering (only for the visible view) ----

function paintBusy(v: View) {
    if (v !== current) return;
    for (const b of [upBtn, refreshBtn, uploadBtn, downloadBtn]) b.disabled = v.busy;
}

function paintStatus(v: View) {
    if (v !== current) return;
    error.textContent = v.status;
    error.classList.toggle('info', v.statusOk);
}

function paintSelection(v: View) {
    if (v !== current) return;
    Array.from(body.rows).forEach((row, j) => row.classList.toggle('sel', j === v.selected));
    body.rows[v.selected]?.scrollIntoView({block: 'nearest'});
}

function paintList(v: View) {
    if (v !== current) return;
    pathInput.value = v.cwd;
    body.innerHTML = '';
    v.entries.forEach((e, i) => {
        const tr = body.insertRow();
        if (e.isDir) tr.className = 'dir';
        tr.insertCell().textContent = e.name;
        const size = tr.insertCell();
        size.className = 'num';
        size.textContent = e.isDir ? '' : formatSize(e.size);
        tr.insertCell().textContent = e.modTime;
        tr.addEventListener('mousedown', () => select(v, i));
        tr.addEventListener('dblclick', () => enter(v, i));
    });
    paintSelection(v);
}

function show(v: View) {
    current = v;
    title.textContent = v.heading;
    overlay.hidden = false;
    paintList(v);
    paintBusy(v);
    paintStatus(v);
    placeXfer();
}

function hide() {
    current = undefined;
    overlay.hidden = true;
    placeXfer();
}

// ---- actions ----

function setBusy(v: View, on: boolean) {
    v.busy = on;
    paintBusy(v);
}

function setStatus(v: View, msg: string, ok = false) {
    v.status = msg;
    v.statusOk = ok;
    paintStatus(v);
}

function select(v: View, i: number) {
    v.selected = Math.max(-1, Math.min(i, v.entries.length - 1));
    paintSelection(v);
}

async function load(v: View, dir: string) {
    setStatus(v, '');
    setBusy(v, true);
    try {
        v.entries = (await FileList(v.tabId, dir)) ?? [];
        v.cwd = dir;
        v.selected = v.entries.length > 0 ? 0 : -1;
        paintList(v);
    } catch (e) {
        setStatus(v, String(e));
        if (v === current) pathInput.value = v.cwd;
    } finally {
        setBusy(v, false);
    }
}

async function enter(v: View, i: number) {
    const e = v.entries[i];
    if (!e || v.busy) return;
    if (e.isDir) await load(v, joinPath(v.cwd, e.name));
    else await download(v);
}

async function download(v: View) {
    const e = v.entries[v.selected];
    if (!e || e.isDir || v.busy) return;
    setStatus(v, '');
    setBusy(v, true);
    try {
        const local = await FileDownload(v.tabId, joinPath(v.cwd, e.name), e.size);
        if (local) setStatus(v, `저장했습니다: ${local}`, true);
    } catch (err) {
        setStatus(v, String(err));
    } finally {
        setBusy(v, false);
        if (v === current) panel.focus();
    }
}

async function upload(v: View) {
    if (v.busy) return;
    setStatus(v, '');
    setBusy(v, true);
    try {
        const n = await FileUpload(v.tabId, v.cwd);
        if (n > 0) {
            setBusy(v, false);
            await load(v, v.cwd);
            setStatus(v, `${n}개 파일을 업로드했습니다`, true);
        }
    } catch (err) {
        setStatus(v, String(err));
    } finally {
        setBusy(v, false);
        if (v === current) panel.focus();
    }
}

/**
 * Opens (or shows again) the file transfer window for a tab's connection.
 * Throws if the connection doesn't support file access (e.g. Telnet).
 */
export async function openFiles(tabId: number, host: string, opts: {onClose?: () => void} = {}) {
    const existing = views.get(tabId);
    if (existing) {
        if (activeTab() === tabId) show(existing);
        return;
    }
    const res = await FileOpen(tabId);
    const v: View = {
        tabId,
        heading: `파일 전송 (${res.protocol}) - ${host}`,
        cwd: res.home || '/',
        entries: [],
        selected: -1,
        busy: false,
        status: '',
        statusOk: false,
        onClose: opts.onClose,
    };
    views.set(tabId, v);
    if (activeTab() === tabId) {
        show(v);
        panel.focus();
    }
    await load(v, v.cwd);
}

/** Shows the file window of tabId if it has one, otherwise hides the window. */
export function showFilesFor(tabId: number) {
    const v = views.get(tabId);
    if (v) show(v);
    else hide();
}

export function focusFiles() {
    if (current) panel.focus();
}

/** Closes a tab's file window (the visible one by default), cancelling its transfer. */
export function closeFiles(tabId?: number) {
    const v = tabId === undefined ? current : views.get(tabId);
    if (!v) return;
    if (v.busy) FileCancel(v.tabId);
    views.delete(v.tabId);
    if (v === current) hide();
    v.onClose?.();
}

/** Drops a tab's file window without callbacks (the tab itself is closing). */
export function forgetFiles(tabId: number) {
    const v = views.get(tabId);
    if (!v) return;
    views.delete(tabId);
    if (v === current) hide();
}

upBtn.addEventListener('click', () => current && load(current, parentPath(current.cwd)));
refreshBtn.addEventListener('click', () => current && load(current, current.cwd));
uploadBtn.addEventListener('click', () => current && upload(current));
downloadBtn.addEventListener('click', () => current && download(current));
$<HTMLButtonElement>('fClose').addEventListener('click', () => closeFiles());
$<HTMLButtonElement>('filesX').addEventListener('click', () => closeFiles());

pathInput.addEventListener('keydown', ev => {
    if (ev.key === 'Enter' && current) {
        ev.preventDefault();
        load(current, pathInput.value.trim() || '/');
        panel.focus();
    }
    ev.stopPropagation();
});

panel.addEventListener('keydown', ev => {
    const v = current;
    if (!v) return;
    switch (ev.key) {
        case 'Escape':
            closeFiles();
            break;
        case 'ArrowDown':
            select(v, v.selected + 1);
            break;
        case 'ArrowUp':
            select(v, v.selected - 1);
            break;
        case 'Home':
            select(v, 0);
            break;
        case 'End':
            select(v, v.entries.length - 1);
            break;
        case 'Enter':
            enter(v, v.selected);
            break;
        case 'Backspace':
            if (!v.busy) load(v, parentPath(v.cwd));
            break;
        case 'F5':
            if (!v.busy) load(v, v.cwd);
            break;
        default:
            return;
    }
    ev.preventDefault();
});

// ---- Progress box (shared by SFTP/SCP/FTP and Zmodem) ----

const xfer = $<HTMLDivElement>('xfer');
const xferName = $<HTMLDivElement>('xferName');
const xferBar = $<HTMLDivElement>('xferBar');
const xferText = $<HTMLSpanElement>('xferText');
let started = 0;
let xferTab = 0;

// Mirrors the Go XferProgress event payload (events aren't part of the generated bindings).
interface XferProgress {
    name: string;
    done: number;
    total: number;
    upload: boolean;
}

// Inside the visible file window when the transfer belongs to it, otherwise floating.
function placeXfer() {
    const slot = current && current.tabId === xferTab ? $<HTMLDivElement>('xferSlot') : document.body;
    if (xfer.parentElement !== slot) slot.appendChild(xfer);
    xfer.classList.toggle('inline', slot !== document.body);
}

EventsOn('xfer:progress', (id: number, p: XferProgress) => {
    if (xfer.hidden || p.done === 0 || id !== xferTab) started = Date.now();
    xferTab = id;
    placeXfer();
    xfer.hidden = false;
    xferName.textContent = `${p.upload ? '⬆' : '⬇'} ${p.name}`;
    const pct = p.total > 0 ? Math.min(100, (p.done / p.total) * 100) : 0;
    xferBar.style.width = `${pct}%`;
    const secs = (Date.now() - started) / 1000;
    const rate = secs > 0.5 ? ` · ${formatSize(p.done / secs)}/s` : '';
    xferText.textContent = p.total > 0
        ? `${formatSize(p.done)} / ${formatSize(p.total)} (${pct.toFixed(0)}%)${rate}`
        : `${formatSize(p.done)}${rate}`;
});

EventsOn('xfer:end', (id: number) => {
    if (id === xferTab) xfer.hidden = true;
});

$<HTMLButtonElement>('xferCancel').addEventListener('click', () => FileCancel(xferTab));
