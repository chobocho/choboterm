// The OS the app runs on. Windows-only parts of the page (translucency,
// PuTTY import) carry the class "win-only" and are hidden on Linux.

export let isLinux = false;

// The page runs in WebKitGTK: the Linux window (or a WebKitGTK browser such
// as GNOME Web), not Chrome, Firefox or an Android WebView. Its IME events
// differ from other browsers' (see createTab in tabs.ts).
export const isWebKitGTK = /Linux/.test(navigator.userAgent) && /AppleWebKit/.test(navigator.userAgent)
    && !/Chrome|Chromium|Android/.test(navigator.userAgent);

// Set by the web server (web.go) when the page runs in a browser instead of
// the choboterm window. base numbers this page's tabs apart from the window's
// and other pages' (see clientOf in web.go).
interface WebPage {
    id: number;
    base: number;
    local: boolean; // the browser is on the PC running choboterm
    window: boolean; // choboterm has a window (not --headless)
}

export const webPage: WebPage | undefined = (window as unknown as {__cbt?: WebPage}).__cbt;

export function setPlatform(platform: string) {
    isLinux = platform === 'linux';
    document.body.classList.toggle('linux', isLinux);
    document.body.classList.toggle('web', webPage !== undefined);
}
