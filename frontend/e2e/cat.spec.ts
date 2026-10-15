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

// Observe the rendered frames, including their actual height above the floor.
// Fake time lets us exercise whole routines without waiting for random idle choices.
async function observeMotion(page: Page, random: number) {
    await page.clock.install();
    await page.evaluate(value => {
        Math.random = () => value;
        const frames: {act: string; lift: number}[] = [];
        (window as unknown as {catMotion: typeof frames}).catMotion = frames;
        const el = document.getElementById('cat')!;
        new MutationObserver(() => {
            const floor = document.getElementById('terms')!.getBoundingClientRect().bottom - 1;
            frames.push({act: el.dataset.act!, lift: floor - el.getBoundingClientRect().bottom});
        }).observe(el, {attributes: true, attributeFilter: ['data-act']});
    }, random);
}

const motion = (page: Page) => page.evaluate(() =>
    (window as unknown as {catMotion: {act: string; lift: number}[]}).catMotion);

test('hovering the cat plays one complete belly roll without restarting on mouse moves', async ({app}) => {
    const page = app.page;
    await dismissGuide(app);
    await observeMotion(page, 0.4);
    await page.clock.runFor(6000); // finish any routine started before randomness was fixed
    await page.evaluate(() => (window as unknown as {catMotion: unknown[]}).catMotion.length = 0);
    const box = (await cat(page).boundingBox())!;
    // Stay over the lower body, which remains under the pointer when the cat lies down.
    await page.mouse.move(box.x + box.width / 2, box.y + box.height - 8);
    for (let i = 0; i < 24; i++) {
        await page.clock.runFor(250);
        const current = (await cat(page).boundingBox())!;
        await page.mouse.move(current.x + current.width / 2 + i % 2, current.y + current.height - 8);
    }
    expect((await motion(page)).map(f => f.act).filter(a => a.startsWith('roll:'))).toEqual([
        'roll:0', 'roll:1', 'roll:2', 'roll:1', 'roll:0', 'roll:1', 'roll:2',
    ]);
    await expect(bubble(page)).toBeHidden();
    await expect(cat(page)).toHaveAttribute('data-pose', 'sit');
});

test('passing over the cat briefly cancels its hover reaction', async ({app}) => {
    const page = app.page;
    await dismissGuide(app);
    await observeMotion(page, 0.4);
    await page.clock.runFor(6000);
    await page.evaluate(() => (window as unknown as {catMotion: unknown[]}).catMotion.length = 0);
    const c = await centre(page);
    await page.mouse.move(c.x, c.y);
    await page.clock.runFor(75);
    await page.mouse.move(4, 4);
    await page.clock.runFor(4000);
    expect((await motion(page)).some(f => f.act.startsWith('roll:'))).toBe(false);
});

async function beginWalk(app: App, dir: 1 | -1) {
    const page = app.page;
    await dismissGuide(app);
    await observeMotion(page, 0.4);
    await page.mouse.move(4, 4);
    await page.clock.runFor(6000);
    // Start from the middle so a distant click makes it walk in either direction.
    const terms = (await page.locator('#terms').boundingBox())!;
    const c = await centre(page);
    await page.mouse.move(c.x, c.y);
    await page.mouse.down();
    await page.mouse.move(terms.x + terms.width / 2, terms.y + terms.height - 25, {steps: 3});
    await page.mouse.up();
    await page.mouse.move(4, 4);
    await page.clock.runFor(3000);
    await page.mouse.click(dir === 1 ? terms.x + terms.width - 8 : terms.x + 8,
        terms.y + terms.height - 20);
    for (let i = 0; i < 40; i++) {
        await page.clock.runFor(100);
        if ((await cat(page).getAttribute('data-act'))?.startsWith('walk:')
            && await cat(page).getAttribute('data-dir') === String(dir)) break;
    }
    await expect(cat(page)).toHaveAttribute('data-act', /^walk:/);
    await expect(cat(page)).toHaveAttribute('data-dir', String(dir));
    await page.evaluate(() => { Math.random = () => 0.1; });
    await page.evaluate(() => (window as unknown as {catMotion: unknown[]}).catMotion.length = 0);
}

