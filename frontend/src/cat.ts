// The cat: walks along the bottom of the window, sits, grooms, looks back,
// stretches, jumps, rolls over and sleeps. Drag it to put it elsewhere;
// click it to run a bit of Lua (with no tab: EvalLua) and see the result.
// Its sprites are made by tools/make_cat.py from the artist's sheet.

import catPng from './assets/cat.png';
import catFrames from './catframes.json';
import {EvalLua, OpenTarget} from '../wailsjs/go/app/App';
import {saveSettings, settings} from './settings';
import {focusActive} from './tabstate';
import {BrowserOpenURL, ClipboardSetText} from '../wailsjs/runtime/runtime';
import {tr} from './i18n';
import {toast} from './notify';

interface Action {
    w: number;
    h: number;
    frames: number[][];
    steps?: number[]; // walk: atlas px the nose moves when frame i comes up (keeps the paws put)
    ms?: number; // walk: how long a frame shows (before TEMPO), when it isn't the default
}
interface Meta {
    height: number;
    w: number;
    h: number;
    actions: Record<string, Action>;
}
const META = catFrames as unknown as Meta;
const SCALE = 0.5; // atlas px -> screen px: the atlas is drawn at twice the size, for HiDPI
// Every frame is shown TEMPO times as long as the durations below say: an
// unhurried cat. The walk's step per frame is fixed by the drawings (it keeps
// the paws put), so a slower walk takes longer frames, not shorter steps.
const TEMPO = 1.7;

type Pose = 'stand' | 'sit' | 'lie';

/** One frame shown for ms; move: screen px the nose goes forward as it comes up. */
interface Step {
    act: string;
    frame: number;
    ms: number;
    move?: number;
    zz?: boolean; // asleep: z's float up
}

const el = document.createElement('div');
el.id = 'cat';
el.title = '고양이: 클릭하면 Lua 실행, 끌어서 옮기기';
el.hidden = true;
el.style.backgroundImage = `url(${catPng})`;
el.style.backgroundSize = `${META.w * SCALE}px ${META.h * SCALE}px`;
const zz = document.createElement('div');
zz.id = 'catZ';
zz.hidden = true;
zz.innerHTML = '<span>z</span><span>Z</span><span>z</span>';
const bubble = document.createElement('div');
bubble.id = 'catBubble';
bubble.hidden = true;
const result = document.createElement('div');
result.id = 'luaResult';
result.className = 'dialog';
result.hidden = true;
document.body.append(el, zz, bubble, result);

let x = 0; // the nose, screen px from the left
let fromBottom = -1; // the feet, screen px above the window's bottom (-1: on the terminals' bottom edge)
let dir: 1 | -1 = 1; // 1: facing right
let pose: Pose = 'stand';
let queue: Step[] = [];
let shown: Step | undefined;
let timer = 0;
let running = false;
let busy = false; // the Lua box or the guide is open: the cat sits and waits

const rand = (a: number, b: number) => a + Math.random() * (b - a);
const irand = (a: number, b: number) => Math.floor(rand(a, b + 1));

function floorY(): number {
    if (fromBottom >= 0) return window.innerHeight - fromBottom;
    const terms = document.getElementById('terms');
    return terms ? terms.getBoundingClientRect().bottom - 1 : window.innerHeight - 4;
}

function size(act: string) {
    const a = META.actions[act];
    return {w: a.w * SCALE, h: a.h * SCALE};
}

function draw(s: Step) {
    shown = s;
    const a = META.actions[s.act];
    const [fx, fy] = a.frames[s.frame];
    const w = a.w * SCALE, h = a.h * SCALE;
    el.style.width = `${w}px`;
    el.style.height = `${h}px`;
    el.style.backgroundPosition = `${-fx * SCALE}px ${-fy * SCALE}px`;
    // The frames have the nose on their right edge: facing left, mirrored, it is on the left.
    el.style.left = `${dir > 0 ? x - w : x}px`;
    el.style.top = `${floorY() - h}px`;
    el.style.transform = dir > 0 ? '' : 'scaleX(-1)';
    zz.hidden = !s.zz;
    if (s.zz) {
        zz.style.left = `${dir > 0 ? x - w * 0.3 : x + w * 0.1}px`;
        zz.style.top = `${floorY() - h - 18}px`;
    }
}

