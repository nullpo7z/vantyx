import { test, expect } from '@playwright/test';
import { loginAsAdmin } from './global-setup';
import { forceClick } from './actions';

test.describe('録画一覧（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    // /api/me 後に nav-recordings の hidden が外れるまで待つ
    await expect(page.locator('#nav-recordings')).toBeVisible({ timeout: 20000 });
    await forceClick(page.locator('#nav-recordings'));
    // initNav のハンドラ登録/描画が完了して録画ページが表示されるまで待つ
    await expect(page.locator('#main-content').getByText('アクセスグループ')).toBeVisible({ timeout: 30000 });
  });

  test('録画ページが表示される', async ({ page }) => {
    // ナビゲーションが録画タブになっていること（実装の active は border-b-2 で表現）
    await expect(page.locator('#nav-recordings')).toHaveClass(/border-b-2/, { timeout: 30000 });

    // メインコンテンツに録画ページ特有の文言が表示されていること（nav の「録画」を拾わないよう main-content に限定）
    const main = page.locator('#main-content');
    await expect(
      main
        .getByText('録画一覧')
        .or(main.getByText('このサーバーの録画はありません'))
        .or(main.getByText('左のグループを選択し、サーバー一覧から「録画を見る」でそのサーバーの録画を表示します。'))
    ).toBeVisible({ timeout: 30000 });
  });
});
