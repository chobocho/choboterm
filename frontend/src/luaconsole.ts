// Lua scripts running in a tab: the box showing their output, and the Lua console.

import {dbg} from './debug';
import {saveSettings, settings} from './settings';
import {ConsoleEval, ConsoleOpen, ConsoleSave, ConsoleInterrupt, ScriptScreen, StartConsole, StopScript} from '../wailsjs/go/app/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {active, focusActive, Tab, tabs} from './tabstate';
import {toast} from './notify';

/** Starts a script in t (start calls Go); errors such as "already running" become a toast. */
export async function runScript(t: Tab, start: () => Promise<void>) {
    if (t.state !== 'on') return toast('연결되어 있지 않습니다');
    try {
        await start();
    } catch (e) {
        toast(String(e));
    }
}

function elapsedText(ms: number): string {
    const s = Math.floor(ms / 1000);
    const h = Math.floor(s / 3600), m = Math.floor(s / 60) % 60, sec = s % 60;
    const mmss = `${m}:${String(sec).padStart(2, '0')}`;
    return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}` : mmss;
}

export interface ScriptBox {
    el: HTMLDivElement;
    head: HTMLSpanElement;
    body: HTMLDivElement;
    timer: number;
    // Lua console only: the input line, earlier inputs, and the input running now.
    input?: HTMLTextAreaElement;
    stop?: HTMLButtonElement;
    open?: HTMLButtonElement;
    history: string[];
    hpos: number;
    log: string[]; // every line shown, for saving: the box itself keeps only the last 200
    busy?: {id: number; started: number};
}

/** The ▶ badge on the tab and the box's heading: the running script's (or console input's) time so far. */
function paintScript(t: Tab) {
    const s = t.script;
    t.run.hidden = !s;
    if (!s) return;
    const box = t.scriptBox;
    if (s.console) {
        const busy = box?.busy;
        const time = busy ? elapsedText(Date.now() - busy.started) : '';
        t.run.textContent = busy ? `▶ ${time}` : 'Lua';
        t.run.title = busy ? 'Lua 콘솔: 실행 중 (Ctrl+C: 중지)' : 'Lua 콘솔 (Ctrl+Shift+K)';
        if (box) {
            box.head.textContent = busy ? `▶ Lua 콘솔 · 실행 중 ${time}` : 'Lua 콘솔';
            box.stop!.hidden = !busy;
        }
        return;
    }
    const time = elapsedText(Date.now() - s.started);
    t.run.textContent = `▶ ${time}`;
    t.run.title = `Lua 스크립트 실행 중: ${s.name}\n탭 우클릭 → 스크립트 중지`;
    if (box) box.head.textContent = `▶ ${s.name} · ${time}`;
}

/**
 * The script box in the corner of the pane. Script messages go here, not
 * into the terminal, where they would mix with the program's own screen.
 */
function scriptBox(t: Tab) {
    if (t.scriptBox) return t.scriptBox;
    const el = document.createElement('div');
    el.className = 'script-box';
    const top = document.createElement('div');
    top.className = 'top';
    const head = document.createElement('span');
    const x = document.createElement('button');
    x.type = 'button';
    x.textContent = '✕';
    x.title = '닫기';
    x.addEventListener('click', () => closeConsole(t));
    top.append(head, x);
    const body = document.createElement('div');
    body.className = 'lines';
    el.append(top, body);
    el.addEventListener('mousedown', ev => ev.stopPropagation()); // keep the terminal's selection
    t.pane.appendChild(el);
    t.scriptBox = {el, head, body, timer: 0, history: [], hpos: 0, log: []};
    return t.scriptBox;
}

// ---- Lua console ----

let evalSeq = 0;

/** Ctrl+Shift+K / tab menu: opens the tab's Lua console, or closes it if it is open. */
export function showConsole(t: Tab) {
    if (t.script?.console) return closeConsole(t);
    runScript(t, () => StartConsole(t.id));
}

/** Closes the script box; a console ends with it, stopping any code it runs. */
function closeConsole(t: Tab) {
    if (t.script?.console) StopScript(t.id);
    closeScriptBox(t);
    focusActive();
}

/** Turns the script box into a console: an input line under the output. */
function consoleBox(t: Tab) {
    const box = scriptBox(t);
    if (box.input) return box;
    box.el.classList.add('console');
    const grip = document.createElement('div');
    grip.className = 'grip';
    grip.title = '끌어서 높이 조절';
    grip.addEventListener('mousedown', ev => resizeConsole(box.el, ev));
    box.el.prepend(grip);
    if (settings.consoleHeight) box.el.style.height = `${settings.consoleHeight}px`;
    const stop = document.createElement('button');
    stop.type = 'button';
    stop.className = 'stop';
    stop.textContent = '중지';
    stop.title = 'Ctrl+C';
    stop.hidden = true;
    stop.addEventListener('click', () => ConsoleInterrupt(t.id));
    const save = document.createElement('button');
    save.type = 'button';
    save.className = 'stop';
    save.textContent = '저장';
    save.title = '입력·출력을 파일로 저장 (Ctrl+S)';
    save.addEventListener('click', () => consoleSave(box));
    const open = document.createElement('button');
    open.type = 'button';
    open.className = 'stop';
    open.textContent = '열기';
    open.title = 'Lua 파일을 입력창으로 불러오기 (Ctrl+O)';
    open.addEventListener('click', () => consoleOpen(box));
    box.head.after(stop, open, save);
    const input = document.createElement('textarea');
    input.className = 'input';
    input.rows = 1;
    input.spellcheck = false;
    input.placeholder = 'Lua · Enter 실행 · Shift+Enter 줄바꿈 · ↑↓ 이전 입력 · Ctrl+O 열기 · Ctrl+S 저장 · Esc 터미널로';
    input.addEventListener('input', () => fitInput(input));
    input.addEventListener('keydown', ev => consoleKey(t, box, ev));
    box.el.appendChild(input);
    box.input = input;
    requestAnimationFrame(() => consoleMinHeight(box.el));
    box.stop = stop;
    box.open = open;
    return box;
}

/** Drags the console's top edge: up makes it taller, up to 80% of the pane (the CSS max-height). The height is kept for next time. */
function resizeConsole(el: HTMLElement, ev: MouseEvent) {
    if (ev.button !== 0) return;
    ev.preventDefault();
    const startY = ev.clientY;
    const startH = el.offsetHeight;
    const maxH = el.parentElement!.clientHeight * 0.8;
    const move = (e: MouseEvent) => {
        el.style.height = `${Math.min(maxH, startH + startY - e.clientY)}px`; // the CSS min-height keeps the input whole
    };
    const up = () => {
        window.removeEventListener('mousemove', move);
        window.removeEventListener('mouseup', up);
        if (el.isConnected) saveSettings(s => s.consoleHeight = el.offsetHeight); // the box may have closed mid-drag
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
}

function fitInput(input: HTMLTextAreaElement) {
    input.style.height = 'auto';
    input.style.height = `${Math.min(input.scrollHeight, 120)}px`;
    input.style.overflowY = input.scrollHeight > 120 ? 'auto' : 'hidden';
    consoleMinHeight(input.parentElement as HTMLElement);
}

/** The console can't get smaller than its heading, the input line and three lines of output. */
function consoleMinHeight(el: HTMLElement) {
    const part = (sel: string) => (el.querySelector(sel) as HTMLElement | null)?.offsetHeight ?? 0;
    const lines = el.querySelector('.lines') as HTMLElement;
    const linesMin = parseFloat(getComputedStyle(lines).minHeight) + 8; // + its padding
    el.style.minHeight = `${part('.top') + part('.input') + linesMin + 2}px`; // + the border
}

function consoleKey(t: Tab, box: ScriptBox, ev: KeyboardEvent) {
    const input = box.input!;
    const lines = input.value.split('\n');
    const caretLine = input.value.slice(0, input.selectionStart).split('\n').length - 1;
    if (ev.key === 'Enter' && !ev.shiftKey) {
        ev.preventDefault();
        consoleRun(t, box);
    } else if (ev.key === 'Escape') {
        ev.preventDefault();
        focusActive();
    } else if (ev.key.toLowerCase() === 's' && ev.ctrlKey && !ev.shiftKey && !ev.altKey) {
        ev.preventDefault();
        consoleSave(box);
    } else if (ev.key.toLowerCase() === 'o' && ev.ctrlKey && !ev.shiftKey && !ev.altKey) {
        ev.preventDefault();
        consoleOpen(box);
    } else if (ev.key === 'c' && ev.ctrlKey && !ev.shiftKey && box.busy && input.selectionStart === input.selectionEnd) {
        ev.preventDefault();
        ConsoleInterrupt(t.id);
    } else if (ev.key === 'ArrowUp' && caretLine === 0 && box.hpos > 0) {
        ev.preventDefault();
        box.hpos--;
        input.value = box.history[box.hpos];
        fitInput(input);
    } else if (ev.key === 'ArrowDown' && caretLine === lines.length - 1 && box.hpos < box.history.length) {
        ev.preventDefault();
        box.hpos++;
        input.value = box.history[box.hpos] ?? '';
        fitInput(input);
    }
}

/** Puts a Lua file's text in the input line, to look at or edit before Enter runs it. */
async function consoleOpen(box: ScriptBox) {
    if (!box.input) return;
    try {
        const f = await ConsoleOpen();
        const input = box.input; // the console may have ended while the dialog was open
        if (!f.name || !input) return box.input?.focus();
        input.value = f.text.replace(/\n+$/, '');
        box.hpos = box.history.length;
        fitInput(input);
        input.setSelectionRange(0, 0);
        input.scrollTop = 0;
        if (!input.value) toast(`빈 파일입니다: ${f.name}`);
    } catch (e) {
        toast(`불러오지 못했습니다: ${e}`);
    }
    box.input?.focus();
}

async function consoleSave(box: ScriptBox) {
    if (!box.log.length) return toast('저장할 내용이 없습니다');
    try {
        const path = await ConsoleSave(box.log.join('\n') + '\n');
        if (path) toast(`저장했습니다: ${path}`);
    } catch (e) {
        toast(`저장하지 못했습니다: ${e}`);
    }
    box.input?.focus();
}

async function consoleRun(t: Tab, box: ScriptBox) {
    const input = box.input!;
    const code = input.value;
    if (!code.trim()) return;
    if (box.busy) return toast('앞의 입력이 실행 중입니다 (Ctrl+C: 중지)');
    if (box.history[box.history.length - 1] !== code) box.history.push(code);
    if (box.history.length > 100) box.history.shift();
    box.hpos = box.history.length;
    input.value = '';
    fitInput(input);
    code.split('\n').forEach((line, i) => scriptLine(t, (i ? '  ' : '> ') + line, 'in'));
    const id = ++evalSeq;
    box.busy = {id, started: Date.now()};
    paintScript(t);
    try {
        await ConsoleEval(t.id, id, code);
    } catch (e) {
        box.busy = undefined;
        scriptLine(t, String(e), 'err');
        paintScript(t);
    }
}

EventsOn('script:result', (id: number, req: number, value: string, err: string) => {
    const t = tabs.get(id);
    const box = t?.scriptBox;
    if (!t || !box || box.busy?.id !== req) return;
    box.busy = undefined;
    if (err) scriptLine(t, err, 'err');
    else if (value) scriptLine(t, value, 'val');
    paintScript(t);
});

function closeScriptBox(t: Tab) {
    if (!t.scriptBox) return;
    clearTimeout(t.scriptBox.timer);
    t.scriptBox.el.remove();
    t.scriptBox = undefined;
    if (t === active) focusActive();
}

function scriptLine(t: Tab, text: string, cls = '') {
    const box = scriptBox(t);
    for (const line of text.split('\n')) {
        const div = document.createElement('div');
        if (cls) div.className = cls;
        div.textContent = line;
        box.body.appendChild(div);
        box.log.push(line);
    }
    if (box.log.length > 22000) box.log.splice(0, box.log.length - 20000); // trimmed in batches, not a line at a time
    while (box.body.childElementCount > 200) box.body.firstElementChild!.remove();
    box.body.scrollTop = box.body.scrollHeight;
}

window.setInterval(() => {
    for (const t of tabs.values()) if (t.script) paintScript(t);
}, 1000);

EventsOn('script:start', (id: number, s: {name: string; started: number; console: boolean}) => {
    const t = tabs.get(id);
    if (!t) return;
    t.script = {name: s.name, started: s.started, console: s.console};
    closeScriptBox(t); // a new box: the last script's may be a console, or the other way round
    const box = s.console ? consoleBox(t) : scriptBox(t);
    paintScript(t);
    if (s.console) {
        scriptLine(t, 'send · expect · sleep · screen · print 사용 가능. 식을 입력하면 값을 보여 줍니다 (Lua 5.1).', 'hint');
        box.input!.focus();
    }
});
EventsOn('script:end', (id: number, ms: number, msg: string, stopped: boolean) => {
    const t = tabs.get(id);
    if (!t) return;
    const name = t.script?.name ?? '스크립트';
    t.script = undefined;
    paintScript(t);
    const time = elapsedText(ms);
    const box = scriptBox(t);
    if (box.input) {
        // The console is over: no more input.
        // Save stays: what was typed and shown can still be kept.
        box.input.remove();
        box.stop?.remove();
        box.open?.remove();
        box.input = box.stop = box.open = box.busy = undefined;
        box.el.style.minHeight = '';
    }
    if (stopped) {
        box.el.className = 'script-box stopped';
        box.head.textContent = `■ ${name} · 멈춤 · ${time}`;
        scriptLine(t, msg);
        box.timer = window.setTimeout(() => closeScriptBox(t), 5000);
    } else if (msg) {
        // Errors stay until closed: the line number is needed to fix the script.
        box.el.className = 'script-box failed';
        box.head.textContent = `✖ ${name} · ${time}`;
        scriptLine(t, msg, 'err');
    } else {
        box.el.className = 'script-box done';
        box.head.textContent = `✔ ${name} · 끝 · ${time}`;
        box.timer = window.setTimeout(() => closeScriptBox(t), 5000);
    }
});
EventsOn('script:print', (id: number, text: string) => {
    const t = tabs.get(id);
    if (t) scriptLine(t, text);
});
// screen(): the text the tab shows now.
EventsOn('script:screen', (id: number, req: number) => {
    const t = tabs.get(id);
    if (!t) return;
    const b = t.term.buffer.active;
    const lines: string[] = [];
    for (let y = b.viewportY; y < b.viewportY + t.term.rows; y++) {
        lines.push(b.getLine(y)?.translateToString(true) ?? '');
    }
    const text = lines.join('\n').replace(/\s+$/, '');
    dbg(`tab ${id} screen() #${req}: ${b.type} buffer, viewport ${b.viewportY} of ${b.baseY} (scrolled up ${b.baseY - b.viewportY}), ${text.length} chars`);
    ScriptScreen(id, req, text);
});
