import { test, expect } from '@playwright/test';
import { loginAsAdmin, openAddGroupModal, goToUsersManagement } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

/** ツリーで省略表示されたグループ名にマッチする locator（左サイドバー内にスコープ） */
function groupInTree(page: import('@playwright/test').Page, groupName: string) {
  const escaped = groupName.slice(0, 40).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return page.locator('aside').getByText(new RegExp(`^${escaped}`)).first();
}

test.describe('タグの挙動（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('複数タグをカンマ区切りで保存するとすべてピル表示される', async ({ page }) => {
    const groupName = uniq('e2e-tags-multi');
    await openAddGroupModal(page);
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#btn-edit-group-tags'));
    await page.locator('#edit-tags-input').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#edit-tags-input').fill('prod, staging, dev');
    await forceClick(page.locator('#edit-tags-submit'));

    await expect(page.getByText('prod', { exact: true }).first()).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('staging', { exact: true }).first()).toBeVisible();
    await expect(page.getByText('dev', { exact: true }).first()).toBeVisible();
  });

  test('タグを空にして保存すると「タグなし」になる', async ({ page }) => {
    const groupName = uniq('e2e-tags-empty');
    await openAddGroupModal(page);
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#btn-edit-group-tags'));
    await page.locator('#edit-tags-input').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#edit-tags-input').fill('dummy');
    await forceClick(page.locator('#edit-tags-submit'));
    await expect(page.getByText('dummy')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#btn-edit-group-tags'));
    await page.locator('#edit-tags-input').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#edit-tags-input').fill('');
    await forceClick(page.locator('#edit-tags-submit'));
    await expect(page.getByText('タグなし')).toBeVisible({ timeout: 10000 });
  });

  test('タグ入力の前後スペースはトリムされて保存される', async ({ page }) => {
    const groupName = uniq('e2e-tags-trim');
    await openAddGroupModal(page);
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#btn-edit-group-tags'));
    await page.locator('#edit-tags-input').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#edit-tags-input').fill('  trimmed_a  ,  trimmed_b  ');
    await forceClick(page.locator('#edit-tags-submit'));

    await expect(page.getByText('trimmed_a')).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('trimmed_b')).toBeVisible();
  });

  test('ハイフン・アンダースコアを含むタグが保存・表示される', async ({ page }) => {
    const groupName = uniq('e2e-tags-chars');
    await openAddGroupModal(page);
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#btn-edit-group-tags'));
    await page.locator('#edit-tags-input').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#edit-tags-input').fill('my-tag, my_tag, tag-01');
    await forceClick(page.locator('#edit-tags-submit'));

    await expect(page.getByText('my-tag')).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('my_tag')).toBeVisible();
    await expect(page.getByText('tag-01')).toBeVisible();
  });

  test('タグを編集して再度開くと保存したタグが入力欄に表示される', async ({ page }) => {
    const groupName = uniq('e2e-tags-roundtrip');
    await openAddGroupModal(page);
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#btn-edit-group-tags'));
    await page.locator('#edit-tags-input').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#edit-tags-input').fill('roundtrip-one, roundtrip-two');
    await forceClick(page.locator('#edit-tags-submit'));
    await page.locator('#edit-tags-modal').waitFor({ state: 'hidden', timeout: 15000 }).catch(() => {});
    await expect(page.getByText('roundtrip-one')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#btn-edit-group-tags'));
    const input = page.locator('#edit-tags-input');
    await expect(input).toBeVisible({ timeout: 10000 });
    await expect.poll(async () => input.inputValue()).toMatch(/roundtrip-one/, { timeout: 15000 });
    await expect.poll(async () => input.inputValue()).toMatch(/roundtrip-two/, { timeout: 15000 });
  });

  test('サーバー編集フォームでタグを設定すると一覧に表示される', async ({ page }) => {
    const groupName = uniq('e2e-tags-target');
    await openAddGroupModal(page);
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));

    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill('srv-with-tags');
    await page.locator('#add-target-host').fill('192.0.2.1');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText('srv-with-tags').first()).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('.edit-btn-in-group').first());
    await page.locator('#edit-target-tags').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#edit-target-tags').fill('target-tag1, target-tag2');
    await forceClick(page.locator('#edit-target-submit'));

    await expect(page.getByText('target-tag1')).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('target-tag2')).toBeVisible();
  });

  test('ユーザーのタグを複数保存して再度編集で確認できる', async ({ page }) => {
    await goToUsersManagement(page);
    const username = uniq('e2e-tag-user');
    await forceClick(page.locator('#btn-add-user'));
    await page.locator('#add-user-username').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#add-user-username').fill(username);
    await page.locator('#add-user-password').fill('E2ePass1!');
    await forceClick(page.locator('#add-user-submit'));
    await expect(page.locator('tr', { hasText: username })).toBeVisible({ timeout: 10000 });

    const row = page.locator('tr', { hasText: username }).first();
    await forceClick(row.locator('.edit-user-btn'));
    await page.locator('#edit-user-tags-input').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#edit-user-tags-input').fill('user-tag-a, user-tag-b');
    await forceClick(page.locator('#edit-user-submit'));
    await page.locator('#edit-tags-modal').waitFor({ state: 'hidden', timeout: 15000 }).catch(() => {});
    // ピッカーの候補ボタンと衝突するため、一覧行内に限定して確認
    await expect(row.getByText('user-tag-a', { exact: true }).first()).toBeVisible({ timeout: 15000 });

    await forceClick(row.locator('.edit-user-btn'));
    const userTagsInput = page.locator('#edit-user-tags-input');
    await expect(userTagsInput).toBeVisible({ timeout: 10000 });
    await expect(userTagsInput).toHaveValue(/user-tag-a/);
    await expect(userTagsInput).toHaveValue(/user-tag-b/);
  });
});
