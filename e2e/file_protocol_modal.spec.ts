import { test, expect } from '@playwright/test';
import { loginAsAdmin, goToServerManagement, dismissAddTargetModalIfOpen } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

test.describe('ファイル転送プロトコル選択モーダル（Web操作・接続は行わない）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('SSHターゲットでパスワード保存後にホームでファイルボタンを押すとモーダルが開く', async ({ page }) => {
    await goToServerManagement(page);
    const groupName = uniq('e2e-file');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    const postGroup = page.waitForResponse(
      (r) => r.url().includes('/api/groups') && r.request().method() === 'POST',
      { timeout: 20000 }
    );
    await forceClick(page.locator('#add-group-submit'));
    await postGroup;
    await page
      .getByRole('heading', { name: 'サーバー管理グループを追加' })
      .waitFor({ state: 'hidden', timeout: 15000 })
      .catch(() => {});
    await page.waitForTimeout(500);
    await goToServerManagement(page);
    await forceClick(page.getByText(groupName).first());
    await forceClick(page.locator('#btn-add-target-in-group'));
    await page.locator('#add-target-name').fill('srv-file');
    await page.locator('#add-target-host').fill('192.0.2.1');
    await page.locator('#add-target-port').fill('22');
    await page.locator('#add-target-ssh-username').fill('root');
    await page.locator('#add-target-ssh-password').fill('stored-pass');
    // Docker 環境で /api/targets が一時的に 503 になることがあるため、成功(2xx)するまでリトライする
    let lastStatus = 0;
    for (let attempt = 0; attempt < 10; attempt++) {
      const createTargetRes = page.waitForResponse(
        (r) => r.url().includes('/api/targets') && r.request().method() === 'POST',
        { timeout: 35000 }
      );
      await forceClick(page.locator('#add-target-submit'), { timeout: 20000 });
      const res = await createTargetRes;
      lastStatus = res.status();
      if (res.ok()) break;
      const waitMs = Math.min(5000, 800 + attempt * 600);
      await page.waitForTimeout(attempt < 9 ? waitMs : 0);
      if (attempt === 9) {
        throw new Error(`file_protocol_modal: /api/targets POST did not succeed after 10 attempts (last status: ${lastStatus})`);
      }
    }

    // 成功していてもモーダルが残留するケースがあるので、念のため閉じる
    await dismissAddTargetModalIfOpen(page);

    // 成功していてもモーダルが閉じないケースがあるため、オーバーレイは無視してナビゲーションを直接クリックする
    await page.locator('#nav-targets').evaluate((el) => (el as HTMLAnchorElement).click());
    await expect(page.getByText('アクセスグループ')).toBeVisible({ timeout: 15000 });
    // グループ一覧のクリックも evaluate でオーバーレイの干渉を回避
    await page.getByText(groupName).first().evaluate((el) => (el as HTMLElement).click());
    await page.locator('#main-content').getByText('読み込み中…').waitFor({ state: 'detached', timeout: 25000 }).catch(() => {});
    await expect(page.getByText('srv-file').first()).toBeVisible({ timeout: 30000 });
    await page.locator('.files-open-btn').first().evaluate((el) => (el as HTMLButtonElement).click());
    await expect(page.getByRole('heading', { name: 'ファイル転送プロトコルの選択' })).toBeVisible({ timeout: 5000 });
    await forceClick(page.locator('#file-protocol-cancel'));
    await expect(page.getByRole('heading', { name: 'ファイル転送プロトコルの選択' })).not.toBeVisible();
  });
});
