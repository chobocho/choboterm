// The cat: walks along the bottom of the window, sits and looks about,
// yawns, stretches, looks back, rolls over and hops. When you type it looks
// at the cursor (a click: where you clicked), crouches and now and then
// pounces at it; after 30 s with no key, click or mouse move it walks a
// little, curls up and sleeps until the next one. Drag it to put it
// elsewhere; click it to run a bit of Lua (with no tab: EvalLua) and see
// the result. Its sprites are made by tools/make_cat.py from the artist's
// sheet and a video of the cat walking.

import catPng from './assets/cat.png';
import catFrames from './catframes.json';
import {EvalLua, OpenTarget} from '../wailsjs/go/app/App';
import {saveSettings, settings} from './settings';
import {active, focusActive, tabs} from './tabstate';
import {BrowserOpenURL, ClipboardSetText} from '../wailsjs/runtime/runtime';
import {tr} from './i18n';
import {toast} from './notify';

interface Action {
    w: number;
    h: number;
    frames: number[][];
    steps?: number[]; // walk: atlas px the nose moves when frame i comes up (keeps the paws put)
    ms?: number; // walk: how long a frame shows, as filmed
}
interface Meta {
    height: number;
    w: number;
    h: number;
    actions: Record<string, Action>;
}
const META = catFrames as unknown as Meta;
const SCALE = 0.5; // atlas px -> screen px: the atlas is drawn at twice the size, for HiDPI
// Every frame but the walk's is shown TEMPO times as long as the durations
// below say: an unhurried cat. The walk plays at the video's own pace (one
// stride a second, as a cat strolls); its step per frame is fixed by the
// drawings, which keeps the paws put.
const TEMPO = 1.7;
const IDLE_MS = 30_000; // no key, click or mouse move for this long: the cat goes to sleep
const ATTEND_MS = 3_500; // after the last key or click, it watches that long
const REACH = 150; // screen px: how high above its feet the cat can jump

type Pose = 'stand' | 'sit' | 'lie';

/** One frame shown for ms; move: screen px the nose goes forward as it comes up. */
interface Step {
    act: string;
    frame: number;
    ms: number;
    move?: number;
    lift?: number; // screen px above the floor (in a jump)
    raw?: boolean; // ms is as is, not times TEMPO (the walk, a jump's flight)
    lock?: boolean; // a jump: a key or click doesn't cut it short
    zz?: boolean; // asleep: z's float up
    nap?: boolean; // on the way to a nap for want of input: 'asleep' once it is curled up
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

// What the user does: the last key, click or mouse move, and what the cat
// watches (the cursor as you type, where you clicked) until when.
let lastInput = performance.now();
let asleep = false; // curled up for want of input: the next input wakes it
let watch: {x: number; y: number; until: number; click: boolean} | undefined;
let lastPounce = 0;

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
    const top = floorY() - h - (s.lift ?? 0);
    el.style.width = `${w}px`;
    el.style.height = `${h}px`;
    el.style.backgroundPosition = `${-fx * SCALE}px ${-fy * SCALE}px`;
    // The frames have the nose on their right edge: facing left, mirrored, it is on the left.
    el.style.left = `${dir > 0 ? x - w : x}px`;
    el.style.top = `${top}px`;
    el.style.transform = dir > 0 ? '' : 'scaleX(-1)';
    // what it is doing, for the e2e tests and for looking at in DevTools
    el.dataset.act = `${s.act}:${s.frame}`;
    el.dataset.pose = asleep ? 'asleep' : pose;
    el.dataset.dir = String(dir);
    zz.hidden = !s.zz;
    if (s.zz) {
        zz.style.left = `${dir > 0 ? x - w * 0.3 : x + w * 0.1}px`;
        zz.style.top = `${top - 18}px`;
    }
}

/** Keeps the cat inside the window (after a resize, or near an edge). */
function clampX() {
    const w = shown ? size(shown.act).w : 60;
    const lo = dir > 0 ? w + 2 : 2;
    const hi = dir > 0 ? window.innerWidth - 2 : window.innerWidth - w - 2;
    x = Math.min(Math.max(x, lo), hi);
}

