import API from './api.js'
import { t } from './i18n.js'
import { formatDateTime } from './datetime.js'
import { validateOptionalUserId } from './validation.js'
import { uiAlert, uiConfirm } from './ui_dialog.js'

// ユーザー管理ページ全体を描画する
export async function renderUsersPage({
  mainContent,
  escapeHtml,
  renderTagPills,
  fillExistingTagsPicker,
}) {
  mainContent.innerHTML = `<p class="text-slate-500">${t('common.loading')}</p>`

  const reload = () =>
    renderUsersPage({
      mainContent,
      escapeHtml,
      renderTagPills,
      fillExistingTagsPicker,
    })

  try {
    const users = await API.users()
    const rows = (users || [])
      .map((u) => {
        const userTags = Array.isArray(u.tags) ? u.tags : []
        return `
        <tr class="border-b border-slate-200 hover:bg-slate-50">
          <td class="px-4 py-2 text-sm font-medium text-slate-900">${escapeHtml(u.id)}</td>
          <td class="px-4 py-2 text-sm text-slate-700">${escapeHtml(u.username)}</td>
          <td class="px-4 py-2 text-sm text-slate-600">${escapeHtml(u.role || 'user')}</td>
          <td class="px-4 py-2">
            <button type="button"
              class="user-ssh-keys-btn rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50 shrink-0"
              data-user-id="${escapeHtml(u.id)}"
              data-username="${escapeHtml(u.username)}">
              ${t('users.keysBtn')}
            </button>
          </td>
          <td class="px-4 py-2">
            <div class="flex flex-wrap items-center gap-2">
              <button type="button"
                class="edit-user-btn shrink-0 rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50"
                data-user-id="${escapeHtml(u.id)}"
                data-username="${escapeHtml(u.username)}"
                data-user-role="${escapeHtml(u.role || 'user')}"
                data-user-tags="${escapeHtml((userTags || []).join(','))}">
                ${t('users.edit')}
              </button>
              <button type="button"
                class="delete-user-btn shrink-0 rounded border border-red-200 bg-white px-2 py-1 text-xs font-medium text-red-700 hover:bg-red-50"
                data-user-id="${escapeHtml(u.id)}"
                data-username="${escapeHtml(u.username)}">
                ${t('users.delete')}
              </button>
              ${
                u.totp_enabled
                  ? `<button type="button"
                class="reset-totp-btn shrink-0 rounded border border-amber-300 bg-white px-2 py-1 text-xs font-medium text-amber-800 hover:bg-amber-50"
                data-user-id="${escapeHtml(u.id)}"
                data-username="${escapeHtml(u.username)}">
                ${t('users.resetTotp')}
              </button>`
                  : ''
              }
              <div class="flex flex-wrap items-center gap-2 min-w-0">
                ${
                  userTags.length
                    ? renderTagPills(userTags)
                    : '<span class="text-xs text-slate-400">—</span>'
                }
              </div>
            </div>
          </td>
        </tr>
      `
      })
      .join('')

    mainContent.innerHTML = `
      <div class="w-full flex flex-col">
        <div class="flex items-center justify-between mb-4">
          <h2 class="text-lg font-medium text-slate-800">${t('users.headerTitle')}</h2>
          <button type="button" id="btn-add-user" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('users.addBtn')}</button>
        </div>
        <div class="bg-white rounded-lg border border-slate-200 shadow-sm overflow-hidden">
          <div class="overflow-x-auto">
            <table class="min-w-full text-left text-sm">
              <thead class="bg-slate-50 border-b border-slate-200">
                <tr>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('users.headerId')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('users.headerUsername')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('users.headerRole')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('users.headerKeys')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('users.headerTags')}</th>
                </tr>
              </thead>
              <tbody>${
                rows ||
                `<tr><td colspan="5" class="px-4 py-6 text-center text-slate-500">${t('users.listEmpty')}</td></tr>`
              }</tbody>
            </table>
          </div>
        </div>
      </div>
    `

    mainContent.querySelector('#btn-add-user').addEventListener('click', () => {
      showAddUserModal({ mainContent, escapeHtml, reload })
    })

    mainContent.querySelectorAll('.edit-user-btn').forEach((btn) => {
      btn.addEventListener('click', () => {
        const user = {
          id: btn.dataset.userId || '',
          username: btn.dataset.username || '',
          role: btn.dataset.userRole || 'user',
          tags: (btn.dataset.userTags || '')
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean),
        }
        if (user.id) {
          showEditUserModal({ user, escapeHtml, fillExistingTagsPicker, reload })
        }
      })
    })

    mainContent.querySelectorAll('.delete-user-btn').forEach((btn) => {
      btn.addEventListener('click', async () => {
        const userId = btn.dataset.userId || ''
        const username = btn.dataset.username || userId
        if (!userId) return
        const ok = await uiConfirm(t('users.confirmDelete', { name: username }))
        if (!ok) return
        btn.disabled = true
        try {
          await API.deleteUser(userId)
          reload()
        } catch (err) {
          btn.disabled = false
          await uiAlert(t('users.deleteFailed', { error: err.message || String(err) }))
        }
      })
    })

    mainContent.querySelectorAll('.reset-totp-btn').forEach((btn) => {
      btn.addEventListener('click', async () => {
        const userId = btn.dataset.userId || ''
        const username = btn.dataset.username || userId
        if (!userId) return
        const ok = await uiConfirm(t('users.confirmResetTotp', { name: username }), { danger: true })
        if (!ok) return
        btn.disabled = true
        try {
          await API.adminResetTotp(userId)
          reload()
        } catch (err) {
          btn.disabled = false
          await uiAlert(t('users.resetTotpFailed', { error: err.message || String(err) }))
        }
      })
    })

    mainContent.querySelectorAll('.user-ssh-keys-btn').forEach((btn) => {
      btn.addEventListener('click', () => {
        showUserSSHKeysModal({
          userId: btn.dataset.userId || '',
          username: btn.dataset.username || '',
          escapeHtml,
        })
      })
    })
  } catch (e) {
    mainContent.innerHTML = `<p class="text-sm text-red-600">${escapeHtml(
      e.message || t('users.fetchFailed'),
    )}</p>`
  }
}

