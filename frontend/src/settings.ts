// User preferences, stored by the Go side in %AppData%\choboterm\settings.json.

import {GetSettings, SaveSettings} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

export let settings: main.Settings = main.Settings.createFrom({
    fontSize: 15,
    fontUtf8: 'D2Coding',
    fontEucKr: 'GulimChe',
    theme: 'choboterm',
    cursorStyle: 'block',
    cursorBlink: true,
    scrollback: 5000,
    translucency: 'off',
    opacity: 85,
    glassGpu: false,
    pasteNoConfirm: false,
    keepAlive: 60,
    autoReconnect: true,
    logAuto: false,
    logDir: '',
    logRaw: false,
    consoleHeight: 0,
    highlight: {
        on: true,
        red: 'error, errors, fail, failed, failure, fatal, denied, refused, exception, panic, critical, 오류, 에러, 실패',
        yellow: 'warn, warning, warnings, timeout, deprecated, 경고',
        green: 'success, successful, succeeded, passed, 성공, 완료',
    },
    macros: [],
});

let timer = 0;

export async function loadSettings() {
    try {
        settings = await GetSettings();
        settings.macros ??= [];
    } catch {
        // Keep the defaults.
    }
    return settings;
}

/** Changes settings and writes them shortly after (several quick changes are written once). */
export function saveSettings(change: (s: main.Settings) => void) {
    change(settings);
    clearTimeout(timer);
    timer = window.setTimeout(() => SaveSettings(settings).catch(() => undefined), 300);
}
