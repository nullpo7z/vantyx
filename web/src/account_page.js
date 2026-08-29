import API from './api.js'
import { t } from './i18n.js'
import { setActiveNav } from './nav.js'
import { uiAlert } from './ui_dialog.js'

export function renderUserInfo({ mainContent, meData, escapeHtml, onChangePassword }) {
  // The account page has no nav entry of its own; clear the previous
  // page's active state so e.g. "Recordings" doesn't stay underlined (E-1).
  if (typeof setActiveNav === 'function') setActiveNav('account')
  if (!meData) return
  mainContent.innerHTML = `
      <h2 class="text-lg font-medium text-slate-800 mb-4">${t('account.title')}</h2>
      <div class="bg-white rounded-lg border border-slate-200 shadow-sm overflow-hidden">
        <dl class="divide-y divide-slate-200">
          <div class="px-4 py-3 sm:grid sm:grid-cols-3 sm:gap-4">
            <dt class="text-sm font-medium text-slate-500">${t('account.userId')}</dt>
            <dd class="mt-1 text-sm text-slate-800 sm:mt-0 sm:col-span-2">${escapeHtml(meData.user_id)}</dd>
          </div>
          <div class="px-4 py-3 sm:grid sm:grid-cols-3 sm:gap-4">
            <dt class="text-sm font-medium text-slate-500">${t('account.username')}</dt>
            <dd class="mt-1 text-sm text-slate-800 sm:mt-0 sm:col-span-2">${escapeHtml(meData.username)}</dd>
          </div>
        </dl>
        <div class="px-4 py-3 border-t border-slate-200">
          <button type="button" id="btn-change-password" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('account.changePasswordBtn')}</button>
        </div>
      </div>

      <h2 class="text-lg font-medium text-slate-800 mt-8 mb-4">${t('account.totpTitle')}</h2>
      <div id="account-totp" class="bg-white rounded-lg border border-slate-200 shadow-sm overflow-hidden">
        <p class="px-4 py-3 text-sm text-slate-500">${t('common.loading')}</p>
      </div>
    `
  const btn = mainContent.querySelector('#btn-change-password')
  if (btn && typeof onChangePassword === 'function') {
    btn.addEventListener('click', (e) => {
      e.preventDefault()
      onChangePassword()
    })
  }
  renderTOTPSection(mainContent.querySelector('#account-totp'), { escapeHtml })
}

/**
 * Two-factor authentication card: shows whether TOTP is enabled and
 * offers enrolment or (password-confirmed) removal.
 */
