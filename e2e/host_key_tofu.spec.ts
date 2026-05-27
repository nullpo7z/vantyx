import { test, expect } from '@playwright/test';
import { loginAsAdmin } from './global-setup';

// E2E coverage for the SSH host-key TOFU REST surface.
//
// The Go unit tests cover the full happy / sad paths against an
// in-process mock SSH server. These E2E checks complement them with
// the surface that is actually externally observable: auth, validation
// shape, and that a freshly created target round-trips the
// ssh_host_key_fingerprint field.

const BASE = process.env.E2E_BASE_URL || 'https://localhost:18443';

test.describe('SSH host-key TOFU API', () => {
  test('未認証の probe-host-key は 401', async ({ page }) => {
    const tmp = await page.context().browser()!.newContext({ ignoreHTTPSErrors: true });
    const res = await tmp.request.post(`${BASE}/api/targets/probe-host-key`, {
      data: { host: '127.0.0.1', port: 22 },
    });
    expect(res.status()).toBe(401);
    await tmp.close();
  });

  test('未認証の PUT ssh-host-key は 401', async ({ page }) => {
    const tmp = await page.context().browser()!.newContext({ ignoreHTTPSErrors: true });
    const res = await tmp.request.put(`${BASE}/api/targets/does-not-exist/ssh-host-key`, {
      data: { fingerprint: '' },
    });
    expect(res.status()).toBe(401);
    await tmp.close();
  });

  test('admin で probe-host-key: 不通ホストは 502 dialFailed 系を返す', async ({ page }) => {
    await loginAsAdmin(page, BASE);
    // Port 1 on loopback is virtually never listening; the dial
    // fails fast with "connection refused" / no route, giving us a
    // deterministic 502 from the proxyerrors.WrapTCPDialError path.
    const res = await page.request.post(`${BASE}/api/targets/probe-host-key`, {
      data: { host: '127.0.0.1', port: 1 },
    });
    expect(res.status()).toBe(502);
  });

  test('admin で probe-host-key: 不正な JSON は 400', async ({ page }) => {
    await loginAsAdmin(page, BASE);
    const res = await page.request.post(`${BASE}/api/targets/probe-host-key`, {
      data: 'not-json',
      headers: { 'Content-Type': 'application/json' },
    });
    expect(res.status()).toBe(400);
  });

  test('admin で PUT ssh-host-key: 未存在ターゲットは 404', async ({ page }) => {
    await loginAsAdmin(page, BASE);
    const res = await page.request.put(`${BASE}/api/targets/does-not-exist-e2e/ssh-host-key`, {
      data: { fingerprint: '' },
    });
    expect(res.status()).toBe(404);
  });

  test('admin で PUT ssh-host-key: SHA256: プレフィクスでない値は 400', async ({ page }) => {
    await loginAsAdmin(page, BASE);
    // Pre-create a target so we hit the validation branch and not the
    // 404 above. Use a unique name to avoid clashing with prior runs.
    const uniqueName = `hk-validation-${Date.now()}`;
    // First make sure the 'default' group exists; skip if not (test
    // environments without a group will fall through to 404 above).
    const groupsRes = await page.request.get(`${BASE}/api/groups`);
    if (!groupsRes.ok()) test.skip(true, 'groups list unavailable in this env');
    const groups = await groupsRes.json();
    const groupArr = Array.isArray(groups) ? groups : (groups.items || []);
    if (!groupArr.length) test.skip(true, 'no groups available');
    const groupID: string = groupArr[0].id;
    const create = await page.request.post(`${BASE}/api/targets`, {
      data: {
        name: uniqueName,
        host: '10.255.255.254',
        port: 22,
        protocol: 'ssh',
        group_id: groupID,
      },
    });
    if (!create.ok()) test.skip(true, `create target failed: ${create.status()}`);
    const created = await create.json();
    expect(typeof created.id).toBe('string');
    try {
      const res = await page.request.put(`${BASE}/api/targets/${encodeURIComponent(created.id)}/ssh-host-key`, {
        data: { fingerprint: 'not-a-fingerprint' },
      });
      expect(res.status()).toBe(400);
    } finally {
      await page.request.delete(`${BASE}/api/targets/${encodeURIComponent(created.id)}`).catch(() => {});
    }
  });

  test('admin で PUT ssh-host-key: 空文字でクリア成功', async ({ page }) => {
    await loginAsAdmin(page, BASE);
    const groupsRes = await page.request.get(`${BASE}/api/groups`);
    if (!groupsRes.ok()) test.skip(true, 'groups list unavailable in this env');
    const groups = await groupsRes.json();
    const groupArr = Array.isArray(groups) ? groups : (groups.items || []);
    if (!groupArr.length) test.skip(true, 'no groups available');
    const groupID: string = groupArr[0].id;
    const uniqueName = `hk-clear-${Date.now()}`;
    const create = await page.request.post(`${BASE}/api/targets`, {
      data: {
        name: uniqueName,
        host: '10.255.255.254',
        port: 22,
        protocol: 'ssh',
        group_id: groupID,
      },
    });
    if (!create.ok()) test.skip(true, `create target failed: ${create.status()}`);
    const created = await create.json();
    try {
      const res = await page.request.put(`${BASE}/api/targets/${encodeURIComponent(created.id)}/ssh-host-key`, {
        data: { fingerprint: '' },
      });
      expect(res.status()).toBe(200);
      const body = await res.json();
      // The cleared fingerprint should round-trip as empty.
      expect(body.ssh_host_key_fingerprint || '').toBe('');
    } finally {
      await page.request.delete(`${BASE}/api/targets/${encodeURIComponent(created.id)}`).catch(() => {});
    }
  });

  test('GET /api/targets レスポンスは ssh_host_key_fingerprint 形状を含むことができる', async ({ page }) => {
    await loginAsAdmin(page, BASE);
    const res = await page.request.get(`${BASE}/api/targets`);
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    const items = Array.isArray(body) ? body : (body.items || []);
    // The list may legitimately be empty in clean environments; we
    // only check the field is present-or-absent (omitempty).
    for (const item of items) {
      if ('ssh_host_key_fingerprint' in item && item.ssh_host_key_fingerprint) {
        expect(typeof item.ssh_host_key_fingerprint).toBe('string');
        expect(item.ssh_host_key_fingerprint.startsWith('SHA256:')).toBe(true);
      }
    }
  });
});
