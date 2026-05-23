import { test, expect } from '@playwright/test';
import { loginAsAdmin, openAddGroupModal, goToServerManagement } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

/** ツリーで省略表示されたグループ名にマッチする locator（左サイドバー内） */
function groupInTree(page: import('@playwright/test').Page, groupName: string) {
  const escaped = groupName.slice(0, 40).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return page.locator('aside').getByText(new RegExp(`^${escaped}`)).first();
}

test.describe('ターゲット（サーバー）登録（Web操作・接続は行わない）', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('グループを追加してからサーバーを追加できる', async ({ page }) => {
    const groupName = uniq('e2e-target-group');
    await openAddGroupModal(page);
    await page.getByLabel('名前').fill(groupName);
    const postGroups = page.waitForResponse(
      (r) => r.url().includes('/api/groups') && r.request().method() === 'POST',
      { timeout: 20000 }
    );
    await forceClick(page.locator('#add-group-submit'));
    await postGroups;
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 15000 });
    await page.waitForTimeout(500);

    // ツリーを最新にするため、サーバー管理ビューを再読込
    await goToServerManagement(page);
    let groupLocator = groupInTree(page, groupName);
    try {
      await expect(groupLocator).toBeVisible({ timeout: 30000 });
    } catch {
      // ツリーに出ない場合はメインエリアの一覧から操作する
      groupLocator = page.getByText(groupName).first();
      await expect(groupLocator).toBeVisible({ timeout: 15000 });
    }

    await forceClick(groupLocator);
    await forceClick(page.locator('#btn-add-target-in-group'));
    await expect(page.getByRole('heading', { name: 'サーバーを追加' })).toBeVisible({ timeout: 3000 });

    // ラベルは画面内に複数存在し得るため、ID で確実に入力する
    await page.locator('#add-target-name').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#add-target-name').fill('e2e-server-1');
    await page.locator('#add-target-host').fill('192.0.2.1');
    await page.locator('#add-target-port').fill('22');
    await forceClick(page.locator('#add-target-submit'));

    await expect(page.getByText('e2e-server-1').first()).toBeVisible({ timeout: 10000 });
  });
});
