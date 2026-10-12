import {expect, SHELL, test} from './fixtures';

test('splitting opens a pane with the neighbour\'s server; Alt+arrows move between panes', async ({app}) => {
    await app.connectShell();
    await app.key('Control+Shift+Backslash');
    await expect(app.panes).toHaveCount(2);
    await expect(app.page.locator('#terms .split.row')).toHaveCount(1);
    await expect(app.overlay).toBeVisible();
    await expect(app.host).toHaveValue(SHELL);
    await app.connectShell();

    // One tab bar entry holding both panes.
    await expect(app.page.locator('#tabs .tabgroup')).toHaveCount(1);
    await expect(app.page.locator('#tabs .tabgroup.multi .tab.on')).toHaveCount(2);
    await expect(app.tabs.nth(1)).toHaveClass(/\bactive\b/);
    await expect(app.panes.nth(1)).toHaveClass(/\bfocused\b/);

    await app.key('Alt+ArrowLeft');
    await expect(app.tabs.nth(0)).toHaveClass(/\bactive\b/);
    await expect(app.panes.nth(0)).toHaveClass(/\bfocused\b/);
    await app.run('echo left_^pane');
    await expect(app.screen).toContainText('left_pane');
    await app.key('Alt+ArrowRight');
    await expect(app.tabs.nth(1)).toHaveClass(/\bactive\b/);
    await expect(app.screen).not.toContainText('left_pane');

    // Clicking a pane makes it the active one.
    await app.panes.nth(0).click();
    await expect(app.tabs.nth(0)).toHaveClass(/\bactive\b/);

    // Closing one pane leaves the other filling the tab.
    await app.openMenu(0);
    await expect(app.menuItem('close')).toHaveText('분할 창 닫기');
    await app.menuItem('close').click();
    await app.page.locator('#askOk').click();
    await expect(app.panes).toHaveCount(1);
    await expect(app.page.locator('#terms .split')).toHaveCount(0);
    await expect(app.page.locator('#tabs .tabgroup.multi')).toHaveCount(0);
    await expect(app.activeTab).toHaveClass(/\bon\b/);
    await expect(app.screen).not.toContainText('left_pane');
});

test('cancelling the new pane\'s dialog closes the pane', async ({app}) => {
    await app.connectShell();
    await app.key('Control+Shift+Minus');
    await expect(app.page.locator('#terms .split.col')).toHaveCount(1);
    await expect(app.panes).toHaveCount(2);
    await expect(app.overlay).toBeVisible();
    await app.key('Escape');
    await expect(app.panes).toHaveCount(1);
    await expect(app.page.locator('#terms .split')).toHaveCount(0);
    await expect(app.tabs).toHaveCount(1);
    await expect(app.overlay).toBeHidden();
});

test('a tab whose Connect dialog is open is not split', async ({app}) => {
    await app.key('Control+Shift+Backslash');
    await expect(app.toast).toHaveText('접속 창을 먼저 닫으세요');
    await expect(app.panes).toHaveCount(1);
});

test('the divider is dragged to resize; a double click halves again', async ({app}) => {
    await app.connectShell();
    await app.openMenu(0);
    await app.menuItem('splitRight').click();
    await app.connectShell();
    const gutter = app.page.locator('#terms .gutter');
    const left = app.panes.nth(0);
    const before = (await left.boundingBox())!;

    const g = (await gutter.boundingBox())!;
    await app.page.mouse.move(g.x + g.width / 2, g.y + g.height / 2);
    await app.page.mouse.down();
    await app.page.mouse.move(g.x - 200, g.y + g.height / 2, {steps: 5});
    await app.page.mouse.up();
    await expect.poll(async () => (await left.boundingBox())!.width).toBeLessThan(before.width - 100);

    await gutter.dblclick();
    await expect.poll(async () => Math.abs((await left.boundingBox())!.width - before.width)).toBeLessThan(2);
});
