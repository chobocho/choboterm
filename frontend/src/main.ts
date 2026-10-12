import './debug';
import '@xterm/xterm/css/xterm.css';
import './style.css';

import {helpOpen, toggleHelp} from './help';
import {initTranslucencyPreview} from './prefs';
import {macroForKey} from './macros';
import {ask, askOpen, choose} from './dialog';
import {initAuthPrompt} from './authprompt';
import {Lang, startI18n} from './i18n';
import {openSearch} from './search';
import {loadSettings, settings} from './settings';
import {filesOpen, setActiveTabProvider} from './files';
import {GetLanguage, GetVersion, Quit, WebHideWindow} from '../wailsjs/go/main/App';
import {Environment, EventsOn, WindowUnminimise} from '../wailsjs/runtime/runtime';
import {setPlatform} from './platform';
import {checkUpdate} from './update';
import {active, focusActive, tabs, tabsEl} from './tabstate';
import {appName, setVersion} from './title';
import {applySettings, applyTranslucency, DEFAULT_FONT, setFontSize} from './look';
import {copySelection, pasteClipboard} from './clipboard';
import {ARROWS, isPaneKey, splitKey} from './split';
import {isAppShortcut, modalOpen} from './shortcuts';
import {showConsole} from './luaconsole';
import {disconnect, openFilesFor, toggleEncoding, toggleLog} from './connection';
import {openDialog} from './connectdialog';
import {activate, createTab, cycleTab, focusPane, requestClose, splitTab} from './tabs';
import {sendMacro, showContainers, showForwards, showMacros, showPrefs, showSessions} from './panels';

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;

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

$<HTMLButtonElement>('newTab').addEventListener('click', () => openDialog(null));
tabsEl.addEventListener('dblclick', ev => {
    if (ev.target === tabsEl) openDialog(null); // double-click empty tab bar: new tab
});

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
