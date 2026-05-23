import { test, expect } from '@playwright/test';
import { loginAsAdmin, goToServerManagement } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

test.describe('サーバー追加: プロトコル切替でデフォルトポートが変わる（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    await goToServerManagement(page);
  });

  test('追加フォームでプロトコルを切り替えるとポートが変わる', async ({ page }) => {
    const groupName = uniq('e2e-add-port');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').waitFor({ state: 'visible', timeout: 10000 });
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await forceClick(page.getByText(groupName).first());
    await forceClick(page.locator('#btn-add-target-in-group'));

    const portInput = page.locator('#add-target-port');
    // 初期状態: SSH で 22
    await expect(portInput).toHaveValue('22');

    // 一度別の値を入れてからプロトコル変更し、デフォルトポートにリセットされることを確認する
    await portInput.fill('10000');
    await page.locator('#add-target-protocol').selectOption('telnet');
    await expect(portInput).toHaveValue('23');

    await portInput.fill('10000');
    await page.locator('#add-target-protocol').selectOption('rdp');
    await expect(portInput).toHaveValue('3389');

    await portInput.fill('10000');
    await page.locator('#add-target-protocol').selectOption('ssh');
    await expect(portInput).toHaveValue('22');
  });
});
