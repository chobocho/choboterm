// Saved sessions: the "Save session" window, opened from the Connect dialog
// (☆ Save, Alt+S) or the tab menu, and the session manager (Ctrl+Shift+H).

import {DeleteSession, GetSessions, SaveSession} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {ask, askOpen} from './dialog';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('sessSaveOverlay');
const panel = $<HTMLDivElement>('sessSave');
const titleEl = $<HTMLSpanElement>('sessSaveTitle');
const whereEl = $<HTMLDivElement>('sessSaveWhere');
const nameInput = $<HTMLInputElement>('sessName');
const groupInput = $<HTMLInputElement>('sessGroup');
const groupList = $<HTMLDataListElement>('sessGroups');
const passCheck = $<HTMLInputElement>('sessPass');
const errorEl = $<HTMLDivElement>('sessSaveError');

/** What to save: the connection, and the session it came from (updated in place). */
export interface SessionDraft {
    host: string;
    port: number;
    login: string;
    encoding: string;
    pass: string;
    session?: main.SavedSession;
}

let draft: SessionDraft | undefined;
let resolve: ((s: main.SavedSession | null) => void) | undefined;

export function sessionSaveOpen() {
    return !overlay.hidden;
}

/** Asks for a name and group and saves d; resolves the saved session, or null if cancelled. */
export async function openSaveSession(d: SessionDraft): Promise<main.SavedSession | null> {
    finish(null);
    draft = d;
    const s = d.session;
    titleEl.textContent = s ? '세션 수정' : '세션으로 저장';
    const user = d.login ? d.login + '@' : '';
    whereEl.textContent = d.port > 0 ? `${user}${d.host}:${d.port} · ${d.encoding || 'UTF-8'}` : `${d.host} (로컬 셸)`;
    nameInput.value = s?.name ?? d.host;
    groupInput.value = s?.group ?? '';
    passCheck.disabled = !d.pass;
    passCheck.checked = !!d.pass && !!s?.pass;
    errorEl.textContent = '';
    groupList.innerHTML = '';
    const groups = new Set(((await GetSessions()) ?? []).map(x => x.group).filter(Boolean));
    for (const g of groups) {
        const o = document.createElement('option');
        o.value = g;
        groupList.appendChild(o);
    }
    overlay.hidden = false;
    nameInput.focus();
    nameInput.select();
    return new Promise(r => (resolve = r));
}

function finish(s: main.SavedSession | null) {
    if (!resolve) return;
    overlay.hidden = true;
    const r = resolve;
    resolve = undefined;
    draft = undefined;
    r(s);
}

async function save() {
    const d = draft;
    if (!d) return;
    try {
        const saved = await SaveSession(main.SavedSession.createFrom({
            id: d.session?.id ?? '',
            name: nameInput.value.trim() || d.host,
            group: groupInput.value.trim(),
            host: d.host,
            port: d.port,
            login: d.login,
            encoding: d.encoding,
            pass: d.pass,
        }), passCheck.checked);
        finish(saved);
    } catch (e) {
        errorEl.textContent = String(e);
    }
}

$<HTMLButtonElement>('sessSaveOk').addEventListener('click', save);
$<HTMLButtonElement>('sessSaveCancel').addEventListener('click', () => finish(null));
$<HTMLButtonElement>('sessSaveX').addEventListener('click', () => finish(null));

panel.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.preventDefault();
        finish(null);
    } else if (ev.key === 'Enter' && (ev.target as HTMLElement).id !== 'sessSaveCancel') {
        ev.preventDefault();
        save();
    }
    ev.stopPropagation();
});

// ---- Session manager ----

const smOverlay = $<HTMLDivElement>('smOverlay');
const smPanel = $<HTMLDivElement>('sm');
const smFilter = $<HTMLInputElement>('smFilter');
const smList = $<HTMLUListElement>('smList');
const smForm = $<HTMLDivElement>('smForm');
const smName = $<HTMLInputElement>('smName');
const smGroup = $<HTMLInputElement>('smGroup');
const smHost = $<HTMLInputElement>('smHost');
const smPort = $<HTMLInputElement>('smPort');
const smLogin = $<HTMLInputElement>('smLogin');
const smPass = $<HTMLInputElement>('smPass');
const smSavePass = $<HTMLInputElement>('smSavePass');
const smCode = $<HTMLSelectElement>('smCode');
const smError = $<HTMLDivElement>('smError');
const smOpenBtn = $<HTMLButtonElement>('smOpen');
const smGroupOpen = $<HTMLButtonElement>('smGroupOpen');
const smDup = $<HTMLButtonElement>('smDup');
const smDel = $<HTMLButtonElement>('smDel');
const fields = [smName, smGroup, smHost, smPort, smLogin, smPass, smSavePass, smCode];

