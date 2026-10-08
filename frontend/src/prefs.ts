// Settings window (Ctrl+Shift+O or the ⚙ button).

import {saveSettings, settings} from './settings';
import {ChooseLogDir} from '../wailsjs/go/main/App';
import {FONTS} from './cjkwidth';
import {paletteOf, themeByName, THEMES} from './themes';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('prefsOverlay');
const panel = $<HTMLDivElement>('prefs');
const fontSize = $<HTMLInputElement>('pFont');
const fontUtf8 = $<HTMLSelectElement>('pFontUtf8');
const fontEucKr = $<HTMLSelectElement>('pFontEucKr');
for (const sel of [fontUtf8, fontEucKr]) {
    for (const f of FONTS) sel.add(new Option(f.label, f.name));
}
const theme = $<HTMLSelectElement>('pTheme');
for (const t of THEMES) theme.add(new Option(t.label, t.name));
const preview = $<HTMLDivElement>('pThemePreview');
const cursor = $<HTMLSelectElement>('pCursor');
const blink = $<HTMLInputElement>('pBlink');
const scrollback = $<HTMLInputElement>('pScrollback');
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
    theme.value = themeByName(settings.theme).name;
    paintPreview();
    cursor.value = settings.cursorStyle;
    blink.checked = settings.cursorBlink;
    scrollback.value = String(settings.scrollback);
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

/** Shows a prompt, an ls line and the 16 colors in the selected theme. */
function paintPreview() {
    const t = themeByName(theme.value).theme;
    const p = paletteOf(t);
    preview.style.background = t.background!;
    preview.style.color = t.foreground!;
    preview.innerHTML = '';
    const line = (parts: [string, string?][]) => {
        const div = document.createElement('div');
        for (const [text, color] of parts) {
            const span = document.createElement('span');
            span.textContent = text;
            if (color) span.style.color = color;
            div.appendChild(span);
        }
        preview.appendChild(div);
    };
    line([['user@busan', p[10]], [':'], ['~/src', p[12]], ['$ ls -l']]);
    line([['drwxr-xr-x  '], ['docs', p[12]], ['  '], ['run.sh', p[10]], ['  '], ['a.tar.gz', p[9]], ['  '], ['link', p[14]]]);
    line([['오류: ', p[1]], ['경고 ', p[3]], ['완료', p[2]]]);
    const sw = document.createElement('div');
    sw.className = 'swatches';
    for (const c of p) {
        const s = document.createElement('span');
        s.style.background = c;
        sw.appendChild(s);
    }
    preview.appendChild(sw);
}

theme.addEventListener('change', paintPreview);

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
    const lines = readInt(scrollback, 1000, 100000, '스크롤백');
    if (lines === undefined) return;
    const ka = readInt(keepAlive, 0, 3600, '연결 유지 간격');
    if (ka === undefined) return;
    saveSettings(s => {
        s.fontSize = font;
        s.fontUtf8 = fontUtf8.value;
        s.fontEucKr = fontEucKr.value;
        s.theme = theme.value;
        s.cursorStyle = cursor.value;
        s.cursorBlink = blink.checked;
        s.scrollback = lines;
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
