// Which keys are the app's own rather than the terminal's.

import {pasteConfirmOpen} from './paste';
import {prefsOpen} from './prefs';
import {macroForKey, macrosOpen} from './macros';
import {forwardsOpen} from './forwards';
import {containersOpen} from './containers';
import {askOpen} from './dialog';
import {sessionManagerOpen, sessionSaveOpen} from './sessions';
import {authPromptOpen} from './authprompt';
import {viewerOpen} from './viewer-state';
import {imageOpen} from './imageview';
import {isPaneKey, splitKey} from './split';

/** A window that takes all keys until it is closed. */
export function modalOpen() {
    return pasteConfirmOpen() || prefsOpen() || macrosOpen() || forwardsOpen() || containersOpen() || askOpen() || viewerOpen() || imageOpen() ||
        authPromptOpen() || sessionSaveOpen() || sessionManagerOpen();
}

export function isAppShortcut(ev: KeyboardEvent): boolean {
    if (ev.type !== 'keydown') return false;
    if (ev.key === 'F1' && !ev.ctrlKey && !ev.altKey && !ev.shiftKey) return true;
    if (macroForKey(ev)) return true;
    if (ev.ctrlKey && !ev.altKey && ['=', '+', '-', '0'].includes(ev.key)) return true; // font size
    if (ev.ctrlKey && (ev.key === 'Tab' || ev.key === 'PageUp' || ev.key === 'PageDown')) return true;
    if (isPaneKey(ev) || splitKey(ev)) return true;
    return ev.ctrlKey && ev.shiftKey && !ev.altKey && /^[TNWDEFCVSOMPLHKJ]$/i.test(ev.key);
}
