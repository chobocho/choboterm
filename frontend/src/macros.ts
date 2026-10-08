// Macros: named texts sent to the terminal, from the macro window
// (Ctrl+Shift+M) or with the function key bound to them.

import {main} from '../wailsjs/go/models';
import {saveSettings, settings} from './settings';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

/** Keys a macro can be bound to. F1 alone is help. */
export const MACRO_KEYS: string[] = [];
for (const mod of ['', 'Shift+', 'Ctrl+']) {
    for (let i = 1; i <= 12; i++) {
        if (mod || i > 1) MACRO_KEYS.push(`${mod}F${i}`);
    }
}

/** The key name of a keydown ("F5", "Shift+F5", "Ctrl+F5"), or "" if a macro can't use it. */
export function keyName(ev: KeyboardEvent): string {
    if (ev.type !== 'keydown' || !/^F([1-9]|1[0-2])$/.test(ev.key) || ev.altKey || ev.metaKey) return '';
    if (ev.ctrlKey && ev.shiftKey) return '';
    return (ev.ctrlKey ? 'Ctrl+' : ev.shiftKey ? 'Shift+' : '') + ev.key;
}

/** The macro bound to this keydown, if any. Unbound function keys still reach the terminal. */
export function macroForKey(ev: KeyboardEvent): main.Macro | undefined {
    const k = keyName(ev);
    return k ? settings.macros.find(m => m.key === k) : undefined;
}

/**
 * The text a macro sends: a line break is Enter, and \n \r \t \e \xHH \\
 * are escapes (\e is Esc, \xHH any byte such as \x03 for Ctrl+C).
 */
export function expandMacro(text: string): string {
    return text.replace(/\r\n|\n|\r|\\(x[0-9a-fA-F]{2}|[nrte\\])/g, (_all, esc?: string) => {
        if (!esc) return '\r';
        switch (esc[0]) {
            case 'n':
            case 'r':
                return '\r';
            case 't':
                return '\t';
            case 'e':
                return '\x1b';
            case 'x':
                return String.fromCharCode(parseInt(esc.slice(1), 16));
            default:
                return '\\';
        }
    });
}

// ---- Macro window ----

const overlay = $<HTMLDivElement>('macrosOverlay');
const panel = $<HTMLDivElement>('macros');
const list = $<HTMLUListElement>('mList');
const nameInput = $<HTMLInputElement>('mName');
const keySelect = $<HTMLSelectElement>('mKey');
const kindSelect = $<HTMLSelectElement>('mKind');
const hint = $<HTMLDivElement>('mHint');
const textHint = hint.textContent ?? '';
const textInput = $<HTMLTextAreaElement>('mText');
const form = $<HTMLDivElement>('mForm');
const sendBtn = $<HTMLButtonElement>('mSend');
const delBtn = $<HTMLButtonElement>('mDel');

let selected = -1;
let onSend: ((m: main.Macro) => void) | undefined;
let onClose: (() => void) | undefined;

export function macrosOpen() {
    return !overlay.hidden;
}

function macros() {
    return settings.macros;
}

function save() {
    saveSettings(s => (s.macros = macros()));
}

function label(m: main.Macro) {
    return m.name || m.text.split(/\r?\n/)[0] || '(이름 없음)';
}

function paintList() {
    list.replaceChildren();
    macros().forEach((m, i) => {
        const li = document.createElement('li');
        const key = document.createElement('span');
        key.className = 'key';
        key.textContent = m.key;
        const name = document.createElement('span');
        name.textContent = label(m);
        li.append(key, name);
        if (i === selected) li.className = 'sel';
        li.addEventListener('mousedown', ev => {
            ev.preventDefault();
            select(i);
            list.focus();
        });
        li.addEventListener('dblclick', () => send());
        list.appendChild(li);
    });
    if (macros().length === 0) {
        const li = document.createElement('li');
        li.className = 'empty';
        li.textContent = '+ 추가로 매크로를 만드세요';
        list.appendChild(li);
    }
}

