import API from './api.js'
import { t } from './i18n.js'
import { createRealtimeWatcher, POLL_MS, shouldRefreshSessionList } from './sharing_events.js'
import { uiAlert } from './ui_dialog.js'
import {
  buildSessionsTableHTML,
  bindSessionListActions,
  countIdleSessions,
} from './session_list_shared.js'
import {
  onFileTransfersChange,
  getFileTransferJobs,
  cancelBackgroundTransfer,
  refreshFileTransfers,
  initFileTransferManager,
} from './file_transfer_manager.js'

/** main#main-content のクラス（セッション一覧用・中央寄せしない） */
export const SESSIONS_MAIN_CLASS = 'flex-1 overflow-auto p-6 flex flex-col w-full min-h-0'

let transferListUnsub = null
let sessionListWatchStop = null

function isActiveTransfer(state) {
  return state === 'receiving' || state === 'running'
}

function transferStateLabel(state) {
  switch (state) {
    case 'receiving':
      return t('sessions.transferReceiving')
    case 'running':
      return t('sessions.transferRunning')
    case 'completed':
      return t('sessions.transferCompleted')
    case 'failed':
      return t('sessions.transferFailed')
    case 'cancelled':
      return t('sessions.transferCancelled')
    default:
      return state
  }
}

function transferPercent(job) {
  if (job.state === 'completed') return 100
  if (!job.total || job.total <= 0) return job.progress > 0 ? 50 : 0
  return Math.min(100, Math.round((job.progress / job.total) * 100))
}

function transfersToShow(jobs) {
  const sorted = [...jobs].sort((a, b) => {
    const ta = a.updated_at || a.created_at || ''
    const tb = b.updated_at || b.created_at || ''
    return tb.localeCompare(ta)
  })
  const active = sorted.filter((j) => isActiveTransfer(j.state))
  const recentDone = sorted
    .filter((j) => !isActiveTransfer(j.state))
    .slice(0, 10)
  const seen = new Set()
  const out = []
  for (const j of [...active, ...recentDone]) {
    if (seen.has(j.id)) continue
    seen.add(j.id)
    out.push(j)
  }
  return out
}

