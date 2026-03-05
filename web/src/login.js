import API from './api.js'
import { renderApp } from './app.js'

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
      await API.login(username, password)
      renderApp(container)
    } catch (err) {
      errorEl.textContent = err.message || 'ログインに失敗しました'
      errorEl.classList.remove('hidden')
    } finally {
      btn.disabled = false
    }
  })
}
