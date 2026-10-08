// Saved sessions: the "Save session" window, opened from the Connect dialog
// (☆ Save, Alt+S) or the tab menu.

import {GetSessions, SaveSession} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

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
