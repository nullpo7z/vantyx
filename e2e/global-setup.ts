import { Page } from '@playwright/test';
import { forceClick } from './actions';

const ADMIN_USER = 'admin';
const ADMIN_PASSWORD = 'Admin123!';
const ADMIN_PASSWORD_AFTER_CHANGE = 'Admin123!E2e';

async function tryLogin(page: Page, password: string) {
  await page.getByLabel('ユーザー名').fill(ADMIN_USER);
  await page.getByLabel('パスワード').fill(password);
  await forceClick(page.locator('#login-btn'));
}

const LOGIN_WAIT_MS = 25000;
/** ログイン後ナビが表示されるまでの待ち（フロントの /api/me と描画に余裕を持たせる） */
const NAV_READY_MS = 30000;
const LOGIN_API_TIMEOUT_MS = 15000;

async function waitForNavReady(page: Page) {
  // role により hidden のナビがあるため、常に表示されるホーム(nav-targets)だけを必須にする
  await page.locator('#nav-targets').waitFor({ state: 'visible', timeout: NAV_READY_MS });
  // 録画は全ロールで表示される想定（実装で remove('hidden')）
  await page.locator('#nav-recordings').waitFor({ state: 'visible', timeout: NAV_READY_MS }).catch(() => {});
}

export async function loginAsAdmin(page: Page, baseURL: string) {
  await page.goto(baseURL + '/');
  await page.locator('#login-btn').waitFor({ state: 'visible', timeout: 10000 });
  // ログイン送信前に /api/me のリスナーを登録（フロントが renderApp 後にすぐ呼ぶため取りこぼさない）
  const meRespPromise = page.waitForResponse((r) => r.url().includes('/api/me') && r.request().method() === 'GET', { timeout: 12000 }).catch(() => null);
  const loginRespPromise = page.waitForResponse((r) => r.url().includes('/api/login') && r.request().method() === 'POST', { timeout: LOGIN_API_TIMEOUT_MS });
  await tryLogin(page, ADMIN_PASSWORD);
  let loginResp;
  try {
    loginResp = await loginRespPromise;
  } catch (e) {
    throw new Error(
      `loginAsAdmin: /api/login did not respond within ${LOGIN_API_TIMEOUT_MS}ms. Ensure server is up at ${baseURL} and reachable from the browser.`
    );
  }
  if (!loginResp.ok()) {
    if (loginResp.status() === 401) {
      // パスワードが既に変更済みの可能性（前回 E2E で変更した DB が残っている等）→ もう一つのパスワードで再試行
      await page.locator('#login-error').filter({ hasNotText: '' }).waitFor({ state: 'visible', timeout: 5000 }).catch(() => {});
      await page.goto(baseURL + '/');
      const retryResp = page.waitForResponse((r) => r.url().includes('/api/login') && r.request().method() === 'POST', { timeout: LOGIN_API_TIMEOUT_MS });
      await tryLogin(page, ADMIN_PASSWORD_AFTER_CHANGE);
      const retryLoginResp = await retryResp;
      if (!retryLoginResp.ok()) {
        const body = await retryLoginResp.text().catch(() => '');
        throw new Error(`loginAsAdmin: retry with ${ADMIN_PASSWORD_AFTER_CHANGE} returned ${retryLoginResp.status()}. Body: ${body.slice(0, 200)}`);
      }
      const logoutBtnAfter = page.locator('#logout-btn');
      const changePwHeadingAfter = page.getByRole('heading', { name: 'パスワードの変更' });
      const afterRetry = await Promise.race([
        logoutBtnAfter.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS }).then(() => 'logout' as const),
        changePwHeadingAfter.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS }).then(() => 'change' as const),
      ]).catch(() => 'timeout' as const);
      if (afterRetry === 'change') {
        await page.locator('#new-password').waitFor({ state: 'visible', timeout: 8000 });
        await page.getByLabel('現在のパスワード').fill(ADMIN_PASSWORD_AFTER_CHANGE);
        await page.locator('#new-password').fill(ADMIN_PASSWORD_AFTER_CHANGE);
        await page.locator('#new-password-confirm').fill(ADMIN_PASSWORD_AFTER_CHANGE);
        await forceClick(page.locator('#change-pw-btn'));
        const pwErr = page.locator('#change-pw-error').filter({ hasNotText: '' });
        const afterChange = await Promise.race([
          logoutBtnAfter.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS }).then(() => 'logout' as const),
          pwErr.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS }).then(() => 'error' as const),
        ]).catch(() => 'timeout' as const);
        if (afterChange === 'error') {
          const msg = await pwErr.textContent().catch(() => '');
          throw new Error(`loginAsAdmin: password change (after retry) failed: ${msg?.trim() || '(no message)'}`);
        }
        await logoutBtnAfter.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS });
      } else if (afterRetry === 'logout') {
        await logoutBtnAfter.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS });
      } else {
        throw new Error(`loginAsAdmin: after 401 retry, neither #logout-btn nor パスワードの変更 appeared within ${LOGIN_WAIT_MS}ms`);
      }
      await waitForNavReady(page);
      await page.locator('#nav-groups').waitFor({ state: 'visible', timeout: NAV_READY_MS }).catch(() => {});
      return;
    }
    const body = await loginResp.text().catch(() => '');
    throw new Error(`loginAsAdmin: /api/login returned ${loginResp.status()} ${loginResp.statusText()}. Body: ${body.slice(0, 200)}`);
  }
  const logoutBtn = page.locator('#logout-btn');
  const changePwHeading = page.getByRole('heading', { name: 'パスワードの変更' });
  const loginFailed = page.locator('#login-error').filter({ hasNotText: '' });
  let visible = await Promise.race([
    logoutBtn.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS }).then(() => 'logout' as const),
    changePwHeading.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS }).then(() => 'change' as const),
    loginFailed.waitFor({ state: 'visible', timeout: 5000 }).then(() => 'failed' as const),
  ]).catch(() => 'timeout' as const);
  const meResp = await meRespPromise;
  if (visible === 'timeout' && meResp !== null && meResp.ok()) {
    try {
      await logoutBtn.waitFor({ state: 'visible', timeout: 8000 });
      visible = 'logout';
    } catch {
      /* そのまま timeout で下の throw へ */
    }
  }

  if (visible === 'failed') {
    await page.goto(baseURL + '/');
    const retryResp = page.waitForResponse((r) => r.url().includes('/api/login') && r.request().method() === 'POST');
    await tryLogin(page, ADMIN_PASSWORD_AFTER_CHANGE);
    await retryResp;
    const afterRetry = await Promise.race([
      logoutBtn.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS }).then(() => 'logout' as const),
      changePwHeading.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS }).then(() => 'change' as const),
    ]).catch(() => 'timeout' as const);
    if (afterRetry === 'change') {
      await page.locator('#new-password').waitFor({ state: 'visible', timeout: 8000 });
      await page.getByLabel('現在のパスワード').fill(ADMIN_PASSWORD_AFTER_CHANGE);
      await page.locator('#new-password').fill(ADMIN_PASSWORD_AFTER_CHANGE);
      await page.locator('#new-password-confirm').fill(ADMIN_PASSWORD_AFTER_CHANGE);
      await forceClick(page.locator('#change-pw-btn'));
      const pwErr = page.locator('#change-pw-error').filter({ hasNotText: '' });
      const afterChange = await Promise.race([
        logoutBtn.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS }).then(() => 'logout' as const),
        pwErr.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS }).then(() => 'error' as const),
      ]).catch(() => 'timeout' as const);
      if (afterChange === 'error') {
        const msg = await pwErr.textContent().catch(() => '');
        throw new Error(`loginAsAdmin: password change (after login-error retry) failed: ${msg?.trim() || '(no message)'}`);
      }
      await logoutBtn.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS });
    } else {
      await logoutBtn.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS });
    }
    await waitForNavReady(page);
    await page.locator('#nav-groups').waitFor({ state: 'visible', timeout: NAV_READY_MS }).catch(() => {});
    return;
  }
  if (visible === 'change') {
    await page.locator('#new-password').waitFor({ state: 'visible', timeout: 8000 });
    await page.getByLabel('現在のパスワード').fill(ADMIN_PASSWORD);
    await page.locator('#new-password').fill(ADMIN_PASSWORD_AFTER_CHANGE);
    await page.locator('#new-password-confirm').fill(ADMIN_PASSWORD_AFTER_CHANGE);
    await forceClick(page.locator('#change-pw-btn'));
    const pwErr = page.locator('#change-pw-error').filter({ hasNotText: '' });
    const afterChange = await Promise.race([
      logoutBtn.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS }).then(() => 'logout' as const),
      pwErr.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS }).then(() => 'error' as const),
    ]).catch(() => 'timeout' as const);
    if (afterChange === 'error') {
      const msg = await pwErr.textContent().catch(() => '');
      throw new Error(`loginAsAdmin: password change failed: ${msg?.trim() || '(no message)'}`);
    }
    await logoutBtn.waitFor({ state: 'visible', timeout: LOGIN_WAIT_MS });
  }
  if (visible === 'timeout') {
    const url = page.url();
    const meHint = meResp === null ? ' /api/me was not requested (frontend may not have received login response).' : ` /api/me returned ${meResp.status()}.`;
    throw new Error(
      `loginAsAdmin: /api/login returned 200 but within ${LOGIN_WAIT_MS}ms neither #logout-btn nor change-password nor #login-error appeared.` +
        meHint +
        ` URL: ${url}. Check session cookie (vantyx_session), server logs, and browser console.`
    );
  }
  await waitForNavReady(page);
  await page.locator('#nav-groups').waitFor({ state: 'visible', timeout: NAV_READY_MS }).catch(() => {});
}

