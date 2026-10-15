// How the terminals look: the renderer (GPU or DOM), fonts and font size,
// colors, the cursor and translucency.

import {dbg} from './debug';
import {ambiguousFontReady, fontFamily, NARROW, WIDE} from './cjkwidth';
import {applyCat} from './cat';
import {loadRules} from './highlight';
import {saveSettings, settings} from './settings';
import {themeByName} from './themes';
import {SetTabTheme, TabTheme} from '../wailsjs/go/app/App';
import {isLinux} from './platform';
import {active, fitPane, Tab, tabs, termsEl} from './tabstate';
import {notice, toast} from './notify';
import {updateTitle} from './title';

function applyFontFamily(t: Tab) {
    const wide = t.encoding === 'EUC-KR';
    const font = fontFamily(wide ? settings.fontEucKr : settings.fontUtf8, wide);
    if (t.term.options.fontFamily !== font) t.term.options.fontFamily = font;
}

// EUC-KR screens assume ambiguous-width symbols (─│■○ etc.) take 2 columns.
export function applyEncoding(t: Tab, name: string) {
    t.encoding = name;
    const wide = name === 'EUC-KR';
    const wanted = wide ? WIDE : NARROW;
    applyFontFamily(t);
    // Never let a missing width provider break connecting.
    if (t.term.unicode.versions.includes(wanted)) t.term.unicode.activeVersion = wanted;
}

// Software WebGL (SwiftShader, when the GPU is blocked) is slower than the DOM renderer.
const hardwareWebgl = (() => {
    const gl = document.createElement('canvas').getContext('webgl2');
    if (!gl) return false;
    const info = gl.getExtension('WEBGL_debug_renderer_info');
    const renderer = info ? String(gl.getParameter(info.UNMASKED_RENDERER_WEBGL)) : '';
    gl.getExtension('WEBGL_lose_context')?.loseContext();
    dbg(`webgl renderer: ${renderer || '(unknown)'}`);
    return !/swiftshader|software|llvmpipe/i.test(renderer);
})();

// The default DOM renderer rebuilds a span per styled run on every frame, which makes
// full-screen TUIs with many colors (sc-im, htop...) stutter; load WebGL on demand.
async function useWebgl(t: Tab): Promise<void> {
    if (!hardwareWebgl) {
        dbg('webgl skipped: no hardware WebGL');
        t.webglLoading = false;
        return;
    }
    try {
        const {WebglAddon} = await import('@xterm/addon-webgl');
        // The tab may have closed or switched back to DOM rendering while the chunk loaded.
        if (tabs.get(t.id) !== t || t.webgl || (glassOn() && !translucency.gpu)) return;
        const webgl = new WebglAddon();
        // GPU reset or too many contexts: drop back to the DOM renderer.
        webgl.onContextLoss(() => {
            dbg('webgl context lost; back to DOM renderer');
            webgl.dispose();
            t.webgl = undefined;
            if (t === active) updateTitle();
        });
        t.term.loadAddon(webgl);
        t.webgl = webgl;
        dbg('webgl renderer loaded');
        if (t === active) updateTitle();
    } catch (e) {
        console.warn('WebGL renderer unavailable; using the DOM renderer', e);
    } finally {
        t.webglLoading = false;
    }
}

/**
 * Draws t with the GPU, except on a see-through background, where the GPU
 * renderer draws text without proper anti-aliasing and the browser draws it
 * instead, unless the GPU was asked for there too (Settings).
 */
export function chooseRenderer(t: Tab) {
    const want = !glassOn() || translucency.gpu;
    if (want && !t.webgl && !t.webglLoading) {
        t.webglLoading = true;
        void useWebgl(t);
    } else if (!want && t.webgl) {
        t.webgl.dispose();
        t.webgl = undefined;
    }
    if (t === active) updateTitle();
}

// WebGL caches glyphs, so redraw them once the ambiguous-width face has arrived.
// (Not on every 'loadingdone': redrawing can load fonts again and loop forever.)
ambiguousFontReady.then(() => {
    dbg(`ambiguous font ready; clearing texture atlas of ${tabs.size} tab(s)`);
    tabs.forEach(t => t.term.clearTextureAtlas());
});

// ---- Colors and cursor ----

type CursorStyle = 'block' | 'underline' | 'bar';

// ---- Translucency ----

// The window is always see-through where the page is (main.go), so both kinds
// are drawn here. What is shown now: the saved setting, or the one being tried in Settings.
let translucency = {mode: 'off', opacity: 100, gpu: false};

function seeThroughOn() {
    return translucency.mode !== 'off' && translucency.opacity < 100;
}

/** "배경만": terminal backgrounds are see-through, text stays solid. */
function glassOn() {
    return seeThroughOn() && translucency.mode === 'background';
}

