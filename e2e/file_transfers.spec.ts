import { test, expect } from '@playwright/test';
import { loginAsAdmin } from './global-setup';
import { forceClick } from './actions';

test.describe('ファイル転送（バックグラウンド）', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('セッション一覧にファイル転送セクションがある', async ({ page }) => {
    await forceClick(page.locator('#nav-sessions'));
    const main = page.locator('#main-content');
    await expect(main.getByRole('heading', { name: 'ファイル転送' })).toBeVisible({ timeout: 10000 });
    await expect(main.getByText(/画面を離脱しても転送中/)).toBeVisible();
  });

  test('未認証の file-transfers API は 401', async ({ page }) => {
    const base = process.env.E2E_BASE_URL || 'https://localhost:18443';
    const res = await page.request.get(`${base}/api/file-transfers`);
    expect(res.status()).toBe(401);
  });

  test('認証後の file-transfers API は 200', async ({ page }) => {
    const base = process.env.E2E_BASE_URL || 'https://localhost:18443';
    const res = await page.request.get(`${base}/api/file-transfers`);
    expect(res.status()).toBe(200);
    const body = await res.json();
    expect(Array.isArray(body.items)).toBe(true);
  });
});
