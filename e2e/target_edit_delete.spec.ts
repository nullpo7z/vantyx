import { test, expect } from '@playwright/test';
import { loginAsAdmin, goToServerManagement, openAddGroupModal } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

/** ツリーで省略表示されたグループ名にマッチする locator（左サイドバー内） */
function groupInTree(page: import('@playwright/test').Page, groupName: string) {
  const escaped = groupName.slice(0, 40).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return page.locator('aside').getByText(new RegExp(`^${escaped}`)).first();
}

test.describe('ターゲット: 編集（名前/タグ）・削除（Web操作・接続は行わない）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    await goToServerManagement(page);
  });

  test('サーバーを追加して編集（名前/タグ）できる', async ({ page }) => {
    const groupName = uniq('e2e-target-edit');
    await openAddGroupModal(page);
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));

    await forceClick(page.locator('#btn-add-target-in-group'));
    await expect(page.getByRole('heading', { name: 'サーバーを追加' })).toBeVisible({ timeout: 5000 });
    await page.locator('#add-target-name').fill('e2e-server-1');
    await page.locator('#add-target-host').fill('192.0.2.10');
    await page.locator('#add-target-port').fill('22');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText('e2e-server-1').first()).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('.edit-btn-in-group').first());
    await expect(page.getByRole('heading', { name: 'サーバーを編集' })).toBeVisible({ timeout: 5000 });
    await page.locator('#edit-target-name').fill('e2e-server-1-renamed');
    await page.locator('#edit-target-tags').fill('e2e_target_tag');
    await forceClick(page.locator('#edit-target-submit'));

    await expect(page.getByText('e2e-server-1-renamed')).toBeVisible({ timeout: 10000 });
  });

  test('サーバーを削除できる', async ({ page }) => {
    const groupName = uniq('e2e-target-delete');
    await openAddGroupModal(page);
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));

    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill('e2e-server-del');
    await page.locator('#add-target-host').fill('192.0.2.11');
    await page.locator('#add-target-port').fill('22');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText('e2e-server-del').first()).toBeVisible({ timeout: 10000 });

    // 行の「削除」ボタン（confirm は accept）。行テキストとボタンラベルで特定する
    const deleteBtn = page
      .getByRole('row', { name: /e2e-server-del/ })
      .getByRole('button', { name: '削除' })
      .first();
    await deleteBtn.waitFor({ state: 'visible', timeout: 15000 });
    await forceClick(deleteBtn, { timeout: 15000 });
    await expect(page.getByText('e2e-server-del')).not.toBeVisible({ timeout: 10000 });
  });
});