/** Keeps the cat inside the window (after a resize, or near an edge). */
function clampX() {
    const w = shown ? size(shown.act).w : 60;
    const lo = dir > 0 ? w + 2 : 2;
    const hi = dir > 0 ? window.innerWidth - 2 : window.innerWidth - w - 2;
    x = Math.min(Math.max(x, lo), hi);
}

// ---- What the cat does ----

const add = (act: string, frames: number[], ms: number, extra: Partial<Step> = {}) => {
    for (const f of frames) queue.push({act, frame: f, ms, ...extra});
};
const hold = (act: string, frame: number, ms: number) => queue.push({act, frame, ms});

function walk(cycles: number) {
    const steps = META.actions.walk.steps ?? [];
    for (let c = 0; c < cycles; c++) {
        // The walk comes from a video at its own frame rate (see make_cat.py --walk).
        const ms = META.actions.walk.ms ?? 170;
        for (let i = 0; i < steps.length; i++) queue.push({act: 'walk', frame: i, ms, move: steps[i] * SCALE});
    }
    pose = 'stand';
}

function sitDown() {
    add('sitdown', [0, 1, 2], 200);
    pose = 'sit';
}

function standUp() {
    add('sitdown', [2, 1, 0], 200);
    pose = 'stand';
}

function sleep() {
    add('liedown', [0, 1, 2], 520);
    const n = irand(4, 9);
    for (let i = 0; i < n; i++) add('sleep', [0, 1, 2, 1], 700, {zz: true});
    pose = 'lie';
}

function wakeUp() {
    add('liedown', [2, 1, 0], 380);
    add('stretch', [0], 380);
    hold('stretch', 1, 900);
    add('stretch', [0], 380);
    pose = 'stand';
}

/** Picks what comes next, by what the cat is doing now. */
function plan() {
    if (pose === 'lie') return wakeUp();
    if (pose === 'sit') {
        const r = Math.random();
        if (r < 0.25) add('groom', [0, 1, 2, 1, 0, 1, 2, 1].slice(0, irand(4, 8)), 240);
        else if (r < 0.45) {
            add('lookback', [2, 1], 260);
            hold('lookback', 0, irand(1000, 2200));
            add('lookback', [1, 2], 260);
        } else if (r < 0.6) hold('sitdown', 2, irand(1500, 4000));
        else if (r < 0.72) sleep();
        else standUp();
        return;
    }
    const r = Math.random();
    if (r < 0.5) walk(irand(2, 6));
    else if (r < 0.65) sitDown();
    else if (r < 0.73) hold('sitdown', 0, irand(900, 2200));
    else if (r < 0.81) {
        add('stretch', [0], 380);
        hold('stretch', 1, 900);
        add('stretch', [0], 380);
    } else if (r < 0.89) add('jump', [0, 1, 2], 150, {move: 0});
    else if (r < 0.95) add('roll', [0, 1, 2, 1, 0, 1, 2], 260);
    else hold('back', 0, irand(1200, 2400));
}

function tick() {
    timer = 0;
    if (!running) return;
    if (!queue.length) {
        if (busy) {
            // waiting on the Lua box: sit still
            if (pose !== 'sit') sitDown();
            else hold('sitdown', 2, 1000);
        } else plan();
    }
    const s = queue.shift()!;
    if (s.act === 'jump' && s.frame > 0) s.move = 26;
    if (s.move) {
        // turn round at the window's edge: the nose goes where the tail was
        const w = size(s.act).w;
        const next = x + dir * s.move;
        if ((dir > 0 && next > window.innerWidth - 2) || (dir < 0 && next < 2)) {
            x -= dir * w;
            dir = dir > 0 ? -1 : 1;
        } else x = next;
    }
    clampX();
    draw(s);
    timer = window.setTimeout(tick, s.ms * TEMPO);
}

