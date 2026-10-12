import {defineConfig} from '@playwright/test';

// End-to-end tests of the page: each test starts its own headless choboterm
// (the web server without the window) with its own settings folder and drives
// the page in a browser already installed on this PC. See e2e/README.md.
export default defineConfig({
    testDir: './e2e',
    globalSetup: './e2e/global-setup.ts',
    outputDir: './e2e/.tmp/results',
    timeout: 60_000,
    expect: {timeout: 10_000},
    fullyParallel: true,
    workers: 4,
    forbidOnly: !!process.env.CI,
    reporter: 'list',
    use: {
        // E2E_BROWSER: msedge (default), chrome, or chromium (needs "npx playwright install chromium").
        channel: process.env.E2E_BROWSER === 'chromium' ? undefined : process.env.E2E_BROWSER || 'msedge',
        // Without a GPU the page draws terminals with the DOM renderer, whose text the tests can read.
        launchOptions: {args: ['--disable-gpu']},
        viewport: {width: 1280, height: 720},
        trace: 'retain-on-failure',
    },
});