/** Turns round where it stands: the nose goes where the tail was. */
function turn() {
    x -= dir * (shown ? size(shown.act).w : 60);
    dir = dir > 0 ? -1 : 1;
}

// ---- What the cat does ----

const add = (act: string, frames: number[], ms: number, extra: Partial<Step> = {}) => {
    for (const f of frames) queue.push({act, frame: f, ms, ...extra});
};
const hold = (act: string, frame: number, ms: number) => queue.push({act, frame, ms});

// The sheet's actions, frame by frame:
//   sit      0 looks up, 1-2 looks ahead, 3-4 yawns, 5 eyes shut, 6 looks up
//   stretch  0 stands, 1 looks up, 2-4 stretches, 5 stands, 6 sits
//   doze     0 eyes shut, 1 nods off, 2 lies down, 3-6 curled up asleep
//   wake     0 curled up, 1 lifts its head, 2-3 lies awake, 4-5 sits, 6 stands
//   jump     0 crouches, staring, 1 leaps, 2 flies, 3 lands, 4-5 stands
//   lookback 0 sits turned away, looking back, 1 its back, 2 sits facing
//   roll     0-2 on its back, belly up

/** The pose a frame shows (see the list above). */
function poseOf(s: Step): Pose {
    switch (s.act) {
        case 'sit':
        case 'lookback':
            return 'sit';
        case 'roll':
            return 'lie';
        case 'stretch':
            return s.frame === 6 ? 'sit' : 'stand';
        case 'doze':
            return s.frame < 2 ? 'sit' : 'lie';
        case 'wake':
            return s.frame < 4 ? 'lie' : s.frame < 6 ? 'sit' : 'stand';
        default:
            return 'stand';
    }
}

function walkFrames(n: number) {
    const a = META.actions.walk;
    const steps = a.steps ?? [];
    const start = shown?.act === 'walk' ? (shown.frame + 1) % steps.length : 0;
    for (let k = 0; k < n; k++) {
        const i = (start + k) % steps.length;
        queue.push({act: 'walk', frame: i, ms: a.ms ?? 42, move: steps[i] * SCALE, raw: true});
    }
}

function walk(cycles: number) {
    if (pose === 'sit') standUp();
    walkFrames(cycles * (META.actions.walk.steps?.length ?? 24));
    pose = 'stand';
}

function sitDown() {
    hold('wake', 6, 200);
    hold('wake', 5, 300);
    pose = 'sit';
}

function standUp() {
    hold('wake', 6, 250);
    pose = 'stand';
}

/** Off to sleep: from sitting, nods off and curls up; then sleeps (see
 * plan). It counts as asleep once curled up: until then a key or the mouse
 * only calls the nap off. */
function sleep() {
    if (pose === 'stand') sitDown();
    add('doze', [0], 900, {nap: true});
    add('doze', [1], 1100, {nap: true});
    add('doze', [2], 900, {nap: true});
    add('doze', [3], 700, {nap: true});
    pose = 'lie';
}

function snore() {
    add('doze', [4, 5, 6, 5], 700, {zz: true});
}

/** Woken by a key, a click or the mouse: lifts its head and sits up. */
function wakeUp() {
    queue = [];
    asleep = false;
    hold('wake', 1, 260);
    hold('wake', 3, 220);
    hold('wake', 4, 300);
    pose = 'sit';
}

function stretch() {
    if (pose === 'sit') standUp();
    add('stretch', [0, 2, 3], 300);
    hold('stretch', 3, 900);
    add('stretch', [4, 5], 300);
    pose = 'stand';
}

/** A leap of travel screen px forward, up to apex px high, landing on the floor. */
function leap(travel: number, apex: number) {
    const ms = 360 + apex * 1.6; // higher jumps take longer, as they do
    const n = Math.max(6, Math.round(ms / 40));
    hold('jump', 0, 160);
    for (let k = 1; k <= n; k++) {
        const t = k / n;
        queue.push({
            act: 'jump',
            frame: t < 0.5 ? 1 : 2,
            ms: ms / n,
            move: travel / n,
            lift: k < n ? 4 * apex * t * (1 - t) : 0,
            raw: true,
            lock: true,
        });
    }
    add('jump', [3, 4], 160, {lock: true});
    pose = 'stand';
}

