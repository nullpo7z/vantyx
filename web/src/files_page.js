import API from './api.js'
import {
  initFileTransferManager,
  onFileTransfersChange,
  getFileTransferJobs,
  startBackgroundDownload,
  startBackgroundUpload,
} from './file_transfer_manager.js'

function escapeHtml(s) {
  if (s == null) return ''
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}

function formatSize(bytes) {
  if (bytes === 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let n = bytes
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i += 1
  }
  return i === 0 ? `${n} ${units[i]}` : `${n.toFixed(1)} ${units[i]}`
}

// Termius-style icons (inline SVG)
const iconFolder = `<svg class="w-6 h-6 text-amber-500 flex-shrink-0" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M10 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z"/></svg>`
const iconFile = `<svg class="w-6 h-6 text-slate-400 flex-shrink-0" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M14 2H6c-1.1 0-2 .9-2 2v16c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z"/></svg>`
const iconArrowBack = `<svg class="w-5 h-5" viewBox="0 0 24 24" fill="currentColor"><path d="M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z"/></svg>`
const iconUpload = `<svg class="w-5 h-5" viewBox="0 0 24 24" fill="currentColor"><path d="M9 16h6v-6h4l-7-7-7 7h4v6zm-4 2h14v2H5v-2z"/></svg>`
const iconRefresh = `<svg class="w-5 h-5" viewBox="0 0 24 24" fill="currentColor"><path d="M17.65 6.35A7.958 7.958 0 0012 4c-4.42 0-7.99 3.58-7.99 8s3.57 8 7.99 8c3.73 0 6.84-2.55 7.73-6h-2.08A5.99 5.99 0 0112 18c-3.31 0-6-2.69-6-6s2.69-6 6-6c1.66 0 3.14.69 4.22 1.78L13 11h7V4l-2.35 2.35z"/></svg>`
const iconTerminal = `<svg class="w-5 h-5" viewBox="0 0 24 24" fill="currentColor"><path d="M20 4H4c-1.11 0-1.99.89-1.99 2L2 18c0 1.11.89 2 2 2h16c1.11 0 2-.89 2-2V6c0-1.11-.89-2-2-2zm-9 10l-4-4h2V9h2v1h2l-2 4zm8 0h-6v-2h6v2z"/></svg>`
const iconDownload = `<svg class="w-5 h-5 text-slate-500" viewBox="0 0 24 24" fill="currentColor"><path d="M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z"/></svg>`
const iconDelete = `<svg class="w-5 h-5 text-slate-400 hover:text-red-500" viewBox="0 0 24 24" fill="currentColor"><path d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z"/></svg>`