let all: main.SavedSession[] = [];
let shown: main.SavedSession[] = [];
let current: main.SavedSession | undefined; // selected; id '' = added, not saved yet
let dirty = false;
let saveTimer = 0;
let onOpen: ((list: main.SavedSession[]) => void) | undefined;
let onSmClose: (() => void) | undefined;

export function sessionManagerOpen() {
    return !smOverlay.hidden;
}

/** Opens the manager; open runs with the sessions to connect after the window closed. */
export async function openSessionManager(open: (list: main.SavedSession[]) => void, close: () => void) {
    onOpen = open;
    onSmClose = close;
    smFilter.value = '';
    await reload(current?.id);
    smOverlay.hidden = false;
    smList.focus();
}

export async function closeSessionManager() {
    if (smOverlay.hidden) return;
    await flush();
    smOverlay.hidden = true;
    const cb = onSmClose;
    onSmClose = onOpen = undefined;
    current = undefined;
    cb?.();
}

async function reload(selectId?: string) {
    all = (await GetSessions()) ?? [];
    groupList.innerHTML = '';
    for (const g of new Set(all.map(x => x.group).filter(Boolean))) {
        const o = document.createElement('option');
        o.value = g;
        groupList.appendChild(o);
    }
    paintList(selectId);
}

function paintList(selectId?: string) {
    const words = smFilter.value.toLowerCase().split(/\s+/).filter(Boolean);
    shown = all.filter(x => {
        const text = [x.name, x.host, x.group, x.login].join(' ').toLowerCase();
        return words.every(w => text.includes(w));
    });
    if (current?.id === '') shown.push(current); // a new one stays at the end until it is saved
    if (selectId !== undefined) current = shown.find(x => x.id === selectId);
    if (current && !shown.includes(current)) current = undefined;
    current ??= shown[0];
    smList.replaceChildren();
    let head: string | undefined;
    for (const x of shown) {
        const g = x.id === '' ? '새 세션' : x.group || '그룹 없음';
        if (g !== head) {
            head = g;
            const h = document.createElement('li');
            h.className = 'head';
            h.textContent = '★ ' + g;
            h.addEventListener('mousedown', ev => ev.preventDefault());
            smList.appendChild(h);
        }
        const li = document.createElement('li');
        li.textContent = x.name || x.host || '(새 세션)';
        const meta = document.createElement('span');
        meta.className = 'meta';
        meta.textContent = x.port > 0 ? `${x.login ? x.login + '@' : ''}${x.host}:${x.port}` : x.host ? '로컬 셸' : '';
        li.appendChild(meta);
        if (x === current) li.className = 'sel';
        li.addEventListener('mousedown', ev => {
            ev.preventDefault();
            select(x);
            smList.focus();
        });
        li.addEventListener('dblclick', () => openSelected());
        smList.appendChild(li);
    }
    if (shown.length === 0) {
        const li = document.createElement('li');
        li.className = 'empty';
        li.textContent = all.length === 0 ?
            '저장된 세션이 없습니다. + 추가로 만들거나, 접속 창의 ☆ Save(Alt+S)나 탭 메뉴의 "세션으로 저장"으로 저장하세요.' :
            '찾는 세션이 없습니다.';
        smList.appendChild(li);
    }
    smList.querySelector('.sel')?.scrollIntoView({block: 'nearest'});
    fillForm();
}

function fillForm() {
    const x = current;
    smForm.classList.toggle('disabled', !x);
    for (const el of [...fields, smOpenBtn, smDup, smDel]) el.disabled = !x;
    if (!dirty) fillFields(x);
    const g = x?.id ? x.group : '';
    smGroupOpen.disabled = !g;
    smGroupOpen.textContent = g ? `"${g}" 전체 열기` : '그룹 전체 열기';
}

/** Puts x into the form (not while there are unsaved edits, typed during a save). */
function fillFields(x?: main.SavedSession) {
    smName.value = x?.name ?? '';
    smGroup.value = x?.group ?? '';
    smHost.value = x?.host ?? '';
    smPort.value = x && x.port > 0 ? String(x.port) : '';
    smLogin.value = x?.login ?? '';
    smPass.value = x?.pass ?? '';
    smSavePass.checked = !!x?.pass;
    smCode.value = x?.encoding || 'UTF-8';
    smError.textContent = '';
}

async function select(x: main.SavedSession | undefined) {
    if (x === current) return;
    await flush();
    if (x?.id === '') {
        current = x;
        paintList();
    } else {
        paintList(x?.id);
    }
}

function step(d: number) {
    if (shown.length === 0) return;
    const i = current ? shown.indexOf(current) : -1;
    select(shown[Math.max(0, Math.min(shown.length - 1, i + d))]);
}

