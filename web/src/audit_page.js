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

      <div class="mt-8 bg-white rounded-lg border border-slate-200 shadow-sm overflow-hidden">
        <div class="px-4 py-3 border-b border-slate-200 flex items-center justify-between gap-3 flex-wrap">
          <div>
            <h3 class="text-sm font-semibold text-slate-800">コマンドログ検索</h3>
            <p class="text-xs text-slate-500 mt-0.5">ターミナルで入力された行（stdin）を文字列で検索します。</p>
          </div>
          <button id="cmd-refresh" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">再検索</button>
        </div>
        <div class="p-4 border-b border-slate-200 flex gap-3 flex-wrap items-end">
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">query</label>
            <input id="cmd-filter-query" class="w-64 rounded border border-slate-300 px-3 py-2 text-sm" placeholder="例: sudo, rm -rf" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">user_id</label>
            <input id="cmd-filter-user" class="w-40 rounded border border-slate-300 px-3 py-2 text-sm" placeholder="例: admin" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">target_id</label>
            <input id="cmd-filter-target" class="w-40 rounded border border-slate-300 px-3 py-2 text-sm" placeholder="例: t1" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">limit</label>
            <input id="cmd-filter-limit" type="number" min="1" max="500" value="200" class="w-24 rounded border border-slate-300 px-3 py-2 text-sm" />
          </div>
          <button id="cmd-apply" class="rounded bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">適用</button>
        </div>
        <div id="cmd-error" class="px-4 py-3 text-sm text-red-600 hidden"></div>
        <div class="overflow-x-auto">
          <table class="min-w-full text-left text-sm">
            <thead class="bg-slate-50 border-b border-slate-200">
              <tr>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">時刻</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">ユーザー</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">ターゲット</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">入力</th>
              </tr>
            </thead>
            <tbody id="cmd-rows">
              <tr><td colspan="4" class="px-4 py-6 text-center text-slate-500">検索条件を入力して「適用」を押してください。</td></tr>
            </tbody>
          </table>
        </div>
      </div>

      <p class="text-xs text-slate-500 mt-3">注意: 監査ログはメモリ/DB 上の直近分のみ表示します。コマンドログは stdin に送信された行を best-effort で記録したものであり、完全なシェル履歴とは一致しない場合があります。</p>
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

  // Command logs search
  const cmdErrEl = mainContent.querySelector('#cmd-error')
  const cmdRowsEl = mainContent.querySelector('#cmd-rows')
  const cmdQueryEl = mainContent.querySelector('#cmd-filter-query')
  const cmdUserEl = mainContent.querySelector('#cmd-filter-user')
  const cmdTargetEl = mainContent.querySelector('#cmd-filter-target')
  const cmdLimitEl = mainContent.querySelector('#cmd-filter-limit')

  async function loadCmd() {
    cmdErrEl.classList.add('hidden')
    const q = String(cmdQueryEl.value || '').trim()
    const user_id = String(cmdUserEl.value || '').trim()
    const target_id = String(cmdTargetEl.value || '').trim()
    const limit = Number(cmdLimitEl.value || 200) || 200
    if (!q && !user_id && !target_id) {
      cmdRowsEl.innerHTML = `<tr><td colspan="4" class="px-4 py-6 text-center text-slate-500">検索条件を入力してください（query または user_id/target_id）。</td></tr>`
      return
    }
    cmdRowsEl.innerHTML = `<tr><td colspan="4" class="px-4 py-6 text-center text-slate-500">検索中…</td></tr>`
    try {
      const res = await API.commandLogs({ query: q, user_id, target_id, limit })
      const items = (res && res.items) || []
      if (!items.length) {
        cmdRowsEl.innerHTML = `<tr><td colspan="4" class="px-4 py-6 text-center text-slate-500">該当するコマンドはありません。</td></tr>`
        return
      }
      cmdRowsEl.innerHTML = items
        .map((it) => {
          const time = fmtTime(it.time)
          return `<tr class="border-b border-slate-200 hover:bg-slate-50">
            <td class="px-4 py-2 text-xs text-slate-600 whitespace-nowrap">${escapeHtml(time)}</td>
            <td class="px-4 py-2 text-xs text-slate-700 whitespace-nowrap">${escapeHtml(it.user_id || '')}</td>
            <td class="px-4 py-2 text-xs text-slate-700 whitespace-nowrap">${escapeHtml(it.target_id || '')}</td>
            <td class="px-4 py-2 text-xs text-slate-800 font-mono text-[11px]">${escapeHtml(it.line_text || '')}</td>
          </tr>`
        })
        .join('')
    } catch (e) {
      cmdErrEl.textContent = e.message || '取得に失敗しました'
      cmdErrEl.classList.remove('hidden')
      cmdRowsEl.innerHTML = `<tr><td colspan="4" class="px-4 py-6 text-center text-slate-500">取得に失敗しました</td></tr>`
    }
  }

  mainContent.querySelector('#cmd-refresh').addEventListener('click', loadCmd)
  mainContent.querySelector('#cmd-apply').addEventListener('click', loadCmd)
}

