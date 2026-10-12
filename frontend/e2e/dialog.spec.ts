import * as net from 'net';
import {expect, SHELL, test} from './fixtures';

/** A port on this PC that nothing listens on. */
function closedPort(): Promise<number> {
    return new Promise(resolve => {
        const s = net.createServer();
        s.listen(0, '127.0.0.1', () => {
            const port = (s.address() as net.AddressInfo).port;
            s.close(() => resolve(port));
        });
    });
}

test('the port decides the protocol shown', async ({app}) => {
    const proto = app.page.locator('#proto');
    const jump = app.page.locator('#jump');
    await app.host.fill('example.com');
    for (const [port, name] of [['22', 'SSH'], ['21', 'FTP'], ['23', 'Telnet'], ['2222', '자동 감지']]) {
        await app.port.fill(port);
        await expect(proto).toHaveText(name);
    }
    // Jump hosts are for SSH only.
    await app.port.fill('21');
    await expect(jump).toBeDisabled();
    await app.port.fill('22');
    await expect(jump).toBeEnabled();
    // Only digits stay in the Port field.
    await app.port.fill('22ab');
    await expect(app.port).toHaveValue('22');
});

test('a local shell needs no port, login or password', async ({app}) => {
    await app.host.fill(SHELL);
    await expect(app.page.locator('#proto')).toHaveText('로컬 셸');
    for (const id of ['#port', '#login', '#pass', '#encoding', '#jump']) await expect(app.page.locator(id)).toBeDisabled();
    await app.host.fill('example.com');
    for (const id of ['#port', '#login', '#pass', '#encoding']) await expect(app.page.locator(id)).toBeEnabled();
});

test('a serial port takes its speed in the Port field', async ({app}) => {
    const label = app.page.locator('#portLabel');
    await app.host.fill('example.com');
    await app.port.fill('2222');
    await app.host.fill('COM3');
    await expect(label).toHaveText('Baud');
    await expect(app.port).toHaveValue('115200');
    await expect(app.port).toBeEnabled();
    await expect(app.page.locator('#proto')).toHaveText('시리얼');
    for (const id of ['#login', '#pass', '#jump']) await expect(app.page.locator(id)).toBeDisabled();
    // The server's port comes back with a server's name.
    await app.host.fill('example.com');
    await expect(label).toHaveText('Port');
    await expect(app.port).toHaveValue('2222');
    await expect(app.page.locator('#login')).toBeEnabled();
});

test('the Host field explains itself and lists what it can open', async ({app}) => {
    const tip = app.page.locator('#hostTip');
    const list = app.page.locator('#hostList');
    await app.host.click();
    await expect(tip).toBeVisible();
    await expect(tip).toContainText('~/.ssh/config');

    await app.page.locator('#hostDrop').click();
    await expect(list).toBeVisible();
    await expect(tip).toBeHidden();
    await expect(list.locator('li', {hasText: '로컬 셸'}).first()).toBeVisible();
    // ▼ again closes the list; the Host field keeps the focus.
    await app.page.locator('#hostDrop').click();
    await expect(list).toBeHidden();
    await expect(app.host).toBeFocused();
    // From another field, ▼ opens the list and moves to the Host field.
    await app.port.click();
    await app.page.locator('#hostDrop').click();
    await expect(list).toBeVisible();
    await expect(app.host).toBeFocused();
    await app.key('Escape'); // closes the list, not the dialog
    await expect(list).toBeHidden();
    await expect(app.overlay).toBeVisible();
});

test('typing narrows the list; the arrows pick from it', async ({app}) => {
    test.skip(process.platform !== 'win32', 'the list holds the Windows shells');
    const list = app.page.locator('#hostList');
    await app.host.fill('powersh');
    await expect(list).toBeVisible();
    await expect(list.locator('li')).toHaveCount(1);
    await app.host.press('ArrowDown');
    await expect(app.host).toHaveValue('powershell');
    await expect(list.locator('li.active')).toHaveCount(1);
    await expect(app.page.locator('#proto')).toHaveText('로컬 셸');
    // Enter takes the picked entry: for a local shell the Connect button is next.
    await app.host.press('Enter');
    await expect(list).toBeHidden();
    await expect(app.overlay).toBeVisible();
    await expect(app.page.locator('#ok')).toBeFocused();
});

test('Enter in the Host field connects to what was typed', async ({app}) => {
    await app.host.fill(SHELL);
    await app.host.press('Enter');
    await expect(app.overlay).toBeHidden();
    await expect(app.activeTab).toHaveClass(/\bon\b/);
});

test('a failed connection keeps the dialog open with the error', async ({app}) => {
    const ok = app.page.locator('#ok');
    await app.host.fill('127.0.0.1');
    await app.port.fill(String(await closedPort()));
    await ok.click();
    await expect(app.error).not.toBeEmpty();
    await expect(app.overlay).toBeVisible();
    await expect(ok).toBeEnabled();
    await expect(ok).toHaveText('Connect');
    await expect(app.activeTab).not.toHaveClass(/\bon\b/);
    // The same dialog connects once the values are right.
    await app.connect(SHELL);
    await expect(app.error).toBeEmpty();
});

test('Escape closes the dialog; Enter in the empty tab opens it again', async ({app}) => {
    await app.host.fill('example.com');
    await app.key('Escape');
    await expect(app.overlay).toBeHidden();
    await expect(app.tabs).toHaveCount(1);
    await app.key('Enter');
    await expect(app.overlay).toBeVisible();
    await expect(app.host).toHaveValue('example.com');
    await app.page.locator('#cancel').click();
    await expect(app.overlay).toBeHidden();
});

test('saving a session needs a host', async ({app}) => {
    await app.host.fill('');
    await app.page.locator('#saveSess').click();
    await expect(app.error).toHaveText('Host를 입력하세요');
    await expect(app.page.locator('#sessSaveOverlay')).toBeHidden();
});

test('a session saved from the dialog is listed under its name', async ({app}) => {
    await app.host.fill(SHELL);
    await app.page.locator('#saveSess').click();
    await expect(app.page.locator('#sessSaveOverlay')).toBeVisible();
    await app.page.locator('#sessName').fill('my shell');
    await app.page.locator('#sessSaveOk').click();
    await expect(app.page.locator('#sessSaveOverlay')).toBeHidden();
    await expect(app.toast).toContainText('세션 저장: my shell');

    await app.page.locator('#hostDrop').click();
    const list = app.page.locator('#hostList');
    await expect(list.locator('li.head').first()).toHaveText('★ 저장된 세션');
    await expect(list.locator('li:not(.head)').first()).toContainText('my shell');
    // Connecting with the saved session picked names the tab after it.
    await app.key('Escape');
    await app.page.locator('#ok').click();
    await expect(app.overlay).toBeHidden();
    await expect(app.activeTab.locator('.label')).toHaveText('my shell');
});
