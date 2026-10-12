// The window title (in a browser, the page title): the active tab's
// connection, how it is drawn, and whether the web server runs.

import {tr} from './i18n';
import {EventsOn, WindowSetTitle} from '../wailsjs/runtime/runtime';
import {active} from './tabstate';

export let appName = 'choboterm';
export let version = '';

export function setVersion(v: string) {
    version = v;
    appName = `choboterm V${v}`;
}

// The web server's state, from the "web:status" event (window only).
const webServer = {running: false, clients: 0};

export function updateTitle() {
    let title = appName;
    const t = active;
    if (t?.docker && t.state !== 'idle') {
        title += ` - ${t.docker.name} @ ${t.docker.src.label} (docker)`;
    } else if (t?.req && t.state !== 'idle' && t.proto === 'local') {
        title += ` - ${t.req.host} (로컬)`;
    } else if (t?.req && t.state !== 'idle' && t.proto === 'serial') {
        title += ` - ${t.req.host} ${t.req.port} baud [${t.encoding}]`;
    } else if (t?.req && t.state !== 'idle') {
        const where = `${t.req.host}:${t.req.port}`;
        title += t.state === 'ftp' ? ` - ftp://${where}` : ` - ${where}`;
        title += ` [${t.encoding}]`;
    }
    // At the right end: whether this tab is drawn with GPU acceleration.
    if (t) title += t.webgl ? '  ⚡GPU' : '  🐢CPU';
    // And whether the web server runs, so it isn't forgotten.
    if (webServer.running) title += `  🌐 ${tr('웹 서버')} (${tr(`브라우저 ${webServer.clients}개`)})`;
    // Wails updates the native window title; browsers need the document title.
    document.title = title;
    WindowSetTitle(title);
}

EventsOn('web:status', (running: boolean, clients: number) => {
    webServer.running = running;
    webServer.clients = clients;
    updateTitle();
});