/** Saves the form into the selected session (debounced while typing). */
async function flush() {
    clearTimeout(saveTimer);
    if (!dirty || !current) return;
    dirty = false;
    const x = current;
    if (!smHost.value.trim()) {
        smError.textContent = 'Host를 입력하세요';
        return;
    }
    try {
        const saved = await SaveSession(main.SavedSession.createFrom({
            id: x.id,
            name: smName.value.trim(),
            group: smGroup.value.trim(),
            host: smHost.value.trim(),
            port: Number(smPort.value) || 0,
            login: smLogin.value,
            encoding: smCode.value,
            pass: smPass.value,
        }), smSavePass.checked);
        smError.textContent = '';
        if (current !== x) return; // another one was picked meanwhile
        current = undefined;
        const focused = document.activeElement;
        const caret = focused instanceof HTMLInputElement ? focused.selectionStart : null;
        await reload(saved.id);
        // Repainting refills the form; keep typing where it was.
        if (focused instanceof HTMLElement && smPanel.contains(focused)) {
            focused.focus();
            if (focused instanceof HTMLInputElement && caret !== null && focused.type !== 'checkbox') {
                focused.setSelectionRange(caret, caret);
            }
        }
    } catch (e) {
        smError.textContent = String(e);
    }
}

function edited() {
    dirty = true;
    clearTimeout(saveTimer);
    saveTimer = window.setTimeout(flush, 600);
}

for (const el of fields) {
    el.addEventListener('input', edited);
    el.addEventListener('change', () => flush());
}
smPort.addEventListener('input', () => (smPort.value = smPort.value.replace(/\D/g, '')));
// Typing a password means it is meant to be kept; clearing it means it isn't.
smPass.addEventListener('input', () => (smSavePass.checked = smPass.value !== ''));

async function add() {
    await flush();
    current = main.SavedSession.createFrom({id: '', name: '', group: '', host: '', port: 22, login: '', encoding: 'UTF-8', pass: ''});
    smFilter.value = '';
    paintList();
    smHost.focus();
}

async function duplicate() {
    await flush();
    const x = current;
    if (!x?.id) return;
    const copy = await SaveSession(main.SavedSession.createFrom({...x, id: '', name: `${x.name} (복사)`}), !!x.pass);
    current = undefined;
    await reload(copy.id);
    smName.focus();
    smName.select();
}

async function remove() {
    const x = current;
    if (!x) return;
    if (x.id && !(await ask('세션 삭제', `'${x.name}' 세션을 지울까요?`, '삭제'))) return smList.focus();
    clearTimeout(saveTimer);
    dirty = false;
    const i = shown.indexOf(x);
    if (x.id) await DeleteSession(x.id);
    current = undefined;
    all = all.filter(o => o !== x);
    await reload();
    const next = shown[Math.min(i, shown.length - 1)];
    if (next) paintList(next.id);
    smList.focus();
}

async function openSelected() {
    await flush();
    const x = current;
    if (x?.id) run([x]);
}

async function openGroup() {
    await flush();
    const g = current?.group;
    if (g) run(all.filter(x => x.group === g));
}

function run(list: main.SavedSession[]) {
    const cb = onOpen;
    closeSessionManager().then(() => cb?.(list));
}

smFilter.addEventListener('input', () => paintList(current?.id));
smFilter.addEventListener('keydown', ev => {
    if (ev.key === 'ArrowDown' || ev.key === 'ArrowUp') {
        ev.preventDefault();
        step(ev.key === 'ArrowDown' ? 1 : -1);
    } else if (ev.key === 'Enter') {
        ev.preventDefault();
        openSelected();
    }
});
$<HTMLButtonElement>('smAdd').addEventListener('click', add);
smDup.addEventListener('click', duplicate);
smDel.addEventListener('click', remove);
smOpenBtn.addEventListener('click', openSelected);
smGroupOpen.addEventListener('click', openGroup);
$<HTMLButtonElement>('smClose').addEventListener('click', closeSessionManager);
$<HTMLButtonElement>('smX').addEventListener('click', closeSessionManager);

smList.addEventListener('keydown', ev => {
    switch (ev.key) {
        case 'ArrowDown':
            step(1);
            break;
        case 'ArrowUp':
            step(-1);
            break;
        case 'Home':
            select(shown[0]);
            break;
        case 'End':
            select(shown[shown.length - 1]);
            break;
        case 'Enter':
            openSelected();
            break;
        case 'Delete':
            remove();
            break;
        case 'Insert':
            add();
            break;
        case 'F2':
            smName.focus();
            smName.select();
            break;
        default:
            // Typing a letter in the list starts a search.
            if (ev.key.length === 1 && !ev.ctrlKey && !ev.altKey) smFilter.focus();
            return;
    }
    ev.preventDefault();
});

smPanel.addEventListener('keydown', ev => {
    if (askOpen()) return;
    if (ev.key === 'Escape') {
        ev.preventDefault();
        closeSessionManager();
    } else if (ev.key === 'Enter' && ev.ctrlKey) {
        ev.preventDefault();
        openSelected();
    }
    ev.stopPropagation();
});
