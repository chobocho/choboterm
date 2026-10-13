// The menu of a tab bar entry (right-click) and its color theme submenu.

import {openSaveSession} from './sessions';
import {settings} from './settings';
import {themeByName, THEMES} from './themes';
import {RunScriptFile, ShowScripts, StopScript, SendBreak, ShowLogs} from '../wailsjs/go/app/App';
import {active, focusActive, panesOf, Tab, tabs} from './tabstate';
import {notice, toast} from './notify';
import {chooseTheme} from './look';
import {runScript, showConsole} from './luaconsole';
import {showMemo} from './memo';
import {disconnect, openFilesFor, setState, toggleLog} from './connection';
import {duplicate, reconnect, requestClose, splitTab} from './tabs';
import {showContainers, showForwards} from './panels';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

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

const menu = $<HTMLUListElement>('tabMenu');
let menuTab: Tab | undefined;

export function showMenu(t: Tab, x: number, y: number) {
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
        case 'memo':
            showMemo(t);
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
