import '@xterm/xterm/css/xterm.css';
import './style.css';

import {dbg} from './debug';
import {Terminal} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {WebLinksAddon} from '@xterm/addon-web-links';
import {fontFamily, loadUnicodeWidths, NARROW} from './cjkwidth';
import {helpOpen, toggleHelp} from './help';
import {initTranslucencyPreview, openPrefs} from './prefs';
import {expandMacro, macroForKey, openMacros} from './macros';
import {forwardsTabClosed, openForwards} from './forwards';
import {DockerMode, DockerSource, openContainers} from './containers';
import {ask, askOpen, choose} from './dialog';
import {openSaveSession, openSessionManager} from './sessions';
import {initAuthPrompt} from './authprompt';
import {attachHighlight} from './highlight';
import {Lang, startI18n, tr} from './i18n';
import {attachSearch, closeSearch, openSearch, switchSearch} from './search';
import {loadSettings, settings} from './settings';
import {themeByName, THEMES} from './themes';
import {filesOpen, forgetFiles, setActiveTabProvider, showFilesFor} from './files';
import {
    CloseTab, GetLanguage, RunScript, RunScriptFile, ShowScripts, StopScript, GetHistory, GetLocalShells, GetSerialPorts, GetSessions, GetSSHConfigHosts,
    GetVersion, LookupSSH, Quit, Resize, SendBreak, ShowLogs, WebHideWindow,
} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {BrowserOpenURL, ClipboardSetText, Environment, EventsOn, WindowUnminimise} from '../wailsjs/runtime/runtime';
import {isLinux, setPlatform, webPage} from './platform';
import {checkUpdate} from './update';
import {active, DialogState, fitPane, focusActive, Group, orderedTabs, panesOf, setActive, SplitDir, Tab, tabs, tabsEl, termsEl} from './tabstate';
import {notice, reconnectHint, toast} from './notify';
import {appName, setVersion, updateTitle, version} from './title';
import {applyLook, applySettings, applyTranslucency, chooseRenderer, chooseTheme, DEFAULT_FONT, lookOptions, paintMargin, setFontSize} from './look';
import {copySelection, extendTouchSelection, pasteClipboard, pasteText, selectTouchWord, touchCell} from './clipboard';
import {ARROWS, isPaneKey, paintGroup, splitKey, splitPane, unsplitPane} from './split';
import {isAppShortcut, modalOpen} from './shortcuts';
import {runScript, showConsole} from './luaconsole';
import {cancelRetry, connectTab, disconnect, openFilesFor, retryNow, sendInput, setState, startDocker, toggleEncoding, toggleLog} from './connection';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

// Host field texts that run a program on this PC instead of connecting (see localshell.go).
const LOCAL_SHELL = /^(cmd|powershell|pwsh|wsl)(\.exe)?(\s|$)/i;
const TERMUX_LOCAL_SHELL = /^(bash|zsh|fish|sh)(\s|$)/i;

function isLocal(host: string): boolean {
    const name = host.trim();
    return LOCAL_SHELL.test(name) || (import.meta.env.VITE_TERMUX === '1' && TERMUX_LOCAL_SHELL.test(name));
}

// Host field texts that open a serial port; the Port field is then the speed (see serial.go).
const SERIAL_PORT = /^(com[1-9]\d*|\/dev\/\S+)(\s|$)/i;
const DEFAULT_BAUD = '115200';
const BAUDS = [300, 600, 1200, 2400, 4800, 9600, 14400, 19200, 38400, 57600, 115200, 230400, 460800, 921600];
const SERIAL_SUPPORTED = import.meta.env.VITE_TERMUX !== '1';

function isSerial(host: string): boolean {
    return SERIAL_SUPPORTED && SERIAL_PORT.test(host.trim());
}

// In a browser, this page's tab ids start at its base (see platform.ts).
let nextId = (webPage?.base ?? 0) + 1;
let dragging: Tab | undefined;

function welcome(t: Tab) {
    t.term.write(`choboterm V${version}\r\n`);
    t.term.write(`\x1b[90m${tr('Enter: 접속 창 · Ctrl+Shift+T: 새 탭 · Ctrl+Tab: 탭 전환 · Ctrl+Shift+W: 탭 닫기')}\r\n`);
    t.term.write(`${tr('Ctrl+Shift+E: UTF-8 ↔ EUC-KR · Ctrl+Shift+F: 파일 전송 · Ctrl+Shift+D: 연결 끊기')}\x1b[0m\r\n`);
}