function start() {
    if (running) return;
    running = true;
    el.hidden = false;
    if (!x) {
        x = Math.min(window.innerWidth * 0.3, 260);
        dir = 1;
    }
    queue = [];
    hold('sitdown', 0, 600);
    tick();
    maybeShowGuide();
}

function stop() {
    running = false;
    window.clearTimeout(timer);
    timer = 0;
    el.hidden = true;
    zz.hidden = true;
    closeBubble(false);
}

/** Shows or hides the cat by the settings (at start and after Settings > OK). */
export function applyCat() {
    if (settings.cat) start();
    else stop();
}

document.addEventListener('visibilitychange', () => {
    // nothing to see: don't wake up 6 times a second
    if (document.hidden) {
        window.clearTimeout(timer);
        timer = 0;
    } else if (running && !timer && !dragging) tick();
});
window.addEventListener('resize', () => {
    if (!running || !shown) return;
    clampX();
    draw(shown);
});

// ---- Drag to move, click to run Lua ----

let dragging = false;
let press: {x: number; y: number; id: number} | undefined;

el.addEventListener('pointerdown', ev => {
    if (ev.button !== 0) return;
    ev.preventDefault();
    press = {x: ev.clientX, y: ev.clientY, id: ev.pointerId};
    el.setPointerCapture(ev.pointerId);
});
el.addEventListener('pointermove', ev => {
    if (!press || ev.pointerId !== press.id) return;
    if (!dragging && Math.hypot(ev.clientX - press.x, ev.clientY - press.y) < 5) return;
    if (!dragging) {
        dragging = true;
        window.clearTimeout(timer);
        timer = 0;
        queue = [];
        el.classList.add('dragging');
    }
    // held by the scruff: legs dangling, the pointer at its middle
    const {w, h} = size('jump');
    x = ev.clientX + dir * w / 2;
    fromBottom = Math.max(0, window.innerHeight - (ev.clientY + h / 2));
    draw({act: 'jump', frame: 1, ms: 0});
});
const release = (ev: PointerEvent) => {
    if (!press || ev.pointerId !== press.id) return;
    press = undefined;
    if (!dragging) {
        if (ev.type === 'pointerup') openLuaBox();
        return;
    }
    dragging = false;
    el.classList.remove('dragging');
    // keep the feet inside the window, a little above the very bottom
    fromBottom = Math.min(Math.max(fromBottom, 2), window.innerHeight - size('sitdown').h - 4);
    pose = 'stand';
    queue = [];
    add('jump', [2], 220);
    hold('sitdown', 0, 700);
    clampX();
    if (running) tick();
};
el.addEventListener('pointerup', release);
el.addEventListener('pointercancel', release);

// ---- The bubble: the Lua box, or the first-time guide ----

let lastCode = 'print("야옹")\n1 + 2';

function placeBubble() {
    const r = el.getBoundingClientRect();
    const bw = bubble.offsetWidth, bh = bubble.offsetHeight;
    const left = Math.min(Math.max(r.left + r.width / 2 - bw / 2, 6), window.innerWidth - bw - 6);
    const above = r.top - bh - 10;
    bubble.style.left = `${left}px`;
    bubble.style.top = `${above >= 6 ? above : Math.min(r.bottom + 10, window.innerHeight - bh - 6)}px`;
    bubble.classList.toggle('below', above < 6);
}

function openBubble(html: string) {
    busy = true;
    bubble.innerHTML = html;
    bubble.hidden = false;
    placeBubble();
}

function closeBubble(refocus = true) {
    if (bubble.hidden) return;
    bubble.hidden = true;
    bubble.innerHTML = '';
    busy = false;
    if (refocus) focusActive();
}

