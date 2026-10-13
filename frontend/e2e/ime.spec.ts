import {Page} from '@playwright/test';
import {expect, test} from './fixtures';

// Hangul typed in the Linux window (WebKitGTK with ibus-hangul). The page is
// told it runs in WebKitGTK by its user agent, and the IME's events are
// replayed on xterm's textarea in the order WebKitGTK 2.52 dispatches them
// (recorded with ibus-hangul 1.5.5): every syllable ends twice, first with an
// empty compositionend after the text is deleted, then again after the text
// is inserted back. What the tab sends is read on the tests' Telnet server.

test.use({userAgent: 'Mozilla/5.0 (X11; Ubuntu; Linux x86_64) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/60.5 Safari/605.1.15'});

/** One IME event (or a change of the textarea's text, as the IME makes it). */
type Step =
    | ['down', number, string?] // keydown: keyCode, key
    | ['press', number, string] // keypress (which xterm sends printable keys from)
    | ['up', number, string?]
    | ['start']
    | ['update', string] // compositionupdate with this text, which replaces the composition
    | ['input', string, string | null] // input event: inputType, data (after the text changed)
    | ['end', string] // compositionend with this data
    | ['text', string] // sets the text after the composition start (what the IME shows)
    | ['text+', string] // adds text after everything (a key the IME doesn't compose)
    | ['yield']; // the events so far were one task: timers may run now

/**
 * The events of 2-beolsik keys as WebKitGTK sends them: Hangul jamo keys
 * compose, a space commits. Only what these tests type is described.
 */
function hangul(keys: string[]): Step[] {
    const steps: Step[] = [];
    let composing = '';
    const commit = () => {
        // WebKitGTK: the composed text is deleted and the composition ends
        // empty, then the text comes back outside a composition and it ends again.
        const text = composing;
        // xterm's timers run between the two (each is its own task in WebKitGTK).
        steps.push(['text', ''], ['input', 'deleteCompositionText', null], ['end', ''], ['yield']);
        steps.push(['down', 229], ['text', text], ['input', 'insertFromComposition', text], ['end', text]);
        composing = '';
    };
    const compose = (next: string) => {
        if (composing === '') steps.push(['down', 229], ['start']);
        else steps.push(['down', 229]);
        composing = next;
        steps.push(['update', next], ['text', next], ['input', 'insertCompositionText', next]);
    };
    for (const k of keys) {
        switch (k) {
        case 'ㅎ': compose('ㅎ'); break;
        case 'ㅏ': compose(composing === 'ㅎ' ? '하' : 'ㅏ'); break;
        case 'ㅇ': compose(composing === '하' ? '항' : 'ㅇ'); break;
        case 'ㅣ':
            if (composing === '항') {
                // The final ㅇ moves to the next syllable: "하" is committed, "이" composed.
                composing = '하';
                steps.push(['down', 229]);
                commit();
                compose('이');
            } else compose('ㅣ');
            break;
        case ' ':
            steps.push(['down', 229]);
            commit();
            // The space itself: a plain keydown, and the text WebKitGTK inserts
            // although xterm cancelled the keydown.
            steps.push(['down', 32, ' '], ['press', 32, ' '], ['text+', ' '], ['input', 'insertText', ' '], ['up', 32, ' ']);
            break;
        default: throw new Error(`no key ${k}`);
        }
        steps.push(['yield']);
    }
    return steps;
}

/** Dispatches the steps on the focused terminal, a task at a time (as WebKitGTK does). */
async function replay(page: Page, steps: Step[]) {
    await page.evaluate(async steps => {
        const ta = document.querySelector('#terms .term.focused textarea') as HTMLTextAreaElement;
        let base = ta.value.length; // where the composition starts
        for (const s of steps) {
            switch (s[0]) {
            case 'down':
            case 'press':
            case 'up':
            {
                const ev = new KeyboardEvent({down: 'keydown', press: 'keypress', up: 'keyup'}[s[0]],
                    {key: s[2] ?? 'Unidentified', bubbles: true, cancelable: true, composed: true});
                // KeyboardEventInit has no keyCode, which xterm reads.
                const code = s[1];
                Object.defineProperty(ev, 'keyCode', {get: () => code});
                Object.defineProperty(ev, 'which', {get: () => code});
                if (s[0] === 'press') Object.defineProperty(ev, 'charCode', {get: () => code});
                ta.dispatchEvent(ev);
                break;
            }
            case 'start':
                base = ta.value.length;
                ta.dispatchEvent(new CompositionEvent('compositionstart', {data: '', bubbles: true}));
                break;
            case 'update':
                ta.dispatchEvent(new CompositionEvent('compositionupdate', {data: s[1], bubbles: true}));
                break;
            case 'text':
                ta.value = ta.value.slice(0, base) + s[1];
                break;
            case 'text+':
                ta.value += s[1];
                base = ta.value.length;
                break;
            case 'input':
                ta.dispatchEvent(new InputEvent('input', {inputType: s[1], data: s[2], bubbles: true, composed: true}));
                break;
            case 'end':
                ta.dispatchEvent(new CompositionEvent('compositionend', {data: s[1], bubbles: true}));
                if (s[1] !== '') base = ta.value.length;
                break;
            case 'yield':
                await new Promise(r => setTimeout(r, 0));
                break;
            }
        }
    }, steps);
}

test('Hangul words typed in the Linux window are sent once each, with their spaces', async ({app, server}) => {
    await app.connect('127.0.0.1', server.port);
    await expect(app.screen).toContainText('fake-host login:');
    const before = server.received.length;
    const sent = () => Buffer.from(server.received.slice(before), 'latin1').toString('utf8');

    await replay(app.page, hangul(['ㅎ', 'ㅏ', 'ㅇ', 'ㅣ', ' ', 'ㅎ', 'ㅏ', 'ㅇ', 'ㅣ', ' ']));
    await expect.poll(sent).toBe('하이 하이 ');
    // Nothing comes late (xterm sends compositions from timers).
    await app.page.waitForTimeout(300);
    expect(sent()).toBe('하이 하이 ');
});
