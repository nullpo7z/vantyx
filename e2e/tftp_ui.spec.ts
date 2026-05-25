import { test, expect } from '@playwright/test';
import { loginAsAdmin } from './global-setup';
import { forceClick } from './actions';

test.describe('TFTP UI（スモーク）', () => {
  test.beforeEach(async ({ page }) => {
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
  });

  test('リモート TFTP 用 files ページに TFTP パネルがある', async ({ page }) => {
    await page.goto(
      '/files?target_id=e2e-tftp-placeholder&target_name=TFTP-Test&protocol=tftp',
      { waitUntil: 'domcontentloaded' }
    );
    await expect(page.locator('#files-tftp-box')).toBeVisible({ timeout: 10000 });
    await expect(page.locator('#files-tftp-path')).toBeVisible();
    await expect(page.locator('#files-tftp-download')).toBeVisible();
    await expect(page.locator('#files-back')).toBeVisible();
    await expect(page.locator('#files-back-header')).toBeVisible();
  });

  test('tftp-console ルートが主要 UI を表示する', async ({ page }) => {
    await page.goto(
      '/tftp-console?tftp_target_id=e2e-tftp&ssh_target_id=e2e-ssh&target_name=TFTP-Console',
      { waitUntil: 'domcontentloaded' }
    );
    await expect(page.locator('#tftp-files-list')).toBeAttached({ timeout: 10000 });
    await expect(page.locator('#tftp-refresh')).toBeAttached();
  });

  test('サーバー管理で TFTP プロトコル行にファイルボタンがある', async ({ page }) => {
    await page.locator('#nav-groups').waitFor({ state: 'visible', timeout: 15000 });
    await forceClick(page.locator('#nav-groups'));

    const tftpRow = page.locator('tr').filter({ hasText: /^tftp$/i }).first();
    const hasTftpRow = await tftpRow.isVisible().catch(() => false);
    if (!hasTftpRow) {
      test.skip(true, 'TFTP プロトコルのターゲットが環境に無いためスキップ');
      return;
    }
    await expect(tftpRow.locator('.files-open-btn')).toBeVisible();
  });
});