function openLuaBox() {
    openBubble(`
        <div class="head">🐾 Lua <span class="hint">탭 없이 실행 · Ctrl+Enter</span></div>
        <textarea spellcheck="false" rows="4" placeholder="Lua 코드, 웹 주소, 폴더 경로 (예: 열기 github.com · 폴더 C:\\work)"></textarea>
        <div class="buttons">
            <button type="button" class="run">실행</button>
            <button type="button" class="close">닫기</button>
        </div>`);
    const input = bubble.querySelector('textarea')!;
    const run = bubble.querySelector<HTMLButtonElement>('.run')!;
    input.value = lastCode;
    input.focus();
    input.select();
    const go = async () => {
        lastCode = input.value;
        if (!lastCode.trim()) return;
        run.disabled = true;
        run.textContent = '실행 중...';
        try {
            const msg = message(lastCode);
            if (msg) await openFor(lastCode, msg);
            else {
                const r = await EvalLua(lastCode);
                showResult(lastCode, r);
                // open_url for a browser on another PC: Go couldn't show them there
                r.open?.forEach(u => BrowserOpenURL(u));
            }
        } catch (e) {
            showResult(lastCode, {output: '', value: '', error: String(e), ms: 0});
        } finally {
            run.disabled = false;
            run.textContent = '실행';
            // Back to the code, ready for the next run (the button took the focus).
            if (input.isConnected) input.focus();
        }
    };
    run.addEventListener('click', () => void go());
    bubble.querySelector('.close')!.addEventListener('click', () => closeBubble());
    input.addEventListener('keydown', ev => {
        if (ev.key === 'Enter' && ev.ctrlKey) {
            ev.preventDefault();
            void go();
        } else if (ev.key === 'Escape') {
            ev.stopPropagation();
            closeBubble();
        }
    });
}

// ---- Plain messages ----
// Besides Lua the cat takes a web address or a folder to open: a line that
// is one ("https://...", "www...", "C:\work", "~/src", "/var/log"), or one
// after 열기 / open (a bare "github.com" too) or 폴더 / folder. None of these
// is valid Lua, so code is never taken for a message; a bare "a.b" is Lua
// (math.pi), hence 열기 for an address without its scheme. Go checks what
// to open (http(s) only, folders that exist) and limits how often.

interface Message {
    kind: 'url' | 'folder';
    target: string;
}

