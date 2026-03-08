import API from './api.js'

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

export function renderFilesPage(container) {
  const params = new URLSearchParams(window.location.search)
  const targetId = params.get('target_id') || ''
  const targetName = params.get('target_name') || targetId || 'ファイル'

  let currentPath = '/'
  let loading = false

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

  function renderBreadcrumb() {
    const parts = pathParts(currentPath)
    const segs = [{ name: 'ルート', path: '/' }]
    let acc = ''
    for (const name of parts) {
      acc += '/' + name
      segs.push({ name, path: acc })
    }
    return segs
      .map(
        (s) =>
          `<a href="#" class="files-breadcrumb px-1.5 py-0.5 rounded hover:bg-slate-200 text-slate-700" data-path="${escapeHtml(s.path)}">${escapeHtml(s.name)}</a>`
      )
      .join('<span class="text-slate-400 mx-0.5">/</span>')
  }

  function renderTable(entries) {
    if (!entries || entries.length === 0) {
      return '<tr><td colspan="4" class="px-4 py-8 text-center text-slate-500">このフォルダは空です</td></tr>'
    }
    return entries
      .map((e) => {
        const isDir = e.is_dir
        const size = isDir ? '—' : formatSize(e.size || 0)
        const modTime = e.mod_time || '—'
        const pathNext = currentPath === '/' ? '/' + e.name : currentPath + '/' + e.name
        const row = `
          <tr class="border-b border-slate-200 hover:bg-slate-50">
            <td class="px-4 py-2">
              ${isDir ? `<a href="#" class="files-open-dir text-sky-600 hover:underline font-medium" data-path="${escapeHtml(pathNext)}">${escapeHtml(e.name)}/</a>` : `<span class="text-slate-800">${escapeHtml(e.name)}</span>`}
            </td>
            <td class="px-4 py-2 text-sm text-slate-600">${escapeHtml(String(size))}</td>
            <td class="px-4 py-2 text-sm text-slate-500">${escapeHtml(modTime)}</td>
            <td class="px-4 py-2">
              ${!isDir ? `<button type="button" class="files-download rounded border border-slate-300 bg-white px-2 py-1 text-xs text-slate-600 hover:bg-slate-50 mr-1" data-path="${escapeHtml(pathNext)}">ダウンロード</button>` : ''}
              <button type="button" class="files-delete rounded border border-red-200 bg-white px-2 py-1 text-xs text-red-600 hover:bg-red-50" data-path="${escapeHtml(pathNext)}" data-name="${escapeHtml(e.name)}">削除</button>
            </td>
          </tr>
        `
        return row
      })
      .join('')
  }

  function renderContent(entries = null) {
    const tbody = container.querySelector('#files-tbody')
    const breadEl = container.querySelector('#files-breadcrumb')
    if (breadEl) breadEl.innerHTML = renderBreadcrumb()
    if (tbody) {
      if (entries) tbody.innerHTML = renderTable(entries)
      else tbody.innerHTML = '<tr><td colspan="4" class="px-4 py-8 text-center text-slate-500">読み込み中…</td></tr>'
    }
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
    try {
      const entries = await API.filesList(targetId, currentPath)
      renderContent(entries)
    } catch (err) {
      setError(err.message || '一覧の取得に失敗しました')
      renderContent([])
    } finally {
      loading = false
    }
  }

  container.innerHTML = `
    <div class="min-h-screen w-full flex flex-col bg-slate-50">
      <header class="shrink-0 px-4 sm:px-6 py-3 bg-white border-b border-slate-200 flex items-center justify-between">
        <div class="min-w-0 flex items-center gap-4">
          <a href="/" id="files-back" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50">ホーム</a>
          <div>
            <div class="text-xs text-slate-500">ファイルマネージャー</div>
            <div class="text-sm font-semibold text-slate-800 truncate">${escapeHtml(targetName)}</div>
          </div>
        </div>
      </header>

      <main class="flex-1 overflow-auto p-4 sm:p-6">
        <div class="max-w-4xl mx-auto">
          <p id="files-error" class="mb-4 text-sm text-red-600 hidden"></p>
          <div class="mb-4 flex flex-wrap items-center gap-2">
            <nav id="files-breadcrumb" class="text-sm flex items-center flex-wrap gap-0.5">${renderBreadcrumb()}</nav>
          </div>
          <div class="mb-4 flex items-center gap-2">
            <label class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 cursor-pointer">
              <input type="file" id="files-upload-input" class="sr-only" />
              アップロード
            </label>
          </div>
          <div class="bg-white rounded-lg border border-slate-200 shadow-sm overflow-hidden">
            <table class="min-w-full text-left text-sm">
              <thead class="bg-slate-50 border-b border-slate-200">
                <tr>
                  <th class="px-4 py-2 font-medium text-slate-700">名前</th>
                  <th class="px-4 py-2 font-medium text-slate-700">サイズ</th>
                  <th class="px-4 py-2 font-medium text-slate-700">更新日時</th>
                  <th class="px-4 py-2 font-medium text-slate-700">操作</th>
                </tr>
              </thead>
              <tbody id="files-tbody">
                <tr><td colspan="4" class="px-4 py-8 text-center text-slate-500">読み込み中…</td></tr>
              </tbody>
            </table>
          </div>
        </div>
      </main>
    </div>
  `

  container.querySelector('#files-back').addEventListener('click', (e) => {
    e.preventDefault()
    window.location.href = '/'
  })

  container.addEventListener('click', (e) => {
    const openDir = e.target.closest('.files-open-dir')
    const bread = e.target.closest('.files-breadcrumb')
    const download = e.target.closest('.files-download')
    const del = e.target.closest('.files-delete')
    if (openDir) {
      e.preventDefault()
      currentPath = openDir.dataset.path || '/'
      loadList()
    } else if (bread) {
      e.preventDefault()
      currentPath = bread.dataset.path || '/'
      loadList()
    } else if (download) {
      e.preventDefault()
      const path = download.dataset.path
      if (!path) return
      API.filesDownload(targetId, path)
        .then((blob) => {
          const name = path.split('/').filter(Boolean).pop() || 'download'
          const a = document.createElement('a')
          a.href = URL.createObjectURL(blob)
          a.download = name
          a.click()
          URL.revokeObjectURL(a.href)
        })
        .catch((err) => setError(err.message || 'ダウンロードに失敗しました'))
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
    const remotePath = currentPath === '/' ? '/' + file.name : currentPath + '/' + file.name
    API.filesUpload(targetId, remotePath, file)
      .then(() => {
        input.value = ''
        loadList()
      })
      .catch((err) => setError(err.message || 'アップロードに失敗しました'))
  })

  loadList()
}
