/// <reference types="node" />
import { defineConfig, devices } from '@playwright/test';

const baseURL = process.env.E2E_BASE_URL || 'https://localhost:18443';
const useHTTPS = baseURL.startsWith('https');
// 動画で操作を確認しやすくするため、E2E_VIDEO=1 のときは slowMo で全操作を少しゆっくりにする
const slowMo = process.env.E2E_VIDEO ? 300 : 0;

// 実行順を明示: 認証 → ナビ → アカウント → その他。新規 *.spec.ts を追加したらこの配列にも追加すること。
const testOrder = [
  'auth.spec.ts',
  'navigation.spec.ts',
  'settings_audit_forwarder.spec.ts',
  'account.spec.ts',
  'change_password_modal.spec.ts',
  'active_sessions_modal.spec.ts',
  'add_target_protocol_port.spec.ts',
  'file_protocol_modal.spec.ts',
  'groups.spec.ts',
  'group_tags_members.spec.ts',
  'modals_cancel.spec.ts',
  'nested_group.spec.ts',
  'recordings_ui.spec.ts',
  'recordings.spec.ts',
  'role_non_admin.spec.ts',
  'tag_access_control.spec.ts',
  'tags_behavior.spec.ts',
  'targets.spec.ts',
  'target_edit_delete.spec.ts',
  'target_protocol_auth.spec.ts',
  'target_validation.spec.ts',
  'user_edit_sshkeys.spec.ts',
  'users.spec.ts',
  'validation_errors.spec.ts',
];

export default defineConfig({
  testDir: '.',
  testMatch: testOrder.map((f) => `**/${f}`),
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: process.env.CI ? 'list' : 'html',
  use: {
    baseURL,
    trace: process.env.E2E_VIDEO ? 'on' : 'on-first-retry',
    ignoreHTTPSErrors: useHTTPS,
    // E2E_VIDEO=1 のとき全テストを録画（確認用）。未指定時はリトライ時のみ
    video: process.env.E2E_VIDEO ? 'on' : 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        launchOptions: slowMo ? { slowMo } : {},
      },
    },
  ],
  timeout: 60000,
  expect: { timeout: 15000 },
});
