// Tabs and their panes: creating, showing, closing, splitting, and opening
// the same server again.

import {dbg} from './debug';
import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {WebLinksAddon} from '@xterm/addon-web-links';
import {fontFamily, loadUnicodeWidths, NARROW} from './cjkwidth';
import {forwardsTabClosed} from './forwards';
import {DockerMode, DockerSource} from './containers';
import {ask, askOpen} from './dialog';
import {attachHighlight} from './highlight';
import {tr} from './i18n';
import {attachSearch, closeSearch, switchSearch} from './search';
import {settings} from './settings';
import {filesOpen, forgetFiles, showFilesFor} from './files';
import {CloseTab, Resize} from '../wailsjs/go/app/App';
import {terminal} from '../wailsjs/go/models';
import {BrowserOpenURL, ClipboardSetText} from '../wailsjs/runtime/runtime';
import {isWebKitGTK, webPage} from './platform';
import {active, fitPane, focusActive, Group, orderedTabs, panesOf, setActive, SplitDir, Tab, tabs, tabsEl, termsEl} from './tabstate';
import {notice, reconnectHint, toast} from './notify';
import {updateTitle, version} from './title';
import {applyLook, chooseRenderer, lookOptions, paintMargin} from './look';
import {copySelection, extendTouchSelection, pasteClipboard, pasteText, selectTouchWord, touchCell} from './clipboard';
import {paintGroup, splitPane, unsplitPane} from './split';
import {isAppShortcut} from './shortcuts';
import {cancelRetry, connectTab, openFilesFor, retryNow, sendInput, startDocker} from './connection';
import {hideDialog, loadDialog, openDialog, saveDialog} from './connectdialog';
import {showMenu} from './tabmenu';
import {disposeMemo} from './memo';

// In a browser, this page's tab ids start at its base (see platform.ts).
let nextId = (webPage?.base ?? 0) + 1;
let dragging: Tab | undefined;

function welcome(t: Tab) {
    t.term.write(`choboterm V${version}\r\n`);
    t.term.write(`\x1b[90m${tr('Enter: 접속 창 · Ctrl+Shift+T: 새 탭 · Ctrl+Tab: 탭 전환 · Ctrl+Shift+W: 탭 닫기')}\r\n`);
    t.term.write(`${tr('Ctrl+Shift+E: UTF-8 ↔ EUC-KR · Ctrl+Shift+F: 파일 전송 · Ctrl+Shift+D: 연결 끊기')}\x1b[0m\r\n`);
}

