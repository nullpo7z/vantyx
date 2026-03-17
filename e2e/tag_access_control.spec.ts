import { test, expect } from '@playwright/test';
import { loginAsAdmin, openAddGroupModal, goToServerManagement } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

/** ツリーで省略表示されたグループ名にマッチする locator（左サイドバー内） */
function groupInTree(page: import('@playwright/test').Page, groupName: string) {
  const escaped = groupName.slice(0, 40).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return page.locator('aside').getByText(new RegExp(`^${escaped}`)).first();
}

test.describe('タグでの権限管理（Web操作）', () => {
  test('一般ユーザーはタグが一致するグループ・ターゲットのみ表示される', async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    const baseURL = process.env.E2E_BASE_URL || 'https://localhost:18443';
    await loginAsAdmin(page, baseURL);

    const tagProd = `e2e-acl-prod-${Date.now()}`;
    const tagStaging = `e2e-acl-staging-${Date.now()}`;
    const groupProdName = uniq('e2e-acl-prod');
    const groupStagingName = uniq('e2e-acl-staging');
    const srvProdName = uniq('E2E prod srv');
    const srvStagingName = uniq('E2E staging srv');
    const username = uniq('e2e-acl-user');

    // グループ1: タグ tagProd、サーバー1台
    await openAddGroupModal(page);
    await page.getByLabel('名前').fill(groupProdName);
    const postProdGroup = page.waitForResponse(
      (r) => r.url().includes('/api/groups') && r.request().method() === 'POST',
      { timeout: 20000 }
    );
    await forceClick(page.locator('#add-group-submit'));
    await postProdGroup;
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 15000 });
    await page.waitForTimeout(500);
    // グループ一覧がキャッシュされるため、確実に反映させるために一度リロードしてからサーバー管理へ
    await page.reload();
    await goToServerManagement(page);
    let prodLocator = page.locator(`aside [data-group-select="1"][data-group-id="${groupProdName}"]`).first();
    try {
      await expect(prodLocator).toBeVisible({ timeout: 30000 });
    } catch {
      prodLocator = page.locator('#main-content').getByText(groupProdName).first();
      await expect(prodLocator).toBeVisible({ timeout: 30000 });
    }
    await forceClick(prodLocator);
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });
    await forceClick(page.locator('#btn-edit-group-tags'));
    await expect(page.locator('#edit-tags-modal')).toBeVisible({ timeout: 5000 });
    await page.locator('#edit-tags-input').fill(tagProd);
    await forceClick(page.locator('#edit-tags-submit'));
    // タグ編集モーダルが閉じ切るまで待つ（オーバーレイ残留で後続操作がブロックされるのを防ぐ）
    await page.locator('#edit-tags-modal').waitFor({ state: 'hidden', timeout: 15000 }).catch(() => {});
    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill(srvProdName);
    await page.locator('#add-target-host').fill('192.0.2.1');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText(srvProdName).first()).toBeVisible({ timeout: 10000 });

    // グループ2: タグ tagStaging、サーバー1台
    // 直前にグループ1を選択しているため、そのままだと「子グループ」として作られる。
    // ここではトップレベルに作りたいので root を選択してから追加する。
    await forceClick(page.locator('aside [data-group-select="1"][data-group-id=""]').first());
    await openAddGroupModal(page);
    await page.getByLabel('名前').fill(groupStagingName);
    const postStagingGroup = page.waitForResponse(
      (r) => r.url().includes('/api/groups') && r.request().method() === 'POST',
      { timeout: 20000 }
    );
    await forceClick(page.locator('#add-group-submit'));
    await postStagingGroup;
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 15000 });
    await page.waitForTimeout(500);
    await page.reload();
    await goToServerManagement(page);
    let stagingLocator = page.locator(`aside [data-group-select="1"][data-group-id="${groupStagingName}"]`).first();
    try {
      await expect(stagingLocator).toBeVisible({ timeout: 30000 });
    } catch {
      // ツリーに反映されない/スクロール外のケースに備えてメインコンテンツ側も見る
      const main = page.locator('#main-content');
      stagingLocator = main.getByText(groupStagingName).first();
      await expect(stagingLocator).toBeVisible({ timeout: 30000 });
    }
    await forceClick(stagingLocator);
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });
    await forceClick(page.locator('#btn-edit-group-tags'));
    await expect(page.locator('#edit-tags-modal')).toBeVisible({ timeout: 5000 });
    await page.locator('#edit-tags-input').fill(tagStaging);
    await forceClick(page.locator('#edit-tags-submit'));
    await page.locator('#edit-tags-modal').waitFor({ state: 'hidden', timeout: 15000 }).catch(() => {});
    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill(srvStagingName);
    await page.locator('#add-target-host').fill('192.0.2.2');
    await forceClick(page.locator('#add-target-submit'));
    await expect(page.getByText(srvStagingName).first()).toBeVisible({ timeout: 10000 });

    // 一般ユーザー作成（タグは tagProd のみ。どのグループのメンバーにもしない）
    await forceClick(page.locator('#nav-users'));
    await forceClick(page.locator('#btn-add-user'));
    await page.locator('#add-user-username').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#add-user-username').fill(username);
    await page.locator('#add-user-password').fill('E2ePass1!');
    await page.locator('#add-user-role').selectOption('user');
    await forceClick(page.locator('#add-user-submit'));
    // ユーザーID列/ユーザー名列の2箇所に同じ文字列が出るため strict mode 回避で「行」を確認する
    const row = page.locator('tr', { hasText: username }).first();
    await expect(row).toBeVisible({ timeout: 10000 });
    await forceClick(row.locator('.edit-user-btn'));
    await expect(page.locator('#edit-tags-modal')).toBeVisible({ timeout: 5000 });
    await page.locator('#edit-user-tags-input').fill(tagProd);
    await forceClick(page.locator('#edit-user-submit'));
    await page.locator('#edit-tags-modal').waitFor({ state: 'hidden', timeout: 15000 }).catch(() => {});

    // 一般ユーザーでログインし、サーバー管理で見えるグループ・ターゲットを確認
    await forceClick(page.locator('#logout-btn'));
    await expect(page.locator('#login-btn')).toBeVisible({ timeout: 5000 });
    await page.getByLabel('ユーザー名').fill(username);
    await page.getByLabel('パスワード').fill('E2ePass1!');
    await forceClick(page.locator('#login-btn'));
    await expect(page.locator('#logout-btn')).toBeVisible({ timeout: 15000 });

    // 一般ユーザーは実装上「サーバー管理(nav-groups)」が hidden のため、ホームで表示されるツリー/一覧を確認する
    await expect(page.locator('#nav-targets')).toBeVisible({ timeout: 20000 });
    await forceClick(page.locator('#nav-targets'));
    await expect(page.getByText('アクセスグループ')).toBeVisible({ timeout: 20000 });

    // tagProd のグループのみ表示され、そのターゲットが見える
    await expect(page.getByText(groupProdName).first()).toBeVisible({ timeout: 10000 });
    await forceClick(page.getByText(groupProdName).first());
    await expect(page.getByText(srvProdName).first()).toBeVisible({ timeout: 10000 });

    // tagStaging のグループ・ターゲットは表示されない
    await expect(page.getByText(groupStagingName)).not.toBeVisible();
    await expect(page.getByText(srvStagingName)).not.toBeVisible();
  });
});