/** Creates a tab, or with split a new pane next to split.from in its tab. */
function createTab(split?: {from: Tab; dir: SplitDir}): Tab {
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
        if (t.scriptBox?.el.contains(ev.target as Node)) return; // the Lua console's input pastes as text
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
function splitTab(t: Tab, dir: SplitDir) {
    if (t.dialog) return toast('접속 창을 먼저 닫으세요');
    const n = createTab({from: t, dir});
    n.temp = true; // cancelling its dialog closes the pane again
    openDialog(n, t);
}

/** Moves to the nearest pane in a direction within the active tab. */
function focusPane(dx: number, dy: number) {
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

function activate(t: Tab) {
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

function cycleTab(step: number) {
    const list = orderedTabs();
    if (list.length < 2 || !active) return;
    const i = list.indexOf(active);
    activate(list[(i + step + list.length) % list.length]);
}

/** Closes tab t at the user's request; a tab with a live connection asks first. */
async function requestClose(t: Tab) {
    if (t.state !== 'idle') {
        const yes = await ask('탭 닫기', `"${t.label.textContent}" 탭은 연결되어 있습니다. 닫으면 연결이 끊어집니다. 닫을까요?`, '닫기');
        if (!tabs.has(t.id)) return; // closed some other way while asking
        if (!yes) return askOpen() ? undefined : focusActive(); // asked again for another tab: leave the focus there
    }
    closeTab(t);
}

function closeTab(t: Tab) {
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

async function reconnect(t: Tab) {
    if (t.docker) return startDocker(t).catch(e => notice(t, String(e)));
    if (!t.req) return openDialog(t);
    try {
        if (await connectTab(t, t.req) === 'ftp') await openFilesFor(t);
    } catch (e) {
        notice(t, String(e));
    }
}

async function duplicate(t: Tab) {
    if (t.docker) return openDockerTab(t.docker.src, t.docker.id, t.docker.name, t.docker.mode);
    if (!t.req) return;
    const n = createTab();
    n.session = t.session;
    activate(n);
    try {
        if (await connectTab(n, main.ConnectRequest.createFrom({...t.req})) === 'ftp') await openFilesFor(n);
    } catch (e) {
        notice(n, String(e));
    }
}

/** Tab menu "세션으로 저장": saves the tab's connection; the tab then shows the session's name. */
async function saveTabSession(t: Tab) {
    if (!t.req) return;
    const r = t.req;
    const s = await openSaveSession({
        host: r.host, port: r.port, login: r.login, encoding: r.encoding, pass: r.pass, jump: r.jump, session: t.session,
    });
    if (s) {
        for (const o of tabs.values()) {
            if (o === t || (o.session && o.session.id === s.id)) {
                o.session = s;
                setState(o, o.state);
            }
        }
        toast(`세션 저장: ${s.group ? s.group + ' / ' : ''}${s.name}`);
    }
    focusActive();
}

// The window is closing with tabs still connected: ask first.
// Closing the window while browsers use the web server: quit everything,
// or close only the window and keep serving them.
EventsOn('app:closeWeb', async (browsers: number) => {
    WindowUnminimise();
    const n = [...tabs.values()].filter(t => t.state !== 'idle').length;
    let msg = `브라우저 ${browsers}개가 웹 서버에 연결되어 있습니다.\n\n모두 종료하면 브라우저의 탭도 끊어집니다.`;
    if (n > 0) msg += `\n이 창의 연결된 탭 ${n}개는 어느 쪽이든 끊어집니다.`;
    msg += '\n\n창만 닫으면 웹 서버는 계속 돌고, choboterm을 다시 실행하면 창이 나타납니다. 설정 > 웹 서버에서 끌 수 있습니다.';
    const c = await choose('choboterm 끝내기', msg, '모두 종료', '창만 닫기 (웹 서버 유지)');
    if (c === 'ok') return Quit();
    if (c === 'alt') {
        try {
            await WebHideWindow();
            return;
        } catch (e) {
            await ask('choboterm 끝내기', String(e));
        }
    }
    if (!askOpen()) focusActive();
});

EventsOn('app:close', async () => {
    WindowUnminimise();
    const n = [...tabs.values()].filter(t => t.state !== 'idle').length;
    if (n === 0 || await ask('choboterm 끝내기', `연결된 탭이 ${n}개 있습니다. 창을 닫으면 모두 끊어집니다. 닫을까요?`, '닫기')) return Quit();
    if (!askOpen()) focusActive();
});

const menu = $<HTMLUListElement>('tabMenu');
let menuTab: Tab | undefined;

function showMenu(t: Tab, x: number, y: number) {
    menuTab = t;
    const enable = (act: string, on: boolean) =>
        menu.querySelector(`[data-act="${act}"]`)!.classList.toggle('disabled', !on);
    enable('reconnect', true);
    enable('duplicate', !!t.req || !!t.docker);
    enable('files', t.state !== 'idle' && !!t.req && t.proto !== 'local' && t.proto !== 'serial');
    (menu.querySelector('[data-act="break"]') as HTMLElement).hidden = !(t.state === 'on' && t.proto === 'serial');
    enable('forwards', t.state === 'on' && t.proto === 'ssh');
    enable('disconnect', t.state !== 'idle');
    enable('saveSession', !!t.req);
    enable('log', !!t.log || t.state === 'on');
    enable('script', t.state === 'on' && !t.script);
    enable('console', t.state === 'on' && (!t.script || t.script.console));
    enable('stopScript', !!t.script);
    menu.querySelector('[data-act="log"]')!.textContent = t.log ? '로그 기록 중지' : '로그 기록 시작';
    menu.querySelector('[data-act="close"]')!.textContent = panesOf(t.group).length > 1 ? '분할 창 닫기' : '탭 닫기';
    hideThemeMenu();
    menu.hidden = false;
    const r = menu.getBoundingClientRect();
    menu.style.left = `${Math.min(x, window.innerWidth - r.width - 4)}px`;
    menu.style.top = `${Math.min(y, window.innerHeight - r.height - 4)}px`;
}

function hideMenu() {
    menu.hidden = true;
    menuTab = undefined;
    hideThemeMenu();
}

// The "색 테마 ▸" submenu: Settings' theme, then every theme, the current one checked.
const themeMenu = $<HTMLUListElement>('themeMenu');
let themeMenuTab: Tab | undefined;

function showThemeMenu(t: Tab, li: HTMLElement) {
    themeMenuTab = t;
    themeMenu.innerHTML = '';
    const item = (name: string, label: string, bg: string, fg: string) => {
        const el = document.createElement('li');
        el.dataset.theme = name;
        if ((t.theme ?? '') === name) el.classList.add('checked');
        const sw = document.createElement('span');
        sw.className = 'swatch';
        sw.style.background = bg;
        sw.style.color = fg;
        sw.textContent = 'Aa';
        el.append(sw, label);
        themeMenu.appendChild(el);
    };
    const def = themeByName(settings.theme);
    item('', `기본값 (설정: ${def.label})`, def.theme.background!, def.theme.foreground!);
    const sep = document.createElement('li');
    sep.className = 'sep';
    themeMenu.appendChild(sep);
    for (const th of THEMES) item(th.name, th.label, th.theme.background!, th.theme.foreground!);
    themeMenu.hidden = false;
    const r = li.getBoundingClientRect();
    const m = themeMenu.getBoundingClientRect();
    const right = r.right + m.width <= window.innerWidth;
    themeMenu.style.left = `${right ? r.right - 2 : r.left - m.width + 2}px`;
    themeMenu.style.top = `${Math.max(4, Math.min(r.top - 4, window.innerHeight - m.height - 4))}px`;
}

function hideThemeMenu() {
    themeMenu.hidden = true;
    themeMenuTab = undefined;
}

themeMenu.addEventListener('mousedown', ev => ev.stopPropagation());
themeMenu.addEventListener('click', ev => {
    const li = (ev.target as HTMLElement).closest('li');
    const t = themeMenuTab;
    if (!li || !t || li.classList.contains('sep')) return;
    hideMenu();
    chooseTheme(t, li.dataset.theme ?? '');
    if (t === active) focusActive();
});

menu.addEventListener('mousedown', ev => ev.stopPropagation());
// Pointing at "색 테마 ▸" opens its submenu; pointing at another item closes it.
menu.addEventListener('mouseover', ev => {
    const li = (ev.target as HTMLElement).closest('li');
    if (!li || !menuTab || li.classList.contains('sep')) return;
    if (li.dataset.act === 'theme') {
        if (themeMenu.hidden) showThemeMenu(menuTab, li);
    } else {
        hideThemeMenu();
    }
});
menu.addEventListener('click', ev => {
    const li = (ev.target as HTMLElement).closest('li');
    const t = menuTab;
    if (!li || !t || li.classList.contains('disabled') || li.classList.contains('sep')) return;
    if (li.dataset.act === 'theme') return showThemeMenu(t, li);
    hideMenu();
    switch (li.dataset.act) {
        case 'reconnect':
            reconnect(t);
            break;
        case 'duplicate':
            duplicate(t);
            break;
        case 'containers':
            showContainers();
            break;
        case 'splitRight':
            splitTab(t, 'row');
            break;
        case 'splitDown':
            splitTab(t, 'col');
            break;
        case 'files':
            openFilesFor(t);
            break;
        case 'forwards':
            showForwards(t);
            break;
        case 'break':
            SendBreak(t.id).then(() => notice(t, 'Break 신호를 보냈습니다')).catch(e => notice(t, String(e)));
            break;
        case 'script':
            runScript(t, async () => void await RunScriptFile(t.id));
            break;
        case 'console':
            showConsole(t);
            break;
        case 'stopScript':
            StopScript(t.id);
            break;
        case 'scripts':
            ShowScripts().catch(e => toast(String(e)));
            break;
        case 'log':
            toggleLog(t);
            break;
        case 'logs':
            ShowLogs(t.id).catch(e => notice(t, String(e)));
            break;
        case 'saveSession':
            saveTabSession(t);
            break;
        case 'disconnect':
            disconnect(t);
            break;
        case 'close':
            requestClose(t);
            break;
    }
});
window.addEventListener('mousedown', hideMenu);
window.addEventListener('blur', hideMenu);

$<HTMLButtonElement>('newTab').addEventListener('click', () => openDialog(null));
tabsEl.addEventListener('dblclick', ev => {
    if (ev.target === tabsEl) openDialog(null); // double-click empty tab bar: new tab
});

const overlay = $<HTMLDivElement>('overlay');
const form = $<HTMLFormElement>('connect');
const host = $<HTMLInputElement>('host');
const port = $<HTMLInputElement>('port');
const login = $<HTMLInputElement>('login');
const pass = $<HTMLInputElement>('pass');
const encoding = $<HTMLSelectElement>('encoding');
const jump = $<HTMLInputElement>('jump');
const JUMP_HINT = jump.placeholder;
const proto = $<HTMLSpanElement>('proto');
const portLabel = $<HTMLLabelElement>('portLabel');
const error = $<HTMLDivElement>('error');
const ok = $<HTMLButtonElement>('ok');
const cancel = $<HTMLButtonElement>('cancel');
const hostDrop = $<HTMLButtonElement>('hostDrop');
const hostList = $<HTMLUListElement>('hostList');
const hostTip = $<HTMLDivElement>('hostTip');

type ListEntry = Pick<main.HostEntry, 'host' | 'port' | 'login' | 'encoding' | 'pass'> & {
    jump?: string;
    fromConfig?: boolean;
    label?: string;
    session?: main.SavedSession;
};

let history: main.HostEntry[] = [];
let sessions: main.SavedSession[] = [];
let configHosts: main.SSHConfigHost[] = [];
let localShells: main.LocalShell[] = [];
let serialPorts: main.SerialPort[] = [];
let serverPort = ''; // the Port field from before a serial port was typed, put back after
let entries: ListEntry[] = []; // saved sessions, history, then ~/.ssh/config names, local shells and serial ports not in it
let visible: ListEntry[] = []; // entries matching what is typed in the Host field
let filter = '';
let activeIndex = -1;
let chosen: main.SavedSession | undefined; // saved session the fields came from
let resolved = '';

/** Marks whether the Port field now holds a serial port's speed, without changing it. */
function syncPortMode() {
    if (isSerial(host.value) && !isLocal(host.value)) {
        port.dataset.baud = '1';
    } else {
        delete port.dataset.baud;
    }
}

function updateProto() {
    // A local shell needs no port, login or password, and is always UTF-8.
    const local = isLocal(host.value);
    // A serial port takes its speed in the Port field, and no login or password.
    const serial = !local && isSerial(host.value);
    port.disabled = encoding.disabled = local;
    login.disabled = pass.disabled = local || serial;
    portLabel.textContent = serial ? 'Baud' : 'Port';
    if (serial && !port.dataset.baud) {
        serverPort = port.value;
        port.dataset.baud = '1';
        if (!BAUDS.includes(Number(port.value))) port.value = DEFAULT_BAUD; // a server's port
    } else if (!serial && port.dataset.baud) {
        delete port.dataset.baud;
        port.value = serverPort || '22';
    }
    const p = Number(port.value);
    jump.disabled = local || serial || p === 21 || p === 23; // jump hosts are for SSH only
    if (local) {
        proto.textContent = '로컬 셸';
        return;
    }
    if (serial) {
        proto.textContent = '시리얼';
        return;
    }
    // Other ports are detected from the server greeting when connecting.
    proto.textContent = p === 22 ? 'SSH' : p === 21 ? 'FTP' : p === 23 ? 'Telnet' : '자동 감지';
}

/** Reads what the Host list offers: saved sessions, history, ~/.ssh/config names, local shells. */
async function loadEntries() {
    [history, sessions, configHosts, localShells, serialPorts] = await Promise.all([
        GetHistory(), GetSessions(), GetSSHConfigHosts(), GetLocalShells(), GetSerialPorts(),
    ]).then(r => r.map(x => x ?? []) as [main.HostEntry[], main.SavedSession[], main.SSHConfigHost[], main.LocalShell[], main.SerialPort[]]);
    const seen = new Set(history.map(h => h.host));
    const saved = new Set(sessions.map(s => `${s.host}:${s.port}`));
    entries = [...sessions.map(s => ({
        host: s.host, port: s.port, login: s.login, encoding: s.encoding, pass: s.pass, jump: s.jump, session: s,
    })), ...history.filter(h => !saved.has(`${h.host}:${h.port}`)), ...configHosts.filter(c => !seen.has(c.host)).map(c => ({
        host: c.host, port: c.port, login: c.login, encoding: 'UTF-8', pass: '', fromConfig: true,
    })), ...localShells.filter(s => !seen.has(s.name)).map(s => ({
        host: s.name, port: 0, login: '', encoding: 'UTF-8', pass: '', label: s.label,
    })), ...serialPorts.filter(s => !seen.has(s.name)).map(s => ({
        host: s.name, port: Number(DEFAULT_BAUD), login: '', encoding: 'UTF-8', pass: '', label: s.label,
    }))];
}

/**
 * Opens the Connect dialog. t: the tab to connect in, null: a new tab,
 * undefined: the active tab if it's empty, otherwise a new tab.
 */
async function openDialog(t?: Tab | null, from?: Tab) {
    if (active?.dialog) saveDialog(active);
    let tab: Tab;
    if (t === null || (t === undefined && !(active && active.state === 'idle'))) {
        tab = createTab();
        tab.temp = true;
    } else {
        tab = t ?? active!;
    }
    if (tab.dialog) return activate(tab);
    cancelRetry(tab);

    await loadEntries();
    const src = tab.req ? tab : from; // a new split pane starts with its neighbour's server
    const r = src?.req;
    if (r) {
        applyEntry({host: r.host, port: r.port, login: r.login, encoding: r.encoding, pass: r.pass, jump: r.jump, session: src.session});
    } else if (!host.value && history.length > 0) {
        applyEntry(history[0]);
    } else {
        fillSavedPass();
    }
    tab.dialog = {
        host: host.value, port: port.value, login: login.value, pass: pass.value,
        encoding: encoding.value, jump: jump.value, error: '', connecting: false, resolved, session: chosen,
    };
    if (active === tab) {
        loadDialog(tab);
        focusActive();
    } else {
        activate(tab);
    }
    host.select();
}

function saveDialog(t: Tab) {
    const d = t.dialog;
    if (!d) return;
    d.host = host.value;
    d.port = port.value;
    d.login = login.value;
    d.pass = pass.value;
    d.encoding = encoding.value;
    d.jump = jump.value;
    d.error = error.textContent ?? '';
    d.resolved = resolved;
    d.session = chosen;
    hideList();
}

function loadDialog(t: Tab) {
    const d = t.dialog!;
    host.value = d.host;
    port.value = d.port;
    login.value = d.login;
    pass.value = d.pass;
    encoding.value = d.encoding;
    jump.value = d.jump;
    syncPortMode();
    resolved = d.resolved;
    chosen = d.session;
    error.textContent = d.error;
    paintConnecting(d);
    updateProto();
    overlay.hidden = false;
}

function hideDialog() {
    hideList();
    hideTip();
    overlay.hidden = true;
}

function paintConnecting(d: DialogState) {
    ok.disabled = cancel.disabled = d.connecting;
    ok.innerHTML = d.connecting ? '접속 중...' : '<u>C</u>onnect';
}

/** Closes tab t's dialog. A tab opened just for a cancelled dialog is closed too. */
function dismissDialog(t: Tab, cancelled: boolean) {
    t.dialog = undefined;
    if (t === active) hideDialog();
    if (cancelled && t.temp && t.state === 'idle' && tabs.size > 1) return closeTab(t);
    t.temp = false;
    if (t === active) focusActive();
}

function cancelDialog() {
    if (active?.dialog && !active.dialog.connecting) dismissDialog(active, true);
}

function applyEntry(e: ListEntry) {
    chosen = e.session;
    host.value = e.host;
    // A local shell (port 0) keeps the port for later; a serial port's speed
    // doesn't replace the server port that comes back after it.
    if (isSerial(e.host) && !port.dataset.baud) serverPort = port.value;
    if (e.port > 0) port.value = String(e.port);
    syncPortMode();
    login.value = e.login;
    encoding.value = e.encoding || 'UTF-8';
    pass.value = e.pass ?? '';
    jump.value = e.jump ?? '';
    jump.placeholder = JUMP_HINT;
    resolved = host.value.trim();
    updateProto();
}

/**
 * Fills Port/Login from ~/.ssh/config when the Host field holds a config
 * name ("busan") or an ssh command line ("ssh -p 2222 me@busan"); the field
 * keeps just the name. Values the user set after that are left alone.
 */
async function resolveHost() {
    const text = host.value.trim();
    if (!text || text === resolved || isLocal(text) || isSerial(text)) return;
    const r = await LookupSSH(text);
    if (host.value.trim() !== text) return; // typed on meanwhile
    host.value = r.host;
    if (r.port > 0) port.value = String(r.port);
    if (r.login) login.value = r.login;
    if (r.jump) jump.value = r.jump; // ssh -J
    jump.placeholder = r.configJump ? `ssh config: ${r.configJump}` : JUMP_HINT;
    resolved = host.value.trim();
    updateProto();
    fillSavedPass();
    if (document.activeElement === host) showTip(r);
}

function hideTip() {
    hostTip.hidden = true;
}

function tipLine(text: string, cls = '') {
    const div = document.createElement('div');
    if (cls) div.className = cls;
    div.textContent = text;
    hostTip.appendChild(div);
}

/** Explains what the Host field accepts, or what the typed config name stands for. */
function showTip(r?: main.SSHTarget) {
    if (!hostList.hidden) return hideTip();
    hostTip.innerHTML = '';
    if (r?.fromConfig) {
        const user = r.login ? r.login + '@' : '';
        tipLine(`~/.ssh/config의 ${r.host}`, 'head');
        tipLine(`→ ${user}${r.hostName || r.host}:${r.port || 22}`);
    } else {
        tipLine('주소 또는 ~/.ssh/config의 Host 이름', 'head');
        tipLine('예) busan · ssh busan');
        tipLine('     ssh -p 2222 user@busan', 'pre');
        tipLine(isLinux ? '이 PC의 셸: bash · zsh · fish · sh' : '이 PC의 셸: cmd · powershell · wsl (wsl -d Ubuntu)');
        tipLine(isLinux ? '시리얼 포트: /dev/ttyUSB0 · /dev/ttyUSB0 9600 7E1 (Port 칸 = 속도)' : '시리얼 포트: COM3 · COM3 9600 7E1 rtscts (Port 칸 = 속도)');
        if (configHosts.length > 0) {
            const names = configHosts.slice(0, 6).map(c => c.host).join(', ');
            tipLine(`등록된 이름: ${names}${configHosts.length > 6 ? ' …' : ''} (▼ 목록)`, 'names');
        } else {
            tipLine('~/.ssh/config가 없거나 등록된 Host가 없습니다.', 'names');
        }
    }
    hostTip.hidden = false;
}

let tipSeq = 0;
async function refreshTip() {
    const text = host.value.trim();
    const seq = ++tipSeq;
    const r = text ? await LookupSSH(text) : undefined;
    if (seq === tipSeq && document.activeElement === host) showTip(r);
}

// Fills the remembered password for the host/port currently typed in:
// the picked session's, another session's for that server, or the history's (Telnet).
function fillSavedPass() {
    const h = host.value.trim(), p = Number(port.value);
    const same = (e: {host: string; port: number}) => e.host === h && e.port === p;
    if (chosen && !same(chosen)) chosen = undefined;
    pass.value = (chosen ?? sessions.find(s => same(s) && s.pass) ?? history.find(same))?.pass ?? '';
}

/** Entries that contain every word typed in the Host field (name, address, group or login). */
function matching(): ListEntry[] {
    const words = filter.toLowerCase().split(/\s+/).filter(Boolean);
    if (words.length === 0) return entries;
    return entries.filter(e => {
        const text = [e.host, e.login, e.label, e.session?.name, e.session?.group].join(' ').toLowerCase();
        return words.every(w => text.includes(w));
    });
}

/** The heading an entry goes under; only saved sessions get headings. */
function section(e: ListEntry): string {
    if (e.session) return '★ ' + (e.session.group || '저장된 세션');
    return sessions.length > 0 ? '최근 · ssh config · 로컬 셸 · 시리얼' : '';
}

function showList() {
    hostList.innerHTML = '';
    visible = matching();
    if (visible.length === 0) return hideList();
    hideTip();
    let head = '';
    visible.forEach((e, i) => {
        const sec = section(e);
        if (sec !== head) {
            head = sec;
            const h = document.createElement('li');
            h.className = 'head';
            h.textContent = sec;
            h.addEventListener('mousedown', ev => ev.preventDefault());
            hostList.appendChild(h);
        }
        const li = document.createElement('li');
        li.textContent = e.session?.name ?? e.host;
        const meta = document.createElement('span');
        meta.className = 'meta';
        const where = e.session && e.session.name !== e.host ? `${e.host} · ` : '';
        meta.textContent = isLocal(e.host) ? `로컬 셸${e.label ? ' · ' + e.label : ''}` :
            isSerial(e.host) ? `${where}시리얼 · ${e.port} baud${e.label ? ' · ' + e.label : ''}` :
            `${where}${e.port}${e.login ? ' · ' + e.login : ''}${e.fromConfig ? ' · ssh config' : ''}`;
        li.appendChild(meta);
        li.addEventListener('mousedown', ev => {
            ev.preventDefault();
            applyEntry(e);
            hideList();
            focusAfterHost();
        });
        if (i === activeIndex) {
            li.className = 'active';
            requestAnimationFrame(() => li.scrollIntoView({block: 'nearest'}));
        }
        hostList.appendChild(li);
    });
    hostList.hidden = false;
}

function hideList() {
    hostList.hidden = true;
    activeIndex = -1;
    filter = '';
}

hostDrop.addEventListener('click', () => {
    if (hostList.hidden) {
        showList();
        host.focus();
    } else {
        hideList();
    }
});

host.addEventListener('blur', () => {
    hideList();
    hideTip();
});
host.addEventListener('focus', () => refreshTip());
host.addEventListener('input', () => {
    chosen = undefined;
    updateProto(); // "cmd", "wsl"... are local shells
    // Typing narrows the list down; with nothing typed the tooltip explains the field.
    filter = host.value.trim();
    activeIndex = -1;
    if (filter) showList();
    else hideList();
    if (hostList.hidden) refreshTip();
});
host.addEventListener('change', () => {
    fillSavedPass();
    resolveHost();
});
port.addEventListener('change', fillSavedPass);
host.addEventListener('keydown', ev => {
    if (ev.key === 'ArrowDown' || ev.key === 'ArrowUp') {
        ev.preventDefault();
        if (hostList.hidden) visible = matching();
        if (visible.length === 0) return;
        const step = ev.key === 'ArrowDown' ? 1 : -1;
        activeIndex = (activeIndex + step + visible.length) % visible.length;
        applyEntry(visible[activeIndex]);
        showList();
    } else if (ev.key === 'Enter' && !hostList.hidden) {
        // Nothing picked with the arrows: the list only opened because of typing,
        // so Enter connects to what was typed, as it does without the list.
        if (activeIndex < 0) return hideList();
        ev.preventDefault();
        hideList();
        focusAfterHost();
    }
});

/** After choosing a host: the password, or Connect for a local shell. */
function focusAfterHost() {
    (pass.disabled ? ok : pass).focus();
}

port.addEventListener('input', () => {
    port.value = port.value.replace(/\D/g, '');
    updateProto();
});

async function doConnect() {
    const t = active;
    if (!t?.dialog || t.dialog.connecting) return;
    await resolveHost();
    if (t !== active || !t.dialog || t.dialog.connecting) return;
    saveDialog(t);
    await connectDialog(t);
}

/** Connects tab t with what its Connect dialog holds; the dialog closes on success or shows the error. */
async function connectDialog(t: Tab) {
    const d = t.dialog;
    if (!d) return;
    d.error = '';
    d.connecting = true;
    error.textContent = '';
    paintConnecting(d);
    // The user may switch tabs while connecting; results go to tab t.
    const serial = isSerial(d.host);
    try {
        const protocol = await connectTab(t, main.ConnectRequest.createFrom({
            host: d.host.trim(),
            port: Number(d.port),
            login: serial ? '' : d.login,
            pass: serial ? '' : d.pass,
            encoding: d.encoding,
            jump: serial ? '' : d.jump.trim(),
        }));
        d.connecting = false;
        t.session = d.session;
        setState(t, t.state); // the label is the session's name
        if (t.dialog === d) dismissDialog(t, false);
        if (protocol === 'ftp') await openFilesFor(t);
    } catch (e) {
        d.connecting = false;
        d.error = String(e);
        if (t.dialog === d && t === active) {
            error.textContent = d.error;
            paintConnecting(d);
        }
    }
}

form.addEventListener('submit', ev => {
    ev.preventDefault();
    doConnect();
});
cancel.addEventListener('click', cancelDialog);
$<HTMLButtonElement>('close').addEventListener('click', cancelDialog);
$<HTMLButtonElement>('saveSess').addEventListener('click', saveFromDialog);

/** ☆ Save: saves what is typed in the Connect dialog as a session (or updates the picked one). */
async function saveFromDialog() {
    const t = active;
    if (!t?.dialog || t.dialog.connecting) return;
    hideList();
    await resolveHost();
    if (!host.value.trim()) {
        error.textContent = 'Host를 입력하세요';
        return host.focus();
    }
    const local = isLocal(host.value);
    const bare = local || isSerial(host.value); // no login, password or jump host
    const s = await openSaveSession({
        host: host.value.trim(),
        port: local ? 0 : Number(port.value),
        login: bare ? '' : login.value,
        encoding: local ? 'UTF-8' : encoding.value,
        pass: bare ? '' : pass.value,
        jump: bare ? '' : jump.value.trim(),
        session: chosen,
    });
    if (s) {
        chosen = s;
        await loadEntries();
        toast(`세션 저장: ${s.group ? s.group + ' / ' : ''}${s.name}`);
    }
    if (t === active && t.dialog) ok.focus();
}

form.addEventListener('keydown', ev => {
    if (ev.key === 'Escape') {
        ev.preventDefault();
        if (!hostList.hidden) hideList();
        else cancelDialog();
    } else if (ev.altKey && (ev.key === 'c' || ev.key === 'C')) {
        ev.preventDefault();
        doConnect();
    } else if (ev.altKey && (ev.key === 'a' || ev.key === 'A')) {
        ev.preventDefault();
        cancelDialog();
    } else if (ev.altKey && (ev.key === 's' || ev.key === 'S')) {
        ev.preventDefault();
        saveFromDialog();
    }
});

function showPrefs() {
    if (!modalOpen()) openPrefs(applySettings, focusActive);
}

$<HTMLButtonElement>('prefsBtn').addEventListener('click', showPrefs);

function showSessions() {
    if (!modalOpen()) openSessionManager(openSessions, focusActive);
}

$<HTMLButtonElement>('sessBtn').addEventListener('click', showSessions);

/**
 * Connects saved sessions: the first in the active tab if it is empty, the
 * others in new tabs. Each goes through its tab's Connect dialog, which stays
 * open with the error if the connection fails.
 */
async function openSessions(list: main.SavedSession[]) {
    for (const s of list) {
        await openDialog(list.length > 1 && s !== list[0] ? null : undefined);
        const t = active;
        if (!t?.dialog || t.dialog.connecting) continue;
        applyEntry({host: s.host, port: s.port, login: s.login, encoding: s.encoding, pass: s.pass, jump: s.jump, session: s});
        saveDialog(t);
        void connectDialog(t);
    }
}

function sendMacro(t: Tab, m: main.Macro) {
    if (t.state !== 'on') return toast('연결되어 있지 않습니다');
    if (m.kind === 'lua') return runScript(t, () => RunScript(t.id, m.name || '매크로', m.text));
    const text = expandMacro(m.text);
    if (text) sendInput(t, text);
}

function showMacros() {
    if (modalOpen()) return;
    openMacros(m => active && sendMacro(active, m), focusActive);
}

function showForwards(t: Tab) {
    if (modalOpen()) return;
    if (t.state !== 'on' || t.proto !== 'ssh' || !t.req) return toast('포트 포워딩은 SSH 접속에서만 사용할 수 있습니다');
    if (t !== active) activate(t);
    openForwards(t.id, t.req, focusActive);
}

function showContainers() {
    if (modalOpen()) return;
    const t = active;
    const srcs: DockerSource[] = [];
    if (t && t.state === 'on' && t.proto === 'ssh' && t.req) {
        srcs.push({tab: t.id, label: `${t.req.login ? t.req.login + '@' : ''}${t.req.host}`, host: t.req.host, port: Number(t.req.port)});
    }
    openContainers(srcs, (src, c, mode) => openDockerTab(src, c.id, c.name, mode), focusActive);
}

async function openDockerTab(src: DockerSource, id: string, name: string, mode: DockerMode) {
    const n = createTab();
    n.docker = {src, id, name, mode};
    activate(n);
    try {
        await startDocker(n);
    } catch (e) {
        notice(n, String(e));
    }
}

window.addEventListener('keydown', ev => {
    if (!isAppShortcut(ev)) return;
    const t = active;
    const key = ev.key.toUpperCase();
    const macro = macroForKey(ev);
    // In the file window F5 refreshes; dialogs keep their keys too.
    if (macro && (t?.dialog || filesOpen() || helpOpen() || modalOpen())) return;
    // Copy / paste belong to the terminal; in a text box they keep their usual meaning.
    const el = ev.target;
    const typing = el instanceof HTMLInputElement ||
        (el instanceof HTMLTextAreaElement && !el.classList.contains('xterm-helper-textarea'));
    if ((key === 'C' || key === 'V') && (typing || t?.dialog || filesOpen() || helpOpen() || modalOpen())) return;
    ev.preventDefault();
    ev.stopPropagation();
    if (modalOpen()) return;
    if (macro) return t && sendMacro(t, macro);
    if (ev.key === 'F1') return toggleHelp(focusActive);
    // Other shortcuts wait until the help window is closed.
    if (helpOpen()) return;
    // Tab shortcuts work even while a Connect dialog or file window is open.
    if (ev.key === 'Tab') return cycleTab(ev.shiftKey ? -1 : 1);
    if (ev.key === 'PageDown') return cycleTab(1);
    if (ev.key === 'PageUp') return cycleTab(-1);
    if (ev.key === '=' || ev.key === '+') return setFontSize(settings.fontSize + 1);
    if (ev.key === '-') return setFontSize(settings.fontSize - 1);
    if (ev.key === '0') return setFontSize(DEFAULT_FONT);
    const dir = splitKey(ev);
    if (dir) return t && splitTab(t, dir);
    if (isPaneKey(ev)) {
        if (!t?.dialog && !filesOpen()) focusPane(...ARROWS[ev.key]);
        return;
    }
    switch (key) {
        case 'C':
            if (t) copySelection(t);
            break;
        case 'V':
            if (t) pasteClipboard(t);
            break;
        case 'S':
            if (t && !t.dialog && !filesOpen()) openSearch(t.search, focusActive);
            break;
        case 'O':
            showPrefs();
            break;
        case 'M':
            showMacros();
            break;
        case 'H':
            showSessions();
            break;
        case 'K':
            if (t) showConsole(t);
            break;
        case 'P':
            if (t) showForwards(t);
            break;
        case 'J':
            showContainers();
            break;
        case 'L':
            if (t) toggleLog(t);
            break;
        case 'T':
        case 'N':
            openDialog(null);
            break;
        case 'W':
            if (t) requestClose(t);
            break;
        case 'D':
            if (t) disconnect(t);
            break;
        case 'E':
            if (t) toggleEncoding(t);
            break;

        case 'F':
            if (t) openFilesFor(t);
            break;
    }
}, true);

// ---- Start ----

setActiveTabProvider(() => active?.id ?? 0);

// Login prompts name the connection they are for: the Connect dialog's values while it is connecting.
initAuthPrompt(id => {
    const t = tabs.get(id);
    const d = t?.dialog;
    const r = d ? {login: d.login, host: d.host.trim(), port: d.port} : t?.req;
    if (!r) return '';
    return `${r.login ? r.login + '@' : ''}${r.host}:${r.port}`;
}, focusActive);

Promise.all([GetVersion(), loadSettings(), GetLanguage(), Environment()]).then(([v, , lang, env]) => {
    setVersion(v);
    setPlatform(env.platform);
    startI18n(lang === 'en' ? 'en' : 'ko' as Lang);
    initTranslucencyPreview(applyTranslucency);
    $<HTMLSpanElement>('connectTitle').textContent = `${appName} - Connect to`;
    document.title = appName;
    applySettings();
    const first = createTab();
    activate(first);
    openDialog(first);
    // Only when turned on in the settings; the Go side skips it otherwise.
    if (settings.updateCheck) window.setTimeout(() => void checkUpdate(false), 3000);
});