for (const dir of [1, -1] as const) {
    test(`a cat walking ${dir === 1 ? 'right' : 'left'} stops and stretches as it approaches the pointer`, async ({app}) => {
        const page = app.page;
        await beginWalk(app, dir);
        const box = (await cat(page).boundingBox())!;
        const nose = dir === 1 ? box.x + box.width : box.x;
        // Leave the pointer still: the cat starts outside its 70 px reaction range.
        await page.mouse.move(nose + dir * 100, box.y + box.height - 12);
        await page.clock.runFor(300);
        await expect(cat(page)).toHaveAttribute('data-act', /^walk:/);
        await page.clock.runFor(1200);
        await expect(cat(page)).toHaveAttribute('data-act', /^stretch:/);
        const stopped = (await cat(page).boundingBox())!;
        const distance = ((dir === 1 ? stopped.x + stopped.width : stopped.x) - nose) * dir;
        expect(distance).toBeGreaterThanOrEqual(29);
        expect(distance).toBeLessThanOrEqual(33);
        await page.clock.runFor(3000);
        const stretches = (await motion(page)).map(f => f.act).filter(a => a.startsWith('stretch:'));
        // Sitting down also uses stretch:5/6; only a new stretch:0 starts another stretch.
        expect(stretches.slice(0, 6)).toEqual([
            'stretch:0', 'stretch:1', 'stretch:2', 'stretch:3', 'stretch:4', 'stretch:5',
        ]);
        expect(stretches.filter(a => a === 'stretch:0')).toHaveLength(1);
    });
}

test('a walking cat ignores a pointer behind it or far above it', async ({app}) => {
    const page = app.page;
    await beginWalk(app, 1);
    let box = (await cat(page).boundingBox())!;
    await page.mouse.move(box.x - 30, box.y + box.height - 10);
    await page.clock.runFor(300);
    await expect(cat(page)).toHaveAttribute('data-act', /^walk:/);
    box = (await cat(page).boundingBox())!;
    await page.mouse.move(box.x + box.width + 40, box.y - 120);
    await page.clock.runFor(300);
    await expect(cat(page)).toHaveAttribute('data-act', /^walk:/);
    expect((await motion(page)).some(f => f.act.startsWith('stretch:'))).toBe(false);
});

test('regular stretching completes even while keys keep arriving', async ({app}) => {
    const page = app.page;
    await dismissGuide(app);
    await observeMotion(page, 0.4);
    for (let i = 0; i < 52; i++) {
        await page.keyboard.press('Shift');
        await page.clock.runFor(500);
    }
    const acts = (await motion(page)).map(f => f.act);
    const start = acts.indexOf('stretch:0');
    expect(start).toBeGreaterThanOrEqual(0);
    expect(acts.slice(start, start + 6)).toEqual([
        'stretch:0', 'stretch:1', 'stretch:2', 'stretch:3', 'stretch:4', 'stretch:5',
    ]);
});

test('sleep effects appear immediately and waking plays every frame before stretching', async ({app}) => {
    const page = app.page;
    await dismissGuide(app);
    await observeMotion(page, 0.4);
    // Stop on the very first sleeping frame rather than a later snore loop.
    for (let i = 0; i < 100; i++) {
        if (await cat(page).getAttribute('data-pose') === 'asleep') break;
        await page.clock.runFor(500);
    }
    await expect(cat(page)).toHaveAttribute('data-pose', 'asleep');
    await expect(page.locator('#catZ')).toBeVisible();
    await expect(page.locator('#catZ span').first()).toHaveCSS('animation-name', 'catz');
    await page.keyboard.press('Shift');
    await expect(page.locator('#catZ')).toBeHidden();
    for (let i = 0; i < 12; i++) {
        await page.keyboard.press('Shift');
        await page.clock.runFor(500);
    }
    const acts = (await motion(page)).map(f => f.act);
    const start = acts.indexOf('wake:0');
    expect(start).toBeGreaterThanOrEqual(0);
    expect(acts.slice(start, start + 13)).toEqual([
        'wake:0', 'wake:1', 'wake:2', 'wake:3', 'wake:4', 'wake:5', 'wake:6',
        'stretch:0', 'stretch:1', 'stretch:2', 'stretch:3', 'stretch:4', 'stretch:5',
    ]);
});

test('a high target produces a low jump and a complete landing', async ({app}) => {
    const page = app.page;
    await dismissGuide(app);
    await observeMotion(page, 0);
    await page.clock.runFor(3000);
    const box = (await cat(page).boundingBox())!;
    await page.mouse.click(box.x + box.width + 50, box.y - 100);
    await page.clock.runFor(4000);
    const frames = await motion(page);
    const peak = Math.max(...frames.filter(f => f.act.startsWith('jump:')).map(f => f.lift));
    expect(peak).toBeGreaterThan(20);
    expect(peak).toBeLessThanOrEqual(28);
    const acts = frames.map(f => f.act);
    const landing = acts.indexOf('jump:3');
    expect(landing).toBeGreaterThanOrEqual(0);
    expect(acts.slice(landing, landing + 3)).toEqual(['jump:3', 'jump:4', 'jump:5']);
});

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

