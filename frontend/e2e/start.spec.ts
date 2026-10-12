import {expect, SHELL, test} from './fixtures';

test('starts with one empty tab and its Connect dialog', async ({app}) => {
    await expect(app.tabs).toHaveCount(1);
    await expect(app.activeTab.locator('.label')).toHaveText('새 탭');
    await expect(app.activeTab).not.toHaveClass(/\bon\b/);
    await expect(app.page.locator('#connectTitle')).toHaveText(/^choboterm V\d+\.\d+\.\d+ - Connect to$/);
    await expect(app.page).toHaveTitle(/^choboterm V\d+\.\d+\.\d+\s+🐢CPU$/);
    await expect(app.host).toBeFocused();
    await expect(app.screen).toContainText(/choboterm V\d+\.\d+\.\d+/);
    await expect(app.screen).toContainText('Ctrl+Shift+T: 새 탭');
});

test('connects to a shell on this PC and shows its output', async ({app}) => {
    await app.connectShell();
    await expect(app.activeTab.locator('.label')).toHaveText(SHELL);
    await expect(app.activeTab).toHaveAttribute('title', `로컬 셸: ${SHELL}`);
    await expect(app.page).toHaveTitle(new RegExp(` - ${SHELL} \\(로컬\\)\\s+🐢CPU$`));
    // The welcome text is cleared when the tab connects.
    await expect(app.screen).not.toContainText('Ctrl+Shift+T: 새 탭');
    await app.run('echo out_^put');
    await expect(app.screen).toContainText('out_put');
});