/** サーバー管理に遷移し、グループ一覧（aside 内 #btn-add-group）が表示され安定するまで待つ。*/
export async function goToServerManagement(page: Page) {
  const nav = page.locator('#nav-groups');
  await nav.waitFor({ state: 'visible', timeout: 15000 });

  for (let attempt = 0; attempt < 2; attempt++) {
    // add-target-modal が残っているとナビがクリックできず詰まるので先に強制クローズ
    await dismissAddTargetModalIfOpen(page);
    await forceClick(nav, { timeout: 15000 }).catch(async () => {
      await dismissAddTargetModalIfOpen(page);
      await forceClick(nav, { timeout: 15000 });
    });
    await page.locator('#main-content').getByText('読み込み中…').waitFor({ state: 'detached', timeout: 25000 }).catch(() => {});
    const btn = page.locator('aside').locator('#btn-add-group');
    try {
      await btn.waitFor({ state: 'visible', timeout: attempt === 0 ? 30000 : 45000 });
      // レンダリング直後に一瞬だけ表示→消えることがあるので「安定」まで待つ
      await page.waitForTimeout(400);
      await btn.waitFor({ state: 'visible', timeout: 7000 });
      return;
    } catch {
      if (attempt === 0) continue;
      throw new Error('goToServerManagement: #btn-add-group in aside did not become visible after 2 attempts');
    }
  }
}

