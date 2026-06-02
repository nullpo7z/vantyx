import API from './api.js'
import { t } from './i18n.js'
import { safeUrl } from './dom_helpers.js'
import { targetFullPathForDisplay } from './session_list_shared.js'
import { uiAlert, uiConfirm } from './ui_dialog.js'

const POLL_MS = 2000

function exportStateLabel(state) {
  const s = String(state || '')
  if (s === 'queued') return t('recordingExports.stateQueued')
  if (s === 'running') return t('recordingExports.stateRunning')
  if (s === 'completed') return t('recordingExports.stateCompleted')
  if (s === 'failed') return t('recordingExports.stateFailed')
  if (s === 'cancelled') return t('recordingExports.stateCancelled')
  return s || '—'
}

function exportProgressStageLabel(stage, state) {
  const s = String(stage || '').trim()
  if (s === 'queued') return t('recordingExports.stageQueued')
  if (s === 'waiting') return t('recordingExports.stageWaiting')
  if (s === 'starting') return t('recordingExports.stageStarting')
  if (s === 'agg') return t('recordingExports.stageAgg')
  if (s === 'ffmpeg') return t('recordingExports.stageFfmpeg')
  if (s === 'finalize') return t('recordingExports.stageFinalize')
  if (s === 'cancelled' || String(state || '') === 'cancelled') return t('recordingExports.stageCancelled')
  if (s === 'completed') return t('recordingExports.stageCompleted')
  return ''
}

function exportStateClass(state) {
  const s = String(state || '')
  if (s === 'completed') return 'text-emerald-700'
  if (s === 'failed') return 'text-red-600'
  if (s === 'cancelled') return 'text-amber-700'
  if (s === 'running') return 'text-sky-700'
  return 'text-slate-600'
}

function formatDisplayTime(iso) {
  const v = String(iso || '').trim()
  if (!v) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  return d.toLocaleString()
}

function abbreviateRecordingId(id) {
  const s = String(id || '').trim()
  if (s.length <= 20) return s
  return `${s.slice(0, 10)}…${s.slice(-8)}`
}

function exportJobLabel(job) {
  const format = String(job.format || '').toUpperCase()
  const fullPath = targetFullPathForDisplay({
    target_name: job.target_name,
    target_id: job.target_id,
    target_path: job.target_path,
  })
  const sessionName = String(job.session_name || '').trim()
  const startedAt = String(job.recording_started_at || '').trim()
  if (fullPath && fullPath !== '—') return `${fullPath} (${format})`
  if (sessionName) return `${sessionName} (${format})`
  if (startedAt) return `${startedAt} (${format})`
  const recordingId = String(job.recording_id || '').trim()
  if (recordingId) return `${abbreviateRecordingId(recordingId)} (${format})`
  return format || '—'
}

function renderSessionCell(job, escapeHtml) {
  const fullPath = targetFullPathForDisplay({
    target_name: job.target_name,
    target_id: job.target_id,
    target_path: job.target_path,
  })
  const sessionName = String(job.session_name || '').trim()
  const description = String(job.session_description || '').trim()
  const startedAt = String(job.recording_started_at || '').trim()
  const channelType = String(job.channel_type || '').trim()
  const metaParts = []
  if (sessionName) metaParts.push(sessionName)
  if (description) metaParts.push(description)
  if (startedAt) metaParts.push(startedAt)
  if (channelType) metaParts.push(channelType)
  const title = [fullPath, ...metaParts].filter((p) => p && p !== '—').join(' · ')
  const metaHtml =
    metaParts.length > 0
      ? `<div class="text-xs text-slate-500 mt-0.5 truncate" title="${escapeHtml(metaParts.join(' · '))}">${escapeHtml(metaParts.join(' · '))}</div>`
      : ''
  return `<div class="min-w-0" title="${escapeHtml(title)}">
    <div class="text-sm font-medium text-slate-900 font-mono break-all">${escapeHtml(fullPath)}</div>
    ${metaHtml}
  </div>`
}

function jobsSnapshot(items) {
  return JSON.stringify(
    (items || []).map((j) => ({
      id: j.job_id,
      state: j.state,
      updated_at: j.updated_at,
      error: j.error,
      progress: j.progress,
      progress_stage: j.progress_stage,
    })),
  )
}

function renderProgressCell(state, progress, progressStage, escapeHtml) {
  const showProgress = state === 'queued' || state === 'running'
  const pct = Math.max(0, Math.min(100, Number(progress) || 0))
  const stageLabel = exportProgressStageLabel(progressStage, state)
  const progressHint =
    showProgress && stageLabel ? `${stageLabel} · ${pct}%` : showProgress ? `${pct}%` : ''
  const progressBar = showProgress
    ? `<div class="mt-1.5 w-full max-w-[140px] bg-slate-200 rounded-full h-1.5 overflow-hidden" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow="${pct}">
        <div class="bg-sky-600 h-1.5 rounded-full transition-[width] duration-300" style="width:${pct}%"></div>
      </div>`
    : ''
  const stageHtml =
    progressHint !== ''
      ? `<div class="text-xs text-slate-500 mt-0.5 whitespace-nowrap">${escapeHtml(progressHint)}</div>`
      : ''
  return `<div class="min-w-0">
    <div class="whitespace-nowrap ${exportStateClass(state)}">${escapeHtml(exportStateLabel(state))}</div>
    ${progressBar}${stageHtml}
  </div>`
}

