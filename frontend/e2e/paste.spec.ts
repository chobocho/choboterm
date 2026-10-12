import {expect, test} from './fixtures';
import type {App} from './fixtures';

// A paste event with its own text: the PC's clipboard is left alone.
function paste(app: App, text: string) {
    return app.page.evaluate(t => {
        const data = new DataTransfer();
        data.setData('text/plain', t);
        document.querySelector('#terms .term.focused')!
            .dispatchEvent(new ClipboardEvent('paste', {clipboardData: data, bubbles: true, cancelable: true}));
    }, text);
}

test.skip(process.platform !== 'win32', 'uses cmd commands');

test('one line is pasted as typed', async ({app}) => {
    await app.connectShell();
    await paste(app, 'echo one_^line');
    await expect(app.screen).toContainText('echo one_^line');
    await expect(app.page.locator('#pasteOverlay')).toBeHidden();
    await app.key('Enter');
    await expect(app.screen).toContainText('one_line');
});

test('several lines are confirmed first', async ({app}) => {
    await app.connectShell();
    const overlay = app.page.locator('#pasteOverlay');
    const text = 'echo first_^line\r\necho second_^line\r\n';

    await paste(app, text);
    await expect(overlay).toBeVisible();
    await expect(app.page.locator('#pastePreview')).toContainText('echo second_^line');
    await app.page.locator('#pasteCancel').click();
    await expect(overlay).toBeHidden();
    await expect(app.screen).not.toContainText('first_');

    await paste(app, text);
    await app.page.locator('#pasteOk').click();
    await expect(overlay).toBeHidden();
    await expect(app.screen).toContainText('first_line');
    await expect(app.screen).toContainText('second_line');
});

test('nothing is pasted into a tab that is not connected', async ({app}) => {
    await app.key('Escape');
    await paste(app, 'echo first\r\necho second\r\n');
    await expect(app.page.locator('#pasteOverlay')).toBeHidden();
    await expect(app.screen).not.toContainText('echo first');
});