export async function renderSessionsPage({
  mainContent,
  escapeHtml,
  sessionEndModal,
  openTerminalTab,
  getRdpResolutionForTarget,
}) {
  if (sessionListWatchStop) {
    sessionListWatchStop()
    sessionListWatchStop = null
  }
  if (transferListUnsub) {
    transferListUnsub()
    transferListUnsub = null
  }
  initFileTransferManager()

  mainContent.className = SESSIONS_MAIN_CLASS
  mainContent.innerHTML = `
    <div class="w-full max-w-6xl mx-auto flex flex-col gap-4">
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <h2 class="text-lg font-semibold text-slate-800 leading-normal">${t('sessions.pageTitle')}</h2>
          <p class="text-sm text-slate-500 mt-1 leading-normal">${t('sessions.pageIntro')}</p>
        </div>
        <button type="button" id="sessions-refresh-btn" class="shrink-0 rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm">${t('sessions.refresh')}</button>
      </div>
      <div id="sessions-idle-banner" class="hidden rounded-md border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 leading-normal"></div>
      <section class="bg-white rounded-lg border border-slate-200 shadow-sm">
        <div class="px-4 py-3 border-b border-slate-200">
          <h3 class="text-sm font-semibold text-slate-800">${t('sessions.sectionTerminalRdp')}</h3>
        </div>
        <div id="sessions-table-wrap">
          <p class="text-slate-500 p-6 text-sm leading-normal">${t('common.loading')}</p>
        </div>
      </section>
      <section class="bg-white rounded-lg border border-slate-200 shadow-sm">
        <div class="px-4 py-3 border-b border-slate-200">
          <h3 class="text-sm font-semibold text-slate-800">${t('sessions.sectionFileTransfers')}</h3>
          <p class="text-xs text-slate-500 mt-0.5 leading-normal">${t('sessions.fileTransfersIntro')}</p>
        </div>
        <div id="file-transfers-table-wrap">
          <p class="text-slate-500 p-6 text-sm leading-normal">${t('common.loading')}</p>
        </div>
      </section>
    </div>
  `

  const tableWrap = mainContent.querySelector('#sessions-table-wrap')
  const transfersWrap = mainContent.querySelector('#file-transfers-table-wrap')
  const idleBanner = mainContent.querySelector('#sessions-idle-banner')
  const refreshBtn = mainContent.querySelector('#sessions-refresh-btn')

  const renderTransfers = () => {
    const jobs = transfersToShow(getFileTransferJobs())
    if (jobs.length === 0) {
      transfersWrap.innerHTML =
        `<p class="text-slate-500 p-8 text-center text-sm leading-normal">${t('sessions.noTransfers')}</p>`
      return
    }
    const rows = jobs
      .map((j) => {
        const pct = transferPercent(j)
        const dir = j.direction === 'upload' ? t('sessions.directionUpload') : t('sessions.directionDownload')
        const active = isActiveTransfer(j.state)
        const errCls = j.state === 'failed' ? 'text-red-600' : 'text-slate-700'
        return `
          <tr class="border-b border-slate-100 last:border-0">
            <td class="px-4 py-2.5 text-sm ${errCls} whitespace-nowrap">${escapeHtml(dir)}</td>
            <td class="px-4 py-2.5 text-sm ${errCls} min-w-0">
              <div class="truncate font-medium" title="${escapeHtml(j.file_name || '')}">${escapeHtml(j.file_name || '—')}</div>
              <div class="text-xs text-slate-500 truncate" title="${escapeHtml(j.target_name || j.target_id || '')}">${escapeHtml(j.target_name || j.target_id || '')}</div>
            </td>
            <td class="px-4 py-2.5 text-sm ${errCls} min-w-[8rem]">
              <div class="flex items-center gap-2">
                <div class="w-16 h-1.5 bg-slate-200 rounded-full overflow-hidden flex-shrink-0">
                  <div class="h-full bg-sky-500" style="width:${pct}%"></div>
                </div>
                <span class="text-xs whitespace-nowrap">${active && pct > 0 ? `${pct}%` : escapeHtml(transferStateLabel(j.state))}</span>
              </div>
              ${j.error ? `<div class="text-xs text-red-600 mt-0.5 truncate" title="${escapeHtml(j.error)}">${escapeHtml(j.error)}</div>` : ''}
            </td>
            <td class="px-4 py-2.5 text-sm whitespace-nowrap">
              ${active ? `<button type="button" class="cancel-file-transfer text-xs text-slate-600 hover:text-red-600" data-id="${escapeHtml(j.id)}">${t('sessions.cancelTransfer')}</button>` : ''}
            </td>
          </tr>
        `
      })
      .join('')
    transfersWrap.innerHTML = `
      <div class="overflow-x-auto">
        <table class="w-full text-left text-sm border-collapse">
          <thead class="bg-slate-50 border-b border-slate-200">
            <tr>
              <th class="px-4 py-2 text-left text-xs font-semibold text-slate-700 whitespace-nowrap">${t('sessions.headerDirection')}</th>
              <th class="px-4 py-2 text-left text-xs font-semibold text-slate-700">${t('sessions.headerFileTarget')}</th>
              <th class="px-4 py-2 text-left text-xs font-semibold text-slate-700">${t('sessions.headerProgress')}</th>
              <th class="px-4 py-2 text-left text-xs font-semibold text-slate-700 whitespace-nowrap">${t('common.actions')}</th>
            </tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>
    `
    transfersWrap.querySelectorAll('.cancel-file-transfer').forEach((btn) => {
      btn.addEventListener('click', async () => {
        const id = btn.dataset.id
        if (!id) return
        try {
          await cancelBackgroundTransfer(id)
          renderTransfers()
        } catch (err) {
          await uiAlert(err.message || t('sessions.cancelTransferFailed'))
        }
      })
    })
  }

  transferListUnsub = onFileTransfersChange(renderTransfers)

  const refresh = async () => {
    try {
      const [sessionsRes, rdpRes] = await Promise.all([API.terminalSessions(), API.rdpSessions()])
      const sessions = sessionsRes.items || []
      const rdpSessions = rdpRes.items || []
      const idleN = countIdleSessions(sessions, rdpSessions)
      if (idleBanner) {
        if (idleN > 0) {
          idleBanner.classList.remove('hidden')
          idleBanner.textContent = t('sessions.idleBanner', { count: idleN })
        } else {
          idleBanner.classList.add('hidden')
        }
      }
      if (sessions.length === 0 && rdpSessions.length === 0) {
        tableWrap.innerHTML =
          `<p class="text-slate-500 p-8 text-center text-sm leading-normal">${t('sessions.noTerminalRdp')}</p>`
      } else {
        const { body } = buildSessionsTableHTML(sessions, rdpSessions, escapeHtml)
        tableWrap.innerHTML = `
        <div class="overflow-x-auto">
          <table class="w-full text-left text-sm border-collapse">
            <thead class="bg-slate-50 border-b border-slate-200">
              <tr>
                <th class="px-4 py-3 text-left text-xs font-semibold text-slate-700 whitespace-nowrap">${t('common.name')}</th>
                <th class="px-4 py-3 text-left text-xs font-semibold text-slate-700 min-w-[8rem]">${t('common.description')}</th>
                <th class="px-4 py-3 text-left text-xs font-semibold text-slate-700 whitespace-nowrap" title="prod/network/router1">${t('common.target')}</th>
                <th class="sessions-col-shrink px-2 py-3 text-left text-xs font-semibold text-slate-700 whitespace-nowrap">${t('sessions.headerType')}</th>
                <th class="sessions-col-shrink px-2 py-3 text-left text-xs font-semibold text-slate-700 whitespace-nowrap">${t('sessions.headerLastActivity')}</th>
                <th class="sessions-col-actions px-2 py-3 text-left text-xs font-semibold text-slate-700 whitespace-nowrap">${t('common.actions')}</th>
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
      }
      await refreshFileTransfers()
      renderTransfers()
    } catch (err) {
      tableWrap.innerHTML = `<p class="text-red-600 p-6 text-sm leading-normal">${escapeHtml(err.message || t('sessions.loadFailed', { error: '' }).replace(/:\s*$/, ''))}</p>`
    }
  }

  refreshBtn?.addEventListener('click', () => refresh())
  sessionListWatchStop = createRealtimeWatcher({
    shouldRefresh: shouldRefreshSessionList,
    onRefresh: refresh,
    pollMs: POLL_MS.sessionList,
  })
  await refresh()
}
