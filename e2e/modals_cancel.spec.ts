import { test, expect } from '@playwright/test';
import { loginAsAdmin, goToServerManagement, goToUsersManagement, openAddGroupModal } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

/** ツリーで省略表示されたグループ名にマッチする locator（左サイドバー内） */
function groupInTree(page: import('@playwright/test').Page, groupName: string) {
  const escaped = groupName.slice(0, 40).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return page.locator('aside').getByText(new RegExp(`^${escaped}`)).first();
}

test.describe('モーダル: キャンセル・閉じるで閉じられる（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('グループ追加モーダルを開いてキャンセルで閉じる', async ({ page }) => {
    await openAddGroupModal(page);
    await expect(page.getByRole('heading', { name: 'サーバー管理グループを追加' })).toBeVisible({ timeout: 5000 });
    await forceClick(page.locator('#add-group-cancel'));
    await expect(page.getByRole('heading', { name: 'サーバー管理グループを追加' })).not.toBeVisible();
  });

  test('ユーザー追加モーダルを開いて×で閉じる', async ({ page }) => {
    await goToUsersManagement(page);
    await page.locator('#btn-add-user').waitFor({ state: 'visible', timeout: 10000 });
    await forceClick(page.locator('#btn-add-user'));
    await expect(page.getByRole('heading', { name: 'ユーザーを追加' })).toBeVisible({ timeout: 5000 });
    await forceClick(page.locator('#add-user-close'));
    // モーダルのラッパー要素が hidden クラスで非表示になり、中身がクリアされることを厳密に確認する
    const modal = page.locator('#add-user-modal');
    await expect(modal).toHaveClass(/hidden/, { timeout: 5000 });
    await expect(modal).toBeHidden();
  });

  test('サーバー追加モーダルを開いてキャンセルで閉じる', async ({ page }) => {
    await openAddGroupModal(page);
    const groupName = uniq('e2e-m');
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await forceClick(page.locator('#btn-add-target-in-group'));
    await expect(page.getByRole('heading', { name: 'サーバーを追加' })).toBeVisible({ timeout: 5000 });
    await forceClick(page.locator('#add-target-cancel'));
    await expect(page.getByRole('heading', { name: 'サーバーを追加' })).not.toBeVisible();
  });

  test('サーバー編集モーダルを開いてキャンセルで閉じる', async ({ page }) => {
    await openAddGroupModal(page);
    const groupName = uniq('e2e-me');
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill('srv');
    await page.locator('#add-target-host').fill('192.0.2.1');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText('srv').first()).toBeVisible({ timeout: 10000 });
    // 編集ボタンが存在することを確認してからクリックし、モーダル本体が開いたことを検証する
    const editBtn = page.locator('.edit-btn-in-group').first();
    await expect(editBtn).toBeVisible({ timeout: 10000 });
    await forceClick(editBtn);

    // 実装では編集モーダルも #add-target-modal を流用している
    const editModal = page.locator('#add-target-modal');
    await expect(editModal).toBeVisible({ timeout: 5000 });
    await expect(page.getByRole('heading', { name: 'サーバーを編集' })).toBeVisible({ timeout: 5000 });

    await forceClick(page.locator('#edit-target-cancel'));
    // モーダルラッパーが hidden になり、非表示になっていることを確認する
    await expect(editModal).toHaveClass(/hidden/, { timeout: 5000 });
    await expect(editModal).toBeHidden();
  });

  test('タグ編集モーダルを開いて×で閉じる', async ({ page }) => {
    await openAddGroupModal(page);
    const groupName = uniq('e2e-tag');
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });
    await forceClick(page.locator('#btn-edit-group-tags'));
    const editTagsModal = page.locator('#edit-tags-modal');
    await expect(editTagsModal).toBeVisible({ timeout: 5000 });
    await expect(page.getByRole('heading', { name: /タグを編集/ })).toBeVisible({ timeout: 5000 });
    await forceClick(page.locator('#edit-tags-close'));
    // モーダルラッパーが hidden になり、非表示になっていることを確認する
    await expect(editTagsModal).toHaveClass(/hidden/, { timeout: 5000 });
    await expect(editTagsModal).toBeHidden();
  });

  test('メンバー追加モーダルを開いてキャンセルで閉じる', async ({ page }) => {
    await openAddGroupModal(page);
    const groupName = uniq('e2e-mem');
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });
    await forceClick(page.locator('#btn-add-member'));
    await expect(page.getByRole('heading', { name: 'メンバーを追加' })).toBeVisible({ timeout: 5000 });
    await forceClick(page.locator('#add-member-cancel'));
    await expect(page.getByRole('heading', { name: 'メンバーを追加' })).not.toBeVisible();
  });

  test('ユーザー編集モーダルを開いてキャンセルで閉じる', async ({ page }) => {
    await goToUsersManagement(page);
    const row = page.locator('tr', { hasText: 'admin' }).first();
    await expect(row).toBeVisible({ timeout: 10000 });
    await forceClick(row.locator('.edit-user-btn'));

    // 実装ではユーザー編集モーダルは #edit-tags-modal を流用している
    const editUserModal = page.locator('#edit-tags-modal');
    await expect(editUserModal).toBeVisible({ timeout: 5000 });
    await expect(page.getByRole('heading', { name: 'ユーザーを編集' })).toBeVisible({ timeout: 5000 });

    await forceClick(page.locator('#edit-user-cancel'));
    // モーダルラッパーが hidden になり、非表示になっていることを確認する
    await expect(editUserModal).toHaveClass(/hidden/, { timeout: 5000 });
    await expect(editUserModal).toBeHidden();
  });

  test('パスワード変更モーダルを開いてキャンセルで閉じる', async ({ page }) => {
    await page.locator('#user-name').waitFor({ state: 'visible', timeout: 5000 });
    await forceClick(page.locator('#user-name'));
    // アカウントページに遷移したことを確認（これがないと他ページのままになり得る）
    await expect(page.locator('#main-content').getByText('ユーザー情報')).toBeVisible({ timeout: 15000 });
    const changePwBtn = page.locator('#btn-change-password');
    await changePwBtn.waitFor({ state: 'visible', timeout: 20000 });
    await changePwBtn.evaluate((el) => (el as HTMLButtonElement).click());
    const changePwModal = page.locator('#change-password-modal');
    await expect(changePwModal).toBeVisible({ timeout: 5000 });
    await expect(page.getByRole('heading', { name: 'パスワードを変更' })).toBeVisible({ timeout: 5000 });

    await forceClick(page.locator('#change-password-cancel'));
    // モーダルラッパーが hidden になり、非表示になっていることを確認する
    await expect(changePwModal).toHaveClass(/hidden/, { timeout: 5000 });
    await expect(changePwModal).toBeHidden();
  });
});