/**
 * "창 전체" fades the tab bar and the terminals, text included; dialogs and
 * menus are outside them and stay opaque. "배경만" paints each pane's
 * background see-through.
 */
export function applyTranslucency(mode: string, opacity: number, gpu: boolean) {
    if (isLinux) mode = 'off'; // the window can't be see-through there
    translucency = {mode, opacity, gpu};
    const body = document.body.classList;
    body.toggle('seethrough', seeThroughOn());
    body.toggle('glass', glassOn());
    body.toggle('fade', seeThroughOn() && mode === 'window');
    document.documentElement.style.setProperty('--fade', String(opacity / 100));
    for (const t of tabs.values()) {
        chooseRenderer(t);
        applyLook(t);
    }
    paintMargin();
    dbg(`translucency ${mode} ${opacity}% gpu=${gpu} seethrough=${seeThroughOn()} glass=${glassOn()}`);
}

/** #rrggbb with the background opacity, for see-through panes. */
function seeThrough(color: string): string {
    const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})/i.exec(color);
    if (!m) return color;
    const [r, g, b] = m.slice(1).map(h => parseInt(h, 16));
    return `rgba(${r}, ${g}, ${b}, ${translucency.opacity / 100})`;
}

/** Terminal options set in the Settings window, besides the font; t's own theme if it has one. */
export function lookOptions(t?: Tab) {
    const style = (['block', 'underline', 'bar'] as const).find(s => s === settings.cursorStyle) ?? 'block';
    const theme = themeByName(t?.theme || settings.theme).theme;
    return {
        // Before theme: xterm works out its colors when the theme is set.
        allowTransparency: glassOn(),
        // With see-through panes the pane draws the background, once.
        theme: glassOn() ? {...theme, background: '#00000000'} : theme,
        cursorStyle: style as CursorStyle,
        cursorBlink: settings.cursorBlink,
        scrollback: settings.scrollback > 0 ? settings.scrollback : 5000,
    };
}

/** Applies the settings to every tab (after the Settings window's OK). */
export function applySettings() {
    applyTranslucency(settings.translucency || 'off', settings.opacity || 85, settings.glassGpu);
    applyFontSize();
    loadRules();
    for (const t of tabs.values()) t.hl.reset();
    applyCat();
}

export function applyLook(t: Tab) {
    Object.assign(t.term.options, lookOptions(t));
    const bg = themeByName(t.theme || settings.theme).theme.background ?? '#000';
    t.pane.style.background = glassOn() ? seeThrough(bg) : bg;
    if (t === active) paintMargin();
}

/** The margin around the terminals takes the active tab's background (inside the panes when see-through). */
export function paintMargin() {
    const bg = themeByName(active?.theme || settings.theme).theme.background ?? '#000';
    document.documentElement.style.setProperty('--term-bg', seeThroughOn() ? 'transparent' : bg);
}

/** Picks a color theme for tab t ("" = Settings) and remembers it for its server. */
export async function chooseTheme(t: Tab, name: string) {
    t.theme = name || undefined;
    applyLook(t);
    const label = name ? themeByName(name).label : '기본값 (설정 따름)';
    let saved = false;
    try {
        saved = await SetTabTheme(t.id, name);
    } catch (e) {
        notice(t, String(e));
    }
    if (!saved) return toast(`색 테마: ${label} (이 탭만)`);
    // Other tabs on the same server follow.
    for (const o of tabs.values()) {
        if (o === t || !o.req) continue;
        const theme = (await TabTheme(o.id)) || undefined;
        if (theme !== o.theme) {
            o.theme = theme;
            applyLook(o);
        }
    }
    toast(`색 테마: ${label} (${t.req?.host ?? '이 서버'}에 저장)`);
}

// ---- Font size ----

const MIN_FONT = 8;
const MAX_FONT = 40;
export const DEFAULT_FONT = 15;

export function setFontSize(n: number) {
    n = Math.max(MIN_FONT, Math.min(MAX_FONT, Math.round(n)));
    if (n !== settings.fontSize) {
        saveSettings(s => (s.fontSize = n));
        applyFontSize();
    }
    toast(`글꼴 크기 ${n}`);
}

function applyFontSize() {
    // Hidden tabs are fitted again when they are shown.
    for (const t of tabs.values()) {
        t.term.options.fontSize = settings.fontSize;
        applyFontFamily(t);
        fitPane(t);
    }
}

termsEl.addEventListener('wheel', ev => {
    if (!ev.ctrlKey) return;
    ev.preventDefault();
    ev.stopPropagation(); // don't scroll the terminal as well
    setFontSize(settings.fontSize + (ev.deltaY < 0 ? 1 : -1));
}, {capture: true, passive: false});
