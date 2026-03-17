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

test.describe('ターゲット編集: プロトコル・認証方式の表示切替（Web操作・接続は行わない）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    await goToServerManagement(page);
  });

  test('プロトコルを切り替えるとデフォルトポートが変わる', async ({ page }) => {
    const groupName = uniq('e2e-protocol-port');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));

    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill('e2e-port-test');
    await page.locator('#add-target-host').fill('192.0.2.1');
    await page.locator('#add-target-port').fill('22');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText('e2e-port-test').first()).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('.edit-btn-in-group').first());
    await expect(page.getByRole('heading', { name: 'サーバーを編集' })).toBeVisible({ timeout: 5000 });

    const portInput = page.locator('#edit-target-port');
    await expect(portInput).toHaveValue('22');

    await page.locator('#edit-target-protocol').selectOption('telnet');
    await expect(portInput).toHaveValue('23');

    await page.locator('#edit-target-protocol').selectOption('rdp');
    await expect(portInput).toHaveValue('3389');

    await page.locator('#edit-target-protocol').selectOption('ssh');
    await expect(portInput).toHaveValue('22');
  });

  test('SSHで認証方式を切り替えると表示が変わる', async ({ page }) => {
    const groupName = uniq('e2e-auth-type');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));

    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill('e2e-ssh-auth');
    await page.locator('#add-target-host').fill('192.0.2.2');
    await page.locator('#add-target-port').fill('22');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText('e2e-ssh-auth').first()).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('.edit-btn-in-group').first());
    await expect(page.getByRole('heading', { name: 'サーバーを編集' })).toBeVisible({ timeout: 5000 });

    await expect(page.locator('#edit-target-protocol').inputValue()).resolves.toBe('ssh');
    await expect(page.locator('#edit-target-ssh-password')).toBeVisible({ timeout: 10000 });

    await page.getByRole('radio', { name: '公開鍵認証（パスフレーズなし）' }).check();
    await expect(page.locator('#edit-target-ssh-private-key')).toBeVisible();
    await expect(page.locator('#edit-target-ssh-password')).not.toBeVisible();

    await page.getByRole('radio', { name: '公開鍵認証（パスフレーズあり）' }).check();
    await expect(page.locator('#edit-target-ssh-key-passphrase')).toBeVisible();
  });
});
