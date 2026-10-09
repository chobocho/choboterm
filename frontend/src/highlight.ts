// Keyword highlighting: words such as "error" or "warning" in the output get
// a coloured background. It is drawn over the screen with xterm decorations;
// the output itself is left alone, so programs keep their own colours.
//
// Only the screen area can still change, so each pass re-scans the lines
// that were on screen at the last pass up to the bottom of the screen now;
// lines further up are scrollback and keep their decorations until they
// scroll out. Full-screen programs (the alternate screen) are not scanned.

import {IDecoration, IMarker, Terminal} from '@xterm/xterm';
import {settings} from './settings';

interface Rule {
    re: RegExp;
    bg: string;
    fg: string;
}

const COLORS = [
    {bg: '#c0392b', fg: '#ffffff'}, // red
    {bg: '#e0b000', fg: '#000000'}, // yellow
    {bg: '#1e8449', fg: '#ffffff'}, // green
];

const MAX_SCAN = 5000; // lines per pass when a lot of output came at once
const MAX_PER_LINE = 20;

let rules: Rule[] = [];

/** Splits "a, b, 오류" into words (commas or new lines). */
export function splitWords(s: string): string[] {
    return s.split(/[,\n]/).map(w => w.trim()).filter(Boolean);
}

function escape(s: string) {
    return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** Rebuilds the rules from the settings. ASCII words match whole words only, case-insensitively. */
export function loadRules() {
    const h = settings.highlight;
    rules = [];
    if (!h?.on) return;
    [h.red, h.yellow, h.green].forEach((words, i) => {
        const parts = splitWords(words ?? '').map(w =>
            /^[\w-]+$/.test(w) ? `(?<![\\w-])${escape(w)}(?![\\w-])` : escape(w));
        if (parts.length) rules.push({re: new RegExp(parts.join('|'), 'gi'), ...COLORS[i]});
    });
}

interface Marked {
    marker: IMarker;
    decos: IDecoration[];
}

export interface Highlighter {
    schedule(): void;
    reset(): void;
}

// About markers: xterm keeps decorations in a list sorted by their marker's
// line, and a disposed marker's line is -1. When a marker is disposed while
// its decoration is in that list (xterm itself does it when a line is erased,
// e.g. by "clear"), the decoration can't be found to be removed: a -1 stays
// in the middle of the list and lookups for other lines fail (marks vanish).
// So markers that had decorations are never disposed here (a line keeps its
// marker for the next scan), and repairList() drops such leftovers.

interface SortedDecorations {
    _array?: {isDisposed: boolean; marker: IMarker}[];
    _flushCleanupInserted?: () => void;
    _flushCleanupDeleted?: () => void;
}

/** Removes disposed decorations xterm failed to remove (internal API, skipped if it changed). */
function repairList(term: Terminal) {
    const list: SortedDecorations | undefined = (term as any)._core?._decorationService?._decorations;
    if (!list || !Array.isArray(list._array) || !list._flushCleanupInserted || !list._flushCleanupDeleted) return;
    list._flushCleanupInserted();
    list._flushCleanupDeleted();
    const a = list._array;
    if (a.some(d => d.isDisposed || d.marker.line < 0)) list._array = a.filter(d => !d.isDisposed && d.marker.line >= 0);
}

export function attachHighlight(term: Terminal): Highlighter {
    let live: Marked[] = []; // lines that may still change (on screen)
    let done: Marked[] = []; // lines in the scrollback, kept until they scroll out
    let top: IMarker | undefined; // the top of the screen at the last pass (never decorated)
    let timer = 0;

    /** Marks the matches on buffer line y (absolute), reusing the line's marker if it has one. */
    function scanLine(y: number, cursorAbs: number, marker: IMarker | undefined): Marked | undefined {
        const line = term.buffer.active.getLine(y);
        const text = line?.translateToString(true) ?? '';
        if (!line || !rules.some(r => (r.re.lastIndex = 0, r.re.test(text)))) return marker && {marker, decos: []};
        // String index → cell column (wide characters take two cells).
        const col: number[] = [];
        let s = '';
        for (let x = 0; x < line.length; x++) {
            const cell = line.getCell(x);
            if (!cell || cell.getWidth() === 0) continue;
            const ch = cell.getChars() || ' ';
            for (let i = 0; i < ch.length; i++) col.push(x);
            s += ch;
        }
        col.push(line.length);
        const decos: IDecoration[] = [];
        for (const r of rules) {
            r.re.lastIndex = 0;
            for (let m; (m = r.re.exec(s)) && decos.length < MAX_PER_LINE;) {
                if (!m[0]) {
                    r.re.lastIndex++;
                    continue;
                }
                marker ??= term.registerMarker(y - cursorAbs);
                const x = col[m.index];
                const end = col[m.index + m[0].length] ?? line.length;
                const d = term.registerDecoration({marker, x, width: Math.max(1, end - x), backgroundColor: r.bg, foregroundColor: r.fg});
                if (d) decos.push(d);
            }
        }
        return marker && {marker, decos};
    }

    function pass() {
        timer = 0;
        repairList(term);
        const buf = term.buffer.active;
        if (buf.type === 'alternate') return;
        const cursorAbs = buf.baseY + buf.cursorY;
        const screenTop = buf.baseY;
        const bottom = screenTop + term.rows - 1;
        let from = top && !top.isDisposed ? Math.min(top.line, screenTop) : screenTop;
        from = Math.max(from, bottom - MAX_SCAN);
        // Lines from `from` down are scanned again: drop their marks, keep their markers.
        const reuse = new Map<number, IMarker>();
        for (const m of live) {
            if (m.marker.isDisposed) continue;
            if (m.marker.line < from) {
                if (m.decos.length) done.push(m); // scrollback now: it stays as it is
            } else {
                m.decos.forEach(d => d.dispose());
                reuse.set(m.marker.line, m.marker);
            }
        }
        live = [];
        for (let y = from; y <= bottom; y++) {
            const m = rules.length ? scanLine(y, cursorAbs, reuse.get(y)) : reuse.has(y) ? {marker: reuse.get(y)!, decos: []} : undefined;
            if (!m) continue;
            if (y >= screenTop) live.push(m); // still on screen: may change
            else if (m.decos.length) done.push(m);
        }
        top?.dispose();
        top = term.registerMarker(screenTop - cursorAbs);
        if (done.length > 2000) done = done.filter(m => !m.marker.isDisposed); // scrolled out
    }

    term.onResize(() => schedule());
    term.buffer.onBufferChange(() => schedule());

    function schedule() {
        if (!timer) timer = window.setTimeout(pass, 60);
    }

    return {
        schedule,
        /** Rules changed: drop all marks and scan the screen again. */
        reset() {
            for (const m of done) m.decos.forEach(d => d.dispose());
            done = [];
            top?.dispose();
            top = undefined; // live markers are reused by the next pass
            schedule();
        },
    };
}
