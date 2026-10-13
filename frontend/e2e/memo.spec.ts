import {expect, test} from './fixtures';

test('memo pad paste is not intercepted by the terminal', async ({app}) => {
    await app.key('Escape');
    await app.key('Control+Shift+B');
    const input = app.page.locator('.memo-input');
    await expect(input).toBeVisible();

    // Native paste inserts the clipboard text only if no handler cancels this event.
    const allowed = await input.evaluate(el => {
        const data = new DataTransfer();
        data.setData('text/plain', 'clipboard note');
        return el.dispatchEvent(new ClipboardEvent('paste', {
            clipboardData: data,
            bubbles: true,
            cancelable: true,
        }));
    });
    expect(allowed).toBe(true);
});

test('memo pad autosaves and restores its text when reopened', async ({app}) => {
    await app.key('Escape');
    const memo = app.page.locator('.memo-box');
    const input = app.page.locator('.memo-input');

    await app.key('Control+Shift+B');
    await expect(memo).toBeVisible();
    await input.fill('remember this note\nsecond line');
    await app.key('Control+Shift+B');
    await expect(memo).toBeHidden();

    await app.page.locator('#memoBtn').click();
    await expect(memo).toBeVisible();
    await expect(input).toHaveValue('remember this note\nsecond line');
});