test('a click elsewhere: the cat turns to it and watches it', async ({app}) => {
    const page = app.page;
    await dismissGuide(app);
    const terms = (await page.locator('#terms').boundingBox())!;
    const y = terms.y + terms.height - 30; // low enough to jump at
    for (const [px, d] of [[terms.x + 20, '-1'], [terms.x + terms.width - 20, '1']] as const) {
        await page.mouse.click(px, y);
        // turned that way (after a jump already in the air has landed)
        await expect(cat(page)).toHaveAttribute('data-dir', d, {timeout: 4000});
        // and comes, crouches, stares or pounces: no sitting about looking back
        await expect(cat(page)).toHaveAttribute('data-act', /^(walk|jump|sit|wake):/);
    }
});

test('typing: the cat turns to the cursor', async ({app}) => {
    const page = app.page;
    await dismissGuide(app);
    await app.connectShell();
    // the cat put down at the right end, facing away from the prompt on the left
    const terms = (await page.locator('#terms').boundingBox())!;
    const from = await centre(page);
    await page.mouse.move(from.x, from.y);
    await page.mouse.down();
    await page.mouse.move(terms.x + terms.width - 60, terms.y + terms.height - 40, {steps: 6});
    await page.mouse.up();
    await page.keyboard.type('echo meow', {delay: 80});
    await expect(cat(page)).toHaveAttribute('data-dir', '-1', {timeout: 4000});
});

test('with nothing to do for 30 s it naps, and a key wakes it', async ({app}) => {
    test.setTimeout(90_000);
    const page = app.page;
    await dismissGuide(app);
    // no key, click or mouse move: a short stroll, then curled up asleep
    await expect(cat(page)).toHaveAttribute('data-pose', 'asleep', {timeout: 60_000});
    await expect(page.locator('#catZ')).toBeVisible({timeout: 5000});
    await page.waitForTimeout(3000);
    await expect(cat(page)).toHaveAttribute('data-pose', 'asleep'); // and stays asleep
    await page.keyboard.press('Shift');
    await expect(cat(page)).not.toHaveAttribute('data-pose', 'asleep', {timeout: 2000});
    await expect(page.locator('#catZ')).toBeHidden({timeout: 2000});
});

test('clicking the cat runs Lua; the runs gather in one log to copy', async ({app}) => {
    const page = app.page;
    await dismissGuide(app);
    const c = await centre(page);
    await page.mouse.click(c.x, c.y);
    const input = bubble(page).locator('textarea');
    await expect(input).toBeFocused();
    await input.fill('print("meow")\n6 * 7');
    await input.press('Control+Enter');
    const result = page.locator('#luaResult');
    const log = result.locator('textarea.log');
    await expect(log).toHaveValue(/^> print\("meow"\)\n {2}6 \* 7\nmeow\n= 42\n-- \d+ms$/);
    await expect(log).not.toBeEditable();
    // the code keeps the focus, for the next run
    await expect(input).toBeFocused();

    // a second run is added below the first; errors show as errors (send() has no tab here)
    await input.fill('send("ls\\r")');
    await bubble(page).getByRole('button', {name: '실행'}).click();
    await expect(log).toHaveValue(/= 42\n-- \d+ms\n\n> send\("ls\\r"\)\n오류: .*탭 없이.*\n-- \d+ms$/);
    await expect(input).toBeFocused();

    // 모두 복사 copies the whole log
    await page.evaluate(() => {
        const w = window as unknown as {copied: string};
        w.copied = '';
        navigator.clipboard.writeText = async (t: string) => void (w.copied = t);
    });
    await result.getByRole('button', {name: '모두 복사'}).click();
    const all = await log.inputValue();
    await expect.poll(() => page.evaluate(() => (window as unknown as {copied: string}).copied)).toBe(all);

    // closed, it keeps the log for the next run; 지우기 empties it
    await log.press('Escape');
    await expect(result).toBeHidden();
    await expect(input).toBeFocused();
    await input.fill('1 + 2');
    await input.press('Control+Enter');
    await expect(log).toHaveValue(/^> print\("meow"\)[^]*\n\n> 1 \+ 2\n= 3\n-- \d+ms$/);
    await result.getByRole('button', {name: '지우기'}).click();
    await expect(log).toHaveValue('');
    // the 닫기 button closes it too, back to the code
    await result.getByRole('button', {name: '닫기', exact: true}).click();
    await expect(result).toBeHidden();
    await expect(input).toBeFocused();
    await input.press('Escape');
    await expect(bubble(page)).toBeHidden();
});

