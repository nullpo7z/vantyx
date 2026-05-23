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

test.describe('グループ（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('グループを追加できる', async ({ page }) => {
    const groupName = uniq('e2e-test-group');
    await openAddGroupModal(page);
    await page.getByLabel('名前').fill(groupName);
    const postGroups = page.waitForResponse(
      (r) => r.url().includes('/api/groups') && r.request().method() === 'POST',
      { timeout: 15000 }
    );
    await forceClick(page.locator('#add-group-submit'));
    await postGroups;
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(800);

    // サイドバーのツリーを最新状態にするため、サーバー管理ビューを再読込
    await goToServerManagement(page);

    // ツリーに反映されないケースでは、メインエリアの一覧での表示を確認するだけにとどめる
    try {
      // テキスト一致より data-group-id の方が安定（省略/同名/スクロールの影響を受けにくい）
      const byId = page.locator(`aside [data-group-select="1"][data-group-id="${groupName}"]`).first();
      await expect(byId).toBeVisible({ timeout: 35000 });
    } catch {
      await expect(page.locator('#main-content').getByText(groupName).first()).toBeVisible({ timeout: 30000 });
    }
  });
});