function showAddUserModal({ reload }) {
  const modal = document.getElementById('add-user-modal')
  modal.classList.remove('hidden')
  modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('users.addTitle')}</h3>
            <button id="add-user-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="add-user-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('users.fieldUsername')}</label>
                <input type="text" id="add-user-username" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('users.fieldUsernamePlaceholder')}" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('users.fieldPassword')}</label>
                <input type="password" id="add-user-password" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('users.fieldPasswordPlaceholder')}" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('users.fieldRole')}</label>
                <select id="add-user-role" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                  <option value="user">${t('users.roleUser')}</option>
                  <option value="admin">${t('users.roleAdmin')}</option>
                </select>
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('users.fieldId')}</label>
                <input type="text" id="add-user-id" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('users.fieldIdPlaceholder')}" />
              </div>
              <p id="add-user-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="add-user-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('users.cancel')}</button>
              <button type="submit" id="add-user-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('users.submit')}</button>
            </div>
          </form>
        </div>
      </div>
    `
  const close = () => {
    modal.classList.add('hidden')
    modal.innerHTML = ''
  }
  modal.querySelector('#add-user-close').addEventListener('click', close)
  modal.querySelector('#add-user-cancel').addEventListener('click', close)
  modal.querySelector('#add-user-form').addEventListener('submit', async (e) => {
    e.preventDefault()
    const errorEl = modal.querySelector('#add-user-error')
    const submitBtn = modal.querySelector('#add-user-submit')
    errorEl.classList.add('hidden')
    const username = modal.querySelector('#add-user-username').value.trim()
    const password = modal.querySelector('#add-user-password').value
    const role = modal.querySelector('#add-user-role').value || 'user'
    const rawId = modal.querySelector('#add-user-id').value.trim()
    const idValidationError = validateOptionalUserId(rawId)
    if (idValidationError) {
      errorEl.textContent = idValidationError
      errorEl.classList.remove('hidden')
      return
    }
    const id = rawId || undefined
    if (!username || !password) {
      errorEl.textContent = t('users.usernamePasswordRequired')
      errorEl.classList.remove('hidden')
      return
    }
    submitBtn.disabled = true
    try {
      await API.createUser({ id, username, password, role })
      close()
      await reload()
    } catch (err) {
      errorEl.textContent = err.message || t('users.createFailed', { error: '' }).replace(/:\s*$/, '')
      errorEl.classList.remove('hidden')
    } finally {
      submitBtn.disabled = false
    }
  })
}

function showEditUserModal({ user, escapeHtml, fillExistingTagsPicker, reload }) {
  const modal = document.getElementById('edit-tags-modal')
  modal.classList.remove('hidden')
  const tagsStr = (user.tags || []).join(', ')
  modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('users.editTitle')}</h3>
            <button id="edit-user-modal-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="edit-user-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('users.fieldIdValue')}</label>
                <p class="text-sm text-slate-800">${escapeHtml(user.id)}</p>
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('users.fieldUsername')}</label>
                <p class="text-sm text-slate-800">${escapeHtml(user.username)}</p>
              </div>
              <div>
                <label for="edit-user-role" class="block text-xs font-medium text-slate-600 mb-1.5">${t('users.fieldRole')}</label>
                <select id="edit-user-role" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                  <option value="user"${(user.role || 'user') === 'user' ? ' selected' : ''}>${t('users.roleUser')}</option>
                  <option value="admin"${user.role === 'admin' ? ' selected' : ''}>${t('users.roleAdmin')}</option>
                </select>
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('users.fieldTagsLabel')}</label>
                <input type="text" id="edit-user-tags-input" value="${escapeHtml(
                  tagsStr,
                )}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('users.fieldTagsPlaceholder')}" />
                <div id="edit-user-tags-input-picker" class="mt-2"></div>
              </div>
              <p id="edit-user-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="edit-user-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('users.cancel')}</button>
              <button type="submit" id="edit-user-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('users.save')}</button>
            </div>
          </form>
        </div>
      </div>
    `
  const close = () => {
    modal.classList.add('hidden')
    modal.innerHTML = ''
  }
  modal.querySelector('#edit-user-modal-close').addEventListener('click', close)
  modal.querySelector('#edit-user-cancel').addEventListener('click', close)
  fillExistingTagsPicker(modal, 'edit-user-tags-input')
  modal.querySelector('#edit-user-form').addEventListener('submit', async (e) => {
    e.preventDefault()
    const errorEl = modal.querySelector('#edit-user-error')
    const submitBtn = modal.querySelector('#edit-user-submit')
    const raw = modal.querySelector('#edit-user-tags-input').value.trim()
    const tags = raw ? raw.split(',').map((t) => t.trim()).filter(Boolean) : []
    const role = modal.querySelector('#edit-user-role').value
    errorEl.classList.add('hidden')
    submitBtn.disabled = true
    try {
      if (role !== (user.role || 'user')) {
        await API.updateUser(user.id, { role })
      }
      await API.setUserTags(user.id, tags)
      close()
      await reload()
    } catch (err) {
      errorEl.textContent = err.message || t('users.saveFailed', { error: '' }).replace(/:\s*$/, '')
      errorEl.classList.remove('hidden')
    } finally {
      submitBtn.disabled = false
    }
  })
}