async function renderTOTPSection(card, { escapeHtml }) {
  if (!card) return
  let status
  try {
    status = await API.totpStatus()
  } catch (err) {
    card.innerHTML = `<p class="px-4 py-3 text-sm text-red-600">${escapeHtml(err.message || t('account.totpLoadFailed'))}</p>`
    return
  }
  const enabled = !!(status && status.enabled)
  card.innerHTML = `
      <div class="px-4 py-3 flex flex-wrap items-center justify-between gap-3">
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${
              enabled ? 'bg-emerald-100 text-emerald-800' : 'bg-slate-100 text-slate-600'
            }">${enabled ? t('account.totpEnabled') : t('account.totpDisabled')}</span>
            ${
              enabled && typeof status.recovery_codes_left === 'number'
                ? `<span class="text-xs text-slate-500">${t('account.totpRecoveryLeft', { n: status.recovery_codes_left })}</span>`
                : ''
            }
          </div>
          <p class="mt-1 text-xs text-slate-500">${t('account.totpHint')}</p>
        </div>
        <div class="shrink-0">
          ${
            enabled
              ? `<button type="button" id="btn-totp-disable" class="rounded border border-red-200 bg-white px-3 py-1.5 text-sm font-medium text-red-700 hover:bg-red-50 shadow-sm transition-colors">${t('account.totpDisableBtn')}</button>`
              : `<button type="button" id="btn-totp-enable" class="rounded bg-sky-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('account.totpEnableBtn')}</button>`
          }
        </div>
      </div>
    `
  const reload = () => renderTOTPSection(card, { escapeHtml })
  const enableBtn = card.querySelector('#btn-totp-enable')
  if (enableBtn) enableBtn.addEventListener('click', () => showTOTPSetupModal({ escapeHtml, onDone: reload }))
  const disableBtn = card.querySelector('#btn-totp-disable')
  if (disableBtn) disableBtn.addEventListener('click', () => showTOTPDisableModal({ onDone: reload }))
}

/**
 * Enrolment modal: fetch a fresh secret, show the QR / manual key, and
 * confirm with the first code. Recovery codes are shown exactly once.
 */
async function showTOTPSetupModal({ escapeHtml, onDone }) {
  const modal = document.getElementById('change-password-modal')
  if (!modal) return
  const close = () => {
    modal.classList.add('hidden')
    modal.innerHTML = ''
  }
  modal.classList.remove('hidden')
  modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('account.totpSetupTitle')}</h3>
            <button id="totp-setup-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <div id="totp-setup-body" class="px-6 py-5"><p class="text-sm text-slate-500">${t('common.loading')}</p></div>
        </div>
      </div>
    `
  modal.querySelector('#totp-setup-close').addEventListener('click', close)
  const body = modal.querySelector('#totp-setup-body')

  let setup
  try {
    setup = await API.totpSetup()
  } catch (err) {
    body.innerHTML = `<p class="text-sm text-red-600">${escapeHtml(err.message || t('account.totpSetupFailed'))}</p>`
    return
  }

  body.innerHTML = `
      <form id="totp-setup-form" class="space-y-4">
        <p class="text-sm text-slate-600">${t('account.totpSetupStep1')}</p>
        <div class="flex justify-center">
          <img src="${escapeHtml(setup.qr_png || '')}" alt="QR" width="200" height="200" class="border border-slate-200 rounded" />
        </div>
        <details class="text-xs text-slate-600">
          <summary class="cursor-pointer select-none">${t('account.totpManualKey')}</summary>
          <code class="block mt-1 break-all rounded bg-slate-50 border border-slate-200 px-2 py-1 font-mono text-slate-800">${escapeHtml(setup.secret || '')}</code>
        </details>
        <div>
          <label for="totp-setup-code" class="block text-xs font-medium text-slate-600 mb-1.5">${t('account.totpSetupStep2')}</label>
          <input type="text" id="totp-setup-code" required inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}"
            class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 tracking-widest focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="123456" />
        </div>
        <p id="totp-setup-error" class="text-sm text-red-600 hidden"></p>
        <div class="flex justify-end gap-3 pt-2">
          <button type="button" id="totp-setup-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('account.cancel')}</button>
          <button type="submit" id="totp-setup-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('account.totpConfirmBtn')}</button>
        </div>
      </form>
    `
  body.querySelector('#totp-setup-cancel').addEventListener('click', close)
  const input = body.querySelector('#totp-setup-code')
  input.focus()
  body.querySelector('#totp-setup-form').addEventListener('submit', async (e) => {
    e.preventDefault()
    const errorEl = body.querySelector('#totp-setup-error')
    const submit = body.querySelector('#totp-setup-submit')
    errorEl.classList.add('hidden')
    submit.disabled = true
    try {
      const res = await API.totpConfirm(input.value.trim())
      showRecoveryCodes(body, res && res.recovery_codes, { escapeHtml, onClose: () => {
        close()
        if (typeof onDone === 'function') onDone()
      } })
    } catch (err) {
      errorEl.textContent = err.message || t('account.totpConfirmFailed')
      errorEl.classList.remove('hidden')
      input.select()
    } finally {
      submit.disabled = false
    }
  })
}

function showRecoveryCodes(body, codes, { escapeHtml, onClose }) {
  const list = Array.isArray(codes) ? codes : []
  body.innerHTML = `
      <div class="space-y-4">
        <p class="text-sm text-emerald-700 font-medium">${t('account.totpEnabledNow')}</p>
        <p class="text-sm text-slate-600">${t('account.totpRecoveryIntro')}</p>
        <div class="grid grid-cols-2 gap-x-4 gap-y-1 rounded bg-slate-50 border border-slate-200 px-3 py-2 font-mono text-sm text-slate-800">
          ${list.map((c) => `<span>${escapeHtml(c)}</span>`).join('')}
        </div>
        <div class="flex justify-end gap-3 pt-2">
          <button type="button" id="totp-recovery-copy" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('account.totpRecoveryCopy')}</button>
          <button type="button" id="totp-recovery-done" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('account.totpRecoveryDone')}</button>
        </div>
      </div>
    `
  body.querySelector('#totp-recovery-copy').addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(list.join('\n'))
    } catch {
      /* clipboard unavailable: codes are still on screen */
    }
  })
  body.querySelector('#totp-recovery-done').addEventListener('click', onClose)
}