/** Picks what comes next, by what the cat is doing now. */
function plan() {
    if (asleep) return snore();
    if (watch && performance.now() < watch.until) return attend();
    watch = undefined;
    if (performance.now() - lastInput >= IDLE_MS) {
        // nobody about: a short stroll, then a nap until somebody comes back
        walk(irand(1, 3));
        return sleep();
    }
    if (pose === 'lie') {
        hold('wake', 3, 400);
        hold('wake', 4, 600);
        pose = 'sit';
        return;
    }
    if (pose === 'sit') {
        const r = Math.random();
        if (r < 0.2) {
            add('sit', [2, 3, 4], 260);
            hold('sit', 4, 500);
            add('sit', [3, 5], 300);
            hold('sit', 1, 800);
        } else if (r < 0.4) {
            add('lookback', [2, 1], 260);
            hold('lookback', 0, irand(1000, 2200));
            add('lookback', [1, 2], 260);
        } else if (r < 0.62) {
            hold('sit', irand(0, 2), irand(1200, 3000));
            hold('sit', 6, irand(800, 1800));
        } else if (r < 0.72) {
            add('roll', [0, 1, 2, 1, 0, 1, 2], 260);
            hold('wake', 4, 500);
        } else standUp();
        return;
    }
    const r = Math.random();
    if (r < 0.55) walk(irand(2, 5));
    else if (r < 0.72) sitDown();
    else if (r < 0.8) hold('stretch', irand(0, 1), irand(900, 2200));
    else if (r < 0.9) stretch();
    else leap(rand(30, 60), rand(10, 24)); // a hop, for nothing
}

// ---- Watching the cursor ----

/** Where the cat looks: the terminal's cursor as you type in it, else the
 * middle of the box being typed in. */
function cursorPoint(): {x: number; y: number} | undefined {
    const ae = document.activeElement as HTMLElement | null;
    const t = ae?.classList.contains('xterm-helper-textarea')
        ? Array.from(tabs.values()).find(t => t.pane.contains(ae))
        : !ae || ae === document.body ? active : undefined;
    if (t) {
        const screen = t.term.element?.querySelector('.xterm-screen');
        if (!screen) return undefined;
        const r = screen.getBoundingClientRect();
        if (!r.width || !t.term.cols || !t.term.rows) return undefined;
        const b = t.term.buffer.active;
        const cw = r.width / t.term.cols, ch = r.height / t.term.rows;
        const row = Math.min(Math.max(b.baseY + b.cursorY - b.viewportY, 0), t.term.rows - 1);
        return {x: r.left + (b.cursorX + 0.5) * cw, y: r.top + (row + 0.5) * ch};
    }
    if (!ae) return undefined;
    const r = ae.getBoundingClientRect();
    return r.width ? {x: r.left + r.width / 2, y: r.top + r.height / 2} : undefined;
}

/** What to do while watching: turn to it, come closer if it is far, then
 * stare at it, crouched (or sat looking up, when it is out of reach), and
 * now and then pounce. */
function attend() {
    const w = watch!;
    const dx = w.x - x;
    const sw = shown ? size(shown.act).w : 60;
    // facing away (the point is behind the nose, past the middle of the body)
    if (dx * dir < -sw / 2) {
        if (pose === 'lie') {
            hold('wake', 4, 200);
            pose = 'sit';
        }
        turn();
    }
    const dist = Math.abs(w.x - x);
    const high = floorY() - w.y; // how far above the feet
    if (dist > 260) {
        walk(0);
        walkFrames(Math.min(48, Math.ceil((dist - 140) / ((META.actions.walk.steps?.[0] ?? 2.75) * SCALE))));
        return;
    }
    const now = performance.now();
    const reach = high < REACH + 30;
    const chance = w.click ? 0.5 : reach ? 0.3 : 0.1;
    if (now - lastPounce > 2500 && Math.random() < chance) {
        lastPounce = now;
        if (pose !== 'stand') standUp();
        hold('jump', 0, 450); // crouched, staring, then...
        // land with the front paws on it, but not off the window's edge
        let travel = Math.max(0, (w.x - x) * dir + 4);
        travel = Math.min(travel, dir > 0 ? window.innerWidth - 2 - x : x - 2, 240);
        leap(travel, Math.min(Math.max(high - size('jump').h * 0.4, 14), REACH));
        hold('jump', 0, 500);
        return;
    }
    if (reach) {
        if (pose === 'sit') standUp();
        hold('jump', 0, 600);
        pose = 'stand';
    } else {
        if (pose !== 'sit') sitDown();
        hold('sit', Math.random() < 0.7 ? 0 : 6, 700);
    }
}

