// The windows opened from a tab: settings, sessions, macros, port forwarding, docker containers.

import {openPrefs} from './prefs';
import {expandMacro, openMacros} from './macros';
import {openForwards} from './forwards';
import {DockerSource, openContainers} from './containers';
import {openSessionManager} from './sessions';
import {RunScript} from '../wailsjs/go/main/App';
import {config} from '../wailsjs/go/models';
import {active, focusActive, Tab} from './tabstate';
import {toast} from './notify';
import {applySettings} from './look';
import {modalOpen} from './shortcuts';
import {runScript} from './luaconsole';
import {sendInput} from './connection';
import {openSessions} from './connectdialog';
import {activate, openDockerTab} from './tabs';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

export function showPrefs() {
    if (!modalOpen()) openPrefs(applySettings, focusActive);
}

$<HTMLButtonElement>('prefsBtn').addEventListener('click', showPrefs);

export function showSessions() {
    if (!modalOpen()) openSessionManager(openSessions, focusActive);
}

$<HTMLButtonElement>('sessBtn').addEventListener('click', showSessions);

export function sendMacro(t: Tab, m: config.Macro) {
    if (t.state !== 'on') return toast('연결되어 있지 않습니다');
    if (m.kind === 'lua') return runScript(t, () => RunScript(t.id, m.name || '매크로', m.text));
    const text = expandMacro(m.text);
    if (text) sendInput(t, text);
}

export function showMacros() {
    if (modalOpen()) return;
    openMacros(m => active && sendMacro(active, m), focusActive);
}

export function showForwards(t: Tab) {
    if (modalOpen()) return;
    if (t.state !== 'on' || t.proto !== 'ssh' || !t.req) return toast('포트 포워딩은 SSH 접속에서만 사용할 수 있습니다');
    if (t !== active) activate(t);
    openForwards(t.id, t.req, focusActive);
}

export function showContainers() {
    if (modalOpen()) return;
    const t = active;
    const srcs: DockerSource[] = [];
    if (t && t.state === 'on' && t.proto === 'ssh' && t.req) {
        srcs.push({tab: t.id, label: `${t.req.login ? t.req.login + '@' : ''}${t.req.host}`, host: t.req.host, port: Number(t.req.port)});
    }
    openContainers(srcs, (src, c, mode) => openDockerTab(src, c.id, c.name, mode), focusActive);
}