/**
 * Removing the second factor re-authenticates with the password so a
 * hijacked browser session alone cannot weaken the account.
 */
async function showTOTPDisableModal({ onDone }) {
  const modal = document.getElementById('change-password-modal')
  if (!modal) return
  const close = () => {
    modal.classList.add('hidden')
    modal.innerHTML = ''
  }
  modal.classList.remove('hidden')
  modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('account.totpDisableTitle')}</h3>
            <button id="totp-disable-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="totp-disable-form">
            <div class="px-6 py-5 space-y-4">
              <p class="text-sm text-slate-600">${t('account.totpDisableIntro')}</p>
              <div>
                <label for="totp-disable-password" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.currentPassword')}</label>
                <input type="password" id="totp-disable-password" autocomplete="current-password" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" />
              </div>
              <p id="totp-disable-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="totp-disable-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('account.cancel')}</button>
              <button type="submit" id="totp-disable-submit" class="rounded bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700 shadow-sm transition-colors">${t('account.totpDisableBtn')}</button>
            </div>
          </form>
        </div>
      </div>
    `
  modal.querySelector('#totp-disable-close').addEventListener('click', close)
  modal.querySelector('#totp-disable-cancel').addEventListener('click', close)
  modal.querySelector('#totp-disable-form').addEventListener('submit', async (e) => {
    e.preventDefault()
    const errorEl = modal.querySelector('#totp-disable-error')
    const submit = modal.querySelector('#totp-disable-submit')
    errorEl.classList.add('hidden')
    submit.disabled = true
    try {
      await API.totpDisable(modal.querySelector('#totp-disable-password').value)
      close()
      await uiAlert(t('account.totpDisabledNow'))
      if (typeof onDone === 'function') onDone()
    } catch (err) {
      errorEl.textContent = err.message || t('account.totpDisableFailed')
      errorEl.classList.remove('hidden')
    } finally {
      submit.disabled = false
    }
  })
}

export function showChangePasswordModal() {
  const modal = document.getElementById('change-password-modal')
  if (!modal) return
  modal.classList.remove('hidden')
  modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('account.modalTitle')}</h3>
            <button id="change-password-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="change-password-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label for="change-password-current" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.currentPassword')}</label>
                <input type="password" id="change-password-current" autocomplete="current-password" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('account.currentPlaceholder')}" />
              </div>
              <div>
                <label for="change-password-new" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.newPassword')}</label>
                <input type="password" id="change-password-new" autocomplete="new-password" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('account.newPlaceholder')}" />
              </div>
              <div>
                <label for="change-password-confirm" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.newPasswordConfirm')}</label>
                <input type="password" id="change-password-confirm" autocomplete="new-password" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('account.confirmPlaceholder')}" />
              </div>
              <p id="change-password-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="change-password-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('account.cancel')}</button>
              <button type="submit" id="change-password-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('account.submit')}</button>
            </div>
          </form>
        </div>
      </div>
    `
  const close = () => {
    modal.classList.add('hidden')
    modal.innerHTML = ''
  }
  modal.querySelector('#change-password-close').addEventListener('click', close)
  modal.querySelector('#change-password-cancel').addEventListener('click', close)
  modal.querySelector('#change-password-form').addEventListener('submit', async (e) => {
    e.preventDefault()
    const errorEl = modal.querySelector('#change-password-error')
    const submitBtn = modal.querySelector('#change-password-submit')
    const current = modal.querySelector('#change-password-current').value
    const newPass = modal.querySelector('#change-password-new').value
    const confirmPass = modal.querySelector('#change-password-confirm').value
    errorEl.classList.add('hidden')
    if (newPass !== confirmPass) {
      errorEl.textContent = t('account.mismatch')
      errorEl.classList.remove('hidden')
      return
    }
    submitBtn.disabled = true
    try {
      await API.changePassword(current, newPass)
      close()
      await uiAlert(t('account.success'))
    } catch (err) {
      errorEl.textContent = err.message || t('account.failed')
      errorEl.classList.remove('hidden')
    } finally {
      submitBtn.disabled = false
    }
  })
}
