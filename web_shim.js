// choboterm web shim: lets the page built for the Wails window run in a
// browser. It provides what Wails injects into the window, window.go (calls
// into Go) and window.runtime (events, clipboard, ...), over one WebSocket.
// The protocol is described at the top of web_client.go.
(() => {
    'use strict';
    const cbt = window.__cbt; // {id, key, base, local, platform, arch, lang, window}
    const ko = cbt.lang !== 'en';
    const T = (k, e) => (ko ? k : e);

    // ---- Connection ----

    const listeners = new Map(); // event name -> [{cb, left}] (left < 0: no limit)
    const pending = new Map(); // call id -> {msg, resolve, reject}
    let ws = null;
    let seq = 0; // messages received
    let acked = 0; // last seq acknowledged
    let unackedBytes = 0;
    let ackTimer = 0;
    let nextCall = 1;
    let retry = 0;
    let gone = false;
    let connectedOnce = false;

    function connect() {
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(`${proto}//${location.host}/web/ws?c=${cbt.id}&k=${cbt.key}&seq=${seq}`);
        ws.binaryType = 'arraybuffer';
        ws.onopen = () => {
            retry = 0;
            status(null);
            // Calls made while disconnected, or sent but unanswered when the
            // connection dropped: the server runs each id only once.
            for (const p of pending.values()) ws.send(p.msg);
        };
        ws.onmessage = ev => {
            seq++;
            if (typeof ev.data === 'string') {
                unackedBytes += ev.data.length;
                onText(JSON.parse(ev.data));
            } else {
                unackedBytes += ev.data.byteLength;
                onBinary(ev.data);
            }
            scheduleAck();
        };
        ws.onclose = ev => {
            ws = null;
            if (gone) return;
            if (ev.code === 1001 && ev.reason === 'closed') {
                // The server closed this page on purpose (choboterm quit, the
                // web server stopped): reconnecting would never work.
                gone = true;
                status('gone');
                return;
            }
            status('reconnecting');
            setTimeout(connect, Math.min(5000, 200 * 2 ** retry++));
        };
    }

    // Acknowledge often enough that the server's output queue keeps moving,
    // rarely enough not to add a message per output chunk.
    function scheduleAck() {
        if (unackedBytes >= 256 * 1024 || seq - acked >= 64) return ack();
        if (!ackTimer) ackTimer = setTimeout(ack, 100);
    }

    function ack() {
        clearTimeout(ackTimer);
        ackTimer = 0;
        if (seq === acked || !ws || ws.readyState !== WebSocket.OPEN) return;
        ws.send(JSON.stringify({ack: seq}));
        acked = seq;
        unackedBytes = 0;
    }

    function onText(m) {
        if (m.gone) {
            gone = true;
            status('gone');
            ws && ws.close();
            return;
        }
        if (m.r !== undefined) {
            const p = pending.get(m.r);
            if (!p) return;
            pending.delete(m.r);
            if (m.x !== undefined) p.reject(m.x);
            else p.resolve(m.v);
            return;
        }
        if (m.e !== undefined) {
            if (m.e.startsWith('web:')) return internal(m.e, m.d);
            emit(m.e, m.d);
        }
    }

    const OP_OUTPUT = 1;

    function onBinary(buf) {
        const dv = new DataView(buf);
        if (dv.getUint8(0) === OP_OUTPUT) {
            // term:data with the bytes as they are (the window gets base64).
            emit('term:data', [dv.getUint32(1, true), new Uint8Array(buf, 5)]);
        }
    }

    function emit(name, args) {
        const ls = listeners.get(name);
        if (!ls) return;
        for (const l of [...ls]) {
            try {
                l.cb(...args);
            } catch (e) {
                console.error(`event ${name}:`, e);
            }
            if (l.left > 0 && --l.left === 0) off(name, l);
        }
    }

    function on(name, cb, max) {
        const l = {cb, left: max};
        if (!listeners.has(name)) listeners.set(name, []);
        listeners.get(name).push(l);
        return () => off(name, l);
    }

    function off(name, l) {
        const ls = listeners.get(name);
        if (!ls) return;
        const i = ls.indexOf(l);
        if (i >= 0) ls.splice(i, 1);
    }

    function call(method, args) {
        return new Promise((resolve, reject) => {
            const id = nextCall++;
            const msg = JSON.stringify({c: id, m: method, a: args});
            pending.set(id, {msg, resolve, reject});
            if (ws && ws.readyState === WebSocket.OPEN) ws.send(msg);
        }).then(v => {
            if (method === 'Connect' || method === 'DockerOpen') connectedOnce = true;
            return v;
        });
    }

    // ---- Status overlay (reconnecting / gone) ----

    let statusEl = null;

    function status(state) {
        if (!document.body) {
            document.addEventListener('DOMContentLoaded', () => status(state), {once: true});
            return;
        }
        if (!state) {
            statusEl && statusEl.remove();
            statusEl = null;
            return;
        }
        if (!statusEl) {
            statusEl = document.createElement('div');
            statusEl.style.cssText = 'position:fixed;inset:0;z-index:100000;display:grid;place-items:center;' +
                'background:rgba(0,0,0,.55);color:#eee;font:15px system-ui,sans-serif;text-align:center';
            document.body.appendChild(statusEl);
        }
        if (state === 'gone') {
            statusEl.innerHTML = `<div><p>${T('choboterm과의 연결이 끝났습니다. (서버가 다시 시작되었거나 오래 끊겨 있었습니다)',
                'The connection to choboterm has ended (the server restarted, or it was disconnected too long).')}</p>` +
                `<button>${T('새로 고침', 'Reload')}</button></div>`;
            statusEl.querySelector('button').onclick = () => location.reload();
        } else {
            statusEl.innerHTML = `<p>${T('다시 연결하는 중...', 'Reconnecting...')}</p>`;
        }
    }

    // ---- Files: the server's file choosers and downloads ----

    function internal(name, args) {
        const [a] = args;
        if (name === 'web:pick') pickFiles(a);
        else if (name === 'web:pickDone') closePick(a);
        else if (name === 'web:download') download(a.url, a.name);
    }

    function download(url, name) {
        const link = document.createElement('a');
        link.href = url;
        link.download = name;
        document.body.appendChild(link);
        link.click();
        link.remove();
    }

    function req(method, url, body) {
        return fetch(url, {method, body, headers: {'X-Cbt-Key': cbt.key}}).then(r => {
            if (!r.ok) return r.text().then(t => Promise.reject(new Error(t || r.statusText)));
            return r.status === 204 ? null : r.json();
        });
    }

    // upload sends files ({rel, file} or {rel, dir: true}) into a stage,
    // reporting progress, and returns the paths of the top-level names.
    async function upload(stage, items, progress) {
        const total = items.reduce((n, it) => n + (it.file ? it.file.size : 0), 0);
        let done = 0;
        for (const it of items) {
            const p = encodeURIComponent(it.rel);
            if (it.dir) {
                await req('PUT', `/web/stage/${stage}/dir?p=${p}`);
                continue;
            }
            await new Promise((resolve, reject) => {
                // XMLHttpRequest: fetch can't report upload progress.
                const x = new XMLHttpRequest();
                x.open('PUT', `/web/stage/${stage}/file?p=${p}`);
                x.setRequestHeader('X-Cbt-Key', cbt.key);
                x.upload.onprogress = e => progress && progress(it.rel, done + e.loaded, total);
                x.onload = () => (x.status < 300 ? resolve() : reject(new Error(x.responseText || x.statusText)));
                x.onerror = () => reject(new Error(T('업로드하지 못했습니다', 'Upload failed')));
                x.send(it.file);
            });
            done += it.file.size;
        }
        const tops = [...new Set(items.map(it => it.rel.split('/')[0]))];
        const r = await req('POST', `/web/stage/${stage}/done`, JSON.stringify({tops}));
        return r.paths || [];
    }

    // pickFiles shows the browser's file chooser for a picker in Go. The
    // chooser is also behind a button: browsers open it only right after a
    // click, and the click that asked for it may be too long ago.
    let pickBox = null;

    function pickFiles(p) {
        closePick();
        const box = document.createElement('div');
        box.dataset.id = p.id;
        box.style.cssText = 'position:fixed;inset:0;z-index:99999;display:grid;place-items:center;background:rgba(0,0,0,.45);' +
            'font:14px system-ui,sans-serif;color:#ddd';
        box.innerHTML = `<div style="background:#2b2b2b;padding:20px;border-radius:8px;min-width:300px;max-width:90vw">
            <p style="margin:0 0 12px"><b></b></p><p class="st" style="margin:0 0 12px;opacity:.8"></p>
            <input type="file" hidden>
            <button class="go">${T('파일 선택...', 'Choose files...')}</button> <button class="no">${T('취소', 'Cancel')}</button></div>`;
        box.querySelector('b').textContent = p.title || T('파일 선택', 'Choose files');
        const input = box.querySelector('input');
        input.multiple = !!p.multiple;
        if (p.accept) input.accept = p.accept;
        const st = box.querySelector('.st');
        const cancel = () => {
            if (ws) ws.send(JSON.stringify({cancel: p.id}));
            closePick();
        };
        box.querySelector('.no').onclick = cancel;
        box.querySelector('.go').onclick = () => input.click();
        input.addEventListener('cancel', cancel);
        input.onchange = async () => {
            const files = [...input.files];
            if (files.length === 0) return cancel();
            box.querySelectorAll('button').forEach(b => (b.disabled = true));
            try {
                await upload(p.id, files.map(f => ({rel: f.name, file: f})), (name, n, total) => {
                    st.textContent = `⬆ ${name} ${total ? Math.floor((n / total) * 100) : 100}%`;
                });
                closePick();
            } catch (e) {
                st.textContent = String(e.message || e);
                box.querySelector('.no').disabled = false;
            }
        };
        document.body.appendChild(box);
        pickBox = box;
        try {
            input.click(); // works if the user's click is still fresh
        } catch (e) { /* the button is there */ }
    }

    function closePick(id) {
        if (pickBox && (id === undefined || pickBox.dataset.id === id)) {
            pickBox.remove();
            pickBox = null;
        }
    }

    // ---- Drag and drop (runtime.OnFileDrop) ----

    let dropCb = null;
    let dropTargetOnly = true;

    // isDropTarget mirrors Wails: an element styled --wails-drop-target: drop.
    function dropTarget(el) {
        for (; el && el.nodeType === 1; el = el.parentElement) {
            if (getComputedStyle(el).getPropertyValue('--wails-drop-target').trim() === 'drop') return el;
        }
        return null;
    }

    let activeTarget = null;

    function setActive(el) {
        if (activeTarget === el) return;
        activeTarget && activeTarget.classList.remove('wails-drop-target-active');
        activeTarget = el;
        el && el.classList.add('wails-drop-target-active');
    }

    const hasFiles = e => e.dataTransfer && [...e.dataTransfer.types].includes('Files');

    window.addEventListener('dragover', e => {
        if (!hasFiles(e)) return;
        e.preventDefault(); // never let the browser open the dropped file
        const t = dropCb && (dropTargetOnly ? dropTarget(e.target) : document.body);
        e.dataTransfer.dropEffect = t ? 'copy' : 'none';
        setActive(dropTargetOnly ? t : null);
    });
    window.addEventListener('dragleave', e => {
        if (!e.relatedTarget) setActive(null);
    });
    window.addEventListener('drop', async e => {
        if (!hasFiles(e)) return;
        e.preventDefault();
        setActive(null);
        if (!dropCb || (dropTargetOnly && !dropTarget(e.target))) return;
        const cb = dropCb;
        const entries = [...e.dataTransfer.items].map(i => i.webkitGetAsEntry && i.webkitGetAsEntry()).filter(Boolean);
        const loose = entries.length ? [] : [...e.dataTransfer.files]; // no entry API
        const x = e.clientX, y = e.clientY;
        try {
            const items = loose.map(f => ({rel: f.name, file: f}));
            for (const en of entries) await walk(en, en.name, items);
            if (items.length === 0) return;
            const {id} = await req('POST', '/web/stage');
            const paths = await upload(id, items, null);
            cb(x, y, paths);
        } catch (err) {
            console.error('drop upload failed', err);
            alert(T('끌어 놓은 파일을 올리지 못했습니다: ', 'Could not upload the dropped files: ') + (err.message || err));
        }
    });

    // walk collects a dropped file or folder (with its contents).
    async function walk(entry, rel, out) {
        if (entry.isFile) {
            out.push({rel, file: await new Promise((res, rej) => entry.file(res, rej))});
            return;
        }
        const reader = entry.createReader();
        let any = false;
        for (;;) {
            const batch = await new Promise((res, rej) => reader.readEntries(res, rej));
            if (batch.length === 0) break;
            any = true;
            for (const c of batch) await walk(c, `${rel}/${c.name}`, out);
        }
        if (!any) out.push({rel, dir: true});
    }

    // ---- Clipboard ----

    function copyFallback(text) {
        // Without HTTPS (another PC over plain HTTP) there is no clipboard API.
        const ta = document.createElement('textarea');
        ta.value = text;
        ta.style.cssText = 'position:fixed;top:-1000px';
        document.body.appendChild(ta);
        ta.select();
        const ok = document.execCommand('copy');
        ta.remove();
        return ok;
    }

    // ---- window.go and window.runtime ----

    const App = new Proxy({}, {get: (_, method) => (...args) => call(String(method), args)});
    window.go = {main: {App}};

    const noop = () => {};
    const log = (...a) => console.log(...a);
    window.runtime = {
        LogPrint: log, LogTrace: log, LogDebug: log, LogInfo: log, LogWarning: log, LogError: log, LogFatal: log,
        EventsOnMultiple: (name, cb, max) => on(name, cb, max),
        EventsOn: (name, cb) => on(name, cb, -1),
        EventsOnce: (name, cb) => on(name, cb, 1),
        EventsOff: (...names) => names.forEach(n => listeners.delete(n)),
        EventsOffAll: () => listeners.clear(),
        EventsEmit: (name, ...data) => emit(name, data), // page-local, as nothing in Go listens
        WindowSetTitle: title => { document.title = title; },
        WindowReload: () => location.reload(),
        WindowReloadApp: () => location.reload(),
        WindowIsFullscreen: () => Promise.resolve(!!document.fullscreenElement),
        WindowFullscreen: () => document.documentElement.requestFullscreen && document.documentElement.requestFullscreen(),
        WindowUnfullscreen: () => document.fullscreenElement && document.exitFullscreen(),
        WindowIsMaximised: () => Promise.resolve(false),
        WindowIsMinimised: () => Promise.resolve(false),
        WindowIsNormal: () => Promise.resolve(true),
        WindowGetSize: () => Promise.resolve({w: window.innerWidth, h: window.innerHeight}),
        WindowGetPosition: () => Promise.resolve({x: 0, y: 0}),
        ScreenGetAll: () => Promise.resolve([]),
        BrowserOpenURL: url => window.open(url, '_blank', 'noopener'),
        Environment: () => Promise.resolve({buildType: 'production', platform: cbt.platform, arch: cbt.arch}),
        Quit: noop, Hide: noop, Show: noop,
        ClipboardGetText: () =>
            navigator.clipboard && navigator.clipboard.readText
                ? navigator.clipboard.readText().catch(() => '')
                : Promise.resolve(''),
        ClipboardSetText: text =>
            navigator.clipboard && navigator.clipboard.writeText
                ? navigator.clipboard.writeText(text).then(() => true, () => copyFallback(text))
                : Promise.resolve(copyFallback(text)),
        OnFileDrop: (cb, useDropTarget) => {
            dropCb = cb;
            dropTargetOnly = useDropTarget !== false;
        },
        OnFileDropOff: () => { dropCb = null; },
        CanResolveFilePaths: () => false,
        ResolveFilePaths: noop,
    };
    // The rest of the window API (size, position, theme...) means nothing here.
    for (const n of ['WindowSetAlwaysOnTop', 'WindowSetSystemDefaultTheme', 'WindowSetLightTheme', 'WindowSetDarkTheme',
        'WindowCenter', 'WindowSetSize', 'WindowSetMaxSize', 'WindowSetMinSize', 'WindowSetPosition', 'WindowHide',
        'WindowShow', 'WindowMaximise', 'WindowToggleMaximise', 'WindowUnmaximise', 'WindowMinimise', 'WindowUnminimise',
        'WindowSetBackgroundColour']) {
        window.runtime[n] = noop;
    }

    // Keys like Ctrl+W close the browser tab, not the terminal's: ask first
    // once something was connected.
    window.addEventListener('beforeunload', e => {
        if (connectedOnce && !gone) {
            e.preventDefault();
            e.returnValue = '';
        }
    });

    connect();
})();
