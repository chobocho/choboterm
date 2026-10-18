import {test as base, expect, Locator, Page} from '@playwright/test';
import {ChildProcess, execFileSync, spawn} from 'child_process';
import * as fs from 'fs';
import * as net from 'net';
import * as path from 'path';

const WINDOWS = process.platform === 'win32';

/** The headless choboterm built by global-setup.ts. */
export const EXE = path.join(__dirname, '.tmp', 'bin', WINDOWS ? 'choboterm-e2e.exe' : 'choboterm-e2e');

/** A Host field text that starts a shell on this PC. */
export const SHELL = WINDOWS ? 'cmd' : 'bash';

// The access token is written before the program starts, so the URL is known.
const TOKEN = 'e2e'.padEnd(48, '0');

function freePort(): Promise<number> {
    return new Promise((resolve, reject) => {
        const s = net.createServer();
        s.once('error', reject);
        s.listen(0, '127.0.0.1', () => {
            const port = (s.address() as net.AddressInfo).port;
            s.close(() => resolve(port));
        });
    });
}

function listening(port: number): Promise<boolean> {
    return new Promise(resolve => {
        const c = net.connect(port, '127.0.0.1');
        c.once('connect', () => {
            c.destroy();
            resolve(true);
        });
        c.once('error', () => resolve(false));
    });
}

async function waitForPort(port: number, child: ChildProcess) {
    for (let i = 0; i < 200; i++) {
        if (child.exitCode !== null) throw new Error(`choboterm exited with code ${child.exitCode} before listening`);
        if (await listening(port)) return;
        await new Promise(r => setTimeout(r, 50));
    }
    throw new Error(`choboterm did not listen on port ${port}`);
}

/** Stops the program this test started (and the shells it runs), by its own process id only. */
function stop(child: ChildProcess) {
    if (child.exitCode !== null || child.pid === undefined) return;
    try {
        if (WINDOWS) execFileSync('taskkill', ['/pid', String(child.pid), '/t', '/f'], {stdio: 'ignore'});
        else child.kill('SIGTERM');
    } catch {
        // already gone
    }
}

/**
 * A TCP server standing in for a Telnet server: it greets, keeps what the
 * page sends, and can end or break its connections.
 */
export class FakeServer {
    private server = net.createServer();
    private sockets = new Set<net.Socket>();
    port = 0;
    /** Everything received so far (Telnet option negotiation included). */
    received = '';
    /** How many connections were accepted so far. */
    accepted = 0;

    async start(): Promise<this> {
        this.server.on('connection', s => {
            this.accepted++;
            this.sockets.add(s);
            s.on('data', d => (this.received += d.toString('latin1')));
            s.on('close', () => this.sockets.delete(s));
            s.on('error', () => undefined);
            s.write('fake-host login: ');
        });
        await new Promise<void>(resolve => this.server.listen(0, '127.0.0.1', resolve));
        this.port = (this.server.address() as net.AddressInfo).port;
        return this;
    }

    send(text: string) {
        for (const s of this.sockets) s.write(text);
    }

    /** Sends a line and returns it, for the test to find on the screen. */
    say(line: string): string {
        this.send(`\r\n${line}\r\n`);
        return line;
    }

    /** Ends the connections normally (the page shows "연결이 종료되었습니다"). */
    end() {
        for (const s of this.sockets) s.end();
    }

    /** Breaks the connections (a reset: the page treats the connection as lost). */
    reset() {
        for (const s of this.sockets) s.resetAndDestroy();
    }

    async close() {
        for (const s of this.sockets) s.destroy();
        await new Promise(resolve => this.server.close(resolve));
    }
}

/**
 * A small FTP server standing in for a real one, for the file window: it logs
 * anyone in, lists the folders of `tree` (MLSD over EPSV) and refuses changes.
 */