const FOLDER = /^["']?(?:[A-Za-z]:[\\/]|\\\\|~(?:[\\/]|$)|\/)/;

function message(text: string): Message | undefined {
    const s = text.trim();
    if (!s || s.includes('\n')) return undefined;
    let m = /^(?:폴더|folder)\s+(.+)$/i.exec(s);
    if (m) return {kind: 'folder', target: m[1]};
    m = /^(?:열기|open)\s+(.+)$/i.exec(s);
    if (m) return {kind: FOLDER.test(m[1]) ? 'folder' : 'url', target: m[1]};
    if (/^(?:https?:\/\/|www\.)\S+$/i.test(s)) return {kind: 'url', target: s};
    if (FOLDER.test(s)) return {kind: 'folder', target: s};
    return undefined;
}

async function openFor(code: string, msg: Message) {
    const started = performance.now();
    const ms = () => Math.round(performance.now() - started);
    try {
        const o = await OpenTarget(msg.kind, msg.target);
        // A browser on another PC opens the address itself, still inside the click.
        if (o.inPage) BrowserOpenURL(o.target);
        const done = msg.kind === 'url' ? `브라우저로 열었습니다: ${o.target}` : `폴더를 열었습니다: ${o.target}`;
        showResult(code, {output: tr(done), value: '', error: '', ms: ms()});
    } catch (e) {
        showResult(code, {output: '', value: '', error: String(e), ms: ms()});
    }
}

function maybeShowGuide() {
    if (settings.catIntro) return;
    window.setTimeout(() => {
        if (!running || !bubble.hidden || settings.catIntro) return;
        openBubble(`
            <div class="head">🐾 안녕하세요!</div>
            <p>저를 클릭하면 Lua 코드를 실행할 수 있어요.</p>
            <p>끌어서 다른 곳에 옮길 수도 있어요.</p>
            <p class="hint">숨기려면 설정(Ctrl+Shift+O) → 고양이 보이기를 끄세요.</p>
            <div class="buttons">
                <button type="button" class="ok">알겠어요</button>
                <button type="button" class="hide">지금 숨기기</button>
            </div>`);
        bubble.querySelector('.ok')!.addEventListener('click', () => {
            saveSettings(s => (s.catIntro = true));
            closeBubble();
        });
        bubble.querySelector('.hide')!.addEventListener('click', () => {
            saveSettings(s => {
                s.catIntro = true;
                s.cat = false;
            });
            closeBubble();
            stop();
        });
    }, 1500);
}

// ---- The result window ----
// Every run is added to one log, as in the Lua console: the code after "> "
// (and "  " on its next lines), what it printed, its value after "= " or its
// error after "오류: ", and "-- 12ms". A read-only textarea holds the log:
// it scrolls, its text is selected and copied like any text box, and no
// keyboard pops up on a phone. A textarea has no colours, hence the
// prefixes. The labels are put through tr() because the page's translator
// sees text nodes, not a textarea's value.

interface Result {
    output: string;
    value: string;
    error: string;
    ms: number;
}

const LOG_MAX = 256 << 10; // characters kept; the oldest runs go first

result.tabIndex = -1;
result.innerHTML = `
    <div class="titlebar"><span class="title">Lua 결과</span>
        <button type="button" class="close" title="닫기 (Esc)">✕</button></div>
    <textarea class="log" readonly spellcheck="false" wrap="soft"></textarea>
    <div class="buttons">
        <button type="button" class="copy">모두 복사</button>
        <button type="button" class="clear">지우기</button>
        <button type="button" class="done">닫기</button>
    </div>`;
const log = result.querySelector<HTMLTextAreaElement>('.log')!;
result.querySelectorAll('.close, .done').forEach(b => b.addEventListener('click', () => closeResult()));
result.querySelector('.copy')!.addEventListener('click', () => {
    if (!log.value) return;
    ClipboardSetText(log.value).then(
        () => toast('복사했습니다'),
        e => toast(String(e)),
    );
});
result.querySelector('.clear')!.addEventListener('click', () => {
    log.value = '';
    log.focus();
});
result.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.stopPropagation();
        closeResult();
    }
});

function showResult(code: string, r: Result) {
    const lines = code.replace(/\s+$/, '').split('\n').map((l, i) => (i ? '  ' : '> ') + l);
    if (r.output) lines.push(r.output);
    if (r.value) lines.push(r.value.split('\n').map(l => '= ' + l).join('\n'));
    if (r.error) lines.push(tr(`오류: ${r.error}`));
    lines.push(`-- ${r.ms}ms`);
    let text = (log.value ? log.value + '\n\n' : '') + lines.join('\n');
    if (text.length > LOG_MAX) {
        // Cut at the start of a run, so no run is left half there.
        const cut = text.indexOf('\n\n> ', text.length - LOG_MAX);
        text = cut >= 0 ? text.slice(cut + 2) : text.slice(text.length - LOG_MAX);
    }
    log.value = text;
    if (result.hidden) {
        result.hidden = false;
        placeResult();
    }
    log.scrollTop = log.scrollHeight;
}

/** Keeps the log off the code box: on a narrow window (a phone) it would
 * cover the box it was run from, so it goes above it, shorter if need be.
 * Done only as it opens, so a size the user dragged it to stays. */
function placeResult() {
    result.style.bottom = '';
    if (bubble.hidden) return;
    const b = bubble.getBoundingClientRect();
    const r = result.getBoundingClientRect();
    if (r.right <= b.left || b.right <= r.left || r.bottom <= b.top || b.bottom <= r.top) return;
    const room = b.top - 16; // above the box, less a margin at the top
    if (room < 180) return; // too little room: covering it is the lesser evil
    result.style.bottom = `${window.innerHeight - b.top + 8}px`;
    if (r.height > room) result.style.height = `${room}px`;
}

function closeResult() {
    result.hidden = true; // the log is kept for the next run; 지우기 empties it
    const input = bubble.querySelector('textarea');
    if (input) input.focus();
    else focusActive();
}
