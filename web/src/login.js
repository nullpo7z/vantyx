/**
 * @file Login screen and the forced-password-change screen shown to
 * users who still have the default admin password.
 */

import API from './api.js'
import { renderApp } from './app.js'

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
          <h1 class="text-xl font-semibold text-slate-800 text-center mb-2">パスワードの変更</h1>
          <p class="text-sm text-slate-600 text-center mb-5">初期パスワードのままのため、パスワードを変更してください。（8文字以上・大文字・小文字・数字・記号を含む）</p>
          <form id="change-password-form" class="space-y-5">
            <div>
              <label for="current-password" class="block text-xs font-medium text-slate-600 mb-1.5">現在のパスワード</label>
              <input type="password" id="current-password" required
                class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white"
                autocomplete="current-password" />
            </div>
            <div>
              <label for="new-password" class="block text-xs font-medium text-slate-600 mb-1.5">新しいパスワード</label>
              <input type="password" id="new-password" required
                class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white"
                autocomplete="new-password" minlength="8" />
            </div>
            <div>
              <label for="new-password-confirm" class="block text-xs font-medium text-slate-600 mb-1.5">新しいパスワード（確認）</label>
              <input type="password" id="new-password-confirm" required
                class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white"
                autocomplete="new-password" minlength="8" />
            </div>
            <p id="change-pw-error" class="text-sm text-red-600 hidden"></p>
            <button type="submit" id="change-pw-btn"
              class="w-full rounded bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">
              変更する
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
      errorEl.textContent = '新しいパスワードが一致しません'
      errorEl.classList.remove('hidden')
      return
    }
    btn.disabled = true
    try {
      await API.changePassword(current, newPw)
      renderApp(container)
    } catch (err) {
      errorEl.textContent = err.message || 'パスワードの変更に失敗しました'
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
  container.innerHTML = `
    <div class="flex-1 flex items-center justify-center p-4">
      <div class="w-full max-w-sm bg-white rounded-lg shadow-sm border border-slate-200 overflow-hidden">
        <div class="px-5 py-6">
          <h1 class="text-2xl font-semibold text-slate-800 text-center mb-6">Vantyx</h1>
          <form id="login-form" class="space-y-5">
            <div>
              <label for="username" class="block text-xs font-medium text-slate-600 mb-1.5">ユーザー名</label>
              <input type="text" id="username" name="username" required
                class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400"
                autocomplete="username" placeholder="ユーザー名" />
            </div>
            <div>
              <label for="password" class="block text-xs font-medium text-slate-600 mb-1.5">パスワード</label>
              <input type="password" id="password" name="password" required
                class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400"
                autocomplete="current-password" placeholder="••••••••" />
            </div>
            <p id="login-error" class="text-sm text-red-600 hidden"></p>
            <button type="submit" id="login-btn"
              class="w-full rounded bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">
              ログイン
            </button>
          </form>
        </div>
      </div>
    </div>
  `

  const form = document.getElementById('login-form')
  const errorEl = document.getElementById('login-error')
  const btn = document.getElementById('login-btn')

  form.addEventListener('submit', async (e) => {
    e.preventDefault()
    errorEl.classList.add('hidden')
    const username = document.getElementById('username').value.trim()
    const password = document.getElementById('password').value
    if (!username) return

    btn.disabled = true
    try {
      const data = await API.login(username, password)
      if (data.require_password_change) {
        renderChangePassword(container)
      } else {
        renderApp(container)
      }
    } catch (err) {
      errorEl.textContent = err.message || 'ログインに失敗しました'
      errorEl.classList.remove('hidden')
    } finally {
      btn.disabled = false
    }
  })
}
