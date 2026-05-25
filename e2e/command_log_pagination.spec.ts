import { test, expect } from '@playwright/test';
import { loginAsAdmin } from './global-setup';
import { forceClick } from './actions';

test.describe('コマンドログページング（監査画面）', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('コマンドログタブに件数表示とさらに読み込みボタン領域がある', async ({ page }) => {
    await page.locator('#nav-audit').waitFor({ state: 'visible', timeout: 15000 });
    await forceClick(page.locator('#nav-audit'));

    await forceClick(page.locator('#audit-tab-btn-cmd'));
    await expect(page.locator('#audit-panel-cmd')).toBeVisible({ timeout: 10000 });
    await expect(page.locator('#cmd-count')).toBeAttached();
    await expect(page.locator('#cmd-load-more')).toBeAttached();
  });

  test('コマンドログ取得 API が items を返す', async ({ page }) => {
    await page.locator('#nav-audit').waitFor({ state: 'visible', timeout: 15000 });
    await forceClick(page.locator('#nav-audit'));
    await forceClick(page.locator('#audit-tab-btn-cmd'));

    await forceClick(page.locator('#cmd-apply'));

    const cmdResp = page.waitForResponse(
      (r) => r.url().includes('/api/commands') && r.request().method() === 'GET',
      { timeout: 15000 }
    );
    await expect(page.locator('#cmd-rows')).not.toContainText('検索中…', { timeout: 10000 });

    const resp = await cmdResp;
    expect(resp.status()).toBe(200);
    const body = await resp.json().catch(() => ({}));
    expect(body).toHaveProperty('items');
    expect(Array.isArray(body.items)).toBe(true);
  });
});
