// Small in-app question windows: a yes/no question, a question with two
// answers besides Cancel, and a one-line text input.

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('askOverlay');
const panel = $<HTMLDivElement>('ask');
const title = $<HTMLSpanElement>('askTitle');
const message = $<HTMLDivElement>('askMsg');
const input = $<HTMLInputElement>('askInput');
const okBtn = $<HTMLButtonElement>('askOk');
const altBtn = $<HTMLButtonElement>('askAlt');

// What the second answer button resolves with (see choose).
const ALT = '\u0000alt';

let resolve: ((value: string | null) => void) | undefined;

export function askOpen() {
    return !overlay.hidden;
}

function open(heading: string, msg: string, ok: string, value: string | null, alt = ''): Promise<string | null> {
    finish(null);
    title.textContent = heading;
    message.textContent = msg;
    okBtn.textContent = ok;
    altBtn.textContent = alt;
    altBtn.hidden = alt === '';
    input.hidden = value === null;
    overlay.hidden = false;
    if (value === null) {
        okBtn.focus();
    } else {
        input.value = value;
        input.focus();
        // Select the name without its extension, like Explorer.
        const dot = value.lastIndexOf('.');
        input.setSelectionRange(0, dot > 0 ? dot : value.length);
    }
    return new Promise(r => (resolve = r));
}

function finish(value: string | null) {
    if (!resolve) return;
    overlay.hidden = true;
    const r = resolve;
    resolve = undefined;
    r(value);
}

/** Asks a yes/no question; resolves true for OK. */
export async function ask(heading: string, msg: string, ok = '확인'): Promise<boolean> {
    return (await open(heading, msg, ok, null)) !== null;
}

/**
 * Asks a question with two answers besides Cancel: resolves 'ok' (the
 * default, Enter), 'alt', or null for Cancel / Esc.
 */
export async function choose(heading: string, msg: string, ok: string, alt: string): Promise<'ok' | 'alt' | null> {
    const v = await open(heading, msg, ok, null, alt);
    return v === null ? null : v === ALT ? 'alt' : 'ok';
}

/** Asks for a line of text; resolves null if cancelled or left unchanged/empty. */
export async function askText(heading: string, msg: string, value = ''): Promise<string | null> {
    const v = await open(heading, msg, '확인', value);
    const s = v?.trim();
    return s && s !== value ? s : null;
}

okBtn.addEventListener('click', () => finish(input.hidden ? '' : input.value));
altBtn.addEventListener('click', () => finish(ALT));
$<HTMLButtonElement>('askCancel').addEventListener('click', () => finish(null));
$<HTMLButtonElement>('askX').addEventListener('click', () => finish(null));

panel.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.preventDefault();
        finish(null);
    } else if (ev.key === 'Enter' && (ev.target as HTMLElement).id === 'askAlt') {
        ev.preventDefault();
        finish(ALT);
    } else if (ev.key === 'Enter' && (ev.target as HTMLElement).id !== 'askCancel') {
        ev.preventDefault();
        finish(input.hidden ? '' : input.value);
    }
    ev.stopPropagation();
});
