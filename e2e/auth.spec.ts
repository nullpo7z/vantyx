import { test, expect } from '@playwright/test';
import { forceClick } from './actions';

const ADMIN_USER = 'admin';
const ADMIN_PASSWORD = 'Admin123!';

test.describe('認証（Web操作）', () => {
  test('ログイン画面が表示される', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'Vantyx' })).toBeVisible();
    await expect(page.getByLabel('ユーザー名')).toBeVisible();
    await expect(page.getByLabel('パスワード')).toBeVisible();
    await expect(page.locator('#login-btn')).toBeVisible();
  });

  test('不正な認証でログインに失敗する', async ({ page }) => {
    await page.goto('/');
    await page.getByLabel('ユーザー名').fill('wrong');
    await page.getByLabel('パスワード').fill('wrong');
    await forceClick(page.locator('#login-btn'));
    await expect(page.locator('#login-btn')).toBeVisible({ timeout: 5000 });
    await expect(page.locator('#logout-btn')).not.toBeVisible();
    const errMsg = page.locator('#login-error').filter({ hasNotText: '' });
    await expect(errMsg).toBeVisible({ timeout: 5000 });
  });

  test('正しい認証でログインしアプリ画面が表示される', async ({ page }) => {
    for (const password of [ADMIN_PASSWORD, 'Admin123!E2e']) {
      await page.goto('/');
      const loginResp = page.waitForResponse((r) => r.url().includes('/api/login') && r.request().method() === 'POST');
      await page.getByLabel('ユーザー名').fill(ADMIN_USER);
      await page.getByLabel('パスワード').fill(password);
      await forceClick(page.locator('#login-btn'));
      await loginResp;
      const logout = page.locator('#logout-btn');
      const changePw = page.getByRole('heading', { name: 'パスワードの変更' });
      const loginError = page.locator('#login-error').filter({ hasNotText: '' });
      const which = await Promise.race([
        logout.waitFor({ state: 'visible', timeout: 15000 }).then(() => 'logout' as const),
        changePw.waitFor({ state: 'visible', timeout: 15000 }).then(() => 'change' as const),
        loginError.waitFor({ state: 'visible', timeout: 5000 }).then(() => 'fail' as const),
      ]).catch(() => 'timeout' as const);
      if (which === 'fail') continue;
      if (which === 'change') {
        await page.getByLabel('現在のパスワード').fill(password);
        await page.locator('#new-password').fill('Admin123!E2e');
        await page.locator('#new-password-confirm').fill('Admin123!E2e');
        await forceClick(page.locator('#change-pw-btn'));
        await expect(logout).toBeVisible({ timeout: 15000 });
      }
      await expect(page.locator('#nav-targets')).toBeVisible({ timeout: 20000 });
      return;
    }
    throw new Error('Login failed with both passwords');
  });

  test('ログアウト後ログイン画面に戻る', async ({ page }) => {
    await page.goto('/');
    for (const password of [ADMIN_PASSWORD, 'Admin123!E2e']) {
      const loginResp = page.waitForResponse((r) => r.url().includes('/api/login') && r.request().method() === 'POST');
      await page.getByLabel('ユーザー名').fill(ADMIN_USER);
      await page.getByLabel('パスワード').fill(password);
      await forceClick(page.locator('#login-btn'));
      await loginResp;
      const logout = page.locator('#logout-btn');
      const changePw = page.getByRole('heading', { name: 'パスワードの変更' });
      const loginError = page.locator('#login-error').filter({ hasNotText: '' });
      const which = await Promise.race([
        logout.waitFor({ state: 'visible', timeout: 15000 }).then(() => 'logout' as const),
        changePw.waitFor({ state: 'visible', timeout: 15000 }).then(() => 'change' as const),
        loginError.waitFor({ state: 'visible', timeout: 5000 }).then(() => 'fail' as const),
      ]).catch(() => 'timeout' as const);
      if (which === 'fail') continue;
      if (which === 'change') {
        await page.getByLabel('現在のパスワード').fill(password);
        await page.locator('#new-password').fill('Admin123!E2e');
        await page.locator('#new-password-confirm').fill('Admin123!E2e');
        await forceClick(page.locator('#change-pw-btn'));
        await expect(logout).toBeVisible({ timeout: 15000 });
      }
      await expect(logout).toBeVisible({ timeout: 5000 });
      await forceClick(logout);
      await expect(page.getByRole('heading', { name: 'Vantyx' })).toBeVisible({ timeout: 15000 });
      await expect(page.locator('#login-btn')).toBeVisible();
      return;
    }
    throw new Error('Login failed with both passwords');
  });
});
