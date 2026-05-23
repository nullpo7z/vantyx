import { test, expect } from '@playwright/test';
import { loginAsAdmin, goToServerManagement } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

function groupInTree(page: import('@playwright/test').Page, groupName: string) {
  const escaped = groupName.slice(0, 40).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return page.locator('aside').getByText(new RegExp(`^${escaped}`)).first();
}

test.describe('Telnet ターミナル UI（接続モーダルのみ）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    await goToServerManagement(page);
  });

  test('Telnet ターゲットに接続ボタンがあり、モーダルに秘密鍵欄がない', async ({ page }) => {
    const groupName = uniq('e2e-telnet-ui');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));

    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill('e2e-telnet-host');
    await page.locator('#add-target-host').fill('192.0.2.10');
    await page.locator('#add-target-protocol').selectOption('telnet');
    await expect(page.locator('#add-target-port')).toHaveValue('23');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText('e2e-telnet-host').first()).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#nav-targets'));
    await forceClick(groupInTree(page, groupName));
    const connectBtn = page.locator('.terminal-open-btn').first();
    await expect(connectBtn).toBeVisible({ timeout: 10000 });
    await forceClick(connectBtn);

    const modal = page.locator('#ssh-credential-modal');
    await expect(modal).toBeVisible({ timeout: 5000 });
    await expect(modal.getByText('Telnet ユーザー名')).toBeVisible();
    await expect(modal.getByText('秘密鍵のパスフレーズ')).not.toBeVisible();
  });
});
