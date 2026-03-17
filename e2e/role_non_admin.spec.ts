import { test, expect } from '@playwright/test';
import { loginAsAdmin, goToUsersManagement } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

test.describe('一般ユーザー: ユーザー管理が表示されない（Web操作）', () => {
  test('role=user でログインするとユーザー管理リンクが非表示', async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    const username = uniq('e2euser');
    await goToUsersManagement(page);
    await forceClick(page.locator('#btn-add-user'));
    await page.locator('#add-user-username').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#add-user-username').fill(username);
    await page.locator('#add-user-password').fill('E2ePass1!');
    await page.locator('#add-user-role').selectOption('user');
    await forceClick(page.locator('#add-user-submit'));
    await page.locator('#add-user-modal').waitFor({ state: 'hidden', timeout: 8000 });
    await expect(page.locator('tr', { hasText: username })).toBeVisible({ timeout: 15000 });

    await forceClick(page.locator('#logout-btn'));
    await expect(page.locator('#login-btn')).toBeVisible({ timeout: 5000 });

    await page.getByLabel('ユーザー名').fill(username);
    await page.getByLabel('パスワード').fill('E2ePass1!');
    await forceClick(page.locator('#login-btn'));
    await expect(page.locator('#logout-btn')).toBeVisible({ timeout: 15000 });
    await expect(page.locator('#nav-users')).not.toBeVisible();
    await expect(page.locator('#nav-targets')).toBeVisible();
  });
});
