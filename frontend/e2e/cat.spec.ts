import * as fs from 'fs';
import * as path from 'path';
import {Page} from '@playwright/test';
import {App, expect, test} from './fixtures';

// The cat (cat.ts). It never stands still, so it is driven by coordinates
// from its box rather than by Playwright's clicks, which wait for an element
// to stop moving.

const cat = (page: Page) => page.locator('#cat');
const bubble = (page: Page) => page.locator('#catBubble');

async function centre(page: Page) {
    const b = await cat(page).boundingBox();
    if (!b) throw new Error('the cat has no box');
    return {x: b.x + b.width / 2, y: b.y + b.height / 2};
}

function saved(app: App): Record<string, unknown> {
    try {
        return JSON.parse(fs.readFileSync(path.join(app.configDir, 'settings.json'), 'utf8'));
    } catch {
        return {};
    }
}

// The other tests run without the cat; here it is on, as at a first start.
test.beforeEach(async ({app}) => {
    const file = path.join(app.configDir, 'settings.json');
    const s = JSON.parse(fs.readFileSync(file, 'utf8'));
    fs.writeFileSync(file, JSON.stringify({...s, cat: true, catIntro: false}));
    await app.page.reload();
});

/** Closes the first-time guide (it opens 1.5 s after the cat appears). */
async function dismissGuide(app: App) {
    await expect(bubble(app.page)).toContainText('안녕하세요', {timeout: 5000});
    await bubble(app.page).getByRole('button', {name: '알겠어요'}).click();
    await expect(bubble(app.page)).toBeHidden();
}

test('the cat is there from the first start, and its guide shows once', async ({app}) => {
    await expect(cat(app.page)).toBeVisible();
    // it stands on the terminals' bottom edge
    const terms = await app.page.locator('#terms').boundingBox();
    const box = await cat(app.page).boundingBox();
    expect(Math.abs(box!.y + box!.height - (terms!.y + terms!.height))).toBeLessThan(4);
    await dismissGuide(app);
    await expect.poll(() => saved(app).catIntro).toBe(true);
    await app.page.reload();
    await expect(cat(app.page)).toBeVisible();
    await app.page.waitForTimeout(2500);
    await expect(bubble(app.page)).toBeHidden();
});

test('the guide can hide the cat, and Settings bring it back', async ({app}) => {
    await expect(bubble(app.page)).toContainText('안녕하세요', {timeout: 5000});
    await bubble(app.page).getByRole('button', {name: '지금 숨기기'}).click();
    await expect(cat(app.page)).toBeHidden();
    await expect.poll(() => saved(app).cat).toBe(false);
    await app.key('Control+Shift+O');
    const box = app.page.locator('#pCat');
    await expect(box).not.toBeChecked();
    await box.check();
    await app.page.locator('#pOk').click();
    await expect(cat(app.page)).toBeVisible();
    await expect.poll(() => saved(app).cat).toBe(true);
});

test('dragging puts the cat elsewhere', async ({app}) => {
    await dismissGuide(app);
    const from = await centre(app.page);
    const to = {x: 420, y: 260};
    await app.page.mouse.move(from.x, from.y);
    await app.page.mouse.down();
    await app.page.mouse.move((from.x + to.x) / 2, (from.y + to.y) / 2, {steps: 4});
    await app.page.mouse.move(to.x, to.y, {steps: 4});
    // held: it follows the pointer
    const held = await centre(app.page);
    expect(Math.abs(held.x - to.x)).toBeLessThan(6);
    expect(Math.abs(held.y - to.y)).toBeLessThan(6);
    await app.page.mouse.up();
    await expect(bubble(app.page)).toBeHidden(); // a drag is not a click
    // it lands there and stays at that height
    await app.page.waitForTimeout(1500);
    const box = await cat(app.page).boundingBox();
    expect(Math.abs(box!.y + box!.height - (to.y + 30))).toBeLessThan(40);
});

test('clicking the cat runs Lua and shows the result', async ({app}) => {
    await dismissGuide(app);
    const c = await centre(app.page);
    await app.page.mouse.click(c.x, c.y);
    const input = bubble(app.page).locator('textarea');
    await expect(input).toBeFocused();
    await input.fill('print("meow") return 6 * 7');
    await input.press('Control+Enter');
    const result = app.page.locator('#luaResult');
    await expect(result).toBeVisible();
    await expect(result.locator('pre').nth(1)).toHaveText('meow');
    await expect(result.locator('pre.value')).toHaveText('42');
    // errors show as errors; send() has no tab here
    await result.press('Escape');
    await expect(result).toBeHidden();
    await expect(input).toBeFocused();
    await input.fill('send("ls\\r")');
    await bubble(app.page).getByRole('button', {name: '실행'}).click();
    await expect(result.locator('pre.error')).toContainText('탭 없이');
    await result.press('Escape');
    await input.press('Escape');
    await expect(bubble(app.page)).toBeHidden();
});
