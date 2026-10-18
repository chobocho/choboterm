import {expect, test} from './fixtures';

// FTPS against a real server with a self-signed certificate, e.g. pyftpdlib's
// TLS_FTPHandler (login user / pass):
//
//     E2E_FTPS=127.0.0.1:2121 npx playwright test ftps
//
// Skipped when E2E_FTPS isn't set.
const where = process.env.E2E_FTPS ?? '';

test('a self-signed certificate is asked about once, then remembered', async ({app}) => {
    test.skip(!where, 'E2E_FTPS not set');
    const [host, port] = where.split(':');
    const page = app.page;
    const ask = page.locator('#authOverlay');

    const connect = async () => {
        await expect(app.overlay).toBeVisible();
        await app.host.fill(host);
        await app.port.fill(port);
        await page.locator('#login').fill('user');
        await page.locator('#pass').fill('pass');
        await page.locator('#ok').click();
    };

    // Refused: no file window.
    await connect();
    await expect(ask).toBeVisible();
    await expect(page.locator('#authTitle')).toHaveText('확인할 수 없는 인증서');
    await expect(page.locator('#authMsg')).toContainText(`${where} 서버의 인증서를 확인할 수 없습니다.`);
    await expect(page.locator('#authMsg')).toContainText('자체 서명');
    await expect(page.locator('#authMsg')).toContainText('지문: SHA256:');
    await page.locator('#authCancel').click();
    await expect(app.error).toContainText('인증서를 신뢰하지 않아 접속하지 않았습니다');
    await expect(page.locator('#filesOverlay')).toBeHidden();

    // Trusted: an FTPS file window.
    await page.locator('#ok').click();
    await expect(ask).toBeVisible();
    await page.locator('#authOk').click();
    await expect(page.locator('#filesOverlay')).toBeVisible();
    await expect(page.locator('#filesTitle')).toHaveText(`파일 전송 (FTPS) - ${host}`);

    // The next connection to the server isn't asked about it.
    await page.keyboard.press('Escape');
    await app.newTab();
    await connect();
    await expect(page.locator('#filesOverlay')).toBeVisible();
    await expect(ask).toBeHidden();
});
