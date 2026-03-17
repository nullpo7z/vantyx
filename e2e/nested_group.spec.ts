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

test.describe('親子グループ（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    await goToServerManagement(page);
  });

  test('親グループを選択して子グループを追加できる', async ({ page }) => {
    const parentName = uniq('e2e-parent');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(parentName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, parentName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, parentName));

    await forceClick(page.locator('#btn-add-group'));
    await expect(page.getByRole('heading', { name: 'サーバー管理グループを追加' })).toBeVisible({ timeout: 5000 });
    const childName = uniq('e2e-child');
    await page.getByLabel('名前').fill(childName);
    const createChildRes = page.waitForResponse(
      (r) => r.request().method() === 'POST' && /\/api\/groups$/.test(new URL(r.url()).pathname),
      { timeout: 30000 }
    );
    await forceClick(page.locator('#add-group-submit'));
    const res = await createChildRes;
    if (!res.ok()) throw new Error(`nested_group: /api/groups POST failed with ${res.status()}`);
    await page
      .getByRole('heading', { name: 'サーバー管理グループを追加' })
      .waitFor({ state: 'hidden', timeout: 30000 })
      .catch(() => {});
    await page.waitForTimeout(500);

    // 子グループ追加後のツリー更新を確実に反映させるため、サーバー管理ビューを再読込
    await goToServerManagement(page);
    // 親グループを展開してから子を探す（選択クリックでは展開されない）
    await forceClick(page.locator(`aside [data-group-toggle="1"][data-group-id="${parentName}"]`), { timeout: 15000 });
    try {
      await expect(groupInTree(page, childName)).toBeVisible({ timeout: 30000 });
    } catch {
      await expect(page.getByText(childName).first()).toBeVisible({ timeout: 15000 });
    }
  });
});
