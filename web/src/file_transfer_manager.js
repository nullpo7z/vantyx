import API from './api.js'
import { t } from './i18n.js'

const STORAGE_KEY = 'vantyx_file_transfer_ids'
const POLL_MS_FALLBACK = 2000
const SSE_RECONNECT_MS = 3000
const TERMINAL_DISPLAY_MS = 8000

let pollTimer = null
let sseConn = null
let sseRetryTimer = null
let sseConnected = false
const listeners = new Set()
/** @type {Map<string, object>} */
const jobs = new Map()

function loadWatchedIds() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    const arr = raw ? JSON.parse(raw) : []
    return Array.isArray(arr) ? arr.filter((x) => typeof x === 'string') : []
  } catch {
    return []
  }
}

function saveWatchedIds(ids) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify([...new Set(ids)]))
  } catch {
    /* ignore */
  }
}

function notifyListeners() {
  for (const fn of listeners) {
    try {
      fn()
    } catch {
      /* ignore */
    }
  }
}

function notify() {
  notifyListeners()
  renderGlobalBar()
}

function isActive(state) {
  return state === 'receiving' || state === 'running'
}

function stateLabel(state) {
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

function percentFor(job) {
  if (job.state === 'completed') return 100
  if (!job.total || job.total <= 0) return job.progress > 0 ? 50 : 0
  return Math.min(100, Math.round((job.progress / job.total) * 100))
}

let lastJobsSignature = ''
function computeJobsSignature() {
  const ids = [...jobs.keys()].sort()
  const parts = ids.map((id) => {
    const j = jobs.get(id)
    return `${id}:${j?.state || ''}:${j?.progress || 0}:${j?.total || 0}`
  })
  return parts.join('|')
}

function notifyIfChanged() {
  const sig = computeJobsSignature()
  const changed = sig !== lastJobsSignature
  lastJobsSignature = sig
  if (changed) {
    notifyListeners()
  }
  renderGlobalBar()
}

const cleanupTimers = new Map() // jobId -> timeoutId
const cleanedJobIds = new Set() // tombstones for jobs we already cleaned up locally

function hasLocalUploadGhostFor(jobId) {
  const j = jobs.get(jobId)
  return Boolean(j && j._local && j.direction === 'upload')
}

const localAborts = new Map() // tempId -> abort fn

function scheduleCleanup(id, delay = TERMINAL_DISPLAY_MS) {
  if (cleanupTimers.has(id)) return
  const ms = Math.max(0, delay)
  const tid = window.setTimeout(() => {
    cleanupTimers.delete(id)
    cleanedJobIds.add(id)
    const watched = new Set(loadWatchedIds())
    watched.delete(id)
    jobs.delete(id)
    saveWatchedIds([...watched])
    notifyIfChanged()
  }, ms)
  cleanupTimers.set(id, tid)
}

async function applySnapshot(item) {
  if (!item || !item.id) return
  // Already cleaned up locally; ignore subsequent server snapshots (which can
  // keep arriving on SSE reconnect or polling because the server retains
  // finished jobs).
  if (cleanedJobIds.has(item.id)) return
  // Browser-side upload progress is tracked locally via xhr.upload.onprogress
  // (see startBackgroundUpload). Drop server-side "receiving" events for the
  // same job so the bar shows the immediate browser progress rather than the
  // slightly-delayed server view.
  if (item.state === 'receiving' && item.direction === 'upload' && hasLocalUploadGhostFor(item.id)) {
    return
  }
  const prev = jobs.get(item.id)
  const wasCompleted = prev?.state === 'completed'
  const isTerminal =
    item.state === 'completed' || item.state === 'failed' || item.state === 'cancelled'
  let cleanupDelay = TERMINAL_DISPLAY_MS
  if (isTerminal) {
    const updatedAt = item.updated_at ? Date.parse(item.updated_at) : NaN
    if (Number.isFinite(updatedAt)) {
      const elapsed = Date.now() - updatedAt
      if (elapsed >= TERMINAL_DISPLAY_MS) {
        // Job finished long enough ago that we should not show it on (re)load.
        cleanedJobIds.add(item.id)
        return
      }
      cleanupDelay = TERMINAL_DISPLAY_MS - elapsed
    }
  }
  jobs.set(item.id, item)
  const watched = new Set(loadWatchedIds())
  if (isActive(item.state)) {
    watched.add(item.id)
    if (cleanupTimers.has(item.id)) {
      window.clearTimeout(cleanupTimers.get(item.id))
      cleanupTimers.delete(item.id)
    }
  } else {
    if (item.state === 'completed' && item.direction === 'download' && !wasCompleted) {
      await maybeDeliverDownload(item)
    }
    if (isTerminal) {
      scheduleCleanup(item.id, cleanupDelay)
    }
  }
  saveWatchedIds([...watched])
  notifyIfChanged()
}

async function pollOnce() {
  try {
    const res = await API.fileTransfers()
    const items = Array.isArray(res.items) ? res.items : []
    for (const item of items) {
      await applySnapshot(item)
    }
  } catch {
    /* ignore transient errors */
  }
}

const deliveredDownloads = new Set()
const deliveringDownloads = new Set()
const DELIVERED_STORAGE_KEY = 'vantyx_file_transfer_delivered'

function loadPersistedDelivered() {
  try {
    const raw = localStorage.getItem(DELIVERED_STORAGE_KEY)
    const arr = raw ? JSON.parse(raw) : []
    if (!Array.isArray(arr)) return
    for (const id of arr) {
      if (typeof id === 'string' && id) deliveredDownloads.add(id)
    }
  } catch {
    /* ignore */
  }
}

function persistDelivered(id) {
  if (!id) return
  try {
    const ids = [...deliveredDownloads].slice(-100)
    localStorage.setItem(DELIVERED_STORAGE_KEY, JSON.stringify(ids))
  } catch {
    /* ignore */
  }
}

async function maybeDeliverDownload(job) {
  if (!job?.id || deliveredDownloads.has(job.id) || deliveringDownloads.has(job.id)) {
    return
  }
  deliveringDownloads.add(job.id)
  try {
    const blob = await API.fileTransferContent(job.id)
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = job.file_name || 'download'
    a.click()
    URL.revokeObjectURL(a.href)
    deliveredDownloads.add(job.id)
    persistDelivered(job.id)
    await API.fileTransferDelete(job.id).catch(() => {})
  } catch {
    /* allow retry on next snapshot if fetch failed */
  } finally {
    deliveringDownloads.delete(job.id)
  }
}

function stopFallbackPolling() {
  if (pollTimer) {
    window.clearInterval(pollTimer)
    pollTimer = null
  }
}

function startFallbackPolling() {
  if (pollTimer) return
  pollTimer = window.setInterval(pollOnce, POLL_MS_FALLBACK)
}

function connectSSE() {
  if (sseConn) return
  if (typeof window === 'undefined' || typeof window.EventSource !== 'function') {
    startFallbackPolling()
    return
  }
  try {
    sseConn = API.subscribeFileTransferEvents(
      (snap) => {
        sseConnected = true
        stopFallbackPolling()
        void applySnapshot(snap)
      },
      () => {
        sseConn = null
        sseConnected = false
        // start (or resume) fallback polling while disconnected
        startFallbackPolling()
        if (sseRetryTimer) return
        sseRetryTimer = window.setTimeout(() => {
          sseRetryTimer = null
          connectSSE()
        }, SSE_RECONNECT_MS)
      },
    )
  } catch {
    startFallbackPolling()
  }
}

function ensurePolling() {
  connectSSE()
  // Run an immediate poll so existing jobs appear without waiting for SSE backlog.
  pollOnce()
  // Start fallback polling; it will be stopped automatically once SSE delivers an event.
  if (!sseConnected) startFallbackPolling()
}

function escapeHtml(s) {
  if (s == null) return ''
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}

function renderGlobalBar() {
  const slot = document.getElementById('vantyx-file-transfers-slot')
  let bar = document.getElementById('vantyx-file-transfers-bar')
  const hasDetailedView = !slot && !!document.getElementById('file-transfers-table-wrap')
  const active = [...jobs.values()].filter((j) => isActive(j.state) || j.state === 'completed' || j.state === 'failed')
  if (active.length === 0 || hasDetailedView) {
    bar?.remove()
    return
  }
  const inlineClass = 'border border-slate-200 rounded-xl bg-white shadow-sm'
  const fixedClass = 'fixed bottom-0 left-0 right-0 z-[60] border-t border-slate-300 bg-white shadow-lg'
  const expectedParent = slot || document.body
  const expectedClass = slot ? inlineClass : fixedClass
  if (bar && bar.parentElement !== expectedParent) {
    bar.remove()
    bar = null
  }
  if (!bar) {
    bar = document.createElement('div')
    bar.id = 'vantyx-file-transfers-bar'
    expectedParent.appendChild(bar)
  }
  if (bar.className !== expectedClass) {
    bar.className = expectedClass
  }
  const rows = active
    .slice(-6)
    .reverse()
    .map((j) => {
      const pct = percentFor(j)
      const dir = j.direction === 'upload' ? t('fileTransfersBar.arrowUp') : t('fileTransfersBar.arrowDown')
      return `
        <div class="flex items-center gap-2 py-1.5 px-2 text-sm ${j.state === 'failed' ? 'text-red-600' : 'text-slate-700'}">
          <span class="font-mono text-xs text-slate-500">${dir}</span>
          <span class="truncate flex-1 min-w-0" title="${escapeHtml(j.remote_path || '')}">${escapeHtml(j.file_name || j.target_name || j.id)}</span>
          <div class="w-20 h-1.5 bg-slate-200 rounded-full overflow-hidden flex-shrink-0">
            <div class="h-full bg-sky-500" style="width:${pct}%"></div>
          </div>
          <span class="text-xs min-w-[4rem] text-right">${j.state === 'running' || j.state === 'receiving' ? (pct > 0 ? `${pct}%` : stateLabel(j.state)) : stateLabel(j.state)}</span>
          ${isActive(j.state) ? `<button type="button" class="text-xs text-slate-500 hover:text-red-600 cancel-transfer" data-id="${escapeHtml(j.id)}">${t('fileTransfersBar.cancel')}</button>` : ''}
        </div>
      `
    })
    .join('')
  const hasUpload = active.some((j) => j.direction === 'upload')
  const hasDownload = active.some((j) => j.direction === 'download')
  let title = t('fileTransfersBar.titleAll')
  if (hasUpload && !hasDownload) title = t('fileTransfersBar.titleUpload')
  else if (hasDownload && !hasUpload) title = t('fileTransfersBar.titleDownload')
  const html = `
    <div class="px-3 py-2">
      <div class="flex items-center justify-between mb-1 gap-2">
        <span class="text-xs font-semibold text-slate-600">${escapeHtml(title)}</span>
        <a href="#" id="file-transfers-goto-sessions" class="text-xs text-sky-600 hover:text-sky-800 whitespace-nowrap">${t('fileTransfersBar.gotoSessions')}</a>
      </div>
      <div class="max-h-32 overflow-auto">${rows}</div>
    </div>
  `
  if (bar.dataset.barHtml === html) return
  bar.dataset.barHtml = html
  bar.innerHTML = html
  bar.querySelector('#file-transfers-goto-sessions')?.addEventListener('click', (e) => {
    e.preventDefault()
    const navEl = document.getElementById('nav-sessions')
    if (navEl) {
      navEl.click()
      return
    }
    window.location.href = '/?view=sessions'
  })
  bar.querySelectorAll('.cancel-transfer').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const id = btn.dataset.id
      if (!id) return
      try {
        await cancelBackgroundTransfer(id)
      } catch {
        /* ignore */
      }
    })
  })
}