test('in English, a Lua error is translated after its place and function', async ({app}) => {
    const page = app.page;
    const file = path.join(app.configDir, 'settings.json');
    fs.writeFileSync(file, JSON.stringify({...saved(app), language: 'en'}));
    await page.reload();
    await bubble(page).getByRole('button', {name: 'Got it'}).click({timeout: 5000});
    const c = await centre(page);
    await page.mouse.click(c.x, c.y);
    const input = bubble(page).locator('textarea');
    await input.fill('send("ls\\r")');
    await input.press('Control+Enter');
    await expect(page.locator('#luaResult textarea.log')).toHaveValue(
        /\nError: console:1: send: send\(\) can't be used when running without a tab\n-- \d+ms$/);
});

test('the help tells about the cat and its setting', async ({app}) => {
    await app.key('F1');
    const help = app.page.locator('#help');
    await expect(help).toBeVisible();
    await expect(help).toContainText('고양이 보이기');
    await expect(help.locator('td, th, dt, div').filter({hasText: /^고양이$/})).toHaveCount(1);
});

// The test's browser logs in with the token only, so it counts as one on
// another PC: addresses open in it (window.open), folders can't be shown.
// Opening on this PC's screen is tested on the Go side (open_test.go).
test('the cat opens web addresses it is told to, as a message or with open_url', async ({app}) => {
    const page = app.page;
    await dismissGuide(app);
    await page.evaluate(() => {
        const w = window as unknown as {opened: string[]};
        w.opened = [];
        window.open = (u?: string | URL) => (w.opened.push(String(u)), null);
    });
    const opened = () => page.evaluate(() => (window as unknown as {opened: string[]}).opened.join(' '));
    const c = await centre(page);
    await page.mouse.click(c.x, c.y);
    const input = bubble(page).locator('textarea');
    const log = page.locator('#luaResult textarea.log');
    const run = async (code: string) => {
        await input.fill(code);
        await input.press('Control+Enter');
    };

    await run('https://example.com/a');
    await expect(log).toHaveValue(/^> https:\/\/example\.com\/a\n브라우저로 열었습니다: https:\/\/example\.com\/a\n-- \d+ms$/);
    await run('열기 github.com/chobocho');
    await expect(log).toHaveValue(/\n브라우저로 열었습니다: https:\/\/github\.com\/chobocho\n-- \d+ms$/);
    await run('open_url("example.org")\nreturn 1');
    await expect(log).toHaveValue(/\n> open_url\("example\.org"\)\n {2}return 1\n= 1\n-- \d+ms$/);
    await expect.poll(opened).toBe('https://example.com/a https://github.com/chobocho https://example.org');

    // refused: not http(s); a folder from another PC
    await run('열기 javascript:alert(1)');
    await expect(log).toHaveValue(/\n오류: .*http · https.*\n-- \d+ms$/);
    await run('폴더 /tmp');
    await expect(log).toHaveValue(/\n오류: .*폴더는 이 PC에서만 열 수 있습니다\n-- \d+ms$/);
    // Lua stays Lua: a.b is a field, not an address
    await run('math.pi > 3');
    await expect(log).toHaveValue(/\n> math\.pi > 3\n= true\n-- \d+ms$/);
    expect(await opened()).toBe('https://example.com/a https://github.com/chobocho https://example.org');
});

test('Esc closes the result window first, then the Lua box', async ({app}) => {
    const page = app.page;
    await dismissGuide(app);
    const c = await centre(page);
    await page.mouse.click(c.x, c.y);
    const input = bubble(page).locator('textarea');
    const result = page.locator('#luaResult');
    await input.fill('1 + 1');
    await input.press('Control+Enter');
    await expect(result).toBeVisible();
    await expect(input).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(result).toBeHidden();
    await expect(bubble(page)).toBeVisible();
    await expect(input).toBeFocused();
    await page.keyboard.press('Escape');
    await expect(bubble(page)).toBeHidden();
});