export async function dismissAddTargetModalIfOpen(page: Page) {
  const modal = page.locator('#add-target-modal');
  const isVisible = await modal.isVisible().catch(() => false);
  if (!isVisible) return;
  // まずは UI の close/cancel を踏む
  const closeBtn = page.locator('#add-target-close');
  const cancelBtn = page.locator('#add-target-cancel');
  if (await closeBtn.isVisible().catch(() => false)) {
    await closeBtn.evaluate((el) => (el as HTMLButtonElement).click()).catch(() => {});
  } else if (await cancelBtn.isVisible().catch(() => false)) {
    await cancelBtn.evaluate((el) => (el as HTMLButtonElement).click()).catch(() => {});
  } else {
    // 最後の手段: DOM を直接クローズ状態にする（テスト専用の安全策）
    await modal
      .evaluate((el) => {
        el.classList.add('hidden');
        (el as HTMLElement).innerHTML = '';
      })
      .catch(() => {});
  }
  await modal.waitFor({ state: 'hidden', timeout: 3000 }).catch(() => {});
}

/** サーバー管理に遷移し、「サーバー管理グループを追加」モーダルを開くまで行う共通ヘルパー。*/
export async function openAddGroupModal(page: Page) {
  for (let attempt = 0; attempt < 2; attempt++) {
    await goToServerManagement(page);
    const btn = page.locator('aside').locator('#btn-add-group');
    try {
      await btn.waitFor({ state: 'visible', timeout: 20000 });
      await forceClick(btn, { timeout: 20000 });
      await page
        .getByRole('heading', { name: 'サーバー管理グループを追加' })
        .waitFor({ state: 'visible', timeout: 15000 });
      return;
    } catch {
      if (attempt === 0) continue;
      throw new Error('openAddGroupModal: failed to open add-group modal after 2 attempts');
    }
  }
}

