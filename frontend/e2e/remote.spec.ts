import {expect, test} from './fixtures';

// A server on another "machine": the tests' own TCP server, taken for Telnet.

test('a server on an unusual port: label, tooltip, title, input and output', async ({app, server}) => {
    await app.connect('127.0.0.1', server.port);
    const where = `127.0.0.1:${server.port}`;
    await expect(app.activeTab.locator('.label')).toHaveText(where);
    await expect(app.activeTab).toHaveAttribute('title', `telnet://${where}`);
    await expect(app.page).toHaveTitle(new RegExp(` - ${where} \\[UTF-8\\]\\s+🐢CPU$`));
    await expect(app.screen).toContainText(server.say('server ready'));

    await app.page.keyboard.type('typed-in-order-0123456789');
    await expect.poll(() => server.received).toContain('typed-in-order-0123456789');
    server.send('\r\nfrom the server\r\n');
    await expect(app.screen).toContainText('from the server');

    await app.openMenu();
    await expect(app.menuItem('files')).not.toHaveClass(/\bdisabled\b/);
    await expect(app.menuItem('forwards')).toHaveClass(/\bdisabled\b/); // SSH only
});

test('what the server sends at once is not lost when the screen is cleared', async ({app, server}) => {
    await app.connect('127.0.0.1', server.port);
    await expect(app.screen).toContainText('fake-host login:');
    await expect(app.screen).not.toContainText('Ctrl+Shift+T: 새 탭'); // the welcome text is gone

    // Connecting again clears the old output and shows the new greeting.
    await expect(app.screen).toContainText(server.say('old output'));
    await app.openMenu();
    await app.menuItem('reconnect').click();
    await expect.poll(() => server.accepted).toBe(2);
    await expect(app.screen).not.toContainText('old output');
    await expect(app.screen).toContainText('fake-host login:');
});

test('a failed connection leaves the screen as it was', async ({app, server}) => {
    await app.connect('127.0.0.1', server.port);
    await expect(app.screen).toContainText(server.say('still here'));
    await server.close();
    await expect(app.activeTab).not.toHaveClass(/\bon\b/);
    await app.key('Escape'); // the automatic reconnect
    await app.openMenu();
    await app.menuItem('reconnect').click();
    await expect(app.screen).toContainText('still here');
    await expect(app.activeTab).not.toHaveClass(/\bon\b/);
});

test('Ctrl+Shift+E switches the encoding', async ({app, server}) => {
    await app.connect('127.0.0.1', server.port);
    await app.key('Control+Shift+E');
    await expect(app.screen).toContainText('인코딩: EUC-KR');
    await expect(app.page).toHaveTitle(/\[EUC-KR\]/);
    await app.key('Control+Shift+E');
    await expect(app.screen).toContainText('인코딩: UTF-8');
    await expect(app.page).toHaveTitle(/\[UTF-8\]/);
});

test('the server ending the connection: Enter opens the dialog with that server', async ({app, server}) => {
    await app.connect('127.0.0.1', server.port);
    await expect(app.screen).toContainText(server.say('server ready'));
    server.end();
    await expect(app.activeTab).not.toHaveClass(/\bon\b/);
    await expect(app.screen).toContainText('연결이 종료되었습니다');
    await expect(app.screen).toContainText('Enter: 다시 접속');
    await expect(app.activeTab).not.toHaveClass(/\bretry\b/);

    await app.key('Enter');
    await expect(app.overlay).toBeVisible();
    await expect(app.host).toHaveValue('127.0.0.1');
    await expect(app.port).toHaveValue(String(server.port));
    await app.page.locator('#ok').click();
    await expect(app.activeTab).toHaveClass(/\bon\b/);
    expect(server.accepted).toBe(2);
});

test('a broken connection is retried; Esc cancels the retry', async ({app, server}) => {
    await app.connect('127.0.0.1', server.port);
    await expect(app.screen).toContainText(server.say('server ready'));
    server.reset();
    await expect(app.screen).toContainText('3초 후 다시 연결합니다 (1/10)');
    await expect(app.screen).toContainText('Enter: 지금 연결 · Esc: 취소');
    await expect(app.activeTab).toHaveClass(/\bretry\b/);
    await expect(app.activeTab).not.toHaveClass(/\bon\b/);

    await app.key('Escape');
    await expect(app.screen).toContainText('자동 재접속을 취소했습니다');
    await expect(app.activeTab).not.toHaveClass(/\bretry\b/);
    await app.page.waitForTimeout(3500);
    expect(server.accepted).toBe(1);
    await expect(app.activeTab).not.toHaveClass(/\bon\b/);
});

test('Enter retries a broken connection at once, keeping the screen', async ({app, server}) => {
    await app.connect('127.0.0.1', server.port);
    server.send('\r\nold output\r\n');
    await expect(app.screen).toContainText('old output');
    server.reset();
    await expect(app.screen).toContainText('3초 후 다시 연결합니다 (1/10)');
    await app.key('Enter');
    await expect(app.screen).toContainText('다시 연결했습니다');
    await expect(app.activeTab).toHaveClass(/\bon\b/);
    await expect(app.activeTab).not.toHaveClass(/\bretry\b/);
    await expect(app.screen).toContainText('old output');
    expect(server.accepted).toBe(2);
});

test('a broken connection comes back by itself', async ({app, server}) => {
    await app.connect('127.0.0.1', server.port);
    await expect(app.screen).toContainText(server.say('server ready'));
    server.reset();
    await expect(app.screen).toContainText('3초 후 다시 연결합니다 (1/10)');
    await expect(app.screen).toContainText('다시 연결했습니다', {timeout: 15_000});
    await expect(app.activeTab).toHaveClass(/\bon\b/);
});

test('Ctrl+Shift+D during the wait cancels the retry', async ({app, server}) => {
    await app.connect('127.0.0.1', server.port);
    await expect(app.screen).toContainText(server.say('server ready'));
    server.reset();
    await expect(app.activeTab).toHaveClass(/\bretry\b/);
    await app.key('Control+Shift+D');
    await expect(app.screen).toContainText('자동 재접속을 취소했습니다');
    await expect(app.activeTab).not.toHaveClass(/\bretry\b/);
});

test('reconnect and duplicate connect to the same server again', async ({app, server}) => {
    await app.connect('127.0.0.1', server.port);
    await expect(app.screen).toContainText(server.say('server ready'));
    await app.openMenu();
    await app.menuItem('reconnect').click();
    await expect.poll(() => server.accepted).toBe(2);
    await expect(app.activeTab).toHaveClass(/\bon\b/);

    await app.openMenu();
    await app.menuItem('duplicate').click();
    await expect(app.page.locator('#tabs .tab.on')).toHaveCount(2);
    await expect.poll(() => server.accepted).toBe(3);
    await expect(app.tabs.nth(1).locator('.label')).toHaveText(`127.0.0.1:${server.port}`);
});

test('a tab closed while it is connecting does not come back', async ({app, server}) => {
    await app.newTab();
    await app.connect('127.0.0.1', server.port);
    await app.key('Control+Shift+W');
    await app.page.locator('#askOk').click();
    await expect(app.tabs).toHaveCount(1);
    await expect(app.overlay).toBeVisible(); // the first tab's dialog
});