/** Creates a tab, or with split a new pane next to split.from in its tab. */
export function createTab(split?: {from: Tab; dir: SplitDir}): Tab {
    const id = nextId++;

    const pane = document.createElement('div');
    pane.className = 'term';
    let group: Group;
    if (split) {
        group = split.from.group;
        splitPane(split.from.pane, pane, split.dir);
    } else {
        const box = document.createElement('div');
        box.className = 'group';
        box.hidden = true;
        box.appendChild(pane);
        termsEl.appendChild(box);
        const chip = document.createElement('div');
        chip.className = 'tabgroup';
        tabsEl.appendChild(chip);
        group = {box, chip};
    }

    const term = new Terminal({
        fontFamily: fontFamily(settings.fontUtf8, false),
        fontSize: settings.fontSize,
        allowProposedApi: true,
        ...lookOptions(),
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    const search = attachSearch(term);
    // Ctrl+click opens a URL; a plain click stays free for selecting.
    term.loadAddon(new WebLinksAddon((ev, uri) => {
        if (ev.ctrlKey) BrowserOpenURL(uri);
    }, {
        hover: () => (pane.title = 'Ctrl+클릭: 브라우저에서 열기'),
        leave: () => (pane.title = ''),
    }));
    loadUnicodeWidths(term);
    term.unicode.activeVersion = NARROW;
    term.attachCustomKeyEventHandler(ev => !isAppShortcut(ev));
    dbg(`tab ${id} open`);
    term.open(pane);

    // Some Android browser/IME combinations (notably Samsung Internet with
    // Samsung Keyboard) emit Backspace while composing Hangul. Let the IME
    // handle it instead of xterm ending composition and forwarding stale input.
    if (/Android/i.test(navigator.userAgent)) {
        let composing = false;
        pane.addEventListener('compositionstart', () => { composing = true; }, true);
        pane.addEventListener('compositionend', () => { composing = false; }, true);
        pane.addEventListener('keydown', ev => {
            if (composing && (ev.key === 'Backspace' || ev.keyCode === 8)) ev.stopImmediatePropagation();
        }, true);
    }

    // WebKitGTK (the Linux window) ends every Hangul syllable twice: it
    // deletes the composed text and fires compositionend with no data, then
    // inserts the text again (insertFromComposition, with no compositionstart)
    // and fires compositionend once more with the syllable. xterm takes the
    // first end as the real one, so the keydown (229) and text that follow
    // reach it outside a composition: its textarea diff then sends the
    // syllable again, or everything typed since the last Enter ("하이 하이"
    // arrives as "하이 이 하이 하..."). Holding the empty end back keeps xterm
    // composing until the second end, which carries the text, as in Chrome.
    // A composition really cancelled (Backspace over its last jamo) has no
    // second end: the held one is passed on before the next other key.
    if (isWebKitGTK) {
        const textarea = pane.querySelector('textarea')!;
        let held = false; // an empty compositionend kept from xterm
        let passing = false; // dispatching the held one: let it through
        const release = () => {
            if (!held) return;
            held = false;
            dbg(`tab ${id} IME: pass on the held compositionend`);
            passing = true;
            textarea.dispatchEvent(new CompositionEvent('compositionend', {data: ''}));
            passing = false;
        };
        pane.addEventListener('compositionstart', () => { held = false; }, true);
        pane.addEventListener('compositionend', ev => {
            if (!passing && ev.data === '' && !held) {
                held = true;
                ev.stopImmediatePropagation();
                return;
            }
            held = false;
        }, true);
        pane.addEventListener('keydown', ev => { if (ev.keyCode !== 229) release(); }, true);
        pane.addEventListener('focusout', release, true);
    }

    // The Termux build: its IMEs can emit intermediate `input` events holding
    // the whole Hangul composition. xterm sends the composition itself on
    // compositionend, so those would repeat the typed syllables.
    if (import.meta.env.VITE_TERMUX === '1') {
        let composing = false;
        let compositionEnd = 0;
        pane.addEventListener('compositionstart', () => {
            composing = true;
            compositionEnd++;
        }, true);
        pane.addEventListener('compositionend', () => {
            // Some IMEs dispatch the final input event after compositionend.
            // Keep filtering until this event turn finishes so xterm doesn't send it
            // again after CompositionHelper has handled the committed text.
            const ended = ++compositionEnd;
            setTimeout(() => {
                if (compositionEnd === ended) composing = false;
            }, 0);
        }, true);
        pane.addEventListener('input', ev => {
            if (composing || (ev as InputEvent).isComposing) ev.stopImmediatePropagation();
        }, true);
    }

    term.onData(d => dbg(`tab ${id} onData ${JSON.stringify(d)}`));
    term.onRender(({start, end}) => dbg(`tab ${id} render rows ${start}-${end}`));

    const el = document.createElement('div');
    el.className = 'tab';
    el.draggable = true;
    el.dataset.id = String(id);
    const dot = document.createElement('span');
    dot.className = 'dot';
    const label = document.createElement('span');
    label.className = 'label';
    label.textContent = '새 탭';
    const x = document.createElement('button');
    x.className = 'x';
    x.type = 'button';
    x.title = '탭 닫기 (Ctrl+Shift+W)';
    x.textContent = '✕';
    const run = document.createElement('span');
    run.className = 'run';
    run.hidden = true;
    el.append(dot, label, run, x);
    if (split) split.from.el.after(el);
    else group.chip.appendChild(el);

    const hl = attachHighlight(term);
    const t: Tab = {id, group, term, fit, search, hl, pane, el, label, run, state: 'idle', proto: '', encoding: 'UTF-8', outbox: '', sending: false};
    tabs.set(id, t);
    chooseRenderer(t);
    applyLook(t);
    paintGroup(group);
    // Window size, splits and dragged dividers all change the pane's size.
    new ResizeObserver(() => fitPane(t)).observe(pane);

    // Programs that use the mouse themselves (edit, vim, tmux) copy with OSC 52:
    // "c;<base64>". Only setting is honored; a program can't read the clipboard ("?").
    term.parser.registerOscHandler(52, data => {
        const b64 = data.slice(data.indexOf(';') + 1);
        if (b64 === '?' || b64.length > 4_000_000) return true;
        try {
            const text = new TextDecoder().decode(Uint8Array.from(atob(b64), c => c.charCodeAt(0)));
            dbg(`tab ${t.id} osc 52: ${text.length} chars`);
            if (text) ClipboardSetText(text).catch(e => dbg(`clipboard set failed: ${e}`));
        } catch (e) {
            dbg(`tab ${t.id} osc 52: bad data (${e})`);
        }
        return true;
    });

    // Shells can report their folder as OSC 7 file://host/path (e.g. with vte.sh).
    term.parser.registerOscHandler(7, data => {
        const m = /^file:\/\/[^/]*(\/.*)$/.exec(data);
        if (m) {
            try {
                t.osc7 = decodeURIComponent(m[1]);
            } catch {
                t.osc7 = m[1];
            }
        }
        return true;
    });
    term.onTitleChange(title => (t.title = title));

    term.onData(data => {
        if (t.state === 'on') sendInput(t, data);
        else if (t.reconnecting) return;
        else if (t.retry) {
            if (data === '\r') retryNow(t);
            else if (data === '\x1b') {
                cancelRetry(t);
                reconnectHint(t, '자동 재접속을 취소했습니다');
            }
        } else if (data === '\r') t.docker ? reconnect(t) : openDialog(t);
    });
    term.onResize(({cols, rows}) => {
        if (t.state === 'on') Resize(t.id, cols, rows);
    });

    // Clicking (or tabbing) into a pane of a split tab makes it the active one.
    pane.addEventListener('focusin', () => {
        if (active !== t && tabs.has(t.id)) activate(t);
    });

    // Like PuTTY: selecting with the mouse copies, right-click pastes.
    // The button may be let go outside the pane (dragging up past the top
    // selects from the scrollback), so the release is watched on the window.
    pane.addEventListener('mousedown', ev => {
        if (ev.button !== 0) return;
        const up = (e: MouseEvent) => {
            if (e.button !== 0) return;
            window.removeEventListener('mouseup', up, true);
            dbg(`tab ${t.id} (${t.proto}) mouseup: selection ${t.term.getSelection().length} chars, mouse mode ${term.modes.mouseTrackingMode}`);
            copySelection(t);
            // The program on the other side took the drag (edit, vim, tmux...): nothing was selected here.
            const dragged = Math.abs(e.clientX - ev.clientX) + Math.abs(e.clientY - ev.clientY) > 8;
            if (dragged && !ev.shiftKey && term.modes.mouseTrackingMode !== 'none' && !t.term.hasSelection()) {
                toast('프로그램이 마우스를 쓰는 중입니다. Shift+드래그로 선택 · 복사');
            }
        };
        window.addEventListener('mouseup', up, true);
    });
    term.onSelectionChange(() => dbg(`tab ${t.id} selection ${term.getSelection().length} chars`));
    // On Android, a long touch can produce a contextmenu event. Treating it as
    // a right-click here opens the paste prompt and interrupts touch selection.
    let lastTouchAt = 0;
    let touchTimer = 0;
    let touchStart: {x: number; y: number; id: number} | undefined;
    let touchAnchor: {start: number; end: number} | undefined;
    if (import.meta.env.VITE_TERMUX === '1') {
        pane.addEventListener('touchstart', ev => {
            lastTouchAt = Date.now();
            dbg(`tab ${t.id} touchstart: ${ev.touches.length} touch(es)`);
            if (ev.touches.length !== 1) {
                clearTimeout(touchTimer);
                touchTimer = 0;
                touchStart = undefined;
                touchAnchor = undefined;
                return;
            }
            const touch = ev.touches[0];
            touchStart = {x: touch.clientX, y: touch.clientY, id: touch.identifier};
            touchAnchor = undefined;
            clearTimeout(touchTimer);
            touchTimer = window.setTimeout(() => {
                touchTimer = 0;
                if (!touchStart) return;
                const pos = touchCell(t, touchStart.x, touchStart.y);
                if (!pos) return;
                touchAnchor = selectTouchWord(t, pos.col, pos.row);
                if (touchAnchor) dbg(`tab ${t.id} long touch: selected ${t.term.getSelection().length} chars`);
            }, 500);
        }, {passive: true});
        pane.addEventListener('touchmove', ev => {
            if (!touchStart) return;
            const touch = Array.from(ev.touches).find(x => x.identifier === touchStart!.id);
            if (!touch) return;
            if (!touchAnchor) {
                if (Math.abs(touch.clientX - touchStart.x) + Math.abs(touch.clientY - touchStart.y) > 12) {
                    clearTimeout(touchTimer);
                    touchTimer = 0;
                    dbg(`tab ${t.id} touch selection cancelled: moved before long press`);
                    touchStart = undefined;
                }
                return;
            }
            ev.preventDefault();
            const pos = touchCell(t, touch.clientX, touch.clientY, true);
            if (pos) extendTouchSelection(t, touchAnchor, pos.col, pos.row);
        }, {passive: false});
        pane.addEventListener('touchend', ev => {
            if (!touchStart || !Array.from(ev.changedTouches).some(x => x.identifier === touchStart!.id)) return;
            clearTimeout(touchTimer);
            touchTimer = 0;
            if (touchAnchor) {
                ev.preventDefault();
                dbg(`tab ${t.id} long touch end: selection ${t.term.getSelection().length} chars`);
                copySelection(t);
            }
            touchStart = undefined;
            touchAnchor = undefined;
        }, {passive: false});
        pane.addEventListener('touchcancel', () => {
            dbg(`tab ${t.id} touchcancel`);
            clearTimeout(touchTimer);
            touchTimer = 0;
            touchStart = undefined;
            touchAnchor = undefined;
        }, {passive: true});
    }
    pane.addEventListener('contextmenu', ev => {
        ev.preventDefault();
        if (import.meta.env.VITE_TERMUX === '1' && Date.now() - lastTouchAt < 1500) return;
        // Programs using the mouse (mc, htop...) get the click; Shift+right-click still pastes.
        if (term.modes.mouseTrackingMode !== 'none' && !ev.shiftKey) return;
        pasteClipboard(t);
    });
    // Ctrl+V / Shift+Insert go through the same multi-line check.
    pane.addEventListener('paste', ev => {
        const target = ev.target;
        if (t.scriptBox?.el.contains(target as Node)) return; // the Lua console's input pastes as text
        if (target instanceof HTMLInputElement ||
            (target instanceof HTMLTextAreaElement && !target.classList.contains('xterm-helper-textarea'))) return;
        ev.preventDefault();
        ev.stopPropagation();
        pasteText(t, ev.clipboardData?.getData('text/plain') ?? '');
    }, true);

    el.addEventListener('mousedown', ev => {
        if (ev.button === 0) activate(t);
    });
    el.addEventListener('auxclick', ev => {
        if (ev.button === 1) { // middle click closes, like browsers and MobaXterm
            ev.preventDefault();
            requestClose(t);
        }
    });
    el.addEventListener('contextmenu', ev => {
        ev.preventDefault();
        activate(t);
        showMenu(t, ev.clientX, ev.clientY);
    });
    x.addEventListener('mousedown', ev => ev.stopPropagation());
    x.addEventListener('click', () => requestClose(t));

    // Drag to reorder; a split tab moves as a whole.
    el.addEventListener('dragstart', ev => {
        dragging = t;
        t.group.chip.classList.add('dragging');
        ev.dataTransfer?.setData('text/plain', String(id));
    });
    el.addEventListener('dragend', () => {
        dragging = undefined;
        t.group.chip.classList.remove('dragging');
    });
    el.addEventListener('dragover', ev => {
        if (!dragging || dragging.group === t.group) return;
        ev.preventDefault();
        const chip = t.group.chip;
        const r = chip.getBoundingClientRect();
        tabsEl.insertBefore(dragging.group.chip, ev.clientX < r.left + r.width / 2 ? chip : chip.nextSibling);
    });

    welcome(t);
    return t;
}

/** Splits t's pane and opens the Connect dialog in the new one, filled in with t's server. */
export function splitTab(t: Tab, dir: SplitDir) {
    if (t.dialog) return toast('접속 창을 먼저 닫으세요');
    const n = createTab({from: t, dir});
    n.temp = true; // cancelling its dialog closes the pane again
    openDialog(n, t);
}

/** Moves to the nearest pane in a direction within the active tab. */
export function focusPane(dx: number, dy: number) {
    const t = active;
    if (!t) return;
    const c = t.pane.getBoundingClientRect();
    const cx = (c.left + c.right) / 2;
    const cy = (c.top + c.bottom) / 2;
    let best: Tab | undefined;
    let bestDist = Infinity;
    for (const o of panesOf(t.group)) {
        if (o === t) continue;
        const r = o.pane.getBoundingClientRect();
        const beyond = dx < 0 ? r.right <= c.left + 1 : dx > 0 ? r.left >= c.right - 1 :
            dy < 0 ? r.bottom <= c.top + 1 : r.top >= c.bottom - 1;
        if (!beyond) continue;
        const dist = Math.hypot((r.left + r.right) / 2 - cx, (r.top + r.bottom) / 2 - cy);
        if (dist < bestDist) {
            best = o;
            bestDist = dist;
        }
    }
    if (best) activate(best);
}

export function activate(t: Tab) {
    const changed = active !== t;
    if (changed) {
        const old = active;
        if (old) {
            if (old.dialog) saveDialog(old);
            old.el.classList.remove('active');
            old.pane.classList.remove('focused');
        }
        if (old?.group !== t.group) {
            if (old) {
                old.group.box.hidden = true;
                old.group.chip.classList.remove('current');
            }
            t.group.box.hidden = false;
            t.group.chip.classList.add('current');
            for (const p of panesOf(t.group)) p.el.classList.remove('activity');
        }
        setActive(t);
        t.el.classList.add('active');
        t.el.classList.remove('activity');
        t.pane.classList.add('focused');
        t.el.scrollIntoView({block: 'nearest', inline: 'nearest'});
        paintMargin();
        // Each tab keeps its own Connect dialog and file window.
        showFilesFor(t.id);
        switchSearch(t.search);
        if (t.dialog) loadDialog(t);
        else hideDialog();
    }
    for (const p of panesOf(t.group)) fitPane(p);
    updateTitle();
    if (changed) focusActive();
    else if (!t.dialog && !filesOpen()) t.term.focus();
}

export function cycleTab(step: number) {
    const list = orderedTabs();
    if (list.length < 2 || !active) return;
    const i = list.indexOf(active);
    activate(list[(i + step + list.length) % list.length]);
}

/** Closes tab t at the user's request; a tab with a live connection asks first. */
export async function requestClose(t: Tab) {
    if (t.state !== 'idle') {
        const yes = await ask('탭 닫기', `"${t.label.textContent}" 탭은 연결되어 있습니다. 닫으면 연결이 끊어집니다. 닫을까요?`, '닫기');
        if (!tabs.has(t.id)) return; // closed some other way while asking
        if (!yes) return askOpen() ? undefined : focusActive(); // asked again for another tab: leave the focus there
    }
    closeTab(t);
}

export function closeTab(t: Tab) {
    disposeMemo(t);
    cancelRetry(t);
    forwardsTabClosed(t.id);
    forgetFiles(t.id);
    if (t === active) closeSearch();
    if (t.dialog && t === active) hideDialog();
    t.dialog = undefined;
    const list = orderedTabs();
    const i = list.indexOf(t);
    CloseTab(t.id);
    t.term.dispose();
    unsplitPane(t.pane);
    t.el.remove();
    tabs.delete(t.id);
    const g = t.group;
    if (g.box.querySelector('.term')) {
        paintGroup(g);
    } else {
        g.box.remove();
        g.chip.remove();
    }

    if (active === t) {
        setActive(undefined);
        // Closing one pane of a split tab stays in that tab.
        const near = [list[i - 1], list[i + 1]].find(n => n?.group === g);
        const next = near ?? list[i + 1] ?? list[i - 1];
        if (next) activate(next);
    }
    if (tabs.size === 0) {
        const n = createTab();
        activate(n);
        openDialog(n);
    }
}

export async function reconnect(t: Tab) {
    if (t.docker) return startDocker(t).catch(e => notice(t, String(e)));
    if (!t.req) return openDialog(t);
    try {
        if (await connectTab(t, t.req) === 'ftp') await openFilesFor(t);
    } catch (e) {
        notice(t, String(e));
    }
}

export async function duplicate(t: Tab) {
    if (t.docker) return openDockerTab(t.docker.src, t.docker.id, t.docker.name, t.docker.mode);
    if (!t.req) return;
    const n = createTab();
    n.session = t.session;
    activate(n);
    try {
        if (await connectTab(n, terminal.ConnectRequest.createFrom({...t.req})) === 'ftp') await openFilesFor(n);
    } catch (e) {
        notice(n, String(e));
    }
}

export async function openDockerTab(src: DockerSource, id: string, name: string, mode: DockerMode) {
    const n = createTab();
    n.docker = {src, id, name, mode};
    activate(n);
    try {
        await startDocker(n);
    } catch (e) {
        notice(n, String(e));
    }
}
