import {expect, test} from './fixtures';

test('the + button opens a tab; cancelling its dialog closes it again', async ({app}) => {
    await app.page.locator('#newTab').click();
    await expect(app.tabs).toHaveCount(2);
    await expect(app.tabs.nth(1)).toHaveClass(/\bactive\b/);
    await expect(app.overlay).toBeVisible();
    await app.key('Escape');
    await expect(app.tabs).toHaveCount(1);
    // Back in the first tab, whose own dialog is still open.
    await expect(app.tabs.nth(0)).toHaveClass(/\bactive\b/);
    await expect(app.overlay).toBeVisible();
});

test('each tab keeps its own Connect dialog', async ({app}) => {
    await app.host.fill('alpha.example');
    await app.port.fill('2201');
    await app.newTab();
    await app.host.fill('beta.example');
    await app.port.fill('2202');

    await app.key('Control+Tab');
    await expect(app.tabs.nth(0)).toHaveClass(/\bactive\b/);
    await expect(app.host).toHaveValue('alpha.example');
    await expect(app.port).toHaveValue('2201');

    await app.tabs.nth(1).click();
    await expect(app.tabs.nth(1)).toHaveClass(/\bactive\b/);
    await expect(app.host).toHaveValue('beta.example');
    await expect(app.port).toHaveValue('2202');

    await app.key('Control+Shift+Tab');
    await expect(app.host).toHaveValue('alpha.example');
});

test('closing the last tab leaves a new empty one', async ({app}) => {
    await app.key('Escape');
    await expect(app.overlay).toBeHidden();
    const id = await app.activeTab.getAttribute('data-id');
    await app.key('Control+Shift+W'); // not connected: no question
    await expect(app.tabs).toHaveCount(1);
    await expect(app.activeTab).not.toHaveAttribute('data-id', id!);
    await expect(app.overlay).toBeVisible();
});

test('closing a connected tab asks first', async ({app}) => {
    const ask = app.page.locator('#askOverlay');
    await app.connectShell();
    await app.key('Control+Shift+W');
    await expect(ask).toBeVisible();
    await expect(app.page.locator('#askMsg')).toContainText('연결되어 있습니다');
    await app.page.locator('#askCancel').click();
    await expect(ask).toBeHidden();
    await expect(app.activeTab).toHaveClass(/\bon\b/);

    // The tab's ✕ asks the same.
    await app.activeTab.locator('.x').click();
    await expect(ask).toBeVisible();
    await app.page.locator('#askOk').click();
    await expect(ask).toBeHidden();
    await expect(app.tabs).toHaveCount(1);
    await expect(app.activeTab).not.toHaveClass(/\bon\b/);
    await expect(app.overlay).toBeVisible();
});

test('a middle click closes a tab', async ({app}) => {
    await app.newTab();
    await app.tabs.nth(0).dispatchEvent('auxclick', {button: 1});
    await expect(app.tabs).toHaveCount(1);
});

test('tabs have their own shells; output in a hidden tab marks it', async ({app}) => {
    test.skip(process.platform !== 'win32', 'uses cmd commands');
    await app.connectShell();
    await app.newTab();
    await app.connectShell();
    await expect(app.page.locator('#tabs .tab.on')).toHaveCount(2);
    await expect(app.page.locator('#terms .group:not([hidden])')).toHaveCount(1);

    // The second tab prints something a little later, while the first is shown.
    await app.run('ping -n 3 127.0.0.1 >nul & echo late_^out');
    await app.key('Control+Tab');
    await expect(app.tabs.nth(0)).toHaveClass(/\bactive\b/);
    await app.run('echo tab_^one');
    await expect(app.screen).toContainText('tab_one');
    await expect(app.screen).not.toContainText('late_');
    await expect(app.tabs.nth(1)).toHaveClass(/\bactivity\b/);

    await app.key('Control+PageDown');
    await expect(app.tabs.nth(1)).toHaveClass(/\bactive\b/);
    await expect(app.tabs.nth(1)).not.toHaveClass(/\bactivity\b/);
    await expect(app.screen).toContainText('late_out');
    await expect(app.screen).not.toContainText('tab_one');
});

test('the shell ending leaves the tab ready to connect again', async ({app}) => {
    await app.connectShell();
    await app.run('exit');
    await expect(app.activeTab).not.toHaveClass(/\bon\b/);
    await expect(app.screen).toContainText('Enter: 다시 접속');
    await app.key('Enter');
    await expect(app.overlay).toBeVisible();
});
