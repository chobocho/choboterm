// Short messages: the toast over the page, and notices written into a tab's terminal.

import {tr} from './i18n';
import type {Tab} from './tabstate';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const toastEl = $<HTMLDivElement>('toast');
let toastTimer = 0;

export function toast(msg: string) {
    toastEl.textContent = msg;
    toastEl.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = window.setTimeout(() => (toastEl.hidden = true), 1200);
}

export function notice(t: Tab, msg: string) {
    t.term.write(`\r\n\x1b[33m[${tr(msg).replace(/\n/g, '\r\n')}]\x1b[0m\r\n`);
}

/** "Enter: 다시 접속", in yellow after a bracketed reason. */
export function reconnectHint(t: Tab, why: string) {
    t.term.write(`\x1b[33m[${tr(why)}] ${tr('Enter: 다시 접속')}\x1b[0m\r\n`);
}
