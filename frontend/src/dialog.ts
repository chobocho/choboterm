// Small in-app question windows: a yes/no question and a one-line text input.

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

const overlay = $<HTMLDivElement>('askOverlay');
const panel = $<HTMLDivElement>('ask');
const title = $<HTMLSpanElement>('askTitle');
const message = $<HTMLDivElement>('askMsg');
const input = $<HTMLInputElement>('askInput');
const okBtn = $<HTMLButtonElement>('askOk');

let resolve: ((value: string | null) => void) | undefined;

export function askOpen() {
    return !overlay.hidden;
}

function open(heading: string, msg: string, ok: string, value: string | null): Promise<string | null> {
    finish(null);
    title.textContent = heading;
    message.textContent = msg;
    okBtn.textContent = ok;
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

/** Asks for a line of text; resolves null if cancelled or left unchanged/empty. */
export async function askText(heading: string, msg: string, value = ''): Promise<string | null> {
    const v = await open(heading, msg, '확인', value);
    const s = v?.trim();
    return s && s !== value ? s : null;
}

okBtn.addEventListener('click', () => finish(input.hidden ? '' : input.value));
$<HTMLButtonElement>('askCancel').addEventListener('click', () => finish(null));
$<HTMLButtonElement>('askX').addEventListener('click', () => finish(null));

panel.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.preventDefault();
        finish(null);
    } else if (ev.key === 'Enter' && (ev.target as HTMLElement).id !== 'askCancel') {
        ev.preventDefault();
        finish(input.hidden ? '' : input.value);
    }
    ev.stopPropagation();
});