/** A key or a click: the cat watches that spot (and wakes up for it). */
function noticed(p: {x: number; y: number} | undefined, click: boolean) {
    if (!running || busy || dragging || !p) return;
    watch = {x: p.x, y: p.y, until: performance.now() + ATTEND_MS, click};
    // Cut short what it was doing, unless in mid-jump; a cat already looking
    // that way keeps looking (typing doesn't restart it at every key).
    if (shown?.lock || queue.some(s => s.lock)) return;
    if (shown && (shown.act === 'jump' || shown.act === 'sit') && !click && (p.x - x) * dir > -20) return;
    queue = [];
    if (shown) pose = poseOf(shown);
    if (timer) {
        window.clearTimeout(timer);
        timer = 0;
    }
    if (!document.hidden) tick();
}

function onInput(ev: Event) {
    lastInput = performance.now();
    if (!running || dragging) return;
    if (asleep && !busy) {
        wakeUp();
        if (timer && !document.hidden) {
            window.clearTimeout(timer);
            tick();
        }
    } else if (queue.some(s => s.nap) || shown?.nap) {
        // somebody is back before it fell asleep: no nap (sat up if it had lain down)
        queue = [];
        pose = shown ? poseOf(shown) : 'stand';
        if (pose === 'lie') {
            hold('wake', 3, 300);
            hold('wake', 4, 400);
            pose = 'sit';
        }
    }
    if (ev.type === 'keydown') {
        // after the key has been echoed, where the cursor moved to
        window.setTimeout(() => noticed(cursorPoint(), false), 60);
    } else if (ev.type === 'pointerdown') {
        const pe = ev as PointerEvent;
        if (el.contains(pe.target as Node) || bubble.contains(pe.target as Node)) return;
        noticed({x: pe.clientX, y: pe.clientY}, true);
    }
}
for (const t of ['keydown', 'pointerdown', 'pointermove', 'wheel']) {
    window.addEventListener(t, onInput, {capture: true, passive: true});
}

function tick() {
    timer = 0;
    if (!running) return;
    if (!queue.length) {
        if (busy) {
            // waiting on the Lua box: sit still
            if (asleep) wakeUp();
            if (pose !== 'sit') sitDown();
            else hold('sit', 1, 1000);
        } else plan();
    }
    const s = queue.shift()!;
    if (s.nap && s.frame === 3) asleep = true;
    if (s.move) {
        // turn round at the window's edge: the nose goes where the tail was
        const next = x + dir * s.move;
        if ((dir > 0 && next > window.innerWidth - 2) || (dir < 0 && next < 2)) turn();
        else x = next;
    }
    clampX();
    draw(s);
    timer = window.setTimeout(tick, s.raw ? s.ms : s.ms * TEMPO);
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
    lastInput = performance.now();
    asleep = false;
    pose = 'sit';
    hold('sit', 1, 600);
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
    // nothing to see: don't wake up 24 times a second
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
        asleep = false;
        watch = undefined;
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
    fromBottom = Math.min(Math.max(fromBottom, 2), window.innerHeight - size('sit').h - 4);
    pose = 'stand';
    queue = [];
    add('jump', [3, 4], 200);
    hold('jump', 5, 600);
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
            // The code box keeps the focus after a run, so its Esc closes
            // the result window first (as Esc in that window does), and the
            // box only on the next one.
            if (!result.hidden) closeResult();
            else closeBubble();
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
