// Confirmation before pasting several lines into the terminal: each line
// break runs a command, so a wrong clipboard can do real damage.

import {saveSettings, settings} from './settings';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('pasteOverlay');
const panel = $<HTMLDivElement>('paste');
const info = $<HTMLDivElement>('pasteInfo');
const preview = $<HTMLPreElement>('pastePreview');
const noAsk = $<HTMLInputElement>('pasteNoAsk');
const okBtn = $<HTMLButtonElement>('pasteOk');

let resolve: ((ok: boolean) => void) | undefined;

const MAX_PREVIEW = 2000;

export function pasteConfirmOpen() {
    return !overlay.hidden;
}

/** Resolves true if text may be pasted (asking first when it has line breaks). */
export function confirmPaste(text: string): Promise<boolean> {
    if (settings.pasteNoConfirm || !/[\r\n]/.test(text)) return Promise.resolve(true);
    finish(false); // a previous question still open
    const lines = text.replace(/\r\n/g, '\n').split(/[\r\n]/);
    if (lines[lines.length - 1] === '') lines.pop();
    info.textContent = `${lines.length}줄을 붙여넣습니다. 줄마다 Enter가 입력되어 명령이 실행될 수 있습니다.`;
    preview.textContent = text.length > MAX_PREVIEW ? text.slice(0, MAX_PREVIEW) + '\n…' : text;
    noAsk.checked = false;
    overlay.hidden = false;
    okBtn.focus();
    return new Promise(r => (resolve = r));
}

function finish(ok: boolean) {
    if (!resolve) return;
    if (ok && noAsk.checked) saveSettings(s => (s.pasteNoConfirm = true));
    overlay.hidden = true;
    const r = resolve;
    resolve = undefined;
    r(ok);
}

okBtn.addEventListener('click', () => finish(true));
$<HTMLButtonElement>('pasteCancel').addEventListener('click', () => finish(false));
$<HTMLButtonElement>('pasteX').addEventListener('click', () => finish(false));

panel.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.preventDefault();
        finish(false);
    } else if (ev.key === 'Enter' && ev.target !== noAsk) {
        ev.preventDefault();
        finish((ev.target as HTMLElement).id !== 'pasteCancel');
    }
    ev.stopPropagation();
});