export class FakeFtpServer {
    private server = net.createServer();
    private sockets = new Set<net.Socket>();
    port = 0;
    /** The folder PWD reports right after login. */
    home = '/home/e2e';
    /** Folder -> its subfolders. Every folder in a path must be listed. */
    tree: Record<string, string[]> = {
        '/': ['home', 'var'],
        '/home': ['e2e'],
        '/home/e2e': ['projects', 'logs'],
        '/home/e2e/projects': ['app'],
        '/home/e2e/projects/app': [],
        '/home/e2e/logs': [],
        '/var': ['log'],
        '/var/log': [],
    };

    async start(): Promise<this> {
        this.server.on('connection', s => this.serve(s));
        await new Promise<void>(resolve => this.server.listen(0, '127.0.0.1', resolve));
        this.port = (this.server.address() as net.AddressInfo).port;
        return this;
    }

    private serve(s: net.Socket) {
        this.sockets.add(s);
        s.on('close', () => this.sockets.delete(s));
        s.on('error', () => undefined);
        const reply = (line: string) => s.write(`${line}\r\n`);
        let data: Promise<net.Socket> | undefined;
        let buf = '';
        reply('220 fake FTP ready');
        s.on('data', d => {
            buf += d.toString('utf8');
            let i: number;
            while ((i = buf.indexOf('\r\n')) >= 0) {
                const line = buf.slice(0, i);
                buf = buf.slice(i + 2);
                const sp = line.indexOf(' ');
                const cmd = (sp < 0 ? line : line.slice(0, sp)).toUpperCase();
                const arg = sp < 0 ? '' : line.slice(sp + 1);
                switch (cmd) {
                    case 'USER':
                        reply('331 Password please');
                        break;
                    case 'PASS':
                        reply('230 Logged in');
                        break;
                    case 'FEAT':
                        s.write('211-Features:\r\n MLST type*;size*;modify*;\r\n UTF8\r\n211 End\r\n');
                        break;
                    case 'TYPE':
                    case 'OPTS':
                    case 'NOOP':
                        reply('200 OK');
                        break;
                    case 'PWD':
                        reply(`257 "${this.home}"`);
                        break;
                    case 'EPSV': {
                        const l = net.createServer();
                        data = new Promise(resolve => l.once('connection', c => {
                            l.close();
                            resolve(c);
                        }));
                        l.listen(0, '127.0.0.1', () => reply(`229 Entering Extended Passive Mode (|||${(l.address() as net.AddressInfo).port}|)`));
                        break;
                    }
                    case 'MLSD': {
                        const dir = arg.replace(/\/+$/, '') || '/';
                        const subs = this.tree[dir];
                        const conn = data;
                        data = undefined;
                        if (!subs || !conn) {
                            conn?.then(c => c.destroy());
                            reply('550 No such directory');
                            break;
                        }
                        reply('150 Here it comes');
                        conn.then(c => {
                            const lines = subs.map(n => `type=dir;modify=20261018120000; ${n}\r\n`).join('') +
                                'type=file;size=5;modify=20261018120000; readme.txt\r\n';
                            c.end(lines, () => reply('226 Done'));
                        });
                        break;
                    }
                    case 'QUIT':
                        reply('221 Bye');
                        s.end();
                        break;
                    default:
                        reply('502 Not implemented');
                }
            }
        });
    }

    async close() {
        for (const s of this.sockets) s.destroy();
        await new Promise(resolve => this.server.close(resolve));
    }
}

/** The page of one running choboterm, with the parts the tests look at. */
export class App {
    readonly tabs: Locator;
    readonly activeTab: Locator;
    /** The Connect dialog. */
    readonly overlay: Locator;
    readonly host: Locator;
    readonly port: Locator;
    readonly error: Locator;
    readonly toast: Locator;
    /** The panes of the tab that is shown. */
    readonly panes: Locator;
    /** The text on the active pane's terminal. */
    readonly screen: Locator;
    readonly menu: Locator;

