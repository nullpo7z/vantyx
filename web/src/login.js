/**
 * @file Login screen and the forced-password-change screen shown to
 * users who still have the default admin password.
 */

import API from './api.js'
import { captureNextQueryParam, consumePostLoginRedirect } from './auth_redirect.js'
import { applyServerLocale, t } from './i18n.js'
import { applyServerTimezone } from './timezone.js'

/**
 * Render the "you must change your password" screen.
 *
 * @param {HTMLElement} container - SPA root element.
 */
export function renderChangePassword(container) {
  container.innerHTML = `
    <div class="flex-1 flex items-center justify-center p-4">
      <div class="w-full max-w-sm bg-white rounded-lg shadow-sm border border-slate-200 overflow-hidden">
        <div class="px-5 py-6">
          <h1 class="text-xl font-semibold text-slate-800 text-center mb-2">${t('login.changeTitle')}</h1>
          <p class="text-sm text-slate-600 text-center mb-5">${t('login.changeIntro')}</p>
          <form id="change-password-form" class="space-y-5">
            <div>
              <label for="current-password" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.currentPassword')}</label>
              <input type="password" id="current-password" required
                class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white"
                autocomplete="current-password" />
            </div>
            <div>
              <label for="new-password" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.newPassword')}</label>
              <input type="password" id="new-password" required
                class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white"
                autocomplete="new-password" minlength="8" />
            </div>
            <div>
              <label for="new-password-confirm" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.newPasswordConfirm')}</label>
              <input type="password" id="new-password-confirm" required
                class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white"
                autocomplete="new-password" minlength="8" />
            </div>
            <p id="change-pw-error" class="text-sm text-red-600 hidden"></p>
            <button type="submit" id="change-pw-btn"
              class="w-full rounded bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">
              ${t('login.changeSubmit')}
            </button>
          </form>
        </div>
      </div>
    </div>
  `

  const form = document.getElementById('change-password-form')
  const errorEl = document.getElementById('change-pw-error')
  const btn = document.getElementById('change-pw-btn')

  form.addEventListener('submit', async (e) => {
    e.preventDefault()
    errorEl.classList.add('hidden')
    const current = document.getElementById('current-password').value
    const newPw = document.getElementById('new-password').value
    const confirm = document.getElementById('new-password-confirm').value
    if (newPw !== confirm) {
      errorEl.textContent = t('login.mismatch')
      errorEl.classList.remove('hidden')
      return
    }
    btn.disabled = true
    try {
      await API.changePassword(current, newPw)
      if (consumePostLoginRedirect()) return
      const { renderApp } = await import('./app.js')
      renderApp(container)
    } catch (err) {
      errorEl.textContent = err.message || t('login.changeFailed')
      errorEl.classList.remove('hidden')
    } finally {
      btn.disabled = false
    }
  })
}

/**
 * Render the login form. On successful login this switches the SPA to
 * the main app shell (or the "change password" screen when the server
 * indicates the default password is still in use).
 *
 * @param {HTMLElement} container - SPA root element.
 */
export function renderLogin(container) {
  captureNextQueryParam()
  container.innerHTML = `
    <div class="flex-1 flex items-center justify-center p-4">
      <div class="w-full max-w-sm bg-white rounded-lg shadow-sm border border-slate-200 overflow-hidden">
        <div class="px-5 py-6">
          <h1 class="text-2xl font-semibold text-slate-800 text-center mb-6">${t('login.title')}</h1>
          <form id="login-form" class="space-y-5">
            <div>
              <label for="username" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.username')}</label>
              <input type="text" id="username" name="username" required
                class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400"
                autocomplete="username" placeholder="${t('login.usernamePlaceholder')}" />
            </div>
            <div>
              <label for="password" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.password')}</label>
              <input type="password" id="password" name="password" required
                class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400"
                autocomplete="current-password" placeholder="${t('login.passwordPlaceholder')}" />
            </div>
            <p id="login-error" class="text-sm text-red-600 hidden"></p>
            <button type="submit" id="login-btn"
              class="w-full rounded bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">
              ${t('login.submit')}
            </button>
          </form>
          <div id="login-sso" class="hidden mt-5 pt-5 border-t border-slate-200">
            <a id="login-sso-btn" href="/api/auth/oidc/login"
              class="block w-full text-center rounded border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors"></a>
          </div>
        </div>
      </div>
    </div>
  `

  const form = document.getElementById('login-form')
  const errorEl = document.getElementById('login-error')
  const btn = document.getElementById('login-btn')

  showOIDCError(errorEl)
  offerSSO()

  form.addEventListener('submit', async (e) => {
    e.preventDefault()
    errorEl.classList.add('hidden')
    const username = document.getElementById('username').value.trim()
    const password = document.getElementById('password').value
    if (!username) return

    btn.disabled = true
    try {
      const data = await API.login(username, password)
      if (data && data.mfa_required && data.mfa_token) {
        renderTOTPStep(container, data.mfa_token)
        return
      }
      await finishLogin(container, data)
    } catch (err) {
      errorEl.textContent = err.message || t('login.failed')
      errorEl.classList.remove('hidden')
    } finally {
      btn.disabled = false
    }
  })
}

