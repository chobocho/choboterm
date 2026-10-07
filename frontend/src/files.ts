import {
    FileCancel,
    FileDelete,
    FileDownload,
    FileDownloadMany,
    FileList,
    FileMkdir,
    FileOpen,
    FileRename,
    FileUpload,
    FileUploadPaths,
    FileView,
} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {EventsOn, OnFileDrop} from '../wailsjs/runtime/runtime';
import {ask, askText} from './dialog';
import {openViewer, viewerOpen} from './viewer';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('filesOverlay');
const panel = $<HTMLDivElement>('files');
const title = $<HTMLSpanElement>('filesTitle');
const pathInput = $<HTMLInputElement>('fPath');
const listEl = $<HTMLDivElement>('fList');
const body = $<HTMLTableSectionElement>('fBody');
const error = $<HTMLDivElement>('fError');
const upBtn = $<HTMLButtonElement>('fUp');
const refreshBtn = $<HTMLButtonElement>('fRefresh');
const uploadBtn = $<HTMLButtonElement>('fUpload');
const downloadBtn = $<HTMLButtonElement>('fDownload');
const viewBtn = $<HTMLButtonElement>('fView');
const mkdirBtn = $<HTMLButtonElement>('fMkdir');
const renameBtn = $<HTMLButtonElement>('fRename');
const deleteBtn = $<HTMLButtonElement>('fDelete');
const menu = $<HTMLUListElement>('fMenu');

// One file window per tab. Only the active tab's window is shown; the others
// keep their folder, selection and running transfer in the background.
interface View {
    tabId: number;
    heading: string;
    cwd: string;
    entries: main.FileEntry[];
    selected: number; // the row with the keyboard focus
    picked: Set<number>; // selected rows (Ctrl/Shift+click)
    anchor: number; // where a Shift selection starts
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

/** The selected entries, in list order (the focused row if nothing is picked). */
function pickedEntries(v: View): main.FileEntry[] {
    const idx = v.picked.size > 0 ? [...v.picked].sort((a, b) => a - b) : [v.selected];
    return idx.map(i => v.entries[i]).filter((e): e is main.FileEntry => !!e);
}

// ---- rendering (only for the visible view) ----

function paintButtons(v: View) {
    if (v !== current) return;
    const n = pickedEntries(v).length;
    for (const b of [upBtn, refreshBtn, uploadBtn, mkdirBtn]) b.disabled = v.busy;
    downloadBtn.disabled = deleteBtn.disabled = v.busy || n === 0;
    renameBtn.disabled = v.busy || n !== 1;
    viewBtn.disabled = v.busy || !viewable(v);
}

function paintStatus(v: View) {
    if (v !== current) return;
    error.textContent = v.status;
    error.classList.toggle('info', v.statusOk);
}

function paintSelection(v: View) {
    if (v !== current) return;
    Array.from(body.rows).forEach((row, j) => {
        row.classList.toggle('sel', v.picked.has(j));
        row.classList.toggle('cur', j === v.selected);
    });
    body.rows[v.selected]?.scrollIntoView({block: 'nearest'});
    paintButtons(v);
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
        tr.addEventListener('mousedown', ev => {
            if (ev.button === 2 && v.picked.has(i)) return; // right-click keeps a multi-selection
            select(v, i, ev.shiftKey ? 'range' : ev.ctrlKey ? 'toggle' : 'one');
        });
        tr.addEventListener('dblclick', ev => {
            if (!ev.ctrlKey && !ev.shiftKey) enter(v, i);
        });
    });
    paintSelection(v);
}

function show(v: View) {
    current = v;
    title.textContent = v.heading;
    overlay.hidden = false;
    paintList(v);
    paintStatus(v);
    placeXfer();
}

function hide() {
    current = undefined;
    overlay.hidden = true;
    hideMenu();
    placeXfer();
}

// ---- actions ----

function setBusy(v: View, on: boolean) {
    v.busy = on;
    paintButtons(v);
}

function setStatus(v: View, msg: string, ok = false) {
    v.status = msg;
    v.statusOk = ok;
    paintStatus(v);
}

/** Moves the focus to row i; 'one' selects only it, 'toggle' adds/removes it, 'range' selects from the anchor. */
function select(v: View, i: number, mode: 'one' | 'toggle' | 'range' = 'one') {
    i = Math.max(-1, Math.min(i, v.entries.length - 1));
    v.selected = i;
    if (i < 0) {
        v.picked.clear();
    } else if (mode === 'toggle') {
        if (v.picked.has(i)) v.picked.delete(i);
        else v.picked.add(i);
        v.anchor = i;
    } else if (mode === 'range' && v.anchor >= 0) {
        v.picked.clear();
        const [a, b] = v.anchor < i ? [v.anchor, i] : [i, v.anchor];
        for (let j = a; j <= b; j++) v.picked.add(j);
    } else {
        v.picked = new Set([i]);
        v.anchor = i;
    }
    paintSelection(v);
}

