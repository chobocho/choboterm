// A tab's connection: connecting, the automatic reconnect, disconnecting,
// input and output, and what the Go side reports about it.

import {dbg} from './debug';
import {Terminal} from '@xterm/xterm';
import {forwardsTabClosed} from './forwards';
import {containersTabClosed, engineOf} from './containers';
import {tr} from './i18n';
import {settings} from './settings';
import {closeFiles, forgetFiles, openFiles} from './files';
import {CloseTab, Connect, Disconnect, DockerOpen, Send, SetEncoding, StartLog, StopLog, TabTheme} from '../wailsjs/go/app/App';
import {terminal} from '../wailsjs/go/models';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {active, focusActive, Tab, tabs, TabState} from './tabstate';
import {notice, reconnectHint, toast} from './notify';
import {updateTitle} from './title';
import {applyEncoding, applyLook} from './look';

export function setState(t: Tab, state: TabState) {
    t.state = state;
    t.el.classList.toggle('on', state !== 'idle');
    if (t.docker) {
        const d = t.docker;
        t.label.textContent = d.mode === 'logs' ? `📜 ${d.name}` : `🐳 ${d.name}`;
        t.el.title = `${d.mode === 'logs' ? 'docker logs' : 'docker exec'}: ${d.name} @ ${d.src.label}`;
        if (t.log) t.el.title += `
로그 기록 중: ${t.log}`;
    } else if (t.req && t.proto === 'local') {
        t.label.textContent = t.session?.name ?? t.req.host;
        t.el.title = `로컬 셸: ${t.req.host}`;
        if (t.log) t.el.title += `
로그 기록 중: ${t.log}`;
    } else if (t.req && t.proto === 'serial') {
        t.label.textContent = t.session?.name ?? t.req.host.split(/\s/)[0];
        t.el.title = `시리얼: ${t.req.host} (${t.req.port} baud)`;
        if (t.log) t.el.title += `
로그 기록 중: ${t.log}`;
    } else if (t.req) {
        const scheme = t.proto || 'telnet';
        // Show the port only when it isn't the protocol's usual one.
        const usual = [21, 22, 23].includes(Number(t.req.port));
        t.label.textContent = t.session?.name ?? (usual ? t.req.host : `${t.req.host}:${t.req.port}`);
        t.el.title = `${scheme}://${t.req.login ? t.req.login + '@' : ''}${t.req.host}:${t.req.port}`;
        if (t.log) t.el.title += `
로그 기록 중: ${t.log}`;
    }
    if (t === active) updateTitle();
}

/**
 * Connects tab t with req; returns the protocol. Throws on failure.
 * keepScreen keeps the old output (automatic reconnect) instead of clearing it.
 */
export async function connectTab(t: Tab, req: terminal.ConnectRequest, keepScreen = false): Promise<string> {
    cancelRetry(t);
    req.cols = t.term.cols;
    req.rows = t.term.rows;
    forgetFiles(t.id); // a reconnect replaces the old connection's file window
    t.fresh = !keepScreen;
    let protocol: string;
    try {
        protocol = await Connect(t.id, req);
    } catch (e) {
        t.fresh = false; // the screen stays as it was
        throw e;
    }
    if (!tabs.has(t.id)) {
        CloseTab(t.id); // the tab was closed while connecting
        throw new Error('탭이 닫혔습니다');
    }
    t.req = req;
    t.proto = protocol;
    if (protocol === 'local') req.encoding = 'UTF-8'; // the pseudo console speaks UTF-8 only
    freshScreen(t, req.encoding || 'UTF-8'); // FTP: no session opened
    applyEncoding(t, req.encoding || 'UTF-8');
    if (protocol === 'ftp') {
        setState(t, 'ftp');
        t.term.write(`FTP ${req.host}:${req.port}\r\n\x1b[90m${tr('Ctrl+Shift+F: 파일 전송 창 열기 · 탭을 닫으면 연결이 끊어집니다')}\x1b[0m\r\n`);
    } else {
        setState(t, 'on');
        // The theme saved for this server, if any.
        t.theme = (await TabTheme(t.id)) || undefined;
        applyLook(t);
        // The screen was cleared after an automatic log start was announced.
        if (t.log && !keepScreen) notice(t, `로그 기록 중: ${t.log}`);
    }
    return protocol;
}

// ---- Automatic reconnect ----

const RETRY_DELAYS = [3, 5, 10, 20, 30]; // seconds; the last one repeats
const MAX_RETRIES = 10;

