/**
 * @file Account settings page: one place for everything about the
 * signed-in user (profile, language, password, two-factor auth, SSH
 * public keys). Reached from the "Account settings" nav entry and from the
 * user name in the header. Server-wide settings live in
 * system_settings_page.js.
 */

import API from './api.js'
import { formatDateTime } from './datetime.js'
import { getLocale, setLocale, SUPPORTED_LOCALES, t } from './i18n.js'
import { setActiveNav } from './nav.js'
import { uiAlert, uiConfirm } from './ui_dialog.js'
import { createPasskey, webauthnSupported } from './webauthn.js'

function esc(s) {
  return String(s ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
}

const INPUT =
  'w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white'
const BTN_PRIMARY =
  'rounded bg-sky-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors disabled:opacity-50'
const BTN_SECONDARY =
  'rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors disabled:opacity-50'
const BTN_DANGER =
  'rounded border border-red-200 bg-white px-3 py-1.5 text-sm font-medium text-red-700 hover:bg-red-50 shadow-sm transition-colors disabled:opacity-50'

function card(title, hint, body, extra = '') {
  return `
    <section class="mt-4 rounded-lg border border-slate-200 bg-white shadow-sm">
      <div class="px-5 pt-4 pb-3 border-b border-slate-100 flex items-start justify-between gap-3">
        <div>
          <h3 class="text-sm font-semibold text-slate-800">${title}</h3>
          ${hint ? `<p class="mt-0.5 text-xs text-slate-500">${hint}</p>` : ''}
        </div>
        ${extra}
      </div>
      <div class="px-5 py-4">${body}</div>
    </section>`
}

function pill(text, cls = 'bg-slate-100 text-slate-700') {
  return `<span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${cls}">${esc(text)}</span>`
}

/**
 * @param {HTMLElement} container
 * @param {{ meData: object, onMeChanged?: (me: object) => void }} opts
 */
export async function renderAccountPage(container, { meData, onMeChanged } = {}) {
  if (typeof setActiveNav === 'function') setActiveNav('settings')
  if (!meData) return
  const isAdmin = meData.role === 'admin'

  const langOptions = SUPPORTED_LOCALES.map(
    (l) => `<option value="${l.code}"${l.code === getLocale() ? ' selected' : ''}>${t(l.labelKey)}</option>`,
  ).join('')

  // Two columns on wide screens so the page uses the whole viewport:
  // identity + credentials on the left, second factor + SSH keys on the right.
  container.innerHTML = `
    <div class="w-full flex-1 flex flex-col">
      <h2 class="text-lg font-semibold text-slate-800">${t('account.title')}</h2>
      <div class="grid grid-cols-1 xl:grid-cols-2 gap-x-6 items-start">
      <div class="min-w-0">
      ${card(
        t('account.sectionProfile'),
        '',
        `<dl class="grid grid-cols-1 sm:grid-cols-[10rem_1fr] gap-x-4 gap-y-3 text-sm">
          <dt class="text-slate-500">${t('account.username')}</dt><dd class="text-slate-800 font-medium">${esc(meData.username)}</dd>
          <dt class="text-slate-500">${t('account.userId')}</dt><dd class="text-slate-800 font-mono text-xs">${esc(meData.user_id)}</dd>
          <dt class="text-slate-500">${t('account.role')}</dt><dd>${pill(isAdmin ? t('account.roleAdmin') : t('account.roleUser'), isAdmin ? 'bg-sky-100 text-sky-800' : 'bg-slate-100 text-slate-700')}</dd>
          <dt class="text-slate-500">${t('account.groups')}</dt><dd id="account-groups" class="flex flex-wrap gap-1">${
            Array.isArray(meData.groups) && meData.groups.length
              ? meData.groups.map((g) => pill(g.name && g.name !== g.id ? `${g.id} (${g.name})` : g.id)).join('')
              : `<span class="text-xs text-slate-400">${t('account.none')}</span>`
          }</dd>
          <dt class="text-slate-500">${t('account.tags')}</dt><dd class="flex flex-wrap gap-1">${
            Array.isArray(meData.tags) && meData.tags.length
              ? meData.tags.map((x) => pill(x, 'bg-emerald-50 text-emerald-800')).join('')
              : `<span class="text-xs text-slate-400">${t('account.none')}</span>`
          }</dd>
        </dl>
        <p class="mt-3 text-xs text-slate-500">${t('account.profileHint')}</p>`,
      )}

      ${card(
        t('settings.sectionLanguage'),
        t('settings.languageHint'),
        `<select id="settings-language" class="${INPUT} sm:w-64">${langOptions}</select>`,
      )}

      ${card(
        t('account.sectionPassword'),
        t('account.passwordHint'),
        `<form id="account-password-form" class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div class="sm:col-span-2">
            <label for="pw-current" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.currentPassword')}</label>
            <input type="password" id="pw-current" autocomplete="current-password" required class="${INPUT} sm:w-1/2" />
          </div>
          <div>
            <label for="pw-new" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.newPassword')}</label>
            <input type="password" id="pw-new" autocomplete="new-password" required minlength="8" class="${INPUT}" placeholder="${t('account.newPlaceholder')}" />
          </div>
          <div>
            <label for="pw-confirm" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.newPasswordConfirm')}</label>
            <input type="password" id="pw-confirm" autocomplete="new-password" required minlength="8" class="${INPUT}" placeholder="${t('account.confirmPlaceholder')}" />
          </div>
          <div class="sm:col-span-2 flex items-center gap-3">
            <button type="submit" id="pw-submit" class="${BTN_PRIMARY}">${t('account.changePasswordBtn')}</button>
            <span id="pw-status" class="text-sm"></span>
          </div>
        </form>`,
      )}
      </div>
      <div class="min-w-0">
      <section id="account-totp" class="mt-4 rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="px-5 py-4 text-sm text-slate-500">${t('common.loading')}</div>
      </section>

      <section id="account-passkeys" class="mt-4 rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="px-5 py-4 text-sm text-slate-500">${t('common.loading')}</div>
      </section>

      <section id="account-keys" class="mt-4 rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="px-5 py-4 text-sm text-slate-500">${t('common.loading')}</div>
      </section>

      <section id="account-tokens" class="mt-4 rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="px-5 py-4 text-sm text-slate-500">${t('common.loading')}</div>
      </section>
      </div>
      </div>
    </div>
  `

  // Language
  container.querySelector('#settings-language').addEventListener('change', (e) => setLocale(e.target.value))

  // Password (inline, no modal)
  const pwForm = container.querySelector('#account-password-form')
  const pwStatus = container.querySelector('#pw-status')
  pwForm.addEventListener('submit', async (e) => {
    e.preventDefault()
    const current = container.querySelector('#pw-current').value
    const next = container.querySelector('#pw-new').value
    const confirm = container.querySelector('#pw-confirm').value
    pwStatus.className = 'text-sm text-red-600'
    if (next !== confirm) {
      pwStatus.textContent = t('account.mismatch')
      return
    }
    const btn = container.querySelector('#pw-submit')
    btn.disabled = true
    try {
      await API.changePassword(current, next)
      pwForm.reset()
      pwStatus.className = 'text-sm text-emerald-700'
      pwStatus.textContent = t('account.success')
    } catch (err) {
      pwStatus.textContent = err.message || t('account.failed')
    } finally {
      btn.disabled = false
    }
  })

  await Promise.all([
    renderTOTPCard(container.querySelector('#account-totp'), { meData }),
    renderPasskeysCard(container.querySelector('#account-passkeys')),
    renderSSHKeysCard(container.querySelector('#account-keys'), { meData }),
    renderTokensCard(container.querySelector('#account-tokens')),
  ])
  if (typeof onMeChanged === 'function') onMeChanged(meData)
}

/* ------------------------------------------------------------------ */
/* Two-factor authentication                                           */
/* ------------------------------------------------------------------ */

async function renderTOTPCard(section, { meData }) {
  if (!section) return
  let status
  try {
    status = await API.totpStatus()
  } catch (err) {
    section.innerHTML = `<div class="px-5 py-4 text-sm text-red-600">${esc(err.message || t('account.totpLoadFailed'))}</div>`
    return
  }
  const enabled = !!(status && status.enabled)
  const reload = () => renderTOTPCard(section, { meData })

  const statusLine = enabled
    ? `<div class="flex flex-wrap items-center gap-2">
        ${pill(t('account.totpEnabled'), 'bg-emerald-100 text-emerald-800')}
        ${status.confirmed_at ? `<span class="text-xs text-slate-500">${t('account.totpSince', { date: esc(formatDateTime(status.confirmed_at)) })}</span>` : ''}
        <span class="text-xs ${status.recovery_codes_left <= 2 ? 'text-amber-700' : 'text-slate-500'}">${t('account.totpRecoveryLeft', { n: status.recovery_codes_left })}</span>
      </div>`
    : `<div class="flex flex-wrap items-center gap-2">${pill(t('account.totpDisabled'))}</div>`

  const body = enabled
    ? `${statusLine}
       <ul class="mt-3 text-xs text-slate-600 list-disc pl-5 space-y-1">
         <li>${t('account.totpEnabledHint1')}</li>
         <li>${t('account.totpEnabledHint2')}</li>
       </ul>
       <div class="mt-4"><button type="button" id="btn-totp-disable" class="${BTN_DANGER}">${t('account.totpDisableBtn')}</button></div>`
    : `${statusLine}
       <ol class="mt-3 text-xs text-slate-600 list-decimal pl-5 space-y-1">
         <li>${t('account.totpHow1')}</li>
         <li>${t('account.totpHow2')}</li>
         <li>${t('account.totpHow3')}</li>
       </ol>
       <div class="mt-4"><button type="button" id="btn-totp-enable" class="${BTN_PRIMARY}">${t('account.totpEnableBtn')}</button></div>`

  section.innerHTML = `
    <div class="px-5 pt-4 pb-3 border-b border-slate-100">
      <h3 class="text-sm font-semibold text-slate-800">${t('account.totpTitle')}</h3>
      <p class="mt-0.5 text-xs text-slate-500">${t('account.totpHint')}</p>
    </div>
    <div class="px-5 py-4">${body}</div>`

  const enableBtn = section.querySelector('#btn-totp-enable')
  if (enableBtn) enableBtn.addEventListener('click', () => showTOTPWizard({ onDone: reload }))
  const disableBtn = section.querySelector('#btn-totp-disable')
  if (disableBtn) disableBtn.addEventListener('click', () => showTOTPDisableModal({ onDone: reload }))
}

function modalShell(title, stepLabel) {
  return `
    <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
      <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
        <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
          <div>
            <h3 class="font-semibold text-slate-800">${title}</h3>
            ${stepLabel ? `<p id="totp-step-label" class="text-xs text-slate-500 mt-0.5">${stepLabel}</p>` : ''}
          </div>
          <button id="totp-modal-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors" aria-label="${t('common.close')}">&times;</button>
        </div>
        <div id="totp-modal-body" class="px-6 py-5"></div>
      </div>
    </div>`
}

/**
 * Three-step enrolment: scan → confirm code → save recovery codes.
 */
async function showTOTPWizard({ onDone }) {
  const modal = document.getElementById('change-password-modal')
  if (!modal) return
  const close = () => {
    modal.classList.add('hidden')
    modal.innerHTML = ''
  }
  modal.classList.remove('hidden')
  modal.innerHTML = modalShell(t('account.totpSetupTitle'), t('account.totpStep', { n: 1, total: 3 }))
  modal.querySelector('#totp-modal-close').addEventListener('click', close)
  const body = modal.querySelector('#totp-modal-body')
  const stepLabel = modal.querySelector('#totp-step-label')
  body.innerHTML = `<p class="text-sm text-slate-500">${t('common.loading')}</p>`

  let setup
  try {
    setup = await API.totpSetup()
  } catch (err) {
    body.innerHTML = `<p class="text-sm text-red-600">${esc(err.message || t('account.totpSetupFailed'))}</p>`
    return
  }

  // Step 1: scan
  const renderStep1 = () => {
    stepLabel.textContent = t('account.totpStep', { n: 1, total: 3 })
    body.innerHTML = `
      <p class="text-sm text-slate-700">${t('account.totpSetupStep1')}</p>
      <div class="mt-3 flex justify-center">
        <img src="${esc(setup.qr_png || '')}" alt="QR" width="200" height="200" class="border border-slate-200 rounded" />
      </div>
      <details class="mt-3 text-xs text-slate-600">
        <summary class="cursor-pointer select-none">${t('account.totpManualKey')}</summary>
        <code class="block mt-1 break-all rounded bg-slate-50 border border-slate-200 px-2 py-1 font-mono text-slate-800">${esc(setup.secret || '')}</code>
      </details>
      <div class="mt-5 flex justify-end gap-3">
        <button type="button" id="totp-cancel" class="${BTN_SECONDARY}">${t('account.cancel')}</button>
        <button type="button" id="totp-next" class="${BTN_PRIMARY}">${t('account.totpNext')}</button>
      </div>`
    body.querySelector('#totp-cancel').addEventListener('click', close)
    body.querySelector('#totp-next').addEventListener('click', renderStep2)
  }

  // Step 2: confirm code
  const renderStep2 = () => {
    stepLabel.textContent = t('account.totpStep', { n: 2, total: 3 })
    body.innerHTML = `
      <form id="totp-confirm-form">
        <p class="text-sm text-slate-700">${t('account.totpSetupStep2')}</p>
        <input type="text" id="totp-setup-code" required inputmode="numeric" autocomplete="one-time-code" pattern="[0-9 ]{6,7}"
          class="${INPUT} mt-3 text-center text-lg tracking-[0.4em] font-mono" placeholder="123456" />
        <p id="totp-setup-error" class="mt-2 text-sm text-red-600 hidden"></p>
        <div class="mt-5 flex justify-between gap-3">
          <button type="button" id="totp-back" class="${BTN_SECONDARY}">${t('account.totpBack')}</button>
          <button type="submit" id="totp-setup-submit" class="${BTN_PRIMARY}">${t('account.totpConfirmBtn')}</button>
        </div>
      </form>`
    const input = body.querySelector('#totp-setup-code')
    input.focus()
    body.querySelector('#totp-back').addEventListener('click', renderStep1)
    body.querySelector('#totp-confirm-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errorEl = body.querySelector('#totp-setup-error')
      const submit = body.querySelector('#totp-setup-submit')
      errorEl.classList.add('hidden')
      submit.disabled = true
      try {
        const res = await API.totpConfirm(input.value.replace(/\s+/g, ''))
        renderStep3(res && res.recovery_codes)
      } catch (err) {
        errorEl.textContent = err.message || t('account.totpConfirmFailed')
        errorEl.classList.remove('hidden')
        input.select()
      } finally {
        submit.disabled = false
      }
    })
  }

  // Step 3: recovery codes
  const renderStep3 = (codes) => {
    const list = Array.isArray(codes) ? codes : []
    stepLabel.textContent = t('account.totpStep', { n: 3, total: 3 })
    body.innerHTML = `
      <p class="text-sm text-emerald-700 font-medium">${t('account.totpEnabledNow')}</p>
      <p class="mt-2 text-sm text-slate-700">${t('account.totpRecoveryIntro')}</p>
      <div class="mt-3 grid grid-cols-2 gap-x-4 gap-y-1 rounded bg-slate-50 border border-slate-200 px-3 py-2 font-mono text-sm text-slate-800">
        ${list.map((c) => `<span>${esc(c)}</span>`).join('')}
      </div>
      <div class="mt-5 flex justify-end gap-3">
        <button type="button" id="totp-recovery-copy" class="${BTN_SECONDARY}">${t('account.totpRecoveryCopy')}</button>
        <button type="button" id="totp-recovery-done" class="${BTN_PRIMARY}">${t('account.totpRecoveryDone')}</button>
      </div>`
    body.querySelector('#totp-recovery-copy').addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(list.join('\n'))
        body.querySelector('#totp-recovery-copy').textContent = t('common.copied')
      } catch {
        /* clipboard unavailable: codes are still on screen */
      }
    })
    body.querySelector('#totp-recovery-done').addEventListener('click', () => {
      close()
      if (typeof onDone === 'function') onDone()
    })
    // Closing the dialog at this point must not lose the codes silently.
    modal.querySelector('#totp-modal-close').addEventListener('click', () => {
      if (typeof onDone === 'function') onDone()
    })
  }

  renderStep1()
}