export function renderFilesPage(container) {
  const params = new URLSearchParams(window.location.search)
  const targetId = params.get('target_id') || ''
  const targetName = params.get('target_name') || targetId || 'ファイル'
  const isTftp = params.get('protocol') === 'tftp'

  let currentPath = '/'
  let loading = false
  const transfers = [] // { id, serverId?, name, type, status, percent, err? }
  const transferBackend = 'remote'

  initFileTransferManager()

  function percentFromJob(job) {
    if (job.state === 'completed') return 100
    if (!job.total || job.total <= 0) return job.progress > 0 ? 50 : 0
    return Math.min(100, Math.round((job.progress / job.total) * 100))
  }

  function syncTransfersFromServer() {
    const serverJobs = getFileTransferJobs().filter((j) => j.target_id === targetId)
    for (const t of transfers) {
      if (!t.serverId) continue
      const j = serverJobs.find((x) => x.id === t.serverId)
      if (!j) continue
      if (j.state === 'receiving' || j.state === 'running') {
        t.percent = percentFromJob(j)
        renderTransfers()
      } else if (j.state === 'completed') {
        setTransferStatus(t.id, 'done')
        if (t.type === 'upload' && !isTftp) loadList()
      } else if (j.state === 'failed') {
        setTransferStatus(t.id, 'error', new Error(j.error || '転送に失敗しました'))
        setError(j.error || '転送に失敗しました')
      } else if (j.state === 'cancelled') {
        setTransferStatus(t.id, 'error', new Error('キャンセルされました'))
      }
    }
  }

  onFileTransfersChange(syncTransfersFromServer)

  function setError(msg) {
    const el = container.querySelector('#files-error')
    if (el) {
      el.textContent = msg
      el.classList.toggle('hidden', !msg)
    }
  }

  function pathParts(p) {
    if (!p || p === '/') return []
    return p.split('/').filter(Boolean)
  }

  function addTransfer(name, type, status = 'pending', err = null) {
    const id = `t-${Date.now()}-${Math.random().toString(36).slice(2)}`
    transfers.push({ id, name, type, status, percent: 0, err })
    renderTransfers()
    return id
  }

  function setTransferStatus(id, status, err = null) {
    const t = transfers.find((x) => x.id === id)
    if (t) {
      t.status = status
      if (status === 'done') {
        t.percent = 100
      }
      t.err = err
      renderTransfers()
    }
  }

  function setTransferProgress(id, percent) {
    const t = transfers.find((x) => x.id === id)
    if (t && t.status === 'pending') {
      const p = Math.max(0, Math.min(100, Math.round(percent || 0)))
      if (p !== t.percent) {
        t.percent = p
        renderTransfers()
      }
    }
  }

  function renderTransfers() {
    const wrap = container.querySelector('#files-transfers')
    if (!wrap) return
    if (transfers.length === 0) {
      wrap.classList.add('hidden')
      return
    }
    wrap.classList.remove('hidden')
    const list = wrap.querySelector('#files-transfers-list')
    if (!list) return
    list.innerHTML = transfers
      .slice(-8)
      .reverse()
      .map(
        (t) => `
        <div class="flex items-center gap-2 py-1.5 px-2 rounded text-sm ${t.status === 'error' ? 'text-red-600 bg-red-50' : t.status === 'done' ? 'text-slate-500' : 'text-slate-700'}">
          ${t.type === 'download' ? iconDownload : iconUpload}
          <span class="truncate flex-1 min-w-0">${escapeHtml(t.name)}</span>
          <div class="flex items-center gap-2 flex-shrink-0">
            <div class="w-24 h-1.5 bg-slate-200 rounded-full overflow-hidden">
              <div class="h-full ${t.status === 'error' ? 'bg-red-400' : 'bg-sky-500'}" style="width:${t.status === 'done' ? 100 : (t.percent || 0)}%"></div>
            </div>
            <span class="text-xs min-w-[3rem] text-right">
              ${t.status === 'pending' && (t.percent || 0) > 0 ? `${t.percent}%` : t.status === 'pending' ? '転送中…' : t.status === 'done' ? '完了' : (t.err && t.err.message) || 'エラー'}
            </span>
          </div>
        </div>
      `
      )
      .join('')
  }

  async function downloadWithProgress(remotePath, _name, tid) {
    try {
      const snap = await startBackgroundDownload({
        backend: transferBackend,
        targetId,
        path: remotePath,
      })
      const t = transfers.find((x) => x.id === tid)
      if (t) t.serverId = snap.id
      setError('')
    } catch (err) {
      setTransferStatus(tid, 'error', err)
      setError(err.message || 'ダウンロードに失敗しました')
    }
  }

  async function uploadWithProgress(remotePath, file, tid) {
    try {
      const snap = await startBackgroundUpload({
        backend: transferBackend,
        targetId,
        path: remotePath,
        file,
        onProgress: (pct) => setTransferProgress(tid, pct),
      })
      const t = transfers.find((x) => x.id === tid)
      if (t) t.serverId = snap.id
      setError('')
    } catch (err) {
      setTransferStatus(tid, 'error', err)
      setError(err.message || 'アップロードに失敗しました')
    }
  }

  function renderBreadcrumb() {
    const parts = pathParts(currentPath)
    const segs = [{ name: 'ルート', path: '/' }]
    let acc = ''
    for (const name of parts) {
      acc += '/' + name
      segs.push({ name, path: acc })
    }
    return segs
      .map((s, i) => {
        const sep = i > 0 ? '<span class="text-slate-500 mx-1">/</span>' : ''
        return `${sep}<a href="#" class="files-breadcrumb px-2 py-1 rounded-md text-sm text-slate-300 hover:text-white hover:bg-slate-700" data-path="${escapeHtml(s.path)}">${escapeHtml(s.name)}</a>`
      })
      .join('')
  }

  function sortEntries(entries) {
    if (!entries || !entries.length) return entries
    return [...entries].sort((a, b) => {
      if (a.is_dir && !b.is_dir) return -1
      if (!a.is_dir && b.is_dir) return 1
      return (a.name || '').localeCompare(b.name || '', undefined, { sensitivity: 'base' })
    })
  }

  function renderList(entries) {
    const listEl = container.querySelector('#files-list')
    if (!listEl) return
    if (!entries || entries.length === 0) {
      const msg = isTftp
        ? 'TFTP ではディレクトリ一覧は取得できません。下の「パスを指定してダウンロード」をご利用ください。'
        : 'このフォルダは空です'
      listEl.innerHTML = `<div class="py-12 text-center text-slate-500 text-sm">${escapeHtml(msg)}</div>`
      return
    }
    const sorted = sortEntries(entries)
    listEl.innerHTML = sorted
      .map((e) => {
        const isDir = e.is_dir
        const size = isDir ? '—' : formatSize(e.size || 0)
        const modTime = e.mod_time || '—'
        const pathNext = currentPath === '/' ? '/' + e.name : currentPath + '/' + e.name
        const icon = isDir ? iconFolder : iconFile
        const meta = isDir ? modTime : `${size} · ${modTime}`
        return `
          <div class="files-row flex items-center gap-3 px-4 py-3 border-b border-slate-100 hover:bg-slate-50 active:bg-slate-100 ${isDir ? 'cursor-pointer' : ''}" data-path="${escapeHtml(pathNext)}" data-dir="${isDir ? '1' : '0'}" data-name="${escapeHtml(e.name)}">
            <span class="flex items-center justify-center w-9 h-9 rounded-lg flex-shrink-0">${icon}</span>
            <div class="min-w-0 flex-1">
              <div class="font-medium text-slate-800 truncate">${escapeHtml(e.name)}${isDir ? '/' : ''}</div>
              <div class="text-xs text-slate-500 truncate">${escapeHtml(meta)}</div>
            </div>
            ${!isDir ? `
            <div class="flex items-center gap-1 flex-shrink-0">
              <button type="button" class="files-download p-2 rounded-lg hover:bg-slate-200 text-slate-500 hover:text-slate-700" data-path="${escapeHtml(pathNext)}" title="ダウンロード">${iconDownload}</button>
              <button type="button" class="files-delete p-2 rounded-lg hover:bg-red-50 text-slate-400 hover:text-red-500" data-path="${escapeHtml(pathNext)}" data-name="${escapeHtml(e.name)}" title="削除">${iconDelete}</button>
            </div>
            ` : ''}
          </div>
        `
      })
      .join('')
  }

  function renderContent(entries = null) {
    const breadEl = container.querySelector('#files-breadcrumb')
    if (breadEl) breadEl.innerHTML = renderBreadcrumb()
    const listEl = container.querySelector('#files-list')
    if (listEl) {
      if (entries) listEl.innerHTML = '' // will be set by renderList
      else listEl.innerHTML = '<div class="py-12 text-center text-slate-500 text-sm">読み込み中…</div>'
    }
    if (entries) renderList(entries)
  }

  async function loadList() {
    if (!targetId) {
      setError('target_id が指定されていません。')
      renderContent([])
      return
    }
    if (loading) return
    loading = true
    setError('')
    renderContent(null)
    const refreshBtn = container.querySelector('#files-refresh')
    if (refreshBtn) refreshBtn.disabled = true
    try {
      const entries = await API.filesList(targetId, currentPath)
      renderContent(entries)
    } catch (err) {
      setError(err.message || '一覧の取得に失敗しました')
      renderContent([])
    } finally {
      loading = false
      if (refreshBtn) refreshBtn.disabled = false
    }
  }

  const terminalUrl = targetId ? `/terminal?target_id=${encodeURIComponent(targetId)}&target_name=${encodeURIComponent(targetName)}` : '/'

  container.innerHTML = `
    <div class="min-h-screen w-full flex flex-col bg-slate-100">
      <header class="shrink-0 bg-slate-800 text-white">
        <div class="px-4 py-3 flex items-center justify-between gap-3">
          <div class="min-w-0 flex items-center gap-3">
            <a href="/" id="files-back" class="flex items-center justify-center w-10 h-10 rounded-lg hover:bg-slate-700 text-slate-300 hover:text-white" title="ホーム">${iconArrowBack}</a>
            <div class="min-w-0">
              <div class="text-xs text-slate-400">ファイル${isTftp ? ' (TFTP)' : ''}</div>
              <div class="text-sm font-semibold truncate">${escapeHtml(targetName)}</div>
            </div>
          </div>
          ${
            isTftp
              ? ''
              : `<a href="${escapeHtml(terminalUrl)}" target="_blank" rel="noopener" class="flex items-center gap-2 px-3 py-2 rounded-lg bg-slate-700 hover:bg-slate-600 text-sm text-white" title="ターミナルで開く">${iconTerminal}<span class="hidden sm:inline">ターミナル</span></a>`
          }
        </div>
        ${
          isTftp
            ? ''
            : `<div class="px-4 pb-2">
          <nav id="files-breadcrumb" class="flex items-center flex-wrap gap-0.5 text-sm min-h-8">${renderBreadcrumb()}</nav>
        </div>`
        }
      </header>

      <main class="flex-1 overflow-auto">
        <p id="files-error" class="mx-4 mt-3 text-sm text-red-600 hidden"></p>
        ${isTftp ? `
        <div id="files-tftp-box" class="mx-4 mt-3 p-4 bg-amber-50 border border-amber-200 rounded-xl">
          <p class="text-sm text-amber-800 mb-3">リモート TFTP ではパスを指定してダウンロード・アップロードします。ディレクトリ一覧と削除はプロトコル上サポートされません。</p>
          <div class="flex flex-wrap items-center gap-2">
            <input type="text" id="files-tftp-path" class="rounded-lg border border-slate-300 px-3 py-2 text-sm w-64 font-mono bg-white" placeholder="例: config.txt または /path/to/file" />
            <button type="button" id="files-tftp-download" class="rounded-lg bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700">ダウンロード</button>
            <label class="inline-flex items-center gap-2 rounded-lg bg-white border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer shadow-sm">
              <span class="flex items-center">${iconUpload}</span>
              アップロード
              <input type="file" id="files-upload-input" class="sr-only" />
            </label>
          </div>
        </div>
        ` : ''}
        ${
          isTftp
            ? ''
            : `<div class="mx-4 mt-3 flex items-center gap-2">
          <label class="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-white border border-slate-200 text-sm font-medium text-slate-700 hover:bg-slate-50 cursor-pointer shadow-sm">
            <span class="flex items-center">${iconUpload}</span>
            アップロード
            <input type="file" id="files-upload-input" class="sr-only" />
          </label>
          <button type="button" id="files-refresh" class="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-white border border-slate-200 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm" title="更新">${iconRefresh}</button>
        </div>`
        }
        <div class="mx-4 mt-3 mb-4 bg-white rounded-xl border border-slate-200 shadow-sm overflow-hidden ${isTftp ? 'hidden' : ''}">
          <div id="files-list" class="min-h-[200px]">
            <div class="py-12 text-center text-slate-500 text-sm">${isTftp ? '' : '読み込み中…'}</div>
          </div>
        </div>
      </main>

      <div id="files-transfers" class="hidden shrink-0 border-t border-slate-200 bg-white max-h-40 overflow-auto">
        <div class="px-3 py-2 text-xs font-medium text-slate-500 border-b border-slate-100">転送（画面を離れてもバックグラウンドで継続）</div>
        <div id="files-transfers-list" class="px-2 pb-2"></div>
      </div>
    </div>
  `

  container.querySelector('#files-back').addEventListener('click', (e) => {
    e.preventDefault()
    window.location.href = '/'
  })

  const refreshBtn = container.querySelector('#files-refresh')
  if (refreshBtn) refreshBtn.addEventListener('click', () => loadList())

  container.addEventListener('click', (e) => {
    const row = e.target.closest('.files-row')
    const bread = e.target.closest('.files-breadcrumb')
    const download = e.target.closest('.files-download')
    const del = e.target.closest('.files-delete')
    if (row && row.dataset.dir === '1') {
      e.preventDefault()
      e.stopPropagation()
      currentPath = row.dataset.path || '/'
      loadList()
    } else if (bread) {
      e.preventDefault()
      currentPath = bread.dataset.path || '/'
      loadList()
    } else if (download) {
      e.preventDefault()
      const path = download.dataset.path
      if (!path) return
      const name = path.split('/').filter(Boolean).pop() || 'download'
      const tid = addTransfer(name, 'download')
      downloadWithProgress(path, name, tid)
    } else if (del) {
      e.preventDefault()
      const path = del.dataset.path
      const name = del.dataset.name || path
      if (!path || path === '/' || path === '') return
      if (!window.confirm(`「${escapeHtml(name)}」を削除しますか？`)) return
      API.filesDelete(targetId, path)
        .then(() => loadList())
        .catch((err) => setError(err.message || '削除に失敗しました'))
    }
  })

  container.querySelector('#files-upload-input').addEventListener('change', (e) => {
    const input = e.target
    const file = input.files && input.files[0]
    if (!file) return
    let remotePath
    if (isTftp) {
      const pathEl = container.querySelector('#files-tftp-path')
      remotePath = (pathEl && pathEl.value.trim()) || '/' + file.name
      if (!remotePath.startsWith('/')) remotePath = '/' + remotePath
    } else {
      remotePath = currentPath === '/' ? '/' + file.name : currentPath + '/' + file.name
    }
    const tid = addTransfer(file.name, 'upload')
    // 入力はリクエスト送信後にクリアする（同名ファイルを再度アップロードできるように）。
    input.value = ''
    uploadWithProgress(remotePath, file, tid)
  })

  if (isTftp) {
    container.querySelector('#files-tftp-download')?.addEventListener('click', () => {
      const pathEl = container.querySelector('#files-tftp-path')
      const path = pathEl && pathEl.value.trim()
      if (!path) {
        setError('パスを入力してください')
        return
      }
      const remotePath = path.startsWith('/') ? path : '/' + path
      const name = path.split('/').filter(Boolean).pop() || 'download'
      setError('')
      const tid = addTransfer(name, 'download')
      downloadWithProgress(remotePath, name, tid)
    })
  }

  if (isTftp) {
    renderContent([])
  } else {
    loadList()
  }
}
