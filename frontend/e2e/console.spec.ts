import {expect, test} from './fixtures';

test('the Lua console runs what is typed and shows the values', async ({app}) => {
    test.skip(process.platform !== 'win32', 'uses cmd commands');
    await app.connectShell();
    const box = app.page.locator('.term.focused .script-box');
    const input = box.locator('textarea.input');
    const badge = app.activeTab.locator('.run');
    await expect(badge).toBeHidden();

    await app.key('Control+Shift+K');
    await expect(box).toHaveClass(/\bconsole\b/);
    await expect(input).toBeFocused();
    await expect(badge).toHaveText('Lua');
    await expect(box.locator('.top span')).toHaveText('Lua 콘솔');
    await expect(box.locator('.lines .hint')).toContainText('Lua 5.1');

    await app.run('1 + 2');
    await expect(box.locator('.lines .in').last()).toHaveText('> 1 + 2');
    await expect(box.locator('.lines .val').last()).toHaveText('3');
    await expect(input).toHaveValue('');

    await app.run('print("printed " .. 6 * 7)');
    await expect(box.locator('.lines')).toContainText('printed 42');

    await app.run('nosuchfunction()');
    await expect(box.locator('.lines .err').last()).not.toBeEmpty();

    // ↑ brings earlier inputs back, ↓ goes forward to the empty line.
    await app.key('ArrowUp');
    await expect(input).toHaveValue('nosuchfunction()');
    await app.key('ArrowUp');
    await expect(input).toHaveValue('print("printed " .. 6 * 7)');
    await app.key('ArrowDown');
    await app.key('ArrowDown');
    await expect(input).toHaveValue('');

    // Shift+Enter adds a line instead of running.
    await app.page.keyboard.type('x = 5');
    await app.key('Shift+Enter');
    await app.page.keyboard.type('return x * 2');
    await expect(input).toHaveValue('x = 5\nreturn x * 2');
    await app.key('Enter');
    await expect(box.locator('.lines .val').last()).toHaveText('10');

    // send() types into the tab's shell.
    await app.run('send("echo lua_^sent\\r")');
    await expect(app.screen).toContainText('lua_sent');

    // Esc goes back to the terminal; the console stays.
    await app.key('Escape');
    await expect(input).not.toBeFocused();
    await expect(box).toBeVisible();

    // The shortcut again closes the console and ends its script.
    await app.key('Control+Shift+K');
    await expect(input).toHaveCount(0);
    await expect(badge).toBeHidden();
});

test('the console\'s ✕ closes it; the menu shows what a running console allows', async ({app}) => {
    await app.connectShell();
    await app.openMenu();
    await app.menuItem('console').click();
    const box = app.page.locator('.term.focused .script-box');
    await expect(box.locator('textarea.input')).toBeFocused();

    await app.openMenu();
    await expect(app.menuItem('script')).toHaveClass(/\bdisabled\b/); // one script at a time
    await expect(app.menuItem('console')).not.toHaveClass(/\bdisabled\b/);
    await expect(app.menuItem('stopScript')).not.toHaveClass(/\bdisabled\b/);
    await app.page.mouse.click(640, 200);
    await expect(app.menu).toBeHidden();

    await box.locator('.top button', {hasText: '✕'}).click();
    await expect(app.page.locator('.script-box textarea.input')).toHaveCount(0);
    await expect(app.activeTab.locator('.run')).toBeHidden();
});