/**
 * After the server has accepted the credentials (and, if applicable,
 * the second factor), apply the user's locale and move into the app or
 * the stored deep link.
 */
async function finishLogin(container, data) {
  if (data && typeof data.locale === 'string' && data.locale) {
    applyServerLocale(data.locale)
  }
  if (data && typeof data.timezone === 'string') {
    applyServerTimezone(data.timezone)
  }
  if (data && data.require_password_change) {
    renderChangePassword(container)
  } else if (consumePostLoginRedirect()) {
    /* navigating to invitation / deep link */
  } else {
    const { renderApp } = await import('./app.js')
    renderApp(container)
  }
}

/**
 * Second login step for accounts with TOTP enabled: the password was
 * already accepted and the server handed back a short-lived token that
 * must be paired with an authenticator or recovery code.
 */
function renderTOTPStep(container, mfaToken) {
  container.innerHTML = `
    <div class="flex-1 flex items-center justify-center p-4">
      <div class="w-full max-w-sm bg-white rounded-lg shadow-sm border border-slate-200 overflow-hidden">
        <div class="px-5 py-6">
          <h1 class="text-xl font-semibold text-slate-800 text-center mb-2">${t('login.totpTitle')}</h1>
          <p class="text-sm text-slate-600 text-center mb-5">${t('login.totpIntro')}</p>
          <form id="totp-form" class="space-y-5">
            <div>
              <label for="totp-code" class="block text-xs font-medium text-slate-600 mb-1.5">${t('login.totpCode')}</label>
              <input type="text" id="totp-code" name="code" required inputmode="numeric" autocomplete="one-time-code"
                class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 tracking-widest focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400"
                placeholder="${t('login.totpPlaceholder')}" />
              <p class="mt-1.5 text-xs text-slate-500">${t('login.totpRecoveryHint')}</p>
            </div>
            <p id="totp-error" class="text-sm text-red-600 hidden"></p>
            <button type="submit" id="totp-btn"
              class="w-full rounded bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">
              ${t('login.totpSubmit')}
            </button>
            <button type="button" id="totp-back"
              class="w-full rounded border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">
              ${t('login.totpBack')}
            </button>
          </form>
        </div>
      </div>
    </div>
  `
  const form = document.getElementById('totp-form')
  const errorEl = document.getElementById('totp-error')
  const btn = document.getElementById('totp-btn')
  const input = document.getElementById('totp-code')
  input.focus()

  document.getElementById('totp-back').addEventListener('click', () => renderLogin(container))

  form.addEventListener('submit', async (e) => {
    e.preventDefault()
    errorEl.classList.add('hidden')
    const code = input.value.trim()
    if (!code) return
    btn.disabled = true
    try {
      const data = await API.loginTotp(mfaToken, code)
      await finishLogin(container, data)
    } catch (err) {
      errorEl.textContent = err.message || t('login.totpFailed')
      errorEl.classList.remove('hidden')
      input.select()
    } finally {
      btn.disabled = false
    }
  })
}

/**
 * Show the SSO button when the server has an OIDC provider configured.
 * The button is a plain link: the whole flow is server-side redirects.
 */
async function offerSSO() {
  try {
    const methods = await API.authMethods()
    const oidc = methods && methods.oidc
    if (!oidc || !oidc.enabled) return
    const wrap = document.getElementById('login-sso')
    const link = document.getElementById('login-sso-btn')
    if (!wrap || !link) return
    link.textContent = t('login.ssoButton', { name: oidc.display_name || 'SSO' })
    // Carry the deep link the user was heading for through the IdP round trip.
    let next = ''
    try {
      next = sessionStorage.getItem('vantyx_post_login_redirect') || ''
    } catch {
      /* storage unavailable */
    }
    if (next) link.href = '/api/auth/oidc/login?next=' + encodeURIComponent(next)
    wrap.classList.remove('hidden')
  } catch {
    /* methods endpoint unavailable: keep the password form only */
  }
}

/**
 * The OIDC callback redirects back to `/?oidc_error=<reason>` when the
 * IdP round trip fails; surface it once and clean the URL.
 */
function showOIDCError(errorEl) {
  let reason
  try {
    reason = new URLSearchParams(window.location.search).get('oidc_error') || ''
  } catch {
    return
  }
  if (!reason) return
  const known = ['denied', 'state', 'provider_unavailable', 'exchange', 'token', 'not_provisioned', 'resolve', 'session']
  const key = known.includes(reason) ? `login.oidcError.${reason}` : 'login.oidcError.generic'
  errorEl.textContent = t(key)
  errorEl.classList.remove('hidden')
  try {
    const url = new URL(window.location.href)
    url.searchParams.delete('oidc_error')
    window.history.replaceState({}, '', url.pathname + url.search + url.hash)
  } catch {
    /* ignore */
  }
}