/**
 * @param {(jobs: object[]) => void} fn
 */
export function onFileTransfersChange(fn) {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

export function getFileTransferJobs() {
  return [...jobs.values()]
}

export function initFileTransferManager() {
  loadPersistedDelivered()
  ensurePolling()
}

/** 転送一覧を即時更新（セッション画面の「更新」用） */
export async function refreshFileTransfers() {
  await pollOnce()
}

/**
 * @param {{ backend: string, targetId: string, path: string, fileName?: string, onProgress?: (n: number) => void }} opts
 */
export async function startBackgroundDownload(opts) {
  const snap = await API.fileTransferStartDownload({
    backend: opts.backend,
    target_id: opts.targetId,
    path: opts.path,
    transfer: opts.transfer,
  })
  const ids = loadWatchedIds()
  ids.push(snap.id)
  saveWatchedIds(ids)
  jobs.set(snap.id, snap)
  ensurePolling()
  notify()
  return snap
}

/**
 * @param {{ backend: string, targetId: string, path: string, file: File, onProgress?: (pct: number) => void }} opts
 */
export function startBackgroundUpload(opts) {
  return new Promise((resolve, reject) => {
    const tempId = `local-${Date.now()}-${Math.random().toString(36).slice(2)}`
    const localJob = {
      id: tempId,
      target_id: opts.targetId,
      target_name: '',
      backend: opts.backend,
      direction: 'upload',
      remote_path: opts.path,
      file_name: opts.file?.name || 'upload',
      state: 'receiving',
      progress: 0,
      total: opts.file?.size || 0,
      _local: true,
    }
    jobs.set(tempId, localJob)
    ensurePolling()
    notify()

    let lastProgressEmit = 0
    const form = new FormData()
    form.append('backend', opts.backend)
    form.append('target_id', opts.targetId)
    form.append('path', opts.path)
    form.append('file', opts.file)
    if (opts.transfer) form.append('transfer', opts.transfer)
    const xhr = new XMLHttpRequest()
    xhr.open('POST', '/api/file-transfers/upload')
    xhr.withCredentials = true
    localAborts.set(tempId, () => {
      try { xhr.abort() } catch { /* ignore */ }
    })
    if (xhr.upload) {
      xhr.upload.onprogress = (ev) => {
        if (!ev.lengthComputable) return
        localJob.progress = ev.loaded
        localJob.total = ev.total
        const now = Date.now()
        if (now - lastProgressEmit >= 50) {
          lastProgressEmit = now
          notifyIfChanged()
        }
        if (typeof opts.onProgress === 'function') {
          opts.onProgress((ev.loaded / ev.total) * 100)
        }
      }
    }
    xhr.onload = async () => {
      jobs.delete(tempId)
      localAborts.delete(tempId)
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          const snap = JSON.parse(xhr.responseText)
          const ids = loadWatchedIds()
          ids.push(snap.id)
          saveWatchedIds(ids)
          jobs.set(snap.id, snap)
          ensurePolling()
          notify()
          resolve(snap)
        } catch (e) {
          notify()
          reject(e)
        }
      } else {
        let msg = `HTTP ${xhr.status}`
        try {
          const err = JSON.parse(xhr.responseText)
          if (err.message) msg = err.message
        } catch {
          /* ignore */
        }
        notify()
        reject(new Error(msg))
      }
    }
    xhr.onerror = () => {
      jobs.delete(tempId)
      localAborts.delete(tempId)
      notify()
      reject(new Error('network error'))
    }
    xhr.onabort = () => {
      jobs.delete(tempId)
      localAborts.delete(tempId)
      notify()
      reject(new Error('cancelled'))
    }
    xhr.send(form)
  })
}

export async function cancelBackgroundTransfer(id) {
  const abortFn = localAborts.get(id)
  if (abortFn) {
    abortFn()
    return
  }
  await API.fileTransferCancel(id)
  await pollOnce()
}