    constructor(readonly page: Page, readonly url: string, readonly configDir: string) {
        this.tabs = page.locator('#tabs .tab');
        this.activeTab = page.locator('#tabs .tab.active');
        this.overlay = page.locator('#overlay');
        this.host = page.locator('#host');
        this.port = page.locator('#port');
        this.error = page.locator('#error');
        this.toast = page.locator('#toast');
        this.panes = page.locator('#terms .group:not([hidden]) .term');
        this.screen = page.locator('#terms .term.focused .xterm-rows');
        this.menu = page.locator('#tabMenu');
    }

    key(keys: string) {
        return this.page.keyboard.press(keys);
    }

    /** Fills the Connect dialog that is open and connects; waits until the tab is connected. */
    async connect(host: string = SHELL, port?: number) {
        await expect(this.overlay).toBeVisible();
        await this.host.fill(host);
        if (port !== undefined) await this.port.fill(String(port));
        await this.page.locator('#ok').click();
        await expect(this.overlay).toBeHidden();
        await expect(this.activeTab).toHaveClass(/\bon\b/);
    }

    /** Connects the open Connect dialog to a shell on this PC and waits for its prompt. */
    async connectShell() {
        await this.connect(SHELL);
        await expect(this.screen).toContainText(WINDOWS ? '>' : '$');
    }

    /** Types a command line into the active terminal and runs it. */
    async run(command: string) {
        await this.page.keyboard.type(command);
        await this.page.keyboard.press('Enter');
    }

    /** Ctrl+Shift+T: a new tab; waits until it is shown with its Connect dialog. */
    async newTab() {
        const n = await this.tabs.count();
        await this.key('Control+Shift+T');
        await expect(this.tabs).toHaveCount(n + 1);
        await expect(this.tabs.nth(n)).toHaveClass(/\bactive\b/);
        await expect(this.overlay).toBeVisible();
    }

    /** Opens the tab menu of the nth tab bar entry (right-click). */
    async openMenu(index = 0) {
        await this.tabs.nth(index).click({button: 'right'});
        await expect(this.menu).toBeVisible();
    }

    menuItem(act: string): Locator {
        return this.menu.locator(`[data-act="${act}"]`);
    }

    /** The font size the terminals are drawn with. */
    fontSize(): Promise<string> {
        return this.screen.evaluate(el => getComputedStyle(el).fontSize);
    }
}

export const test = base.extend<{app: App; server: FakeServer; ftp: FakeFtpServer}>({
    app: async ({page}, use, testInfo) => {
        // Its own settings, history, sessions and home folder: nothing of the user's is read or changed.
        // No cat: it would walk over what the tests click (cat.spec.ts turns it on).
        const root = testInfo.outputPath('profile');
        const config = path.join(root, 'config');
        const home = path.join(root, 'home');
        const dir = path.join(config, 'choboterm');
        fs.mkdirSync(dir, {recursive: true});
        fs.mkdirSync(home, {recursive: true});
        fs.writeFileSync(path.join(dir, 'settings.json'), JSON.stringify({language: 'ko', autoReconnect: true, updateCheck: false, cat: false}));
        fs.writeFileSync(path.join(dir, 'web-token'), TOKEN + '\n');

        const port = await freePort();
        const child = spawn(EXE, ['--headless', `--web=127.0.0.1:${port}`], {
            env: {...process.env, APPDATA: config, XDG_CONFIG_HOME: config, USERPROFILE: home, HOME: home, CHOBOTERM_DEBUG: ''},
            cwd: home,
            stdio: 'ignore',
            windowsHide: true,
        });
        try {
            await waitForPort(port, child);
            const url = `http://127.0.0.1:${port}/`;
            await page.goto(`${url}?token=${TOKEN}`);
            // The page starts with one tab and its Connect dialog.
            await expect(page.locator('#overlay')).toBeVisible();
            await use(new App(page, url, dir));
        } finally {
            await page.close();
            stop(child);
        }
    },

    server: async ({}, use) => {
        const server = await new FakeServer().start();
        await use(server);
        await server.close();
    },

    ftp: async ({}, use) => {
        const ftp = await new FakeFtpServer().start();
        await use(ftp);
        await ftp.close();
    },
});

export {expect};
