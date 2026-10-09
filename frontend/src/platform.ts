// The OS the app runs on. Windows-only parts of the page (translucency,
// PuTTY import) carry the class "win-only" and are hidden on Linux.

export let isLinux = false;

export function setPlatform(platform: string) {
    isLinux = platform === 'linux';
    document.body.classList.toggle('linux', isLinux);
}