/** ユーザー管理に遷移し、追加ボタンが表示され安定するまで待つ。ビューがサーバー管理に戻る場合に備えてリトライする。*/
export async function goToUsersManagement(page: Page) {
  const nav = page.locator('#nav-users');
  await nav.waitFor({ state: 'visible', timeout: 15000 });
  for (let attempt = 0; attempt < 2; attempt++) {
    await dismissAddTargetModalIfOpen(page);
    await forceClick(nav, { timeout: 15000 }).catch(async () => {
      await dismissAddTargetModalIfOpen(page);
      await forceClick(nav, { timeout: 15000 });
    });
    await page.getByRole('heading', { name: 'ユーザー管理' }).waitFor({ state: 'visible', timeout: 25000 }).catch(() => {});
    await page.locator('#main-content').getByText('読み込み中…').waitFor({ state: 'detached', timeout: 20000 }).catch(() => {});
    const btn = page.locator('#btn-add-user');
    try {
      await btn.waitFor({ state: 'visible', timeout: attempt === 0 ? 25000 : 15000 });
      await page.waitForTimeout(800);
      await btn.waitFor({ state: 'visible', timeout: 5000 });
      return;
    } catch {
      if (attempt === 0) continue;
      throw new Error('goToUsersManagement: #btn-add-user did not become visible after 2 attempts');
    }
  }
}

/** アカウントパネル経由でパスワード変更モーダルを開く（フレーク対策でリトライあり） */
export async function openChangePasswordModal(page: Page) {
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      await page.locator('#nav-targets').waitFor({ state: 'visible', timeout: 15000 });
      await page.locator('#user-name').waitFor({ state: 'visible', timeout: 15000 });
      await forceClick(page.locator('#user-name'));
      await waitForAccountPanel(page);
      const changePwBtn = page.locator('#btn-change-password');
      await changePwBtn.waitFor({ state: 'visible', timeout: 20000 });
      await changePwBtn.evaluate((el) => (el as HTMLButtonElement).click());
      await page.getByRole('heading', { name: 'パスワードを変更' }).waitFor({ state: 'visible', timeout: 15000 });
      return;
    } catch {
      if (attempt === 0) {
        await page.waitForTimeout(500);
        continue;
      }
      throw new Error('openChangePasswordModal: failed to open change-password modal after 2 attempts');
    }
  }
}

/** アカウントパネル（ユーザー名クリック後）で「ユーザー情報」またはパスワード変更ボタンが表示されるまで待つ。*/
export async function waitForAccountPanel(page: Page) {
  await Promise.race([
    page.getByText('ユーザー情報').waitFor({ state: 'visible', timeout: 20000 }),
    page.locator('#btn-change-password').waitFor({ state: 'visible', timeout: 20000 }),
  ]);
}

/** アカウントパネル（ユーザー名クリック後）でパスワード変更ボタンが押せるまで待つ。*/
export async function waitForAccountPanelReady(page: Page) {
  await page.locator('#btn-change-password').waitFor({ state: 'visible', timeout: 12000 });
  await page.waitForTimeout(300);
}
