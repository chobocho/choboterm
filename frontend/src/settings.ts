// User preferences, stored by the Go side in %AppData%\choboterm\settings.json.

import {GetSettings, SaveSettings} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';

export let settings: main.Settings = main.Settings.createFrom({fontSize: 15, pasteNoConfirm: false});

let timer = 0;

export async function loadSettings() {
    try {
        settings = await GetSettings();
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
