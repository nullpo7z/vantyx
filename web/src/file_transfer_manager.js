import API from './api.js'

const STORAGE_KEY = 'vantyx_file_transfer_ids'
const POLL_MS = 1500

let pollTimer = null
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

function notify() {
  for (const fn of listeners) {
    try {
      fn()
    } catch {
      /* ignore */
    }
  }
  renderGlobalBar()
}

function isActive(state) {
  return state === 'receiving' || state === 'running'
}

function stateLabel(state) {
  switch (state) {
    case 'receiving':
      return '受信中…'
    case 'running':
      return '転送中…'
    case 'completed':
      return '完了'
    case 'failed':
      return 'エラー'
    case 'cancelled':
      return 'キャンセル'
    default:
      return state
  }
}

function percentFor(job) {
  if (job.state === 'completed') return 100
  if (!job.total || job.total <= 0) return job.progress > 0 ? 50 : 0
  return Math.min(100, Math.round((job.progress / job.total) * 100))
}

async function pollOnce() {
  try {
    const res = await API.fileTransfers()
    const items = Array.isArray(res.items) ? res.items : []
    const watched = new Set(loadWatchedIds())
    for (const item of items) {
      jobs.set(item.id, item)
      if (isActive(item.state)) watched.add(item.id)
    }
    for (const id of [...watched]) {
      const j = jobs.get(id)
      if (!j || !isActive(j.state)) {
        if (j && j.state === 'completed' && j.direction === 'download') {
          await maybeDeliverDownload(j)
        }
        if (j && (j.state === 'completed' || j.state === 'failed' || j.state === 'cancelled')) {
          setTimeout(() => {
            watched.delete(id)
            jobs.delete(id)
            saveWatchedIds([...watched])
            notify()
          }, 8000)
        } else if (!j) {
          watched.delete(id)
        }
      }
    }
    saveWatchedIds([...watched])
    notify()
  } catch {
    /* ignore transient errors */
  }
}

const deliveredDownloads = new Set()

async function maybeDeliverDownload(job) {
  if (deliveredDownloads.has(job.id)) return
  deliveredDownloads.add(job.id)
  try {
    const blob = await API.fileTransferContent(job.id)
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = job.file_name || 'download'
    a.click()
    URL.revokeObjectURL(a.href)
    await API.fileTransferDelete(job.id).catch(() => {})
  } catch {
    deliveredDownloads.delete(job.id)
  }
}

function ensurePolling() {
  if (pollTimer) return
  pollTimer = window.setInterval(pollOnce, POLL_MS)
  pollOnce()
}

function escapeHtml(s) {
  if (s == null) return ''
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}

function renderGlobalBar() {
  let bar = document.getElementById('vantyx-file-transfers-bar')
  const active = [...jobs.values()].filter((j) => isActive(j.state) || j.state === 'completed' || j.state === 'failed')
  if (active.length === 0) {
    bar?.remove()
    return
  }
  if (!bar) {
    bar = document.createElement('div')
    bar.id = 'vantyx-file-transfers-bar'
    bar.className = 'fixed bottom-0 left-0 right-0 z-[60] border-t border-slate-300 bg-white shadow-lg'
    document.body.appendChild(bar)
  }
  const rows = active
    .slice(-6)
    .reverse()
    .map((j) => {
      const pct = percentFor(j)
      const dir = j.direction === 'upload' ? '↑' : '↓'
      return `
        <div class="flex items-center gap-2 py-1.5 px-2 text-sm ${j.state === 'failed' ? 'text-red-600' : 'text-slate-700'}">
          <span class="font-mono text-xs text-slate-500">${dir}</span>
          <span class="truncate flex-1 min-w-0" title="${escapeHtml(j.remote_path || '')}">${escapeHtml(j.file_name || j.target_name || j.id)}</span>
          <div class="w-20 h-1.5 bg-slate-200 rounded-full overflow-hidden flex-shrink-0">
            <div class="h-full bg-sky-500" style="width:${pct}%"></div>
          </div>
          <span class="text-xs min-w-[4rem] text-right">${j.state === 'running' || j.state === 'receiving' ? (pct > 0 ? `${pct}%` : stateLabel(j.state)) : stateLabel(j.state)}</span>
          ${isActive(j.state) ? `<button type="button" class="text-xs text-slate-500 hover:text-red-600 cancel-transfer" data-id="${escapeHtml(j.id)}">中止</button>` : ''}
        </div>
      `
    })
    .join('')
  bar.innerHTML = `
    <div class="max-w-3xl mx-auto px-3 py-2">
      <div class="flex items-center justify-between mb-1">
        <span class="text-xs font-semibold text-slate-600">ファイル転送（バックグラウンド）</span>
      </div>
      <div class="max-h-32 overflow-auto">${rows}</div>
    </div>
  `
  bar.querySelectorAll('.cancel-transfer').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const id = btn.dataset.id
      if (!id) return
      try {
        await API.fileTransferCancel(id)
        await pollOnce()
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
  ensurePolling()
}

/**
 * @param {{ backend: string, targetId: string, path: string, fileName?: string, onProgress?: (n: number) => void }} opts
 */
export async function startBackgroundDownload(opts) {
  const snap = await API.fileTransferStartDownload({
    backend: opts.backend,
    target_id: opts.targetId,
    path: opts.path,
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
    const form = new FormData()
    form.append('backend', opts.backend)
    form.append('target_id', opts.targetId)
    form.append('path', opts.path)
    form.append('file', opts.file)
    const xhr = new XMLHttpRequest()
    xhr.open('POST', '/api/file-transfers/upload')
    xhr.withCredentials = true
    if (xhr.upload && opts.onProgress) {
      xhr.upload.onprogress = (ev) => {
        if (ev.lengthComputable) {
          opts.onProgress((ev.loaded / ev.total) * 100)
        }
      }
    }
    xhr.onload = async () => {
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
        reject(new Error(msg))
      }
    }
    xhr.onerror = () => reject(new Error('network error'))
    xhr.send(form)
  })
}

export async function cancelBackgroundTransfer(id) {
  await API.fileTransferCancel(id)
  await pollOnce()
}
