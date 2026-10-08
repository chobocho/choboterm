// Questions the Go side asks while logging in: a key passphrase, a one-time
// code, keyboard-interactive prompts. Prompts arriving while one is shown wait their turn.

import {AnswerPrompt} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';

// Matches Prompt / PromptField in prompt.go (sent as an event, so not generated).
interface Prompt {
    id: number;
    tab: number;
    title: string;
    message: string;
    fields: {label: string; secret: boolean}[] | null;
}

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('authOverlay');
const panel = $<HTMLDivElement>('auth');
const title = $<HTMLSpanElement>('authTitle');
const where = $<HTMLDivElement>('authWhere');
const message = $<HTMLDivElement>('authMsg');
const fieldsEl = $<HTMLDivElement>('authFields');

let current: Prompt | undefined;
let inputs: HTMLInputElement[] = [];
const queue: Prompt[] = [];
let describeTab: (id: number) => string = () => '';
let onClose: () => void = () => undefined;

export function authPromptOpen() {
    return !overlay.hidden;
}

/** describe names a tab for the prompt ("user@host:22"); close runs after each prompt closes. */
export function initAuthPrompt(describe: (id: number) => string, close: () => void) {
    describeTab = describe;
    onClose = close;
}

function show(p: Prompt) {
    current = p;
    title.textContent = p.title;
    where.textContent = describeTab(p.tab);
    where.hidden = !where.textContent;
    message.textContent = p.message;
    message.hidden = !p.message;
    fieldsEl.innerHTML = '';
    inputs = (p.fields ?? []).map((f, i) => {
        const label = document.createElement('label');
        label.textContent = f.label || `입력 ${i + 1}`;
        const input = document.createElement('input');
        input.type = f.secret ? 'password' : 'text';
        input.spellcheck = false;
        input.autocomplete = 'off';
        label.appendChild(input);
        fieldsEl.appendChild(label);
        return input;
    });
    overlay.hidden = false;
    (inputs[0] ?? $<HTMLButtonElement>('authOk')).focus();
}

function finish(ok: boolean) {
    const p = current;
    if (!p) return;
    AnswerPrompt(p.id, ok ? inputs.map(i => i.value) : [], ok);
    for (const i of inputs) i.value = ''; // don't leave secrets in the page
    current = undefined;
    overlay.hidden = true;
    const next = queue.shift();
    if (next) show(next);
    else onClose();
}

EventsOn('auth:prompt', (p: Prompt) => {
    if (current) queue.push(p);
    else show(p);
});

// The Go side stopped waiting (timeout): drop that prompt.
EventsOn('auth:promptDone', (id: number) => {
    if (current?.id === id) {
        current = undefined;
        overlay.hidden = true;
        const next = queue.shift();
        if (next) show(next);
        else onClose();
    } else {
        const i = queue.findIndex(p => p.id === id);
        if (i >= 0) queue.splice(i, 1);
    }
});

$<HTMLButtonElement>('authOk').addEventListener('click', () => finish(true));
$<HTMLButtonElement>('authCancel').addEventListener('click', () => finish(false));
$<HTMLButtonElement>('authX').addEventListener('click', () => finish(false));

panel.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.preventDefault();
        finish(false);
    } else if (ev.key === 'Enter' && (ev.target as HTMLElement).id !== 'authCancel') {
        ev.preventDefault();
        // Enter moves to the next field, and answers from the last one.
        const i = inputs.indexOf(ev.target as HTMLInputElement);
        if (i >= 0 && i < inputs.length - 1) inputs[i + 1].focus();
        else finish(true);
    }
    ev.stopPropagation();
});
