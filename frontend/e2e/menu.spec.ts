import {expect, test} from './fixtures';

const disabled = /\bdisabled\b/;

test('an empty tab\'s menu offers only what works without a connection', async ({app}) => {
    await app.key('Escape');
    await app.openMenu();
    for (const act of ['duplicate', 'files', 'forwards', 'disconnect', 'saveSession', 'log', 'script', 'console', 'stopScript']) {
        await expect(app.menuItem(act), act).toHaveClass(disabled);
    }
    await expect(app.menuItem('reconnect')).not.toHaveClass(disabled);
    await expect(app.menuItem('break')).toBeHidden();
    await expect(app.menuItem('close')).toHaveText('탭 닫기');
    // A disabled item does nothing and the menu stays.
    await app.menuItem('disconnect').click();
    await expect(app.menu).toBeVisible();
    // Clicking elsewhere closes the menu.
    await app.page.mouse.click(640, 400);
    await expect(app.menu).toBeHidden();
});

test('a shell tab\'s menu', async ({app}) => {
    await app.connectShell();
    await app.openMenu();
    for (const act of ['files', 'forwards', 'stopScript']) await expect(app.menuItem(act), act).toHaveClass(disabled);
    for (const act of ['reconnect', 'duplicate', 'disconnect', 'saveSession', 'log', 'script', 'console']) {
        await expect(app.menuItem(act), act).not.toHaveClass(disabled);
    }
    await expect(app.menuItem('log')).toHaveText('로그 기록 시작');
});

test('duplicate opens the same server in a new tab', async ({app}) => {
    await app.connectShell();
    await app.openMenu();
    await app.menuItem('duplicate').click();
    await expect(app.menu).toBeHidden();
    await expect(app.page.locator('#tabs .tab.on')).toHaveCount(2);
    await expect(app.tabs.nth(1)).toHaveClass(/\bactive\b/);
    await expect(app.overlay).toBeHidden();
});

test('disconnect, then reconnect from the menu', async ({app}) => {
    await app.connectShell();
    await app.run('echo be_^fore');
    await expect(app.screen).toContainText('be_fore');
    await app.openMenu();
    await app.menuItem('disconnect').click();
    await expect(app.activeTab).not.toHaveClass(/\bon\b/);
    await expect(app.screen).toContainText('연결을 끊었습니다');
    await expect(app.screen).toContainText('Enter: 다시 접속');
    await expect(app.page).toHaveTitle(/^choboterm V[\d.]+\s+🐢CPU$/);

    await app.openMenu();
    await app.menuItem('reconnect').click();
    await expect(app.activeTab).toHaveClass(/\bon\b/);
    // A new connection starts with a clear screen.
    await expect(app.screen).not.toContainText('be_fore');
    await expect(app.overlay).toBeHidden();
});

test('a color theme chosen for the tab', async ({app}) => {
    await app.connectShell();
    const themes = app.page.locator('#themeMenu');
    const items = themes.locator('li:not(.sep)');
    const background = () => app.panes.first().evaluate(el => getComputedStyle(el).backgroundColor);
    const before = await background();

    await app.openMenu();
    await app.menuItem('theme').hover();
    await expect(themes).toBeVisible();
    await expect(items.first()).toHaveClass(/\bchecked\b/); // the one from Settings
    // Pointing at another item closes the submenu.
    await app.menuItem('files').hover();
    await expect(themes).toBeHidden();
    await app.menuItem('theme').hover();
    await items.last().click();
    await expect(app.menu).toBeHidden();
    await expect(themes).toBeHidden();
    await expect(app.toast).toContainText('색 테마:');
    await expect.poll(background).not.toBe(before);

    await app.openMenu();
    await app.menuItem('theme').click();
    await expect(items.last()).toHaveClass(/\bchecked\b/);
    await items.first().click();
    await expect.poll(background).toBe(before);
});

test('the session log is started and stopped', async ({app}) => {
    await app.connectShell();
    await app.openMenu();
    await app.menuItem('log').click();
    await expect(app.activeTab).toHaveClass(/\blog\b/);
    await expect(app.screen).toContainText('로그 기록 시작');
    await expect(app.activeTab).toHaveAttribute('title', /로그 기록 중: /);

    await app.openMenu();
    await expect(app.menuItem('log')).toHaveText('로그 기록 중지');
    await app.menuItem('log').click();
    await expect(app.activeTab).not.toHaveClass(/\blog\b/);
    await expect(app.screen).toContainText('로그 기록을 멈췄습니다');
});

test('saving the tab as a session names the tab', async ({app}) => {
    await app.connectShell();
    await app.openMenu();
    await app.menuItem('saveSession').click();
    await expect(app.page.locator('#sessSaveOverlay')).toBeVisible();
    await app.page.locator('#sessName').fill('build box');
    await app.page.locator('#sessSaveOk').click();
    await expect(app.page.locator('#sessSaveOverlay')).toBeHidden();
    await expect(app.activeTab.locator('.label')).toHaveText('build box');
    await expect(app.toast).toContainText('세션 저장: build box');
});
