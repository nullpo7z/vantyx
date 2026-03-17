import { test, expect } from '@playwright/test';
import { loginAsAdmin, openChangePasswordModal } from './global-setup';
import { forceClick } from './actions';

test.describe('アカウント: パスワード変更モーダル（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('確認不一致の場合エラーが表示される', async ({ page }) => {
    await openChangePasswordModal(page);
    await expect(page.getByRole('heading', { name: 'パスワードを変更' })).toBeVisible({ timeout: 15000 });

    await page.getByLabel('現在のパスワード').fill('Admin123!E2e');
    await page.locator('#change-password-new').fill('NewPass1!');
    await page.locator('#change-password-confirm').fill('NewPass2!');
    await forceClick(page.locator('#change-password-submit'));

    const errorEl = page.locator('#change-password-error');
    await expect(errorEl).toHaveText('新しいパスワードが一致しません', { timeout: 5000 });
    await expect(errorEl).toBeVisible();
    // hidden クラスが外れていることも確認（「見えていないのにPASS」を避ける）
    await expect(errorEl).not.toHaveClass(/hidden/);
    // 動画確認用に少し待つ（E2E_VIDEO=1 のとき）
    if (process.env.E2E_VIDEO) {
      await page.waitForTimeout(1200);
    }
  });
});

