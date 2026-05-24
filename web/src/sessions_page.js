import API from './api.js'
import {
  buildSessionsTableHTML,
  bindSessionListActions,
  countIdleSessions,
} from './session_list_shared.js'

/** main#main-content のクラス（セッション一覧用・中央寄せしない） */
export const SESSIONS_MAIN_CLASS = 'flex-1 overflow-auto p-6 flex flex-col w-full min-h-0'

export async function renderSessionsPage({
  mainContent,
  escapeHtml,
  sessionEndModal,
  openTerminalTab,
  getRdpResolutionForTarget,
  onSubscribeSSE,
}) {
  mainContent.className = SESSIONS_MAIN_CLASS
  mainContent.innerHTML = `
    <div class="w-full max-w-6xl mx-auto flex flex-col gap-4">
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <h2 class="text-lg font-semibold text-slate-800 leading-normal">継続中のセッション</h2>
          <p class="text-sm text-slate-500 mt-1 leading-normal">バックグラウンドで動作中のセッションです。再接続または終了できます。</p>
        </div>
        <button type="button" id="sessions-refresh-btn" class="shrink-0 rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm">更新</button>
      </div>
      <div id="sessions-idle-banner" class="hidden rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 leading-normal"></div>
      <section class="bg-white rounded-lg border border-slate-200 shadow-sm">
        <div id="sessions-table-wrap">
          <p class="text-slate-500 p-6 text-sm leading-normal">読み込み中…</p>
        </div>
      </section>
    </div>
  `

  const tableWrap = mainContent.querySelector('#sessions-table-wrap')
  const idleBanner = mainContent.querySelector('#sessions-idle-banner')
  const refreshBtn = mainContent.querySelector('#sessions-refresh-btn')

  const refresh = async () => {
    try {
      const [sessionsRes, rdpRes] = await Promise.all([API.terminalSessions(), API.rdpSessions()])
      const sessions = sessionsRes.items || []
      const rdpSessions = rdpRes.items || []
      const idleN = countIdleSessions(sessions, rdpSessions)
      if (idleBanner) {
        if (idleN > 0) {
          idleBanner.classList.remove('hidden')
          idleBanner.textContent = `${idleN} 件のセッションが長時間無活動です。必要に応じて再接続するか、終了してください。`
        } else {
          idleBanner.classList.add('hidden')
        }
      }
      if (sessions.length === 0 && rdpSessions.length === 0) {
        tableWrap.innerHTML = '<p class="text-slate-500 p-8 text-center text-sm leading-normal">継続中のセッションはありません。</p>'
        return
      }
      const { body } = buildSessionsTableHTML(sessions, rdpSessions, escapeHtml)
      tableWrap.innerHTML = `
        <div class="overflow-x-auto">
          <table class="w-full text-left text-sm border-collapse">
            <thead class="bg-slate-50 border-b border-slate-200">
              <tr>
                <th class="px-4 py-3 text-left text-xs font-semibold text-slate-700 whitespace-nowrap">名前</th>
                <th class="px-4 py-3 text-left text-xs font-semibold text-slate-700 min-w-[8rem]">説明</th>
                <th class="px-4 py-3 text-left text-xs font-semibold text-slate-700 whitespace-nowrap" title="階層パスとターゲット名（例: prod/network/router1）">ターゲット</th>
                <th class="sessions-col-shrink px-2 py-3 text-left text-xs font-semibold text-slate-700 whitespace-nowrap">種別</th>
                <th class="sessions-col-shrink px-2 py-3 text-left text-xs font-semibold text-slate-700 whitespace-nowrap">最終活動</th>
                <th class="sessions-col-actions px-2 py-3 text-left text-xs font-semibold text-slate-700 whitespace-nowrap">操作</th>
              </tr>
            </thead>
            <tbody>${body}</tbody>
          </table>
        </div>
      `
      bindSessionListActions(tableWrap, {
        openTerminalTab,
        getRdpResolutionForTarget,
        onEnded: refresh,
        escapeHtml,
        sessionEndModal,
      })
    } catch (err) {
      tableWrap.innerHTML = `<p class="text-red-600 p-6 text-sm leading-normal">${escapeHtml(err.message || 'セッション一覧の取得に失敗しました。')}</p>`
    }
  }

  refreshBtn?.addEventListener('click', () => refresh())
  if (typeof onSubscribeSSE === 'function') {
    onSubscribeSSE(refresh)
  }
  await refresh()
}