function paintKeys(m?: main.Macro) {
    keySelect.replaceChildren(new Option('(없음)', ''));
    for (const k of MACRO_KEYS) {
        const owner = macros().find(o => o.key === k && o !== m);
        const opt = new Option(owner ? `${k} - ${label(owner)}` : k, k);
        opt.disabled = !!owner;
        keySelect.appendChild(opt);
    }
    keySelect.value = m?.key ?? '';
}

function select(i: number) {
    selected = Math.max(-1, Math.min(i, macros().length - 1));
    const m = macros()[selected];
    form.classList.toggle('disabled', !m);
    for (const el of [nameInput, keySelect, kindSelect, textInput, sendBtn, delBtn]) el.disabled = !m;
    nameInput.value = m?.name ?? '';
    kindSelect.value = m?.kind ?? '';
    paintKind();
    textInput.value = m?.text ?? '';
    paintKeys(m);
    paintList();
    list.children[selected]?.scrollIntoView({block: 'nearest'});
}

/** The hint and the Send button follow the macro's kind. */
function paintKind() {
    const lua = kindSelect.value === 'lua';
    hint.textContent = lua ? 'send("ls\\r") · expect("%$ ", 10) · sleep(500) · screen() · print(...) · Lua 5.1' : textHint;
    sendBtn.textContent = lua ? '실행' : '보내기';
}

function add() {
    macros().push(main.Macro.createFrom({name: '', key: '', text: '', kind: ''}));
    save();
    select(macros().length - 1);
    nameInput.focus();
}

function remove() {
    if (selected < 0) return;
    macros().splice(selected, 1);
    save();
    select(Math.min(selected, macros().length - 1));
    list.focus();
}

function send() {
    const m = macros()[selected];
    if (!m) return;
    const cb = onSend;
    closeMacros();
    cb?.(m);
}

/** Opens the window; send runs with the chosen macro after the window closed. */
export function openMacros(sendMacro: (m: main.Macro) => void, close: () => void) {
    onSend = sendMacro;
    onClose = close;
    overlay.hidden = false;
    select(selected >= 0 ? selected : 0);
    list.focus();
}

export function closeMacros() {
    if (overlay.hidden) return;
    overlay.hidden = true;
    const cb = onClose;
    onClose = onSend = undefined;
    cb?.();
}

nameInput.addEventListener('input', () => {
    const m = macros()[selected];
    if (!m) return;
    m.name = nameInput.value;
    save();
    paintList();
});
textInput.addEventListener('input', () => {
    const m = macros()[selected];
    if (!m) return;
    m.text = textInput.value;
    save();
    paintList();
});
kindSelect.addEventListener('change', () => {
    const m = macros()[selected];
    if (!m) return;
    m.kind = kindSelect.value;
    save();
    paintKind();
    paintList();
});
keySelect.addEventListener('change', () => {
    const m = macros()[selected];
    if (!m) return;
    m.key = keySelect.value;
    save();
    paintList();
});

$<HTMLButtonElement>('mAdd').addEventListener('click', add);
delBtn.addEventListener('click', remove);
sendBtn.addEventListener('click', send);
$<HTMLButtonElement>('mClose').addEventListener('click', closeMacros);
$<HTMLButtonElement>('macrosX').addEventListener('click', closeMacros);

list.addEventListener('keydown', ev => {
    switch (ev.key) {
        case 'ArrowDown':
            select(selected + 1);
            break;
        case 'ArrowUp':
            select(selected - 1);
            break;
        case 'Enter':
            send();
            break;
        case 'Delete':
            remove();
            break;
        case 'Insert':
            add();
            break;
        default:
            return;
    }
    ev.preventDefault();
});

panel.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.preventDefault();
        closeMacros();
    } else if (ev.key === 'Enter' && ev.ctrlKey) {
        ev.preventDefault();
        send();
    }
    ev.stopPropagation();
});
