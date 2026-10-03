// Scrollback search bar (Ctrl+Shift+S). One bar is shared; each tab has its
// own SearchAddon, and switching tabs searches the newly shown tab.

import {Terminal} from '@xterm/xterm';
import {SearchAddon, ISearchOptions} from '@xterm/addon-search';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const bar = $<HTMLDivElement>('search');
const input = $<HTMLInputElement>('searchInput');
const count = $<HTMLSpanElement>('searchCount');
const caseBtn = $<HTMLButtonElement>('searchCase');
const regexBtn = $<HTMLButtonElement>('searchRegex');

interface Target {
    term: Terminal;
    addon: SearchAddon;
}

let target: Target | undefined;
let onClose: (() => void) | undefined;
let caseSensitive = false;
let regex = false;

const DECORATIONS = {
    matchBackground: '#5c4a00',
    matchOverviewRuler: '#d18616',
    activeMatchBackground: '#d18616',
    activeMatchColorOverviewRuler: '#ffcc00',
};

/** Loads a search addon into term; keep the result for openSearch / switchSearch. */
export function attachSearch(term: Terminal): Target {
    const addon = new SearchAddon();
    term.loadAddon(addon);
    const t = {term, addon};
    addon.onDidChangeResults(({resultIndex, resultCount}) => {
        if (target !== t || !input.value) return;
        if (resultCount === 0) count.textContent = '결과 없음';
        else if (resultIndex < 0) count.textContent = `${resultCount}+`;
        else count.textContent = `${resultIndex + 1}/${resultCount}`;
        bar.classList.toggle('none', resultCount === 0);
    });
    return t;
}

export function searchOpen() {
    return !bar.hidden;
}

function options(): ISearchOptions {
    return {caseSensitive, regex, decorations: DECORATIONS};
}

function paintCount() {
    count.textContent = '';
    bar.classList.remove('none');
}

/** Searches from the bottom (newest output) up, as you type. */
function searchFromEnd() {
    if (!target) return;
    paintCount();
    target.term.clearSelection();
    if (!input.value) {
        target.addon.clearDecorations();
        return;
    }
    try {
        target.addon.findPrevious(input.value, options());
    } catch {
        count.textContent = '잘못된 정규식';
        bar.classList.add('none');
    }
}

function step(up: boolean) {
    if (!target || !input.value) return;
    try {
        if (up) target.addon.findPrevious(input.value, options());
        else target.addon.findNext(input.value, options());
    } catch {
        // Invalid regex: the count already says so.
    }
}

/** Opens the bar for t (prefilled with the selected text), or focuses it again. */
export function openSearch(t: Target, close: () => void) {
    onClose = close;
    const selected = t.term.getSelection();
    if (selected && !selected.includes('\n')) input.value = selected;
    if (target !== t) target?.addon.clearDecorations();
    target = t;
    bar.hidden = false;
    input.focus();
    input.select();
    if (input.value) searchFromEnd();
}

/** The shown tab changed while the bar is open. */
export function switchSearch(t: Target) {
    if (bar.hidden || target === t) return;
    target?.addon.clearDecorations();
    target = t;
    searchFromEnd();
}

export function closeSearch() {
    if (bar.hidden) return;
    bar.hidden = true;
    target?.addon.clearDecorations();
    target = undefined;
    const cb = onClose;
    onClose = undefined;
    cb?.();
}

function toggle(btn: HTMLButtonElement, on: boolean) {
    btn.classList.toggle('on', on);
    searchFromEnd();
    input.focus();
}

input.addEventListener('input', searchFromEnd);
input.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        closeSearch();
    } else if (ev.key === 'Enter') {
        step(!ev.shiftKey); // Enter goes up to older output, Shift+Enter back down
    } else if (ev.altKey && (ev.key === 'c' || ev.key === 'C')) {
        toggle(caseBtn, (caseSensitive = !caseSensitive));
    } else if (ev.altKey && (ev.key === 'r' || ev.key === 'R')) {
        toggle(regexBtn, (regex = !regex));
    } else {
        ev.stopPropagation();
        return;
    }
    ev.preventDefault();
    ev.stopPropagation();
});
caseBtn.addEventListener('click', () => toggle(caseBtn, (caseSensitive = !caseSensitive)));
regexBtn.addEventListener('click', () => toggle(regexBtn, (regex = !regex)));
$<HTMLButtonElement>('searchPrev').addEventListener('click', () => {
    step(true);
    input.focus();
});
$<HTMLButtonElement>('searchNext').addEventListener('click', () => {
    step(false);
    input.focus();
});
$<HTMLButtonElement>('searchClose').addEventListener('click', closeSearch);
// Buttons keep the focus in the text box.
bar.addEventListener('mousedown', ev => {
    if (ev.target !== input) ev.preventDefault();
});
