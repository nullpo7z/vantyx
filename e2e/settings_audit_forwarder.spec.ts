import { test, expect } from '@playwright/test';
import { loginAsAdmin } from './global-setup';
import { forceClick } from './actions';

test.describe('設定: 監査ログ転送（Web操作）', () => {
  test('syslog 転送設定を保存でき、リロード後も保持される', async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');

    await expect(page.locator('#nav-settings')).toBeVisible({ timeout: 20000 });
    await forceClick(page.locator('#nav-settings'));

    const main = page.locator('#main-content');
    await expect(main.getByText('設定')).toBeVisible({ timeout: 10000 });

    const enabled = page.locator('#audit-fwd-enabled');
    const proto = page.locator('#audit-fwd-proto');
    const addr = page.locator('#audit-fwd-addr');
    const app = page.locator('#audit-fwd-app');
    const buffer = page.locator('#audit-fwd-buffer');
    const save = page.locator('#audit-fwd-save');
    const status = page.locator('#audit-fwd-status');

    await enabled.setChecked(true);
    await proto.selectOption('unixgram');
    // unixgram は空なら /dev/log を使うため、ここは空のままで良い
    await addr.fill('');
    await app.fill('vantyx-e2e');
    await buffer.fill('123');
    await forceClick(save);

    await expect(status).toHaveText(/保存しました/, { timeout: 10000 });

    await page.reload();
    await expect(page.locator('#nav-settings')).toBeVisible({ timeout: 20000 });
    await forceClick(page.locator('#nav-settings'));
    await expect(main.getByText('設定')).toBeVisible({ timeout: 10000 });

    await expect(enabled).toBeChecked();
    await expect(proto).toHaveValue('unixgram');
    await expect(app).toHaveValue('vantyx-e2e');
    await expect(buffer).toHaveValue('123');
  });
});