async function showUserSSHKeysModal({ userId, username, escapeHtml }) {
  if (!userId) return
  const modal = document.getElementById('add-ssh-key-modal')
  modal.classList.remove('hidden')
  const safeName = escapeHtml(username || userId)
  modal.innerHTML = `
      <div class="fixed inset-0 bg-black/40 flex items-center justify-center p-4" id="user-ssh-keys-backdrop">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-2xl border border-slate-200 max-h-[90vh] flex flex-col">
          <div class="px-5 py-3 border-b border-slate-200 flex items-center justify-between shrink-0">
            <h3 class="font-semibold text-slate-800">${t('users.keysTitle')} — ${safeName}</h3>
            <button id="user-ssh-keys-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <div class="px-5 py-4 overflow-auto flex-1 min-h-0">
            <p class="text-xs text-slate-600 mb-3">${t('users.keysHint')}</p>
            <div id="user-ssh-keys-list" class="mb-4">${t('users.keysLoading')}</div>
            <div class="border-t border-slate-200 pt-4">
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('users.keyAdd')}</label>
              <textarea id="user-ssh-key-input" rows="2" class="w-full rounded border border-slate-300 px-3 py-2 text-sm font-mono text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="ssh-ed25519 AAAAC3... user@host"></textarea>
              <p id="user-ssh-key-error" class="mt-1 text-sm text-red-600 hidden"></p>
              <button type="button" id="user-ssh-key-add-btn" class="mt-2 rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700">${t('users.keyAddBtn')}</button>
            </div>
          </div>
        </div>
      </div>
    `
  const close = () => {
    modal.classList.add('hidden')
    modal.innerHTML = ''
  }
  const renderList = (keys) => {
    const listEl = modal.querySelector('#user-ssh-keys-list')
    if (!listEl) return
    if (!Array.isArray(keys)) keys = []
    const keyRows = keys
      .map((k) => {
        const keyDisplay =
          (k.key_line || '').length > 56
            ? (k.key_line || '').slice(0, 53) + '...'
            : k.key_line || ''
        return `
          <div class="flex items-center justify-between gap-2 py-2 border-b border-slate-100 text-sm">
            <span class="font-mono text-slate-700 truncate flex-1" title="${escapeHtml(
              k.key_line || '',
            )}">${escapeHtml(keyDisplay)}</span>
            <span class="text-xs text-slate-400 shrink-0">${escapeHtml(formatDateTime(k.created_at))}</span>
            <button type="button" class="user-ssh-key-del-btn rounded border border-red-200 px-2 py-0.5 text-xs text-red-700 hover:bg-red-50 shrink-0" data-key-id="${escapeHtml(
              String(k.id),
            )}">${t('users.keyDelete')}</button>
          </div>
        `
      })
      .join('')
    listEl.innerHTML = keyRows
      ? `<div class="space-y-0">${keyRows}</div>`
      : `<p class="text-slate-500 text-sm">${t('users.keysNone')}</p>`
    modal.querySelectorAll('.user-ssh-key-del-btn').forEach((btn) => {
      btn.addEventListener('click', async () => {
        if (!(await uiConfirm(t('users.keyConfirmDelete'), { danger: true }))) return
        try {
          await API.deleteUserSSHKey(userId, btn.dataset.keyId)
          const keys = await API.userSSHKeys(userId)
          renderList(keys)
        } catch (e) {
          await uiAlert(e.message || t('users.keyDeleteFailed'))
        }
      })
    })
  }

  modal.querySelector('#user-ssh-keys-close').addEventListener('click', close)
  modal
    .querySelector('#user-ssh-keys-backdrop')
    .addEventListener('click', (e) => {
      if (e.target.id === 'user-ssh-keys-backdrop') close()
    })
  modal.querySelector('#user-ssh-key-add-btn').addEventListener('click', async () => {
    const errorEl = modal.querySelector('#user-ssh-key-error')
    const raw = (modal.querySelector('#user-ssh-key-input').value || '').trim()
    const line = raw.split(/\r?\n/)[0]?.trim() || raw
    errorEl.classList.add('hidden')
    if (!line) {
      errorEl.textContent = t('users.keyEnterOneLine')
      errorEl.classList.remove('hidden')
      return
    }
    try {
      await API.addUserSSHKey(userId, line)
      modal.querySelector('#user-ssh-key-input').value = ''
      const keys = await API.userSSHKeys(userId)
      renderList(keys)
    } catch (e) {
      errorEl.textContent = e.message || t('users.keyAddFailed')
      errorEl.classList.remove('hidden')
    }
  })

  try {
    const keys = await API.userSSHKeys(userId)
    renderList(keys)
  } catch (e) {
    const listEl = modal.querySelector('#user-ssh-keys-list')
    if (listEl) {
      listEl.innerHTML = `<p class="text-sm text-red-600">${escapeHtml(
        e.message || t('users.keysFetchFailed'),
      )}</p>`
    }
  }
}