function renderExportRows(items, escapeHtml) {
  if (!items.length) {
    return `<tr><td colspan="6" class="px-4 py-8 text-center text-slate-500">${escapeHtml(t('recordingExports.empty'))}</td></tr>`
  }
  return items
    .map((job) => {
      const state = String(job.state || '')
      const format = String(job.format || '').toUpperCase()
      const fileUrl = job.file_url || ''
      const jobId = String(job.job_id || '')
      const actionLabel = exportJobLabel(job)
      const canCancel = state === 'queued' || state === 'running'
      const canDelete = !canCancel
      const downloadBtn =
        state === 'completed' && fileUrl
          ? `<a href="${safeUrl(fileUrl)}" download class="inline-flex shrink-0 whitespace-nowrap rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50">${escapeHtml(t('recordingExports.download'))}</a>`
          : `<span class="whitespace-nowrap text-xs text-slate-400">${escapeHtml(t('recordingExports.notReady'))}</span>`
      const cancelBtn = canCancel
        ? `<button type="button" class="export-cancel-btn inline-flex shrink-0 whitespace-nowrap rounded border border-amber-200 bg-white px-2 py-1 text-xs font-medium text-amber-700 hover:bg-amber-50" data-export-id="${escapeHtml(jobId)}" data-action-label="${escapeHtml(actionLabel)}">${escapeHtml(t('recordingExports.cancel'))}</button>`
        : ''
      const deleteBtn = canDelete
        ? `<button type="button" class="export-delete-btn inline-flex shrink-0 whitespace-nowrap rounded border border-red-200 bg-white px-2 py-1 text-xs font-medium text-red-600 hover:bg-red-50" data-export-id="${escapeHtml(jobId)}" data-action-label="${escapeHtml(actionLabel)}">${escapeHtml(t('recordingExports.delete'))}</button>`
        : ''
      const errHtml =
        state === 'failed' && job.error
          ? `<div class="text-xs text-red-600 mt-1 break-words">${escapeHtml(job.error)}</div>`
          : ''
      const progress = job.progress
      const progressStage = job.progress_stage
      return `
        <tr class="border-b border-slate-200 hover:bg-slate-50 align-middle" data-export-id="${escapeHtml(jobId)}">
          <td class="px-4 py-2 max-w-0">${renderSessionCell(job, escapeHtml)}</td>
          <td class="px-4 py-2 text-sm text-slate-700 whitespace-nowrap">${escapeHtml(format)}</td>
          <td class="px-4 py-2 text-sm">${renderProgressCell(state, progress, progressStage, escapeHtml)}</td>
          <td class="px-4 py-2 text-sm text-slate-600 whitespace-nowrap">${escapeHtml(formatDisplayTime(job.created_at))}</td>
          <td class="px-4 py-2 text-sm text-slate-600 whitespace-nowrap">${escapeHtml(formatDisplayTime(job.updated_at))}</td>
          <td class="px-4 py-2">
            <div class="inline-flex flex-nowrap items-center gap-2">${downloadBtn}${cancelBtn}${deleteBtn}</div>
            ${errHtml}
          </td>
        </tr>
      `
    })
    .join('')
}

function buildExportsShell(mainContent, escapeHtml, onBackToRecordings) {
  mainContent.innerHTML = `
    <div class="w-full max-w-5xl mx-auto flex flex-col gap-4" id="exports-page-root">
      <div class="flex items-center justify-between flex-wrap gap-3">
        <div>
          <h2 class="text-lg font-semibold text-slate-800">${escapeHtml(t('recordingExports.title'))}</h2>
          <p class="text-sm text-slate-600 mt-1">${escapeHtml(t('recordingExports.subtitle'))}</p>
        </div>
        ${
          typeof onBackToRecordings === 'function'
            ? `<button type="button" id="exports-back-recordings" class="text-sm text-sky-600 hover:text-sky-800 hover:underline">${escapeHtml(t('recordingExports.backToRecordings'))}</button>`
            : ''
        }
      </div>
      <div class="bg-white rounded-lg shadow-sm border border-slate-200 overflow-hidden">
        <div class="overflow-x-auto">
          <table class="min-w-[720px] w-full table-fixed text-left text-sm">
            <colgroup>
              <col class="w-[30%]" />
              <col class="w-[8%]" />
              <col class="w-[14%]" />
              <col class="w-[16%]" />
              <col class="w-[16%]" />
              <col class="w-[16%]" />
            </colgroup>
            <thead class="bg-slate-50 border-b border-slate-200">
              <tr>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${escapeHtml(t('recordingExports.headerSession'))}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700 whitespace-nowrap">${escapeHtml(t('recordingExports.headerFormat'))}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700 whitespace-nowrap">${escapeHtml(t('recordingExports.headerState'))}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700 whitespace-nowrap">${escapeHtml(t('recordingExports.headerQueued'))}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700 whitespace-nowrap">${escapeHtml(t('recordingExports.headerUpdated'))}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700 whitespace-nowrap">${escapeHtml(t('recordingExports.headerActions'))}</th>
              </tr>
            </thead>
            <tbody id="exports-table-body">
              <tr><td colspan="6" class="px-4 py-8 text-center text-slate-500">${escapeHtml(t('recordingExports.loading'))}</td></tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  `
}

