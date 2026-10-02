import {FileCancel, FileClose, FileDownload, FileList, FileOpen, FileUpload} from '../wailsjs/go/main/App';
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

let cwd = '/';
let entries: main.FileEntry[] = [];
let selected = -1;
let busy = false;
let onClosed: (() => void) | undefined;
// When true (FTP), closing the window also drops the connection.
let closeConnection = false;

export function filesOpen() {
    return !overlay.hidden;
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

function setBusy(on: boolean) {
    busy = on;
    for (const b of [upBtn, refreshBtn, uploadBtn, downloadBtn]) b.disabled = on;
}

function select(i: number) {
    selected = Math.max(-1, Math.min(i, entries.length - 1));
    Array.from(body.rows).forEach((row, j) => row.classList.toggle('sel', j === selected));
    body.rows[selected]?.scrollIntoView({block: 'nearest'});
}

function setStatus(msg: string, ok = false) {
    error.textContent = msg;
    error.classList.toggle('info', ok);
}

async function load(dir: string) {
    setStatus('');
    setBusy(true);
    try {
        entries = (await FileList(dir)) ?? [];
        cwd = dir;
        pathInput.value = dir;
        render();
    } catch (e) {
        setStatus(String(e));
        pathInput.value = cwd;
    } finally {
        setBusy(false);
    }
}

function render() {
    body.innerHTML = '';
    entries.forEach((e, i) => {
        const tr = body.insertRow();
        if (e.isDir) tr.className = 'dir';
        tr.insertCell().textContent = e.name;
        const size = tr.insertCell();
        size.className = 'num';
        size.textContent = e.isDir ? '' : formatSize(e.size);
        tr.insertCell().textContent = e.modTime;
        tr.addEventListener('mousedown', () => select(i));
        tr.addEventListener('dblclick', () => activate(i));
    });
    select(entries.length > 0 ? 0 : -1);
}

async function activate(i: number) {
    const e = entries[i];
    if (!e || busy) return;
    if (e.isDir) await load(joinPath(cwd, e.name));
    else await download();
}

async function download() {
    const e = entries[selected];
    if (!e || e.isDir || busy) return;
    setStatus('');
    setBusy(true);
    try {
        const local = await FileDownload(joinPath(cwd, e.name), e.size);
        if (local) setStatus(`저장했습니다: ${local}`, true);
    } catch (err) {
        setStatus(String(err));
    } finally {
        setBusy(false);
        panel.focus();
    }
}

async function upload() {
    if (busy) return;
    setStatus('');
    setBusy(true);
    try {
        const n = await FileUpload(cwd);
        if (n > 0) {
            setBusy(false);
            await load(cwd);
            setStatus(`${n}개 파일을 업로드했습니다`, true);
        }
    } catch (err) {
        setStatus(String(err));
    } finally {
        setBusy(false);
        panel.focus();
    }
}

/**
 * Opens the file transfer window for the current connection.
 * Throws if the connection doesn't support file access (e.g. Telnet).
 */
export async function openFiles(heading: string, opts: {closeConnection?: boolean; onClose?: () => void} = {}) {
    const home = await FileOpen();
    closeConnection = !!opts.closeConnection;
    onClosed = opts.onClose;
    title.textContent = heading;
    overlay.hidden = false;
    panel.focus();
    await load(home || '/');
}

export function closeFiles() {
    if (overlay.hidden) return;
    if (busy) FileCancel();
    overlay.hidden = true;
    if (closeConnection) FileClose();
    onClosed?.();
}

upBtn.addEventListener('click', () => load(parentPath(cwd)));
refreshBtn.addEventListener('click', () => load(cwd));
uploadBtn.addEventListener('click', upload);
downloadBtn.addEventListener('click', download);
$<HTMLButtonElement>('fClose').addEventListener('click', closeFiles);
$<HTMLButtonElement>('filesX').addEventListener('click', closeFiles);

pathInput.addEventListener('keydown', ev => {
    if (ev.key === 'Enter') {
        ev.preventDefault();
        load(pathInput.value.trim() || '/');
        panel.focus();
    }
    ev.stopPropagation();
});

panel.addEventListener('keydown', ev => {
    switch (ev.key) {
        case 'Escape':
            closeFiles();
            break;
        case 'ArrowDown':
            select(selected + 1);
            break;
        case 'ArrowUp':
            select(selected - 1);
            break;
        case 'Home':
            select(0);
            break;
        case 'End':
            select(entries.length - 1);
            break;
        case 'Enter':
            activate(selected);
            break;
        case 'Backspace':
            if (!busy) load(parentPath(cwd));
            break;
        case 'F5':
            if (!busy) load(cwd);
            break;
        default:
            return;
    }
    ev.preventDefault();
});

// ---- Progress box (shared by SFTP/FTP and Zmodem) ----

const xfer = $<HTMLDivElement>('xfer');
const xferName = $<HTMLDivElement>('xferName');
const xferBar = $<HTMLDivElement>('xferBar');
const xferText = $<HTMLSpanElement>('xferText');
let started = 0;

// Mirrors the Go XferProgress event payload (events aren't part of the generated bindings).
interface XferProgress {
    name: string;
    done: number;
    total: number;
    upload: boolean;
}

EventsOn('xfer:progress', (p: XferProgress) => {
    if (xfer.hidden || p.done === 0) started = Date.now();
    // Show progress inside the file window when it's open, otherwise as a floating box.
    const slot = filesOpen() ? $<HTMLDivElement>('xferSlot') : document.body;
    if (xfer.parentElement !== slot) slot.appendChild(xfer);
    xfer.classList.toggle('inline', slot !== document.body);
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

EventsOn('xfer:end', () => {
    xfer.hidden = true;
});

$<HTMLButtonElement>('xferCancel').addEventListener('click', () => FileCancel());
