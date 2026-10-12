// Copying the selection (mouse or long touch) and pasting into a tab.

import {dbg, debugOn} from './debug';
import {confirmPaste} from './paste';
import {ClipboardGetText, ClipboardSetText} from '../wailsjs/runtime/runtime';
import {active, focusActive, Tab} from './tabstate';

export function touchCell(t: Tab, x: number, y: number, clamp = false): {col: number; row: number} | undefined {
    const screen = t.term.element?.querySelector<HTMLElement>('.xterm-screen');
    if (!screen || !t.term.cols || !t.term.rows) return;
    const rect = screen.getBoundingClientRect();
    if (rect.width <= 0 || rect.height <= 0 || (!clamp && (x < rect.left || x >= rect.right || y < rect.top || y >= rect.bottom))) return;
    const col = Math.max(0, Math.min(t.term.cols - 1, Math.floor((x - rect.left) / rect.width * t.term.cols)));
    const row = Math.max(0, Math.min(t.term.rows - 1, Math.floor((y - rect.top) / rect.height * t.term.rows)));
    return {col, row: t.term.buffer.active.viewportY + row};
}

export function selectTouchWord(t: Tab, col: number, row: number): {start: number; end: number} | undefined {
    const buffer = t.term.buffer.active;
    const line = buffer.getLine(row);
    if (!line) return;
    const isWordCell = (x: number) => {
        const cell = line.getCell(x);
        if (!cell) return false;
        const chars = cell.getChars();
        return cell.getWidth() === 0 || (chars !== '' && !/^\\s+$/u.test(chars));
    };
    // A wide character's second cell has no chars of its own.
    if (!isWordCell(col) && col > 0 && line.getCell(col)?.getWidth() === 0) col--;
    if (!isWordCell(col)) return {start: row * t.term.cols + col, end: row * t.term.cols + col};
    let left = col;
    let right = col;
    while (left > 0 && isWordCell(left - 1)) left--;
    while (right + 1 < t.term.cols && isWordCell(right + 1)) right++;
    return {start: row * t.term.cols + left, end: row * t.term.cols + right};
}

export function extendTouchSelection(t: Tab, anchor: {start: number; end: number}, col: number, row: number) {
    const point = row * t.term.cols + col;
    const start = point < anchor.start ? point : anchor.start;
    const end = point > anchor.end ? point : anchor.end;
    t.term.select(start % t.term.cols, Math.floor(start / t.term.cols), end - start + 1);
}

export function copySelection(t: Tab) {
    const text = t.term.getSelection();
    if (text) {
        ClipboardSetText(text)
            .then(async ok => {
                dbg(`clipboard set ${text.length} chars: ${ok}`);
                // Read it back: did the clipboard really take it?
                if (debugOn()) dbg(`clipboard now ${(await ClipboardGetText()) === text ? 'matches' : 'DIFFERS'}`);
            })
            .catch(e => dbg(`clipboard set failed: ${e}`));
    }
}

export async function pasteText(t: Tab, text: string) {
    if (!text || t.state !== 'on') return;
    const ok = await confirmPaste(text);
    if (ok && t.state === 'on') t.term.paste(text);
    if (t === active) focusActive();
}

export async function pasteClipboard(t: Tab) {
    pasteText(t, await ClipboardGetText());
}