/**
 * @param {object} opts
 * @param {HTMLElement} opts.mainContent
 * @param {(s: string) => string} opts.escapeHtml
 * @param {() => void} [opts.onBackToRecordings]
 */
export async function renderRecordingExportsPage({ mainContent, escapeHtml, onBackToRecordings }) {
  let pollTimer = 0
  let shellReady = false
  let lastSnapshot = ''
  let refreshInFlight = false

  const stopPoll = () => {
    if (pollTimer) {
      window.clearInterval(pollTimer)
      pollTimer = 0
    }
  }

  const syncPollTimer = (items) => {
    const hasPending = items.some((j) => {
      const s = String(j.state || '')
      return s === 'queued' || s === 'running'
    })
    if (hasPending && !pollTimer) {
      pollTimer = window.setInterval(() => {
        void refresh(false)
      }, POLL_MS)
    }
    if (!hasPending && pollTimer) {
      stopPoll()
    }
  }

  const wireShellEvents = () => {
    mainContent.querySelector('#exports-back-recordings')?.addEventListener('click', () => {
      stopPoll()
      onBackToRecordings?.()
    })
    mainContent.querySelector('#exports-table-body')?.addEventListener('click', async (e) => {
      const cancelBtn = e.target.closest('.export-cancel-btn')
      if (cancelBtn && !cancelBtn.disabled) {
        const exportId = cancelBtn.dataset.exportId || ''
        if (!exportId) return
        const label = cancelBtn.dataset.actionLabel || ''
        if (!(await uiConfirm(t('recordingExports.confirmCancel', { label })))) return
        cancelBtn.disabled = true
        try {
          await API.cancelRecordingExport(exportId)
          lastSnapshot = ''
          await refresh(false)
        } catch (err) {
          cancelBtn.disabled = false
          await uiAlert(err instanceof Error ? err.message : t('recordingExports.cancelFailed'))
        }
        return
      }
      const btn = e.target.closest('.export-delete-btn')
      if (!btn || btn.disabled) return
      const exportId = btn.dataset.exportId || ''
      if (!exportId) return
      const label = btn.dataset.actionLabel || ''
      if (!(await uiConfirm(t('recordingExports.confirmDelete', { label }), { danger: true }))) return
      btn.disabled = true
      try {
        await API.deleteRecordingExport(exportId)
        lastSnapshot = ''
        await refresh(false)
      } catch (err) {
        btn.disabled = false
        await uiAlert(err instanceof Error ? err.message : t('recordingExports.deleteFailed'))
      }
    })
  }

  async function refresh(initial) {
    if (refreshInFlight) return
    refreshInFlight = true
    try {
      if (initial && !shellReady) {
        buildExportsShell(mainContent, escapeHtml, onBackToRecordings)
        wireShellEvents()
        shellReady = true
      }
      const res = await API.recordingExports()
      const items = (res && res.items) || []
      const snap = jobsSnapshot(items)
      if (snap !== lastSnapshot) {
        const tbody = mainContent.querySelector('#exports-table-body')
        if (tbody) {
          tbody.innerHTML = renderExportRows(items, escapeHtml)
        }
        lastSnapshot = snap
      }
      syncPollTimer(items)
    } catch (e) {
      const msg = e instanceof Error ? e.message : t('recordingExports.fetchFailed')
      if (!shellReady) {
        mainContent.innerHTML = `<p class="text-sm text-red-600">${escapeHtml(msg)}</p>`
      } else {
        const tbody = mainContent.querySelector('#exports-table-body')
        if (tbody) {
          tbody.innerHTML = `<tr><td colspan="6" class="px-4 py-8 text-center text-red-600">${escapeHtml(msg)}</td></tr>`
        }
      }
    } finally {
      refreshInFlight = false
    }
  }

  await refresh(true)
  return stopPoll
}

/** Queue a recording export and return the job snapshot. */
export async function queueRecordingExport(recordingId, format) {
  const job = await API.startRecordingExport(recordingId, format)
  return job
}

/** Show confirmation after queueing, optionally navigating to exports page. */
export async function queueRecordingExportAndNotify(recordingId, format, onGoToExports) {
  try {
    await queueRecordingExport(recordingId, format)
    if (typeof onGoToExports === 'function') {
      onGoToExports()
    }
  } catch (err) {
    await uiAlert(err instanceof Error ? err.message : t('recordingExports.queueFailed'))
  }
}
