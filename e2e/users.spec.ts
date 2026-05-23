import { test, expect } from '@playwright/test';
import { loginAsAdmin, goToUsersManagement } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

test.describe('ユーザー管理（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    await goToUsersManagement(page);
  });

  test('ユーザー一覧が表示される', async ({ page }) => {
    await expect(page.getByText('admin').first()).toBeVisible({ timeout: 5000 });
  });

  test('ユーザーを追加できる', async ({ page }) => {
    const username = uniq('e2euser');
    await forceClick(page.locator('#btn-add-user'));
    await expect(page.getByRole('heading', { name: 'ユーザーを追加' })).toBeVisible({ timeout: 5000 });
    await page.locator('#add-user-username').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#add-user-username').fill(username);
    await page.locator('#add-user-password').fill('E2ePass1!');
    await forceClick(page.locator('#add-user-submit'));
    await expect(page.locator('tr', { hasText: username })).toBeVisible({ timeout: 15000 });
  });

  test('管理者ロールでユーザーを追加すると一覧に admin と表示される', async ({ page }) => {
    const username = `e2eadmin-${Date.now()}`;
    await forceClick(page.locator('#btn-add-user'));
    await page.locator('#add-user-username').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#add-user-username').fill(username);
    await page.locator('#add-user-password').fill('E2ePass1!');
    await page.locator('#add-user-role').selectOption('admin');
    await forceClick(page.locator('#add-user-submit'));
    const row = page.locator('tr', { hasText: username }).first();
    await expect(row).toBeVisible({ timeout: 15000 });
    const roleCell = row.getByRole('cell').nth(2);
    await expect(roleCell).toHaveText('admin');
  });
});
