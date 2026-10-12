// Split panes: the layout of the panes in a tab, the dividers, and the keys.

import {active, focusActive, Group, panesOf, SplitDir} from './tabstate';

export function paintGroup(g: Group) {
    const multi = g.box.querySelectorAll('.term').length > 1;
    g.chip.classList.toggle('multi', multi);
    g.box.classList.toggle('multi', multi);
}

/** Puts pane next to old (right of it for row, below it for col), halving old's space. */
export function splitPane(old: HTMLElement, pane: HTMLElement, dir: SplitDir) {
    const split = document.createElement('div');
    split.className = `split ${dir}`;
    split.style.flex = old.style.flex; // take old's place in its parent
    old.replaceWith(split);
    old.style.flex = '';
    const gutter = document.createElement('div');
    gutter.className = 'gutter';
    gutter.title = '끌어서 크기 조절 · 더블클릭: 반반';
    gutter.addEventListener('mousedown', ev => dragGutter(gutter, ev));
    gutter.addEventListener('dblclick', () => {
        (gutter.previousElementSibling as HTMLElement).style.flex = '';
        (gutter.nextElementSibling as HTMLElement).style.flex = '';
    });
    split.append(old, gutter, pane);
}

/** Takes pane out of its split; the part left over takes the split's place. */
export function unsplitPane(pane: HTMLElement) {
    const split = pane.parentElement;
    pane.remove();
    if (!split?.classList.contains('split')) return;
    const rest = Array.from(split.children).find(el => !el.classList.contains('gutter')) as HTMLElement;
    rest.style.flex = split.style.flex;
    split.replaceWith(rest);
}

function dragGutter(gutter: HTMLElement, ev: MouseEvent) {
    if (ev.button !== 0) return;
    ev.preventDefault();
    const split = gutter.parentElement!;
    const a = gutter.previousElementSibling as HTMLElement;
    const b = gutter.nextElementSibling as HTMLElement;
    const row = split.classList.contains('row');
    document.body.classList.add(row ? 'resizing-row' : 'resizing-col');
    const move = (e: MouseEvent) => {
        const r = split.getBoundingClientRect();
        const pos = row ? (e.clientX - r.left) / r.width : (e.clientY - r.top) / r.height;
        const f = Math.max(0.1, Math.min(0.9, pos));
        a.style.flex = `${f} 1 0`;
        b.style.flex = `${1 - f} 1 0`;
    };
    const up = () => {
        window.removeEventListener('mousemove', move);
        window.removeEventListener('mouseup', up);
        document.body.classList.remove('resizing-row', 'resizing-col');
        focusActive();
    };
    window.addEventListener('mousemove', move);
    window.addEventListener('mouseup', up);
}

export const ARROWS: Record<string, [number, number]> = {ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1]};

/**
 * Ctrl+Shift+\ (a "|": side by side) and Ctrl+Shift+- (one above the other) split
 * the pane, as in tmux. By physical key, so ₩ on Korean keyboards works too.
 */
export function splitKey(ev: KeyboardEvent): SplitDir | undefined {
    if (!ev.ctrlKey || !ev.shiftKey || ev.altKey) return undefined;
    if (ev.code === 'Backslash' || ev.code === 'IntlYen') return 'row';
    if (ev.code === 'Minus') return 'col';
    return undefined;
}

/** Alt+arrow moves between panes, but only in a split tab; otherwise the program gets it. */
export function isPaneKey(ev: KeyboardEvent): boolean {
    return ev.altKey && !ev.ctrlKey && !ev.shiftKey && ev.key in ARROWS &&
        !!active && panesOf(active.group).length > 1;
}
