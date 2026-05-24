import { test, expect } from '@playwright/test';
import { loginAsAdmin } from './global-setup';
import { forceClick } from './actions';

async function openCommandLogTab(page: import('@playwright/test').Page) {
  await page.locator('#nav-audit').waitFor({ state: 'visible', timeout: 15000 });
  await forceClick(page.locator('#nav-audit'));
  const main = page.locator('#main-content');
  await expect(main.getByRole('heading', { name: '監査ログ' })).toBeVisible({ timeout: 10000 });
  await forceClick(page.locator('#audit-tab-btn-cmd'));
  await expect(page.locator('#audit-panel-cmd')).toBeVisible();
}

test.describe('コマンドログ検索（監査ログ画面・Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('タブでコマンドログを選択すると検索 UI が表示される', async ({ page }) => {
    await openCommandLogTab(page);

    await expect(page.locator('#audit-tab-btn-audit')).toBeVisible();
    await expect(page.locator('#audit-tab-btn-cmd')).toBeVisible();
    await expect(page.locator('#cmd-filter-query')).toBeVisible();
    await expect(page.locator('#cmd-filter-from')).toBeVisible();
    await expect(page.locator('#cmd-filter-to')).toBeVisible();
    await expect(page.locator('#cmd-apply')).toBeVisible();
    await expect(page.locator('#cmd-rows')).toBeVisible();
  });

  test('監査ログタブでは監査フィルタが表示される', async ({ page }) => {
    await page.locator('#nav-audit').waitFor({ state: 'visible', timeout: 15000 });
    await forceClick(page.locator('#nav-audit'));

    await expect(page.locator('#audit-panel-audit')).toBeVisible();
    await expect(page.locator('#audit-filter-from')).toBeVisible();
    await expect(page.locator('#audit-panel-cmd')).toBeHidden();
  });

  test('期間のみで「適用」を押すと from 付き API が呼ばれる', async ({ page }) => {
    await openCommandLogTab(page);

    const commandsResp = page.waitForResponse(
      (r) =>
        r.url().includes('/api/commands') &&
        r.request().method() === 'GET' &&
        r.url().includes('from='),
      { timeout: 15000 }
    );

    await forceClick(page.locator('#cmd-apply'));

    const resp = await commandsResp;
    expect(resp.status()).toBe(200);

    await page.locator('#cmd-rows').getByText('検索中…').waitFor({ state: 'detached', timeout: 10000 }).catch(() => {});
    await expect(page.locator('#cmd-error')).toHaveClass(/hidden/);
  });

  test('query を入力して「適用」を押すと API が呼ばれ結果または「該当するコマンドはありません」が表示される', async ({ page }) => {
    await openCommandLogTab(page);

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
