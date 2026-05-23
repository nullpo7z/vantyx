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

test.describe('アクティブなセッションモーダル（Web操作・接続は行わない）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('ホームでアクティブなセッションを開くと空メッセージが表示される', async ({ page }) => {
    // ホームの「アクティブなセッション」ボタンはターゲット行ごとにあるため、先にグループ・ターゲットを1つ作成する
    await goToServerManagement(page);
    const groupName = uniq('e2e-sess-home');
    await page.locator('#btn-add-group').evaluate((el) => (el as HTMLElement).click());
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill('srv-sess-home');
    await page.locator('#add-target-host').fill('192.0.2.1');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText('srv-sess-home').first()).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#nav-targets'));
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await expect(page.getByText('srv-sess-home').first()).toBeVisible({ timeout: 10000 });
    await page.getByRole('button', { name: /アクティブなセッション\s*\(\d+\)/ }).first().waitFor({ state: 'visible', timeout: 10000 });
    await forceClick(page.getByRole('button', { name: /アクティブなセッション\s*\(\d+\)/ }).first());
    // 未選択時は「左のツリーで…」、1サーバー選択時は「このサーバーに対する…」のいずれか
    await expect(
      page.getByText(/アクティブなセッションはありません|このサーバーに対する再接続可能なセッションはありません/)
    ).toBeVisible({ timeout: 5000 });
    await forceClick(page.locator('#active-sessions-close'));
    await expect(page.getByRole('heading', { name: /アクティブなセッション/ })).not.toBeVisible();
  });

  test('グループ選択後にアクティブなセッションを開いて閉じる', async ({ page }) => {
    await goToServerManagement(page);
    const groupName = uniq('e2e-sess');
    await page.locator('#btn-add-group').evaluate((el) => (el as HTMLElement).click());
    await page.getByLabel('名前').waitFor({ state: 'visible', timeout: 10000 });
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await page.waitForTimeout(500);
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill('srv-sess');
    await page.locator('#add-target-host').fill('192.0.2.1');
    const createTargetRes = page.waitForResponse(
      (r) => r.url().includes('/api/targets') && r.request().method() === 'POST',
      { timeout: 30000 }
    );
    await forceClick(page.locator('#add-target-submit'));
    await createTargetRes;
    await expect(page.getByText('srv-sess').first()).toBeVisible({ timeout: 20000 });

    // ホームに戻ってグループを選択した状態でアクティブなセッションボタンを確認する
    await forceClick(page.locator('#nav-targets'));
    await expect(groupInTree(page, groupName)).toBeVisible({ timeout: 30000 });
    await forceClick(groupInTree(page, groupName));
    await expect(page.getByText('srv-sess').first()).toBeVisible({ timeout: 20000 });

    await page.getByRole('button', { name: /アクティブなセッション\s*\(\d+\)/ }).first().waitFor({
      state: 'visible',
      timeout: 20000,
    });
    await forceClick(page.getByRole('button', { name: /アクティブなセッション\s*\(\d+\)/ }).first());
    await expect(page.getByRole('heading', { name: /アクティブなセッション/ })).toBeVisible({ timeout: 5000 });
    await forceClick(page.locator('#active-sessions-close'));
    await expect(page.getByRole('heading', { name: /アクティブなセッション/ })).not.toBeVisible();
  });
});