function scheduleRetry(t: Tab, attempt: number, why: string) {
    cancelRetry(t);
    const reason = `\r\n\x1b[33m[${tr(why).replace(/\n/g, '\r\n')}]\r\n`;
    if (attempt > MAX_RETRIES) {
        t.term.write(`${reason}[${tr(`${MAX_RETRIES}번 시도했지만 연결하지 못했습니다`)}] ${tr('Enter: 다시 접속')}\x1b[0m\r\n`);
        return;
    }
    const secs = RETRY_DELAYS[Math.min(attempt - 1, RETRY_DELAYS.length - 1)];
    t.term.write(`${reason}[${tr(`${secs}초 후 다시 연결합니다 (${attempt}/${MAX_RETRIES})`)}] ${tr('Enter: 지금 연결 · Esc: 취소')}\x1b[0m\r\n`);
    t.el.classList.add('retry');
    t.retry = {attempt, timer: window.setTimeout(() => retryNow(t), secs * 1000)};
}

export function cancelRetry(t: Tab) {
    if (!t.retry) return;
    clearTimeout(t.retry.timer);
    t.retry = undefined;
    t.el.classList.remove('retry');
}

export async function retryNow(t: Tab) {
    const attempt = t.retry?.attempt ?? 1;
    cancelRetry(t);
    if (!t.req || !tabs.has(t.id) || t.state !== 'idle') return;
    t.reconnecting = true;
    t.el.classList.add('retry');
    t.term.write(`\x1b[90m${tr(`다시 연결하는 중... ${t.req.host}:${t.req.port}`)}\x1b[0m\r\n`);
    try {
        await connectTab(t, t.req, true);
        notice(t, '다시 연결했습니다');
    } catch (e) {
        if (tabs.has(t.id) && t.state === 'idle') scheduleRetry(t, attempt + 1, String(e));
    } finally {
        t.reconnecting = false;
        if (!t.retry) t.el.classList.remove('retry');
    }
}

export function disconnect(t: Tab) {
    forwardsTabClosed(t.id);
    if (t.retry) {
        cancelRetry(t);
        reconnectHint(t, '자동 재접속을 취소했습니다');
    }
    if (t.state === 'idle') return;
    closeFiles(t.id);
    Disconnect(t.id);
    setState(t, 'idle');
    t.term.write('\r\n');
    reconnectHint(t, '연결을 끊었습니다');
}

