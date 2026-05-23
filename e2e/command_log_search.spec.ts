import { test, expect } from '@playwright/test';
import { loginAsAdmin } from './global-setup';
import { forceClick } from './actions';

test.describe('コマンドログ検索（監査ログ画面・Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('監査ログ画面を開き、コマンドログ検索ブロックが表示される', async ({ page }) => {
    await page.locator('#nav-audit').waitFor({ state: 'visible', timeout: 15000 });
    await forceClick(page.locator('#nav-audit'));

    const main = page.locator('#main-content');
    await expect(main.getByText('監査ログ')).toBeVisible({ timeout: 10000 });
    await expect(main.getByText('コマンドログ検索')).toBeVisible({ timeout: 10000 });
    await expect(page.locator('#cmd-filter-query')).toBeVisible();
    await expect(page.locator('#cmd-apply')).toBeVisible();
    await expect(page.locator('#cmd-rows')).toBeVisible();
  });

  test('検索条件なしで「適用」を押すと案内メッセージが表示される', async ({ page }) => {
    await page.locator('#nav-audit').waitFor({ state: 'visible', timeout: 15000 });
    await forceClick(page.locator('#nav-audit'));

    const main = page.locator('#main-content');
    await expect(main.getByText('コマンドログ検索')).toBeVisible({ timeout: 10000 });
    await expect(page.locator('#cmd-rows')).toContainText('検索条件を入力して', { timeout: 5000 });

    await forceClick(page.locator('#cmd-apply'));
    await expect(page.locator('#cmd-rows')).toContainText('検索条件を入力してください（query または user_id/target_id）', {
      timeout: 5000,
    });
  });

  test('query を入力して「適用」を押すと API が呼ばれ結果または「該当するコマンドはありません」が表示される', async ({ page }) => {
    await page.locator('#nav-audit').waitFor({ state: 'visible', timeout: 15000 });
    await forceClick(page.locator('#nav-audit'));

    const main = page.locator('#main-content');
    await expect(main.getByText('コマンドログ検索')).toBeVisible({ timeout: 10000 });

    const commandsResp = page.waitForResponse(
      (r) => r.url().includes('/api/commands') && r.request().method() === 'GET',
      { timeout: 15000 }
    );

    await page.locator('#cmd-filter-query').fill('sudo');
    await forceClick(page.locator('#cmd-apply'));

    const resp = await commandsResp;
    expect(resp.status()).toBe(200);

    const body = await resp.json().catch(() => ({}));
    expect(body).toHaveProperty('items');
    expect(Array.isArray(body.items)).toBe(true);

    await page.locator('#cmd-rows').getByText('検索中…').waitFor({ state: 'detached', timeout: 10000 }).catch(() => {});
    await expect(page.locator('#cmd-error')).toHaveClass(/hidden/);
    const rows = page.locator('#cmd-rows');
    await expect(
      rows.getByText('該当するコマンドはありません').or(rows.locator('tr td').first())
    ).toBeVisible({ timeout: 5000 });
  });
});
