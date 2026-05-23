import { test, expect } from '@playwright/test';
import { loginAsAdmin, goToServerManagement } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

/** ツリーで省略表示されたグループ名にマッチする locator（左サイドバー内） */
function groupInTree(page: import('@playwright/test').Page, groupName: string) {
  const escaped = groupName.slice(0, 40).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return page.locator('aside').getByText(new RegExp(`^${escaped}`)).first();
}

test.describe('ターゲット追加: バリデーション（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    await goToServerManagement(page);
  });

  test('名前を空にして送信するとエラーになる', async ({ page }) => {
    const groupName = uniq('e2e-v');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill(' ');
    await page.locator('#add-target-host').fill('192.0.2.1');
    await page.locator('#add-target-port').fill('22');
    await forceClick(page.locator('#add-target-submit'));
    const errorEl = page.locator('#add-target-error');
    await expect(errorEl).toHaveText('名前とホストを入力してください', { timeout: 5000 });
    await expect(errorEl).toBeVisible();
  });
});