function selectAll(v: View) {
    v.picked = new Set(v.entries.map((_, i) => i));
    paintSelection(v);
}

/** Lists dir; names (if given) are selected afterwards, otherwise the first row. */
async function load(v: View, dir: string, names?: string[]) {
    setStatus(v, '');
    setBusy(v, true);
    try {
        v.entries = (await FileList(v.tabId, dir)) ?? [];
        v.cwd = dir;
        v.picked = new Set();
        v.selected = v.anchor = v.entries.length > 0 ? 0 : -1;
        if (names?.length) {
            v.entries.forEach((e, i) => names.includes(e.name) && v.picked.add(i));
            const first = Math.min(...v.picked);
            if (Number.isFinite(first)) v.selected = v.anchor = first;
        } else if (v.selected >= 0) {
            v.picked.add(v.selected);
        }
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

/** Runs a remote operation with the window busy; errors go to the status line. */
async function run(v: View, op: () => Promise<void>) {
    if (v.busy) return;
    setStatus(v, '');
    setBusy(v, true);
    try {
        await op();
    } catch (err) {
        setStatus(v, String(err));
    } finally {
        setBusy(v, false);
        if (v === current && !viewerOpen()) panel.focus();
    }
}

function download(v: View) {
    const list = pickedEntries(v);
    if (list.length === 0) return;
    return run(v, async () => {
        const e = list[0];
        if (list.length === 1 && !e.isDir) {
            const local = await FileDownload(v.tabId, joinPath(v.cwd, e.name), e.size);
            if (local) setStatus(v, `저장했습니다: ${local}`, true);
            return;
        }
        // Several entries or a folder: pick a folder to save into.
        const res = await FileDownloadMany(v.tabId, v.cwd, list);
        if (res.dir) setStatus(v, `${res.count}개 파일을 저장했습니다: ${res.dir}`, true);
    });
}

/** The one selected entry if it is a file the viewer can open. */
function viewable(v: View): main.FileEntry | undefined {
    const list = pickedEntries(v);
    return list.length === 1 && !list[0].isDir ? list[0] : undefined;
}

/** Opens the selected file in the text viewer. */
function viewFile(v: View) {
    const e = viewable(v);
    if (!e) return;
    const path = joinPath(v.cwd, e.name);
    return run(v, async () => {
        const res = await FileView(v.tabId, path, e.size);
        const data = Uint8Array.from(atob(res.data), c => c.charCodeAt(0));
        openViewer({
            name: e.name,
            path,
            size: Math.max(e.size, data.length),
            data,
            truncated: res.truncated,
            onClose: () => v === current && panel.focus(),
        });
    });
}

async function uploaded(v: View, n: number, names: string[]) {
    if (n === 0) return;
    setBusy(v, false);
    await load(v, v.cwd, names);
    setStatus(v, `${n}개 파일을 업로드했습니다`, true);
}

function upload(v: View) {
    return run(v, async () => uploaded(v, await FileUpload(v.tabId, v.cwd), []));
}

/** Uploads files and folders dropped on the window (paths come from Wails). */
function uploadPaths(v: View, paths: string[]) {
    const names = paths.map(p => p.split(/[\\/]/).pop() ?? p);
    return run(v, async () => uploaded(v, await FileUploadPaths(v.tabId, v.cwd, paths), names));
}

async function remove(v: View) {
    const list = pickedEntries(v);
    if (list.length === 0 || v.busy) return;
    const dirs = list.filter(e => e.isDir).length;
    const what = list.length === 1 ? `"${list[0].name}"` : `${list.length}개 항목`;
    const msg = `${what}을(를) 서버에서 삭제할까요? 되돌릴 수 없습니다.` +
        (dirs > 0 ? '\n폴더는 안에 있는 파일과 폴더까지 모두 삭제됩니다.' : '');
    if (!await ask('삭제', msg, '삭제')) return panel.focus();
    // Afterwards select the row after the deleted ones (or before them at the end).
    const gone = new Set(list);
    const last = v.entries.indexOf(list[list.length - 1]);
    const keep = v.entries.slice(last + 1).find(e => !gone.has(e)) ??
        v.entries.slice(0, last).reverse().find(e => !gone.has(e));
    await run(v, async () => {
        let n = 0;
        try {
            n = await FileDelete(v.tabId, v.cwd, list);
        } finally {
            setBusy(v, false);
            await load(v, v.cwd, keep ? [keep.name] : undefined);
        }
        setStatus(v, `${n}개 항목을 삭제했습니다`, true);
    });
}

async function rename(v: View) {
    const list = pickedEntries(v);
    if (list.length !== 1 || v.busy) return;
    const old = list[0].name;
    const name = await askText('이름 바꾸기', `"${old}"의 새 이름:`, old);
    if (!name) return panel.focus();
    await run(v, async () => {
        await FileRename(v.tabId, v.cwd, old, name);
        setBusy(v, false);
        await load(v, v.cwd, [name]);
    });
}

async function mkdir(v: View) {
    if (v.busy) return;
    const name = await askText('새 폴더', `${v.cwd} 안에 만들 폴더 이름:`, '');
    if (!name) return panel.focus();
    await run(v, async () => {
        await FileMkdir(v.tabId, v.cwd, name);
        setBusy(v, false);
        await load(v, v.cwd, [name]);
    });
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
        picked: new Set(),
        anchor: -1,
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
viewBtn.addEventListener('click', () => current && viewFile(current));
mkdirBtn.addEventListener('click', () => current && mkdir(current));
renameBtn.addEventListener('click', () => current && rename(current));
deleteBtn.addEventListener('click', () => current && remove(current));
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
    if (!v || ev.target === pathInput) return;
    const mode = ev.shiftKey ? 'range' : 'one';
    switch (ev.key) {
        case 'Escape':
            if (!menu.hidden) hideMenu();
            else closeFiles();
            break;
        case 'ArrowDown':
            select(v, v.selected + 1, mode);
            break;
        case 'ArrowUp':
            select(v, Math.max(0, v.selected - 1), mode);
            break;
        case 'Home':
            select(v, 0, mode);
            break;
        case 'End':
            select(v, v.entries.length - 1, mode);
            break;
        case 'Enter':
            if (v.picked.size > 1) download(v);
            else enter(v, v.selected);
            break;
        case 'Backspace':
            if (!v.busy) load(v, parentPath(v.cwd));
            break;
        case 'F5':
            if (!v.busy) load(v, v.cwd);
            break;
        case 'Delete':
            remove(v);
            break;
        case 'F2':
            rename(v);
            break;
        case 'F3':
            viewFile(v);
            break;
        case 'F7':
            mkdir(v);
            break;
        case 'a':
        case 'A':
            if (!ev.ctrlKey) return;
            selectAll(v);
            break;
        default:
            return;
    }
    ev.preventDefault();
});

// ---- Right-click menu ----

function showMenu(x: number, y: number) {
    const v = current;
    if (!v) return;
    const list = pickedEntries(v);
    const enable = (act: string, on: boolean) =>
        menu.querySelector(`[data-act="${act}"]`)!.classList.toggle('disabled', !on || v.busy);
    const one = list.length === 1 ? list[0] : undefined;
    menu.querySelector('[data-act="open"]')!.textContent = one?.isDir ? '열기' : '다운로드';
    enable('open', list.length > 0);
    enable('rename', !!one);
    enable('view', !!viewable(v));
    enable('delete', list.length > 0);
    enable('mkdir', true);
    enable('upload', true);
    enable('refresh', true);
    menu.hidden = false;
    const r = menu.getBoundingClientRect();
    menu.style.left = `${Math.min(x, window.innerWidth - r.width - 4)}px`;
    menu.style.top = `${Math.min(y, window.innerHeight - r.height - 4)}px`;
}

function hideMenu() {
    menu.hidden = true;
}

listEl.addEventListener('contextmenu', ev => {
    ev.preventDefault();
    showMenu(ev.clientX, ev.clientY);
});
menu.addEventListener('mousedown', ev => ev.stopPropagation());
menu.addEventListener('click', ev => {
    const li = (ev.target as HTMLElement).closest('li');
    const v = current;
    if (!li || !v || li.classList.contains('disabled') || li.classList.contains('sep')) return;
    hideMenu();
    switch (li.dataset.act) {
        case 'open': {
            const one = pickedEntries(v);
            if (one.length === 1 && one[0].isDir) enter(v, v.entries.indexOf(one[0]));
            else download(v);
            break;
        }
        case 'view':
            viewFile(v);
            break;
        case 'rename':
            rename(v);
            break;
        case 'delete':
            remove(v);
            break;
        case 'mkdir':
            mkdir(v);
            break;
        case 'upload':
            upload(v);
            break;
        case 'refresh':
            load(v, v.cwd);
            break;
    }
});
window.addEventListener('mousedown', hideMenu);
window.addEventListener('blur', hideMenu);

// ---- Drag and drop from Explorer ----

// Only the file list (style --wails-drop-target: drop) accepts drops.
OnFileDrop((_x, _y, paths) => {
    const v = current;
    if (!v || paths.length === 0) return;
    if (v.busy) return setStatus(v, '전송이 끝난 뒤에 다시 끌어 놓으세요');
    uploadPaths(v, paths);
}, true);

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
