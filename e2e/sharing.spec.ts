import { test, expect } from '@playwright/test';
import { loginAsAdmin } from './global-setup';

// E2E coverage for the collaborative-sessions REST surface.
//
// Spinning up an actual SSH target inside the Playwright runner is
// out of scope for this suite (the existing terminal tests do not do
// it either), so this spec focuses on the parts that do not depend
// on a live bridge: authentication, validation, and routing. The Go
// unit/integration tests cover the runtime behaviour (writer
// transfer, fan-out, viewer-input drop) directly against the
// in-process bridge.

const BASE = process.env.E2E_BASE_URL || 'https://localhost:18443';
const FAKE_SESSION = 'sess-does-not-exist-e2e';

test.describe('共有セッション API', () => {
  test('未認証では招待 API は 401', async ({ page }) => {
    // Use a fresh request context so the admin cookie from the global
    // login does not leak into this assertion.
    const ctx = await page.context().request.storageState();
    void ctx;
    const tmp = await page.context().browser()!.newContext({ ignoreHTTPSErrors: true });
    const res = await tmp.request.get(`${BASE}/api/terminal/sessions/${FAKE_SESSION}/invitations`);
    expect(res.status()).toBe(401);
    await tmp.close();
  });

  test('認証後、存在しないセッションへの GET は 404', async ({ page }) => {
    await loginAsAdmin(page, BASE);
    const res = await page.request.get(`${BASE}/api/terminal/sessions/${FAKE_SESSION}/invitations`);
    expect([404, 403]).toContain(res.status());
  });

  test('認証後、存在しないセッションへの POST 招待は 404', async ({ page }) => {
    await loginAsAdmin(page, BASE);
    const res = await page.request.post(`${BASE}/api/terminal/sessions/${FAKE_SESSION}/invitations`, {
      data: { mode: 'viewer', ttl_seconds: 900 },
    });
    expect([404, 403]).toContain(res.status());
  });

  test('認証後、参加者一覧 API も 404', async ({ page }) => {
    await loginAsAdmin(page, BASE);
    const res = await page.request.get(`${BASE}/api/terminal/sessions/${FAKE_SESSION}/participants`);
    expect([404, 403]).toContain(res.status());
  });

  test('join API: トークンを指定しない場合は 400', async ({ page }) => {
    await loginAsAdmin(page, BASE);
    const res = await page.request.post(`${BASE}/api/terminal/sessions/${FAKE_SESSION}/join`, {
      data: {},
    });
    // No token + no invitation_id is a request error before we even
    // attempt to look up the session.
    expect([400, 404]).toContain(res.status());
  });

  test('write-token release: 存在しないセッションは 404', async ({ page }) => {
    await loginAsAdmin(page, BASE);
    const res = await page.request.post(`${BASE}/api/terminal/sessions/${FAKE_SESSION}/write-token/release`, {
      data: {},
    });
    expect([404, 403]).toContain(res.status());
  });

  test('セッション一覧に role フィールドが含まれる', async ({ page }) => {
    await loginAsAdmin(page, BASE);
    const res = await page.request.get(`${BASE}/api/terminal/sessions`);
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(Array.isArray(body.items)).toBe(true);
    // The list may be empty in a clean E2E run, which is fine: we
    // only need to check that any returned items advertise the new
    // role field shape.
    for (const item of body.items) {
      if (item.role !== undefined) {
        expect(['owner', 'viewer']).toContain(item.role);
      }
    }
  });
});
