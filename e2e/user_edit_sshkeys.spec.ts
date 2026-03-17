import { test, expect } from '@playwright/test';
import { loginAsAdmin, goToUsersManagement } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

const DUMMY_SSH_KEY =
  'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDXRLbZZ3G06vMi4NOoFMVNSolFW6SI6maURP2kMCI5+ e2e@test';

test.describe('ユーザー: 編集（タグ）・SSH公開鍵追加/削除（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    await goToUsersManagement(page);
  });

  test('ユーザーのタグを編集できる', async ({ page }) => {
    const username = uniq('e2euser-tags');
    await forceClick(page.locator('#btn-add-user'));
    await page.locator('#add-user-username').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#add-user-username').fill(username);
    await page.locator('#add-user-password').fill('E2ePass1!');
    await forceClick(page.locator('#add-user-submit'));
    await expect(page.locator('tr', { hasText: username })).toBeVisible({ timeout: 15000 });

    // テーブル行内の「編集」
    const row = page.locator('tr', { hasText: username }).first();
    await forceClick(row.locator('.edit-user-btn'));
    await expect(page.getByRole('heading', { name: 'ユーザーを編集' })).toBeVisible({ timeout: 5000 });
    await page.locator('#edit-user-tags-input').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#edit-user-tags-input').fill('e2e_user_tag');
    await forceClick(page.locator('#edit-user-submit'));
    await page.locator('#edit-tags-modal').waitFor({ state: 'hidden', timeout: 15000 }).catch(() => {});
    // ピッカーの候補ボタンと衝突するため、追加したユーザー行内に限定して確認
    await expect(row.getByText('e2e_user_tag', { exact: true }).first()).toBeVisible({ timeout: 15000 });
  });

  test('SSH公開鍵を追加して削除できる', async ({ page }) => {
    const username = uniq('e2euser-sshkey');
    await forceClick(page.locator('#btn-add-user'));
    await page.locator('#add-user-username').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#add-user-username').fill(username);
    await page.locator('#add-user-password').fill('E2ePass1!');
    await forceClick(page.locator('#add-user-submit'));
    await expect(page.locator('tr', { hasText: username })).toBeVisible({ timeout: 15000 });

    const row = page.locator('tr', { hasText: username }).first();
    await forceClick(row.locator('.user-ssh-keys-btn'));
    await expect(page.getByRole('heading', { name: new RegExp(`SSH 公開鍵`) })).toBeVisible({ timeout: 8000 });

    const sshKeyInput = page.locator('#user-ssh-key-input');
    await sshKeyInput.waitFor({ state: 'visible', timeout: 15000 });
    await sshKeyInput.fill(DUMMY_SSH_KEY);
    await forceClick(page.locator('#user-ssh-key-add-btn'));

    // 一覧に表示（ID は不定なので削除ボタンの出現で判定）
    await expect(page.locator('.user-ssh-key-del-btn')).toBeVisible({ timeout: 10000 });
    await forceClick(page.locator('.user-ssh-key-del-btn').first());
    await expect(page.getByText('登録された公開鍵はありません。')).toBeVisible({ timeout: 10000 });
  });
});

