// Diagnostics for CHOBOTERM_DEBUG=1 or debug.on next to the exe: lines go to debug.log next to the exe via the Go side.
// Messages logged before the Go side answers are buffered, then sent or dropped.

import {DebugEnabled, DebugLog} from '../wailsjs/go/main/App';

let enabled: boolean | undefined;
const early: string[] = [];

export function dbg(msg: string) {
    if (enabled) DebugLog(msg);
    else if (enabled === undefined && early.length < 500) early.push(`(early) ${msg}`);
}

export function debugOn() {
    return enabled === true;
}

function str(v: unknown): string {
    if (v instanceof Error) return `${v.name}: ${v.message}\n${v.stack ?? ''}`;
    if (typeof v === 'string') return v;
    try {
        return JSON.stringify(v);
    } catch {
        return String(v);
    }
}

// Hooked right away so errors during startup are captured too.
for (const level of ['warn', 'error'] as const) {
    const orig = console[level].bind(console);
    console[level] = (...args: unknown[]) => {
        orig(...args);
        dbg(`console.${level}: ${args.map(str).join(' ')}`);
    };
}
window.addEventListener('error', ev => dbg(`window.error: ${ev.message} at ${ev.filename}:${ev.lineno}:${ev.colno} ${str(ev.error)}`));
window.addEventListener('unhandledrejection', ev => dbg(`unhandledrejection: ${str(ev.reason)}`));
document.addEventListener('keydown', ev => {
    const a = document.activeElement;
    dbg(`keydown ${ev.key} code=${ev.code} focus=${a?.tagName}.${(a as HTMLElement | null)?.className ?? ''}`);
}, true);
document.addEventListener('mousedown', ev => {
    const el = ev.target as HTMLElement | null;
    dbg(`mousedown ${ev.button} at ${ev.clientX},${ev.clientY} on ${el?.tagName}.${el?.className ?? ''}`);
}, true);

// Heartbeat: frames drawn and timer lag per second. Gaps in the log mean the page froze.
let frames = 0;
const frame = () => {
    frames++;
    requestAnimationFrame(frame);
};
requestAnimationFrame(frame);
let last = performance.now();
setInterval(() => {
    const now = performance.now();
    dbg(`heartbeat frames=${frames} lag=${Math.round(now - last - 1000)}ms visible=${document.visibilityState}`);
    frames = 0;
    last = now;
}, 1000);
try {
    new PerformanceObserver(list => {
        for (const e of list.getEntries()) dbg(`longtask ${Math.round(e.duration)}ms`);
    }).observe({type: 'longtask', buffered: true});
} catch {
    // Not supported.
}

DebugEnabled().then(on => {
    enabled = on;
    if (on) {
        for (const m of early) DebugLog(m);
        dbg(`ua ${navigator.userAgent} dpr=${devicePixelRatio}`);
    }
    early.length = 0;
}, () => {
    enabled = false;
    early.length = 0;
});