/** The folder in the prompt at the cursor: "user@host:~/src$", "~/src $", "[/etc]#"... */
function promptDir(term: Terminal): string {
    const buf = term.buffer.active;
    const line = buf.getLine(buf.baseY + buf.cursorY)?.translateToString(true, 0, buf.cursorX) ?? '';
    // The first path followed by a prompt sign; later ones belong to the typed command.
    const m = /(~[^\s$#%>:]*|\/[^\s$#%>:\])]*)[\])]?\s?[$#%>](?:\s|$)/.exec(line);
    return m ? m[1] : '';
}

/** The folder in a window title like "user@host: ~/src". */
function titleDir(title = ''): string {
    const m = /(?:^|[:\s])(~[^\s]*|\/[^\s]*)\s*$/.exec(title);
    return m ? m[1] : '';
}

/** Where the shell is, as far as the terminal can tell ("" if unknown). */
function shellDir(t: Tab): string {
    if (t.proto !== 'ssh') return '';
    return promptDir(t.term) || t.osc7 || titleDir(t.title);
}

export async function openFilesFor(t: Tab) {
    if (t.state === 'idle' || !t.req || t.dialog) return;
    if (t.proto === 'local') return toast('로컬 셸에서는 파일 전송 창을 쓰지 않습니다');
    if (t.proto === 'serial') return toast('시리얼 포트에서는 파일 전송 창 대신 sz / rz(Zmodem)를 쓰세요');
    try {
        await openFiles(t.id, t.req.host, {startDir: shellDir(t), onClose: () => focusActive()});
    } catch (e) {
        notice(t, String(e));
    }
}

export async function toggleEncoding(t: Tab) {
    if (t.proto === 'local' && t.state !== 'idle') return toast('로컬 셸은 UTF-8만 씁니다');
    const name = await SetEncoding(t.id, t.encoding === 'EUC-KR' ? 'UTF-8' : 'EUC-KR');
    applyEncoding(t, name);
    if (t.req) t.req.encoding = name;
    notice(t, `인코딩: ${name}`);
    updateTitle();
}

// Wails runs each Go call on its own goroutine, so calls made back to back can
// reach the server out of order (e.g. replies to a burst of terminal queries).
// One Send at a time keeps the order; input typed meanwhile goes in the next one.
export async function sendInput(t: Tab, data: string) {
    t.outbox += data;
    if (t.sending) return;
    t.sending = true;
    try {
        while (t.outbox) {
            const chunk = t.outbox;
            t.outbox = '';
            await Send(t.id, chunk);
        }
    } catch (e) {
        t.outbox = '';
        dbg(`tab ${t.id} send failed: ${e}`);
    } finally {
        t.sending = false;
    }
}

/**
 * Clears t's screen for the session being opened, once. A session's first
 * output can arrive before the call that opened it returns, so the screen is
 * cleared when the Go side says the session starts (term:open), not after
 * the call: the output would be cleared with it.
 */
function freshScreen(t: Tab, encoding: string) {
    if (!t.fresh) return;
    t.fresh = false;
    t.term.reset();
    applyEncoding(t, encoding);
}

// A session starts in the tab; its output follows. encoding: the one it is read with.
EventsOn('term:open', (id: number, encoding: string) => {
    const t = tabs.get(id);
    if (t) freshScreen(t, encoding);
});

// The window gets the output in base64; a browser gets the bytes (web_shim.js).
EventsOn('term:data', (id: number, data: string | Uint8Array) => {
    const t = tabs.get(id);
    if (!t) return;
    let bytes: Uint8Array;
    if (typeof data === 'string') {
        const bin = atob(data);
        bytes = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    } else {
        bytes = data;
    }
    const t0 = performance.now();
    t.term.write(bytes, () => {
        dbg(`tab ${id} wrote ${bytes.length} bytes in ${Math.round(performance.now() - t0)}ms`);
        t.hl.schedule();
    });
    if (t.group !== active?.group) t.el.classList.add('activity');
});

// lost: the connection broke (not ended by the server), so it may be reconnected.
EventsOn('term:closed', (id: number, msg: string, lost: boolean) => {
    const t = tabs.get(id);
    if (!t) return;
    dbg(`tab ${id} term:closed "${msg}" lost=${lost} autoReconnect=${settings.autoReconnect}`);
    forgetFiles(t.id);
    forwardsTabClosed(t.id);
    containersTabClosed(t.id);
    setState(t, 'idle');
    if (lost && settings.autoReconnect && t.req) scheduleRetry(t, 1, msg);
    else {
        t.term.write('\r\n');
        reconnectHint(t, msg);
    }
});

// path: the new log file, or "" when logging stopped.
EventsOn('log:changed', (id: number, path: string) => {
    const t = tabs.get(id);
    if (!t) return;
    t.log = path || undefined;
    t.el.classList.toggle('log', !!path);
    setState(t, t.state); // refresh the tooltip
    // While connecting, connectTab announces it once the screen is reset.
    if (t.state === 'on' || !path) notice(t, path ? `로그 기록 시작: ${path}` : '로그 기록을 멈췄습니다');
});

EventsOn('xfer:progress', (id: number) => tabs.get(id)?.el.classList.add('busy'));
EventsOn('xfer:end', (id: number) => tabs.get(id)?.el.classList.remove('busy'));

/** Opens (again) tab t's docker exec / logs session. Throws on failure. */
export async function startDocker(t: Tab) {
    const d = t.docker!;
    // The tab it was opened from is gone: another tab to the same server will do.
    const sshOn = (o?: Tab) => !!o && o.state === 'on' && o.proto === 'ssh' && !!o.req;
    if (d.src.tab && !sshOn(tabs.get(d.src.tab))) {
        const o = [...tabs.values()].find(o => sshOn(o) && o.req!.host === d.src.host && Number(o.req!.port) === d.src.port);
        if (o) d.src = {...d.src, tab: o.id};
    }
    cancelRetry(t);
    forgetFiles(t.id);
    t.fresh = true;
    try {
        await DockerOpen(t.id, d.src.tab, d.id, d.mode, `${d.name}@${d.src.label}`, t.term.cols, t.term.rows, engineOf(d.src));
    } catch (e) {
        t.fresh = false;
        throw e;
    }
    if (!tabs.has(t.id)) {
        CloseTab(t.id); // the tab was closed while opening
        return;
    }
    t.proto = 'docker';
    freshScreen(t, 'UTF-8');
    applyEncoding(t, 'UTF-8');
    setState(t, 'on');
    applyLook(t);
}

// ---- Session log ----

/** Starts or stops writing tab t's output to a file; log:changed reports the result. */
export async function toggleLog(t: Tab) {
    if (t.log) return StopLog(t.id);
    if (t.state !== 'on') return toast('연결된 터미널에서만 로그를 기록할 수 있습니다');
    try {
        await StartLog(t.id);
    } catch (e) {
        notice(t, String(e));
    }
}
