import * as fs from 'fs';
import * as path from 'path';
import {expect, test} from './fixtures';

test('the font size follows Ctrl+= / Ctrl+- / Ctrl+0 and Ctrl+wheel', async ({app}) => {
    await app.key('Escape');
    expect(await app.fontSize()).toBe('15px');
    await app.key('Control+Equal');
    await expect(app.toast).toHaveText('글꼴 크기 16');
    await expect.poll(() => app.fontSize()).toBe('16px');
    await app.key('Control+Minus');
    await app.key('Control+Minus');
    await expect(app.toast).toHaveText('글꼴 크기 14');
    await expect.poll(() => app.fontSize()).toBe('14px');
    await app.key('Control+0');
    await expect(app.toast).toHaveText('글꼴 크기 15');

    await app.page.mouse.move(640, 400);
    await app.page.keyboard.down('Control');
    await app.page.mouse.wheel(0, -100);
    await app.page.keyboard.up('Control');
    await expect(app.toast).toHaveText('글꼴 크기 16');
    await expect.poll(() => app.fontSize()).toBe('16px');

    // The size is a setting (written a moment later): a page opened later starts with it.
    const saved = () => JSON.parse(fs.readFileSync(path.join(app.configDir, 'settings.json'), 'utf8')).fontSize;
    await expect.poll(saved).toBe(16);
    await app.page.reload();
    await expect(app.overlay).toBeVisible();
    await expect.poll(() => app.fontSize()).toBe('16px');
});

test('the windows opened by shortcuts take the keys until they are closed', async ({app}) => {
    await app.key('Escape');
    const windows: [string, string, string][] = [
        ['Control+Shift+O', '#prefsOverlay', '#pCancel'],
        ['Control+Shift+H', '#smOverlay', '#smClose'],
        ['Control+Shift+M', '#macrosOverlay', '#mClose'],
        ['Control+Shift+J', '#dkOverlay', '#dkClose'],
    ];
    for (const [keys, overlay, close] of windows) {
        await app.key(keys);
        await expect(app.page.locator(overlay), keys).toBeVisible();
        await app.key('Control+Shift+T'); // no new tab behind the window
        await expect(app.tabs).toHaveCount(1);
        await app.page.locator(close).click();
        await expect(app.page.locator(overlay), keys).toBeHidden();
    }
    // The toolbar buttons open the same windows.
    await app.page.locator('#prefsBtn').click();
    await expect(app.page.locator('#prefsOverlay')).toBeVisible();
    await app.page.locator('#pCancel').click();
    await app.page.locator('#sessBtn').click();
    await expect(app.page.locator('#smOverlay')).toBeVisible();
    await app.page.locator('#smClose').click();

    await app.key('F1');
    await expect(app.page.locator('#helpOverlay')).toBeVisible();
    await app.key('Control+Shift+T');
    await expect(app.tabs).toHaveCount(1);
    await app.key('F1');
    await expect(app.page.locator('#helpOverlay')).toBeHidden();
});

test('search opens over a terminal', async ({app}) => {
    await app.connectShell();
    await app.key('Control+Shift+S');
    await expect(app.page.locator('#search')).toBeVisible();
    await expect(app.page.locator('#searchInput')).toBeFocused();
    await app.key('Escape');
    await expect(app.page.locator('#search')).toBeHidden();
});

test('what a local shell cannot do is said in a toast', async ({app}) => {
    await app.connectShell();
    await app.key('Control+Shift+E');
    await expect(app.toast).toHaveText('로컬 셸은 UTF-8만 씁니다');
    await app.key('Control+Shift+F');
    await expect(app.toast).toHaveText('로컬 셸에서는 파일 전송 창을 쓰지 않습니다');
    await expect(app.page.locator('#filesOverlay')).toBeHidden();
    await app.key('Control+Shift+P');
    await expect(app.toast).toHaveText('포트 포워딩은 SSH 접속에서만 사용할 수 있습니다');
    await expect(app.page.locator('#pfOverlay')).toBeHidden();
});

test('Ctrl+Shift+D disconnects; Enter opens the dialog with the same server', async ({app}) => {
    await app.connectShell();
    await app.key('Control+Shift+D');
    await expect(app.activeTab).not.toHaveClass(/\bon\b/);
    await expect(app.screen).toContainText('연결을 끊었습니다');
    await app.key('Control+Shift+D'); // nothing more to disconnect
    await app.key('Enter');
    await expect(app.overlay).toBeVisible();
    await expect(app.host).toHaveValue(/.+/);
    await app.page.locator('#ok').click();
    await expect(app.overlay).toBeHidden();
    await expect(app.activeTab).toHaveClass(/\bon\b/);
});

test('shortcuts are not typed into the shell', async ({app}) => {
    test.skip(process.platform !== 'win32', 'uses cmd commands');
    await app.connectShell();
    await app.page.keyboard.type('echo a_^');
    await app.key('Control+Equal');
    await app.key('Control+Shift+O');
    await app.page.locator('#pCancel').click();
    await app.run('b');
    await expect(app.screen).toContainText('a_b');
});

test('scripts and macros need a connection', async ({app}) => {
    await app.key('Escape');
    await app.key('Control+Shift+K');
    await expect(app.toast).toHaveText('연결되어 있지 않습니다');
    await expect(app.page.locator('.script-box')).toHaveCount(0);
    await app.key('Control+Shift+L');
    await expect(app.toast).toHaveText('연결된 터미널에서만 로그를 기록할 수 있습니다');
});
