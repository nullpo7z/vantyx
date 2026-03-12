import API from './api.js'

export function renderUserInfo({ mainContent, meData, escapeHtml, onChangePassword }) {
  if (!meData) return
  mainContent.innerHTML = `
      <h2 class="text-lg font-medium text-slate-800 mb-4">ユーザー情報</h2>
      <div class="bg-white rounded-lg border border-slate-200 shadow-sm overflow-hidden">
        <dl class="divide-y divide-slate-200">
          <div class="px-4 py-3 sm:grid sm:grid-cols-3 sm:gap-4">
            <dt class="text-sm font-medium text-slate-500">ユーザーID</dt>
            <dd class="mt-1 text-sm text-slate-800 sm:mt-0 sm:col-span-2">${escapeHtml(meData.user_id)}</dd>
          </div>
          <div class="px-4 py-3 sm:grid sm:grid-cols-3 sm:gap-4">
            <dt class="text-sm font-medium text-slate-500">ユーザー名</dt>
            <dd class="mt-1 text-sm text-slate-800 sm:mt-0 sm:col-span-2">${escapeHtml(meData.username)}</dd>
          </div>
        </dl>
        <div class="px-4 py-3 border-t border-slate-200">
          <button type="button" id="btn-change-password" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">パスワードを変更</button>
        </div>
      </div>
    `
  const btn = mainContent.querySelector('#btn-change-password')
  if (btn && typeof onChangePassword === 'function') {
    btn.addEventListener('click', (e) => {
      e.preventDefault()
      onChangePassword()
    })
  }
}

export function showChangePasswordModal() {
  const modal = document.getElementById('change-password-modal')
  if (!modal) return
  modal.classList.remove('hidden')
  modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">パスワードを変更</h3>
            <button id="change-password-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="change-password-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label for="change-password-current" class="block text-xs font-medium text-slate-600 mb-1.5">現在のパスワード</label>
                <input type="password" id="change-password-current" autocomplete="current-password" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="現在のパスワード" />
              </div>
              <div>
                <label for="change-password-new" class="block text-xs font-medium text-slate-600 mb-1.5">新しいパスワード</label>
                <input type="password" id="change-password-new" autocomplete="new-password" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="8文字以上・大文字・小文字・数字・記号" />
              </div>
              <div>
                <label for="change-password-confirm" class="block text-xs font-medium text-slate-600 mb-1.5">新しいパスワード（確認）</label>
                <input type="password" id="change-password-confirm" autocomplete="new-password" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="もう一度入力" />
              </div>
              <p id="change-password-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="change-password-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">キャンセル</button>
              <button type="submit" id="change-password-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">変更</button>
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
      errorEl.textContent = '新しいパスワードが一致しません'
      errorEl.classList.remove('hidden')
      return
    }
    submitBtn.disabled = true
    try {
      await API.changePassword({ current_password: current, new_password: newPass })
      close()
      // eslint-disable-next-line no-alert
      alert('パスワードを変更しました')
    } catch (err) {
      errorEl.textContent = err.message || '変更に失敗しました'
      errorEl.classList.remove('hidden')
    } finally {
      submitBtn.disabled = false
    }
  })
}

