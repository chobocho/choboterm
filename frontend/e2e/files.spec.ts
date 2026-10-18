import {expect, test} from './fixtures';
import type {App, FakeFtpServer} from './fixtures';
import * as fs from 'fs';
import * as path from 'path';

// The file window over the tests' own FTP server: bookmarks and shortcuts.

async function openFtp(app: App, ftp: FakeFtpServer) {
    await expect(app.overlay).toBeVisible();
    await app.host.fill('127.0.0.1');
    await app.port.fill(String(ftp.port));
    await app.page.locator('#ok').click();
    await expect(app.page.locator('#filesOverlay')).toBeVisible();
    await expect(app.page.locator('#fPath')).toHaveValue(ftp.home);
    await expect(app.page.locator('#fBody')).toContainText('projects');
}

/** Types a folder into the path box and goes there. */
async function goTo(app: App, dir: string) {
    const input = app.page.locator('#fPath');
    await input.fill(dir);
    await input.press('Enter');
    await expect(input).toHaveValue(dir);
    await expect(app.page.locator('#fBody')).toContainText('readme.txt');
}

test('bookmarks: Ctrl+D adds and removes, Ctrl+1~9 and the ▾ list go there', async ({app, ftp}) => {
    const page = app.page;
    const star = page.locator('#fStar');
    const status = page.locator('#fError');
    const marks = page.locator('#fMarksMenu');
    await openFtp(app, ftp);
    await expect(star).toHaveText('☆');

    await app.key('Control+d');
    await expect(star).toHaveText('★');
    await expect(status).toHaveText('즐겨찾기에 추가했습니다: /home/e2e');

    await goTo(app, '/home/e2e/projects/');
    await expect(star).toHaveText('☆');
    await star.click(); // saved without the trailing slash
    await expect(star).toHaveText('★');
    await expect(status).toHaveText('즐겨찾기에 추가했습니다: /home/e2e/projects');

    await goTo(app, '/var');
    await goTo(app, '/var/log');
    await app.key('Control+1');
    await expect(page.locator('#fPath')).toHaveValue('/home/e2e');
    await app.key('Control+3');
    await expect(status).toHaveText('3번 즐겨찾기가 없습니다');

    // The list: bookmarks with their keys, home and "/", folders visited (not the bookmarked ones).
    await app.key('Control+b');
    await expect(marks).toBeVisible();
    const items = marks.locator('li[data-dir]');
    await expect(items).toHaveText([
        '/home/e2eCtrl+1✕', '/home/e2e/projectsCtrl+2✕', '홈 폴더 (/home/e2e)', '/ (최상위 폴더)', '/var/log', '/var',
    ]);
    await expect(marks.locator('li.active')).toHaveAttribute('data-dir', '/home/e2e'); // where the window is
    await app.key('ArrowDown');
    await app.key('Enter');
    await expect(marks).toBeHidden();
    await expect(page.locator('#fPath')).toHaveValue('/home/e2e/projects');

    // Clicks: ▾ opens it, a row goes there, ✕ takes a bookmark out, Escape only closes the list.
    await page.locator('#fMarks').click();
    await marks.locator('li[data-dir="/var/log"]').click();
    await expect(page.locator('#fPath')).toHaveValue('/var/log');
    await page.locator('#fMarks').click();
    await marks.locator('li[data-mark="/home/e2e/projects"] .remove').click();
    await expect(status).toHaveText('즐겨찾기에서 뺐습니다: /home/e2e/projects');
    await expect(marks.locator('li[data-mark]')).toHaveCount(1);
    await app.key('Escape');
    await expect(marks).toBeHidden();
    await expect(page.locator('#filesOverlay')).toBeVisible();

    await app.key('Control+2');
    await expect(status).toHaveText('2번 즐겨찾기가 없습니다');
    await app.key('Control+1');
    await expect(page.locator('#fPath')).toHaveValue('/home/e2e');
    await app.key('Control+d');
    await expect(star).toHaveText('☆');
    await expect(status).toHaveText('즐겨찾기에서 뺐습니다: /home/e2e');
});

test('bookmarks are kept per server for its other tabs and the next connection', async ({app, ftp}) => {
    await openFtp(app, ftp);
    await goTo(app, '/var/log');
    await app.key('Control+d');
    await expect(app.page.locator('#fStar')).toHaveText('★');

    // Saved with the server's prefs.
    const saved = JSON.parse(fs.readFileSync(path.join(app.configDir, 'sessions.json'), 'utf8'));
    expect(saved.servers).toContainEqual(expect.objectContaining({host: '127.0.0.1', port: ftp.port, bookmarks: ['/var/log']}));

    // A new tab on the same server starts at home but has the bookmark.
    await app.key('Escape'); // closes the file window
    await expect(app.page.locator('#filesOverlay')).toBeHidden();
    await app.newTab();
    await openFtp(app, ftp);
    await expect(app.page.locator('#fStar')).toHaveText('☆');
    await app.key('Control+1');
    await expect(app.page.locator('#fPath')).toHaveValue('/var/log');
    await expect(app.page.locator('#fStar')).toHaveText('★');
});
