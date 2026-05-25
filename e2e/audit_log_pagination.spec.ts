import { test, expect } from '@playwright/test';
import { loginAsAdmin } from './global-setup';
import { forceClick } from './actions';

test.describe('監査ログページング（監査ログ画面）', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('監査ログタブに件数表示とさらに読み込みボタン領域がある', async ({ page }) => {
    await page.locator('#nav-audit').waitFor({ state: 'visible', timeout: 15000 });
    await forceClick(page.locator('#nav-audit'));

    await expect(page.locator('#audit-panel-audit')).toBeVisible({ timeout: 10000 });
    await expect(page.locator('#audit-count')).toBeVisible();
    await expect(page.locator('#audit-load-more')).toBeAttached();
    await expect(page.locator('#audit-filter-event-preset')).toBeVisible();
  });

  test('監査ログ取得 API が items を返す', async ({ page }) => {
    await page.locator('#nav-audit').waitFor({ state: 'visible', timeout: 15000 });
    await forceClick(page.locator('#nav-audit'));

    const auditResp = page.waitForResponse(
      (r) => r.url().includes('/api/audit') && r.request().method() === 'GET',
      { timeout: 15000 }
    );
    await expect(page.locator('#audit-rows')).not.toContainText('読み込み中…', { timeout: 10000 });

    const resp = await auditResp;
    expect(resp.status()).toBe(200);
    const body = await resp.json().catch(() => ({}));
    expect(body).toHaveProperty('items');
    expect(Array.isArray(body.items)).toBe(true);
  });
});
