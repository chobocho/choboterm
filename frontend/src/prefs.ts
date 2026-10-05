// Settings window (Ctrl+Shift+O or the ⚙ button).

import {saveSettings, settings} from './settings';
import {ChooseLogDir} from '../wailsjs/go/main/App';
import {FONTS} from './cjkwidth';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('prefsOverlay');
const panel = $<HTMLDivElement>('prefs');
const fontSize = $<HTMLInputElement>('pFont');
const fontUtf8 = $<HTMLSelectElement>('pFontUtf8');
const fontEucKr = $<HTMLSelectElement>('pFontEucKr');
for (const sel of [fontUtf8, fontEucKr]) {
    for (const f of FONTS) sel.add(new Option(f.label, f.name));
}
const keepAlive = $<HTMLInputElement>('pKeepAlive');
const autoReconnect = $<HTMLInputElement>('pReconnect');
const pasteConfirm = $<HTMLInputElement>('pPaste');
const logAuto = $<HTMLInputElement>('pLogAuto');
const logPlain = $<HTMLInputElement>('pLogPlain');
const logDir = $<HTMLInputElement>('pLogDir');
const error = $<HTMLDivElement>('pError');

let onApply: (() => void) | undefined;
let onClose: (() => void) | undefined;

export function prefsOpen() {
    return !overlay.hidden;
}

/** Opens the window. apply runs after OK saved the settings; close runs when the window closes. */
export function openPrefs(apply: () => void, close: () => void) {
    onApply = apply;
    onClose = close;
    fontSize.value = String(settings.fontSize);
    fontUtf8.value = settings.fontUtf8;
    fontEucKr.value = settings.fontEucKr;
    keepAlive.value = String(settings.keepAlive);
    autoReconnect.checked = settings.autoReconnect;
    pasteConfirm.checked = !settings.pasteNoConfirm;
    logAuto.checked = settings.logAuto;
    logPlain.checked = !settings.logRaw;
    logDir.value = settings.logDir;
    error.textContent = '';
    overlay.hidden = false;
    fontSize.focus();
    fontSize.select();
}

function closePrefs() {
    if (overlay.hidden) return;
    overlay.hidden = true;
    const cb = onClose;
    onClose = onApply = undefined;
    cb?.();
}

function readInt(input: HTMLInputElement, min: number, max: number, what: string): number | undefined {
    const n = Number(input.value.trim());
    if (!Number.isInteger(n) || n < min || n > max) {
        error.textContent = `${what}: ${min}~${max} 사이의 숫자를 입력하세요`;
        input.focus();
        input.select();
        return undefined;
    }
    return n;
}

function apply() {
    const font = readInt(fontSize, 8, 40, '글꼴 크기');
    if (font === undefined) return;
    const ka = readInt(keepAlive, 0, 3600, '연결 유지 간격');
    if (ka === undefined) return;
    saveSettings(s => {
        s.fontSize = font;
        s.fontUtf8 = fontUtf8.value;
        s.fontEucKr = fontEucKr.value;
        s.keepAlive = ka;
        s.autoReconnect = autoReconnect.checked;
        s.pasteNoConfirm = !pasteConfirm.checked;
        s.logAuto = logAuto.checked;
        s.logRaw = !logPlain.checked;
        s.logDir = logDir.value.trim();
    });
    const cb = onApply;
    closePrefs();
    cb?.();
}

$<HTMLButtonElement>('pLogBrowse').addEventListener('click', async () => {
    try {
        const dir = await ChooseLogDir(logDir.value.trim());
        if (dir) logDir.value = dir;
    } catch (e) {
        error.textContent = String(e);
    }
    logDir.focus();
});

$<HTMLButtonElement>('pOk').addEventListener('click', apply);
$<HTMLButtonElement>('pCancel').addEventListener('click', closePrefs);
$<HTMLButtonElement>('prefsX').addEventListener('click', closePrefs);

panel.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.preventDefault();
        closePrefs();
    } else if (ev.key === 'Enter' && !(ev.target instanceof HTMLButtonElement)) {
        ev.preventDefault();
        apply();
    }
    ev.stopPropagation();
});
