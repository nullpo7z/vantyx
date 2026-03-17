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

test.describe('入力バリデーション（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    await goToServerManagement(page);
  });

  test('タグ編集で不正タグを入れるとエラーになる', async ({ page }) => {
    const groupName = uniq('e2e-invalid-tag');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#btn-edit-group-tags'));
    await expect(page.locator('#edit-tags-modal')).toBeVisible({ timeout: 5000 });
    await page.locator('#edit-tags-input').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#edit-tags-input').fill('bad tag'); // space is invalid
    await forceClick(page.locator('#edit-tags-submit'));
    const errorEl = page.locator('#edit-tags-error');
    await expect(errorEl).toHaveText('タグは英数字・ハイフン・アンダースコアのみ、1〜64文字で入力してください', { timeout: 5000 });
    await expect(errorEl).toBeVisible();
    await expect(errorEl).not.toHaveClass(/hidden/);
  });

  test('タグ編集で65文字超のタグを入れるとエラーになる', async ({ page }) => {
    const groupName = uniq('e2e-tag-len');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#btn-edit-group-tags'));
    await expect(page.locator('#edit-tags-modal')).toBeVisible({ timeout: 5000 });
    await page.locator('#edit-tags-input').waitFor({ state: 'visible', timeout: 10000 });
    const longTag = 'a'.repeat(65);
    await page.locator('#edit-tags-input').fill(longTag);
    await forceClick(page.locator('#edit-tags-submit'));
    const errorEl = page.locator('#edit-tags-error');
    await expect(errorEl).toHaveText('タグは英数字・ハイフン・アンダースコアのみ、1〜64文字で入力してください', { timeout: 5000 });
    await expect(errorEl).toBeVisible();
    await expect(errorEl).not.toHaveClass(/hidden/);
  });

  test('ユーザー追加で不正なユーザーIDを入れるとエラーになる', async ({ page }) => {
    await forceClick(page.locator('#nav-users'));
    await expect(page.getByRole('heading', { name: 'ユーザー管理' })).toBeVisible({ timeout: 10000 });
    await forceClick(page.locator('#btn-add-user'));
    await page.locator('#add-user-username').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#add-user-username').fill(uniq('e2euser'));
    await page.locator('#add-user-password').fill('E2ePass1!');
    await page.locator('#add-user-id').fill('invalid id'); // space
    await forceClick(page.locator('#add-user-submit'));
    await expect(
      page.getByText('ユーザーIDは英数字・ハイフン・アンダースコアのみ、最大512文字で入力してください')
    ).toBeVisible({ timeout: 5000 });
  });
});

