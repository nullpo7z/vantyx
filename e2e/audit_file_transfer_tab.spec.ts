import { test, expect } from '@playwright/test';
import { loginAsAdmin } from './global-setup';
import { forceClick } from './actions';

test.describe('監査ログ：ファイル転送タブ', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    await page.locator('#nav-audit').waitFor({ state: 'visible', timeout: 15000 });
    await forceClick(page.locator('#nav-audit'));
    await expect(page.locator('#audit-tab-btn-ft')).toBeVisible({ timeout: 10000 });
  });

  test('タブ切り替えでフィルタ UI と表が表示される', async ({ page }) => {
    await forceClick(page.locator('#audit-tab-btn-ft'));
    await expect(page.locator('#audit-panel-ft')).toBeVisible({ timeout: 10000 });
    await expect(page.locator('#ft-filter-from')).toBeVisible();
    await expect(page.locator('#ft-filter-to')).toBeVisible();
    await expect(page.locator('#ft-filter-state')).toBeVisible();
    await expect(page.locator('#ft-filter-direction')).toBeVisible();
    await expect(page.locator('#ft-filter-backend')).toBeVisible();
    await expect(page.locator('#ft-filter-user')).toBeVisible();
    await expect(page.locator('#ft-filter-target')).toBeVisible();
    await expect(page.locator('#ft-filter-query')).toBeVisible();
    await expect(page.locator('#ft-load-more')).toBeAttached();
    await expect(page.locator('#audit-tab-btn-ft')).toHaveAttribute('aria-selected', 'true');
  });

  test('ファイル転送 API は from パラメータ付きで items を返す', async ({ page }) => {
    const respPromise = page.waitForResponse(
      (r) =>
        r.url().includes('/api/file-transfers') &&
        r.url().includes('from=') &&
        r.request().method() === 'GET',
      { timeout: 15000 }
    );
    await forceClick(page.locator('#audit-tab-btn-ft'));
    await expect(page.locator('#audit-panel-ft')).toBeVisible({ timeout: 10000 });

    const resp = await respPromise;
    expect(resp.status()).toBe(200);
    const body = await resp.json().catch(() => ({}));
    expect(body).toHaveProperty('items');
    expect(Array.isArray(body.items)).toBe(true);
  });

  test('「適用」を押すと API が再リクエストされる', async ({ page }) => {
    await forceClick(page.locator('#audit-tab-btn-ft'));
    // Wait for the initial load to finish.
    await page.waitForResponse(
      (r) => r.url().includes('/api/file-transfers') && r.request().method() === 'GET',
      { timeout: 15000 }
    );

    const secondResp = page.waitForResponse(
      (r) =>
        r.url().includes('/api/file-transfers') &&
        r.url().includes('state=completed') &&
        r.request().method() === 'GET',
      { timeout: 15000 }
    );
    await page.locator('#ft-filter-state').selectOption('completed');
    await forceClick(page.locator('#ft-apply'));
    const resp = await secondResp;
    expect(resp.status()).toBe(200);
  });
});
