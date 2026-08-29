import API from './api.js'
import { t } from './i18n.js'
import { createRealtimeWatcher, POLL_MS, shouldRefreshSessionList } from './sharing_events.js'
import { uiAlert, uiConfirm } from './ui_dialog.js'
import {
  buildSessionsTableHTML,
  bindSessionListActions,
  countIdleSessions,
  formatLastSeen,
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
  isAdmin = false,
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
      ${
        isAdmin
          ? `<section class="bg-white rounded-lg border border-slate-200 shadow-sm">
        <div class="px-4 py-3 border-b border-slate-200">
          <h3 class="text-sm font-semibold text-slate-800">${t('sessions.adminSectionTitle')}</h3>
          <p class="text-xs text-slate-500 mt-0.5 leading-normal">${t('sessions.adminSectionIntro')}</p>
        </div>
        <div id="admin-sessions-table-wrap">
          <p class="text-slate-500 p-6 text-sm leading-normal">${t('common.loading')}</p>
        </div>
      </section>`
          : ''
      }
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
  const adminWrap = mainContent.querySelector('#admin-sessions-table-wrap')
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
      if (adminWrap) await renderAdminSessions()
    } catch (err) {
      tableWrap.innerHTML = `<p class="text-red-600 p-6 text-sm leading-normal">${escapeHtml(err.message || t('sessions.loadFailed', { error: '' }).replace(/:\s*$/, ''))}</p>`
    }
  }

  // Admin oversight: every live session with watch / terminate actions.
  async function renderAdminSessions() {
    let items
    try {
      items = ((await API.adminSessions()) || {}).items || []
    } catch (err) {
      adminWrap.innerHTML = `<p class="text-red-600 p-6 text-sm leading-normal">${escapeHtml(err.message || '')}</p>`
      return
    }
    if (items.length === 0) {
      adminWrap.innerHTML = `<p class="text-slate-500 p-8 text-center text-sm leading-normal">${t('sessions.adminNone')}</p>`
      return
    }
    const kindLabel = (k) => ({ terminal: t('sessions.kindTerminal'), vnc: 'VNC', rdp: 'RDP' })[k] || k
    const rows = items
      .map(
        (s) => `<tr class="border-b border-slate-100 last:border-0${s.idle ? ' bg-amber-50/40' : ''}">
          <td class="px-4 py-2.5 text-sm text-slate-900 font-medium whitespace-nowrap">${escapeHtml(s.owner_username || s.owner_user_id)}</td>
          <td class="px-4 py-2.5 text-sm text-slate-700 font-mono break-all">${escapeHtml(s.target_path ? `${s.target_path}/${s.target_name}` : s.target_name || s.target_id)}${s.name ? `<div class="text-xs text-slate-500 font-sans">${escapeHtml(s.name)}</div>` : ''}</td>
          <td class="px-2 py-2.5 text-sm text-slate-800 whitespace-nowrap">${escapeHtml(kindLabel(s.kind))}${s.protocol && s.kind === 'terminal' ? ` <span class="text-xs text-slate-500">${escapeHtml(String(s.protocol).toUpperCase())}</span>` : ''}</td>
          <td class="px-2 py-2.5 text-xs text-slate-600 whitespace-nowrap">${escapeHtml(formatLastSeen(s.created_at))}</td>
          <td class="px-2 py-2.5 text-xs text-slate-600 whitespace-nowrap">${escapeHtml(formatLastSeen(s.last_seen || s.created_at))}${s.idle ? ` <span class="text-amber-700">(${t('sessions.idleShort')})</span>` : ''}</td>
          <td class="px-2 py-2.5 text-xs text-slate-600">${s.participants && s.participants.length ? escapeHtml(s.participants.join(', ')) : '<span class="text-slate-400">—</span>'}</td>
          <td class="px-2 py-2.5 whitespace-nowrap">
            <div class="flex items-center gap-1.5">
              <button type="button" class="admin-watch rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-700 hover:bg-slate-50" data-kind="${escapeHtml(s.kind)}" data-id="${escapeHtml(s.session_id)}">${s.watching ? t('sessions.adminWatching') : t('sessions.adminWatch')}</button>
              <button type="button" class="admin-terminate rounded border border-red-200 bg-white px-2 py-1 text-xs font-medium text-red-700 hover:bg-red-50" data-kind="${escapeHtml(s.kind)}" data-id="${escapeHtml(s.session_id)}" data-owner="${escapeHtml(s.owner_username || s.owner_user_id)}">${t('sessions.adminTerminate')}</button>
            </div>
          </td>
        </tr>`,
      )
      .join('')
    adminWrap.innerHTML = `
      <div class="overflow-x-auto">
        <table class="w-full text-left text-sm border-collapse">
          <thead class="bg-slate-50 border-b border-slate-200">
            <tr>
              <th class="px-4 py-2 text-xs font-semibold text-slate-700 whitespace-nowrap">${t('sessions.headerOwner')}</th>
              <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('common.target')}</th>
              <th class="px-2 py-2 text-xs font-semibold text-slate-700 whitespace-nowrap">${t('sessions.headerType')}</th>
              <th class="px-2 py-2 text-xs font-semibold text-slate-700 whitespace-nowrap">${t('sessions.headerStarted')}</th>
              <th class="px-2 py-2 text-xs font-semibold text-slate-700 whitespace-nowrap">${t('sessions.headerLastActivity')}</th>
              <th class="px-2 py-2 text-xs font-semibold text-slate-700 whitespace-nowrap">${t('sessions.headerParticipants')}</th>
              <th class="px-2 py-2 text-xs font-semibold text-slate-700 whitespace-nowrap">${t('common.actions')}</th>
            </tr>
          </thead>
          <tbody>${rows}</tbody>
        </table>
      </div>`
    adminWrap.querySelectorAll('.admin-watch').forEach((btn) => {
      btn.addEventListener('click', async () => {
        btn.disabled = true
        try {
          const res = await API.adminWatchSession(btn.dataset.kind, btn.dataset.id)
          if (res && res.url) openTerminalTab(res.url)
          await renderAdminSessions()
        } catch (err) {
          btn.disabled = false
          await uiAlert(err.message || t('common.errorOccurred'))
        }
      })
    })
    adminWrap.querySelectorAll('.admin-terminate').forEach((btn) => {
      btn.addEventListener('click', async () => {
        const owner = btn.dataset.owner || ''
        if (!(await uiConfirm(t('sessions.adminConfirmTerminate', { owner }), { danger: true }))) return
        btn.disabled = true
        try {
          await API.adminTerminateSession(btn.dataset.kind, btn.dataset.id, t('sessions.adminTerminateReasonDefault'))
          await refresh()
        } catch (err) {
          btn.disabled = false
          await uiAlert(err.message || t('common.errorOccurred'))
        }
      })
    })
  }

  refreshBtn?.addEventListener('click', () => refresh())
  sessionListWatchStop = createRealtimeWatcher({
    shouldRefresh: shouldRefreshSessionList,
    onRefresh: refresh,
    pollMs: POLL_MS.sessionList,
  })
  await refresh()
}
