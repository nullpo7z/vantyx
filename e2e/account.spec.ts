import { test, expect } from '@playwright/test';
import { loginAsAdmin } from './global-setup';
import { forceClick } from './actions';

test.describe('アカウント（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('ユーザー名をクリックするとアカウント画面が表示される', async ({ page }) => {
    await forceClick(page.locator('#user-name'));
    // アカウント画面の見出しは「ユーザー情報」。初回は「パスワードの変更」が出る場合もある
    await expect(
      page.getByText('ユーザー情報').or(page.getByText('パスワードの変更'))
    ).toBeVisible({ timeout: 10000 });
  });
});
