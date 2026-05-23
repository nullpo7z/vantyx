import { test, expect } from '@playwright/test';
import { loginAsAdmin, goToServerManagement } from './global-setup';
import { forceClick } from './actions';

test.describe('ナビゲーション（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('ホームを表示できる', async ({ page }) => {
    await forceClick(page.locator('#nav-targets'));
    await expect(page.getByText('アクセスグループ').or(page.getByText('グループがありません'))).toBeVisible({ timeout: 5000 });
  });

  test('録画を表示できる', async ({ page }) => {
    await expect(page.locator('#nav-recordings')).toBeVisible({ timeout: 20000 });
    await forceClick(page.locator('#nav-recordings'));
    const main = page.locator('#main-content');
    await expect(
      main.getByText('録画一覧').or(main.getByText('このサーバーの録画はありません')).or(main.getByText(/左のグループを選択し/))
    ).toBeVisible({ timeout: 20000 });
  });

  test('サーバー管理を表示できる', async ({ page }) => {
    await goToServerManagement(page);
    await expect(
      page.getByText('アクセスグループ').or(page.getByText('グループがありません'))
    ).toBeVisible({ timeout: 5000 });
  });

  test('ユーザー管理を表示できる', async ({ page }) => {
    await forceClick(page.locator('#nav-users'));
    await expect(
      page.getByText('ユーザー').or(page.getByText('ユーザーを追加'))
    ).toBeVisible({ timeout: 5000 });
  });

  test('設定を表示できる', async ({ page }) => {
    await expect(page.locator('#nav-settings')).toBeVisible({ timeout: 20000 });
    await forceClick(page.locator('#nav-settings'));
    const main = page.locator('#main-content');
    await expect(main.getByText('設定')).toBeVisible({ timeout: 10000 });
    await expect(main.getByText('監査ログを syslog / SIEM に転送する')).toBeVisible({ timeout: 10000 });
  });

  test('API リファレンスリンクで /docs が開く', async ({ page }) => {
    const link = page.locator('#nav-api-ref');
    await expect(link).toBeVisible();
    await expect(link).toHaveAttribute('href', '/docs');
  });
});
