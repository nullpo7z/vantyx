import { test, expect } from '@playwright/test';
import { loginAsAdmin, goToServerManagement } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

test.describe('録画UI: グループ・サーバー選択と一覧表示（Web操作・接続は行わない）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    await forceClick(page.locator('#nav-recordings'));
    // nav の「録画」を拾わないよう main-content に限定して確認
    await expect(page.locator('#main-content').getByText('アクセスグループ')).toBeVisible({ timeout: 20000 });
  });

  test('グループ未選択時は説明が表示される', async ({ page }) => {
    await expect(page.locator('#main-content').getByText(/左のグループを選択し/)).toBeVisible({ timeout: 30000 });
  });

  test('グループを選択するとサーバー一覧が表示される', async ({ page }) => {
    await goToServerManagement(page);
    const groupName = uniq('e2e-rec-group');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await expect(page.getByText(groupName).first()).toBeVisible({ timeout: 10000 });
    await forceClick(page.getByText(groupName).first());
    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill('e2e-rec-server');
    await page.locator('#add-target-host').fill('192.0.2.3');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText('e2e-rec-server')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#nav-recordings'));
    const main = page.locator('#main-content');
    await expect(main.getByText('アクセスグループ')).toBeVisible({ timeout: 20000 });
    await forceClick(main.getByText(groupName).first());
    await expect(main.getByText('サーバー一覧')).toBeVisible({ timeout: 20000 });
    await expect(page.locator('.recordings-view-target-btn')).toBeVisible({ timeout: 5000 });
  });

  test('サーバーを選択すると録画一覧エリアが表示される', async ({ page }) => {
    await goToServerManagement(page);
    const groupName = uniq('e2e-rec-target');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await expect(page.getByText(groupName).first()).toBeVisible({ timeout: 10000 });
    await forceClick(page.getByText(groupName).first());
    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill('e2e-rec-srv');
    await page.locator('#add-target-host').fill('192.0.2.4');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText('e2e-rec-srv')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#nav-recordings'));
    const main = page.locator('#main-content');
    await forceClick(main.getByText(groupName).first());
    await forceClick(page.locator('.recordings-view-target-btn').first());
    await expect(
      main.getByText('録画一覧').or(main.getByText('このサーバーの録画はありません')).first()
    ).toBeVisible({ timeout: 20000 });
  });

  test('「← サーバー一覧」でサーバー一覧に戻れる', async ({ page }) => {
    await goToServerManagement(page);
    const groupName = uniq('e2e-rec-back');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await expect(page.getByText(groupName).first()).toBeVisible({ timeout: 10000 });
    await forceClick(page.getByText(groupName).first());
    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill('e2e-back-srv');
    await page.locator('#add-target-host').fill('192.0.2.5');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText('e2e-back-srv')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#nav-recordings'));
    const main = page.locator('#main-content');
    await forceClick(main.getByText(groupName).first());
    await forceClick(page.locator('.recordings-view-target-btn').first());
    await expect(
      main.getByText('録画一覧').or(main.getByText('このサーバーの録画はありません')).first()
    ).toBeVisible({ timeout: 20000 });

    await forceClick(page.locator('#recordings-back-to-servers'));
    await expect(main.getByText('サーバー一覧')).toBeVisible({ timeout: 20000 });
    await expect(page.locator('.recordings-view-target-btn')).toBeVisible();
  });
});
