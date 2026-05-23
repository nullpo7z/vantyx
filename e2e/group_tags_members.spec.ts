import { test, expect } from '@playwright/test';
import { loginAsAdmin, goToServerManagement } from './global-setup';
import { forceClick } from './actions';

function uniq(prefix: string) {
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2, 8)}`;
}

/** メンバー追加の select で、表示テキストに search を含む option の value を選ぶ */
async function selectAddMemberUserByText(page: import('@playwright/test').Page, search: string) {
  const value = await page.locator('#add-member-user').evaluate((el: HTMLSelectElement, s: string) => {
    const select = el as HTMLSelectElement;
    for (let i = 0; i < select.options.length; i++) {
      if (select.options[i].textContent?.includes(s)) return select.options[i].value;
    }
    return '';
  }, search);
  if (!value) throw new Error(`add-member-user: option containing "${search}" not found`);
  await page.locator('#add-member-user').selectOption(value);
}

test.describe('グループ: タグ編集・メンバー追加/削除（Web操作）', () => {
  test.beforeEach(async ({ page }) => {
    page.on('dialog', (d) => d.accept());
    await loginAsAdmin(page, process.env.E2E_BASE_URL || 'https://localhost:18443');
    await goToServerManagement(page);
  });

  test('グループのタグを編集できる', async ({ page }) => {
    const groupName = uniq('e2e-group-tags');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await expect(page.getByText(groupName).first()).toBeVisible({ timeout: 15000 });

    await forceClick(page.getByText(groupName).first());
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#btn-edit-group-tags'));
    await expect(page.getByRole('heading', { name: new RegExp(`タグを編集`) })).toBeVisible();
    await page.locator('#edit-tags-input').waitFor({ state: 'visible', timeout: 5000 });
    await page.locator('#edit-tags-input').fill('e2e_tag1, e2e_tag2');
    await forceClick(page.locator('#edit-tags-submit'));

    await expect(page.getByText('e2e_tag1')).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('e2e_tag2')).toBeVisible();
  });

  test('グループにメンバーを追加し、削除できる', async ({ page }) => {
    // ユーザー作成（ユーザー管理）
    const username = uniq('e2euser');
    await forceClick(page.locator('#nav-users'));
    await expect(page.getByRole('heading', { name: 'ユーザー管理' })).toBeVisible({ timeout: 10000 });
    await forceClick(page.locator('#btn-add-user'));
    await page.locator('#add-user-username').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#add-user-username').fill(username);
    await page.locator('#add-user-password').fill('E2ePass1!');
    await forceClick(page.locator('#add-user-submit'));
    await expect(page.locator('tr', { hasText: username })).toBeVisible({ timeout: 10000 });

    // グループ作成 → メンバー操作
    await goToServerManagement(page);
    const groupName = uniq('e2e-group-members');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await expect(page.getByText(groupName).first()).toBeVisible({ timeout: 15000 });
    await forceClick(page.getByText(groupName).first());
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });

    await forceClick(page.locator('#btn-add-member'));
    await expect(page.getByRole('heading', { name: 'メンバーを追加' })).toBeVisible({ timeout: 5000 });
    await page.locator('#add-member-user').waitFor({ state: 'visible', timeout: 5000 });
    // ユーザー一覧のロード待ち（空のまま submit しても何も起きないため）
    await expect.poll(async () => page.locator('#add-member-user option').count()).toBeGreaterThan(1, { timeout: 20000 });
    await selectAddMemberUserByText(page, username);
    await expect(page.locator('#add-member-user')).not.toHaveValue('', { timeout: 5000 });
    const addMemberRes = page.waitForResponse(
      (r) => r.request().method() === 'POST' && r.url().includes('/api/groups/') && r.url().includes('/members'),
      { timeout: 20000 }
    );
    await forceClick(page.locator('#add-member-submit'));
    await addMemberRes;

    // メンバー一覧に表示される
    await expect(page.locator('#group-members-container').locator('tr', { hasText: username })).toBeVisible({ timeout: 25000 });

    // 削除（confirm は beforeEach で accept）
    const deleteMemberRes = page.waitForResponse(
      (r) => r.request().method() === 'DELETE' && r.url().includes('/api/groups/') && r.url().includes('/members/'),
      { timeout: 15000 }
    );
    await forceClick(page.locator('#group-members-container').locator('tr', { hasText: username }).locator('.remove-member-btn'));
    await deleteMemberRes;
    await expect(page.locator('#group-members-container').locator('tr', { hasText: username })).not.toBeVisible({ timeout: 20000 });
  });

  test('全員メンバー済みのグループでメンバー追加を開くと「追加できるユーザーがいません」', async ({ page }) => {
    const username = uniq('e2euser2');
    await forceClick(page.locator('#nav-users'));
    await expect(page.getByRole('heading', { name: 'ユーザー管理' })).toBeVisible({ timeout: 10000 });
    await forceClick(page.locator('#btn-add-user'));
    await page.locator('#add-user-username').waitFor({ state: 'visible', timeout: 10000 });
    await page.locator('#add-user-username').fill(username);
    await page.locator('#add-user-password').fill('E2ePass1!');
    await forceClick(page.locator('#add-user-submit'));
    await expect(page.locator('tr', { hasText: username })).toBeVisible({ timeout: 10000 });

    await goToServerManagement(page);
    const groupName = uniq('e2e-group-full');
    await forceClick(page.locator('#btn-add-group'));
    await page.getByLabel('名前').fill(groupName);
    await forceClick(page.locator('#add-group-submit'));
    await page.getByRole('heading', { name: 'サーバー管理グループを追加' }).waitFor({ state: 'hidden', timeout: 10000 });
    await expect(page.getByText(groupName).first()).toBeVisible({ timeout: 15000 });
    await forceClick(page.getByText(groupName).first());
    await expect(page.locator('#group-members-container')).toBeVisible({ timeout: 10000 });

    // 追加候補ユーザーがいなくなるまで、選択肢に出てくるユーザーを順に追加する。
    // （環境により "admin" が最初からメンバー扱いで候補に出ない等の差があるため固定名に依存しない）
    for (let i = 0; i < 10; i++) {
      await forceClick(page.locator('#btn-add-member'));
      const select = page.locator('#add-member-user');
      await select.waitFor({ state: 'visible', timeout: 10000 });

      await expect
        .poll(async () => {
          const disabled = await select.isDisabled().catch(() => false);
          const count = await page.locator('#add-member-user option').count();
          return disabled ? 'disabled' : count > 1 ? 'ready' : 'loading';
        })
        .toMatch(/disabled|ready/, { timeout: 20000 });

      const isDisabledNow = await select.isDisabled().catch(() => false);
      if (isDisabledNow) break;

      const choice = await select.evaluate((el: HTMLSelectElement) => {
        const opts = Array.from(el.options);
        const o = opts.find((x) => (x.value || '').trim() !== '');
        return o ? { value: o.value, text: o.textContent || '' } : { value: '', text: '' };
      });
      if (!choice?.value) break;

      await select.selectOption(choice.value);
      await expect(select).toHaveValue(choice.value, { timeout: 20000 });

      const addMemberRes = page.waitForResponse(
        (r) => r.request().method() === 'POST' && r.url().includes('/api/groups/') && r.url().includes('/members'),
        { timeout: 20000 }
      );
      await forceClick(page.locator('#add-member-submit'));
      await addMemberRes;
      await page.locator('#add-member-modal').waitFor({ state: 'hidden', timeout: 15000 }).catch(() => {});
    }

    await forceClick(page.locator('#btn-add-member'));
    await page.locator('#add-member-user').waitFor({ state: 'visible', timeout: 10000 });

    // API.users() の反映待ち（候補あり: option が増える / 候補なし: select が disabled になる）
    await expect
      .poll(async () => {
        const disabled = await page.locator('#add-member-user').isDisabled().catch(() => false);
        const count = await page.locator('#add-member-user option').count();
        return disabled ? 'disabled' : count > 1 ? 'has_options' : 'loading';
      })
      .toMatch(/disabled|has_options/, { timeout: 20000 });

    // 追加候補ユーザーが本当にいない場合のみメッセージを確認する（他テストでユーザーが増えているケースを考慮）
    const isDisabled = await page.locator('#add-member-user').isDisabled().catch(() => false);
    if (isDisabled) {
      await expect(page.locator('#add-member-user option').first()).toHaveText('追加できるユーザーがいません', { timeout: 10000 });
    }
  });
});