async function showTOTPDisableModal({ onDone }) {
  const modal = document.getElementById('change-password-modal')
  if (!modal) return
  const close = () => {
    modal.classList.add('hidden')
    modal.innerHTML = ''
  }
  modal.classList.remove('hidden')
  modal.innerHTML = modalShell(t('account.totpDisableTitle'), '')
  modal.querySelector('#totp-modal-close').addEventListener('click', close)
  const body = modal.querySelector('#totp-modal-body')
  body.innerHTML = `
    <form id="totp-disable-form">
      <p class="text-sm text-slate-700">${t('account.totpDisableIntro')}</p>
      <label for="totp-disable-password" class="block text-xs font-medium text-slate-600 mt-4 mb-1.5">${t('login.currentPassword')}</label>
      <input type="password" id="totp-disable-password" autocomplete="current-password" required class="${INPUT}" />
      <p id="totp-disable-error" class="mt-2 text-sm text-red-600 hidden"></p>
      <div class="mt-5 flex justify-end gap-3">
        <button type="button" id="totp-disable-cancel" class="${BTN_SECONDARY}">${t('account.cancel')}</button>
        <button type="submit" id="totp-disable-submit" class="rounded bg-red-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-red-700 shadow-sm transition-colors disabled:opacity-50">${t('account.totpDisableBtn')}</button>
      </div>
    </form>`
  body.querySelector('#totp-disable-cancel').addEventListener('click', close)
  body.querySelector('#totp-disable-form').addEventListener('submit', async (e) => {
    e.preventDefault()
    const errorEl = body.querySelector('#totp-disable-error')
    const submit = body.querySelector('#totp-disable-submit')
    errorEl.classList.add('hidden')
    submit.disabled = true
    try {
      await API.totpDisable(body.querySelector('#totp-disable-password').value)
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

/* ------------------------------------------------------------------ */
/* SSH public keys (CLI gateway)                                       */
/* ------------------------------------------------------------------ */

function describeKeyLine(line) {
  const parts = String(line || '').trim().split(/\s+/)
  const type = parts[0] || ''
  const blob = parts[1] || ''
  const comment = parts.slice(2).join(' ')
  const short = blob.length > 24 ? `${blob.slice(0, 12)}…${blob.slice(-8)}` : blob
  return { type, short, comment }
}

async function renderSSHKeysCard(section, { meData }) {
  if (!section) return
  const host = window.location.hostname || '<host>'
  let keys = []
  let loadError = ''
  try {
    keys = (await API.mySSHKeys()) || []
  } catch (err) {
    loadError = err.message || t('account.keysLoadFailed')
  }
  const rows = keys
    .map((k) => {
      const d = describeKeyLine(k.key_line)
      return `<li class="flex items-center justify-between gap-3 py-2">
        <div class="min-w-0">
          <div class="text-sm text-slate-800 font-mono truncate">${esc(d.type)} ${esc(d.short)}</div>
          <div class="text-xs text-slate-500">${d.comment ? esc(d.comment) + ' · ' : ''}${esc(formatDateTime(k.created_at))}</div>
        </div>
        <button type="button" class="key-delete ${BTN_DANGER} text-xs py-1" data-key-id="${esc(k.id)}">${t('common.delete')}</button>
      </li>`
    })
    .join('')

  section.innerHTML = `
    <div class="px-5 pt-4 pb-3 border-b border-slate-100">
      <h3 class="text-sm font-semibold text-slate-800">${t('account.keysTitle')}</h3>
      <p class="mt-0.5 text-xs text-slate-500">${t('account.keysHint')}</p>
      <code class="mt-1.5 inline-block rounded bg-slate-50 border border-slate-200 px-2 py-1 font-mono text-xs text-slate-800">ssh -p 2222 ${esc(meData.username)}@${esc(host)}</code>
    </div>
    <div class="px-5 py-4">
      ${loadError ? `<p class="text-sm text-red-600">${esc(loadError)}</p>` : ''}
      ${
        rows
          ? `<ul class="divide-y divide-slate-100">${rows}</ul>`
          : `<p class="text-sm text-slate-500">${t('account.keysNone')}</p>`
      }
      <form id="key-add-form" class="mt-4">
        <label for="key-add-input" class="block text-xs font-medium text-slate-600 mb-1.5">${t('account.keyAdd')}</label>
        <textarea id="key-add-input" rows="2" class="${INPUT} font-mono text-xs" placeholder="ssh-ed25519 AAAAC3... user@host"></textarea>
        <p id="key-add-error" class="mt-1 text-sm text-red-600 hidden"></p>
        <button type="submit" id="key-add-btn" class="${BTN_SECONDARY} mt-2">${t('account.keyAddBtn')}</button>
      </form>
    </div>`

  const reload = () => renderSSHKeysCard(section, { meData })
  section.querySelectorAll('.key-delete').forEach((btn) => {
    btn.addEventListener('click', async () => {
      if (!(await uiConfirm(t('account.keyConfirmDelete'), { danger: true }))) return
      btn.disabled = true
      try {
        await API.deleteMySSHKey(btn.dataset.keyId)
        await reload()
      } catch (err) {
        btn.disabled = false
        await uiAlert(err.message || t('account.keyDeleteFailed'))
      }
    })
  })
  section.querySelector('#key-add-form').addEventListener('submit', async (e) => {
    e.preventDefault()
    const input = section.querySelector('#key-add-input')
    const errorEl = section.querySelector('#key-add-error')
    const btn = section.querySelector('#key-add-btn')
    const line = input.value.trim()
    errorEl.classList.add('hidden')
    if (!line || line.includes('\n')) {
      errorEl.textContent = t('account.keyEnterOneLine')
      errorEl.classList.remove('hidden')
      return
    }
    btn.disabled = true
    try {
      await API.addMySSHKey(line)
      await reload()
    } catch (err) {
      errorEl.textContent = err.message || t('account.keyAddFailed')
      errorEl.classList.remove('hidden')
      btn.disabled = false
    }
  })
}

/* ------------------------------------------------------------------ */
/* API tokens                                                          */
/* ------------------------------------------------------------------ */

async function renderTokensCard(section) {
  if (!section) return
  let tokens
  try {
    tokens = (await API.myTokens()) || []
  } catch (err) {
    section.innerHTML = `<div class="px-5 py-4 text-sm text-red-600">${esc(err.message || t('account.tokensLoadFailed'))}</div>`
    return
  }
  const active = tokens.filter((x) => x.active)
  const rows = active
    .map(
      (x) => `<li class="flex items-center justify-between gap-3 py-2">
        <div class="min-w-0">
          <div class="text-sm text-slate-800"><span class="font-medium">${esc(x.name)}</span> <code class="ml-1 rounded bg-slate-100 px-1 text-xs font-mono">${esc(x.prefix)}…</code> ${pill(x.scope === 'write' ? t('account.tokenScopeWrite') : t('account.tokenScopeRead'), x.scope === 'write' ? 'bg-amber-100 text-amber-800' : 'bg-slate-100 text-slate-700')}</div>
          <div class="text-xs text-slate-500">${t('account.tokenCreated', { date: esc(formatDateTime(x.created_at)) })}${x.expires_at ? ` · ${t('account.tokenExpires', { date: esc(formatDateTime(x.expires_at)) })}` : ` · ${t('account.tokenNoExpiry')}`}${x.last_used_at ? ` · ${t('account.tokenLastUsed', { date: esc(formatDateTime(x.last_used_at)) })}` : ` · ${t('account.tokenNeverUsed')}`}</div>
        </div>
        <button type="button" class="token-revoke ${BTN_DANGER} text-xs py-1" data-id="${esc(x.id)}">${t('account.tokenRevoke')}</button>
      </li>`,
    )
    .join('')
  section.innerHTML = `
    <div class="px-5 pt-4 pb-3 border-b border-slate-100">
      <h3 class="text-sm font-semibold text-slate-800">${t('account.tokensTitle')}</h3>
      <p class="mt-0.5 text-xs text-slate-500">${t('account.tokensHint')}</p>
    </div>
    <div class="px-5 py-4">
      <div id="token-reveal" class="hidden mb-4 rounded border border-emerald-200 bg-emerald-50 px-3 py-2">
        <p class="text-sm text-emerald-800 font-medium">${t('account.tokenRevealTitle')}</p>
        <p class="text-xs text-emerald-700">${t('account.tokenRevealHint')}</p>
        <div class="mt-2 flex items-center gap-2">
          <code id="token-reveal-value" class="flex-1 break-all rounded bg-white border border-emerald-200 px-2 py-1 font-mono text-xs text-slate-800"></code>
          <button type="button" id="token-copy" class="${BTN_SECONDARY} text-xs py-1">${t('common.copy')}</button>
        </div>
        <p class="mt-2 text-[11px] text-slate-600 font-mono">curl -H "Authorization: Bearer &lt;token&gt;" ${esc(window.location.origin)}/api/me</p>
      </div>
      ${rows ? `<ul class="divide-y divide-slate-100">${rows}</ul>` : `<p class="text-sm text-slate-500">${t('account.tokensNone')}</p>`}
      <form id="token-create" class="mt-4 grid grid-cols-1 sm:grid-cols-[1fr_9rem_8rem_auto] gap-2 items-end">
        <div><label for="token-name" class="block text-xs font-medium text-slate-600 mb-1">${t('account.tokenName')}</label><input id="token-name" required maxlength="100" class="${INPUT}" placeholder="${t('account.tokenNamePlaceholder')}" /></div>
        <div><label for="token-scope" class="block text-xs font-medium text-slate-600 mb-1">${t('account.tokenScope')}</label><select id="token-scope" class="${INPUT}"><option value="read">${t('account.tokenScopeRead')}</option><option value="write">${t('account.tokenScopeWrite')}</option></select></div>
        <div><label for="token-days" class="block text-xs font-medium text-slate-600 mb-1">${t('account.tokenExpiry')}</label><select id="token-days" class="${INPUT}"><option value="30">30 ${t('account.days')}</option><option value="90" selected>90 ${t('account.days')}</option><option value="365">365 ${t('account.days')}</option><option value="0">${t('account.tokenNoExpiry')}</option></select></div>
        <button type="submit" class="${BTN_PRIMARY}">${t('account.tokenCreate')}</button>
        <p id="token-error" class="sm:col-span-4 text-sm text-red-600 hidden"></p>
      </form>
    </div>`
  const reload = () => renderTokensCard(section)
  section.querySelector('#token-create').addEventListener('submit', async (e) => {
    e.preventDefault()
    const errEl = section.querySelector('#token-error')
    errEl.classList.add('hidden')
    try {
      const res = await API.createToken({
        name: section.querySelector('#token-name').value.trim(),
        scope: section.querySelector('#token-scope').value,
        expires_in_days: section.querySelector('#token-days').value,
      })
      await reload()
      const reveal = section.querySelector('#token-reveal')
      section.querySelector('#token-reveal-value').textContent = res.token
      reveal.classList.remove('hidden')
      section.querySelector('#token-copy').addEventListener('click', async (ev) => {
        try {
          await navigator.clipboard.writeText(res.token)
          ev.currentTarget.textContent = t('common.copied')
        } catch {
          /* clipboard unavailable */
        }
      })
    } catch (err) {
      errEl.textContent = err.message || t('common.errorOccurred')
      errEl.classList.remove('hidden')
    }
  })
  section.querySelectorAll('.token-revoke').forEach((btn) => {
    btn.addEventListener('click', async () => {
      if (!(await uiConfirm(t('account.tokenConfirmRevoke'), { danger: true }))) return
      btn.disabled = true
      try {
        await API.revokeToken(btn.dataset.id)
        await reload()
      } catch (err) {
        btn.disabled = false
        await uiAlert(err.message || t('common.errorOccurred'))
      }
    })
  })
}

/* ------------------------------------------------------------------ */
/* Passkeys (WebAuthn)                                                 */
/* ------------------------------------------------------------------ */

async function renderPasskeysCard(section) {
  if (!section) return
  let keys
  try {
    keys = (await API.passkeys()) || []
  } catch (err) {
    section.innerHTML = `<div class="px-5 py-4 text-sm text-red-600">${esc(err.message || t('account.passkeysLoadFailed'))}</div>`
    return
  }
  const supported = webauthnSupported()
  const rows = keys
    .map(
      (k) => `<li class="flex items-center justify-between gap-3 py-2">
        <div class="min-w-0">
          <div class="text-sm text-slate-800 font-medium">${esc(k.name)}${k.backed_up ? ` ${pill(t('account.passkeySynced'), 'bg-sky-100 text-sky-800')}` : ''}</div>
          <div class="text-xs text-slate-500">${t('account.tokenCreated', { date: esc(formatDateTime(k.created_at)) })}${k.last_used_at ? ` · ${t('account.tokenLastUsed', { date: esc(formatDateTime(k.last_used_at)) })}` : ` · ${t('account.tokenNeverUsed')}`}</div>
        </div>
        <button type="button" class="passkey-delete ${BTN_DANGER} text-xs py-1" data-id="${esc(k.id)}">${t('common.delete')}</button>
      </li>`,
    )
    .join('')
  section.innerHTML = `
    <div class="px-5 pt-4 pb-3 border-b border-slate-100">
      <h3 class="text-sm font-semibold text-slate-800">${t('account.passkeysTitle')}</h3>
      <p class="mt-0.5 text-xs text-slate-500">${t('account.passkeysHint')}</p>
    </div>
    <div class="px-5 py-4">
      ${keys.length ? pill(t('account.totpEnabled'), 'bg-emerald-100 text-emerald-800') : pill(t('account.totpDisabled'))}
      ${rows ? `<ul class="mt-3 divide-y divide-slate-100">${rows}</ul>` : `<p class="mt-3 text-sm text-slate-500">${t('account.passkeysNone')}</p>`}
      ${
        supported
          ? `<form id="passkey-add" class="mt-4 flex flex-wrap items-end gap-2">
              <div class="flex-1 min-w-[12rem]"><label for="passkey-name" class="block text-xs font-medium text-slate-600 mb-1">${t('account.passkeyName')}</label><input id="passkey-name" maxlength="100" class="${INPUT}" placeholder="${t('account.passkeyNamePlaceholder')}" /></div>
              <button type="submit" class="${BTN_PRIMARY}">${t('account.passkeyAdd')}</button>
              <p id="passkey-add-error" class="w-full text-sm text-red-600 hidden"></p>
            </form>`
          : `<p class="mt-3 text-xs text-amber-700">${t('account.passkeysUnsupported')}</p>`
      }
    </div>`
  const reload = () => renderPasskeysCard(section)
  section.querySelector('#passkey-add')?.addEventListener('submit', async (e) => {
    e.preventDefault()
    const errEl = section.querySelector('#passkey-add-error')
    errEl.classList.add('hidden')
    const name = section.querySelector('#passkey-name').value.trim() || t('account.passkeyDefaultName')
    try {
      const options = await API.passkeyRegisterBegin()
      const credential = await createPasskey(options)
      await API.passkeyRegisterFinish(name, credential)
      await reload()
    } catch (err) {
      errEl.textContent = err && err.name === 'NotAllowedError' ? t('login.passkeyCancelled') : err.message || t('common.errorOccurred')
      errEl.classList.remove('hidden')
    }
  })
  section.querySelectorAll('.passkey-delete').forEach((btn) => {
    btn.addEventListener('click', async () => {
      if (!(await uiConfirm(t('account.passkeyConfirmDelete'), { danger: true }))) return
      btn.disabled = true
      try {
        await API.passkeyDelete(btn.dataset.id)
        await reload()
      } catch (err) {
        btn.disabled = false
        await uiAlert(err.message || t('common.errorOccurred'))
      }
    })
  })
}
