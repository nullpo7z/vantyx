import API from './api.js'

function escapeHtml(s) {
  const div = document.createElement('div')
  div.textContent = String(s ?? '')
  return div.innerHTML
}

function fmtTime(t) {
  try {
    return new Date(t).toLocaleString()
  } catch {
    return String(t || '')
  }
}

function fieldsToText(fields) {
  if (!fields || typeof fields !== 'object') return ''
  const keys = Object.keys(fields).sort()
  return keys
    .filter((k) => k !== 'event')
    .map((k) => `${k}=${String(fields[k])}`)
    .join(' ')
}

export async function renderAuditPage({ mainContent, meData, setActiveNav }) {
  if (!meData || meData.role !== 'admin') {
    mainContent.innerHTML = `<p class="text-sm text-red-600">forbidden: admin only</p>`
    return
  }
  if (typeof setActiveNav === 'function') setActiveNav('audit')

  mainContent.innerHTML = `
    <div class="w-full max-w-5xl">
      <div class="flex items-center justify-between gap-3 flex-wrap mb-4">
        <h2 class="text-lg font-semibold text-slate-800">監査ログ</h2>
        <button id="audit-refresh" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">更新</button>
      </div>
      <div class="bg-white rounded-lg border border-slate-200 shadow-sm overflow-hidden">
        <div class="p-4 border-b border-slate-200 flex gap-3 flex-wrap items-end">
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">event</label>
            <input id="audit-filter-event" class="w-64 rounded border border-slate-300 px-3 py-2 text-sm" placeholder="例: login_" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">user_id</label>
            <input id="audit-filter-user" class="w-64 rounded border border-slate-300 px-3 py-2 text-sm" placeholder="例: admin" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">limit</label>
            <input id="audit-filter-limit" type="number" min="1" max="1000" value="200" class="w-28 rounded border border-slate-300 px-3 py-2 text-sm" />
          </div>
          <button id="audit-apply" class="rounded bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">適用</button>
        </div>
        <div id="audit-error" class="px-4 py-3 text-sm text-red-600 hidden"></div>
        <div class="overflow-x-auto">
          <table class="min-w-full text-left text-sm">
            <thead class="bg-slate-50 border-b border-slate-200">
              <tr>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">時刻</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">event</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">fields</th>
              </tr>
            </thead>
            <tbody id="audit-rows">
              <tr><td colspan="3" class="px-4 py-6 text-center text-slate-500">読み込み中…</td></tr>
            </tbody>
          </table>
        </div>
      </div>
      <p class="text-xs text-slate-500 mt-3">注意: 監査ログはメモリ上の直近分のみ表示します（永続保存ではありません）。</p>
    </div>
  `

  const errEl = mainContent.querySelector('#audit-error')
  const rowsEl = mainContent.querySelector('#audit-rows')
  const eventEl = mainContent.querySelector('#audit-filter-event')
  const userEl = mainContent.querySelector('#audit-filter-user')
  const limitEl = mainContent.querySelector('#audit-filter-limit')

  async function load() {
    errEl.classList.add('hidden')
    rowsEl.innerHTML = `<tr><td colspan="3" class="px-4 py-6 text-center text-slate-500">読み込み中…</td></tr>`
    const limit = Number(limitEl.value || 200) || 200
    const event = String(eventEl.value || '').trim()
    const user_id = String(userEl.value || '').trim()
    try {
      const res = await API.auditLogs({ limit, event, user_id })
      const items = (res && res.items) || []
      if (!items.length) {
        rowsEl.innerHTML = `<tr><td colspan="3" class="px-4 py-6 text-center text-slate-500">ログがありません</td></tr>`
        return
      }
      rowsEl.innerHTML = items
        .map((it) => {
          const time = fmtTime(it.time)
          const ev = it.event || (it.fields && it.fields.event) || ''
          const fieldsText = fieldsToText(it.fields)
          return `<tr class="border-b border-slate-200 hover:bg-slate-50">
            <td class="px-4 py-2 text-xs text-slate-600 whitespace-nowrap">${escapeHtml(time)}</td>
            <td class="px-4 py-2 text-xs font-medium text-slate-900 whitespace-nowrap">${escapeHtml(ev)}</td>
            <td class="px-4 py-2 text-[11px] text-slate-700 font-mono">${escapeHtml(fieldsText)}</td>
          </tr>`
        })
        .join('')
    } catch (e) {
      errEl.textContent = e.message || '取得に失敗しました'
      errEl.classList.remove('hidden')
      rowsEl.innerHTML = `<tr><td colspan="3" class="px-4 py-6 text-center text-slate-500">取得に失敗しました</td></tr>`
    }
  }

  mainContent.querySelector('#audit-refresh').addEventListener('click', load)
  mainContent.querySelector('#audit-apply').addEventListener('click', load)

  await load()
}

