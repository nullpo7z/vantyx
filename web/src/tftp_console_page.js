import API from './api.js'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'

function escapeHtml(s) {
  if (s == null) return ''
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}

function formatSize(bytes) {
  if (!bytes) return '—'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let n = bytes
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i += 1
  }
  return i === 0 ? `${n} ${units[i]}` : `${n.toFixed(1)} ${units[i]}`
}

export function renderTFTPConsolePage(container) {
  const params = new URLSearchParams(window.location.search)
  const tftpTargetId = params.get('tftp_target_id') || ''
  const sshTargetId = params.get('ssh_target_id') || ''
  const targetName = params.get('target_name') || ''

  const tftpServerHost = window.location.hostname || ''
  const tftpServerAddr = tftpServerHost || ''

  let currentPath = '/'
  let loading = false
  const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  let term = null
  let fitAddon = null
  let sshWs = null
  let currentSessionId = ''

  function setError(msg) {
    const el = container.querySelector('#tftp-error')
    if (!el) return
    if (!msg) {
      el.classList.add('hidden')
      el.textContent = ''
      return
    }
    el.textContent = msg
    el.classList.remove('hidden')
  }

  async function loadList() {
    if (!tftpTargetId) {
      setError('TFTP ターゲット ID が指定されていません。')
      renderList([])
      return
    }
    if (loading) return
    loading = true
    setError('')
    const listEl = container.querySelector('#tftp-files-list')
    if (listEl) {
      listEl.innerHTML = '<div class="py-6 text-center text-slate-500 text-sm">読み込み中…</div>'
    }
    try {
      const q = new URLSearchParams()
      if (currentPath && currentPath !== '') q.set('path', currentPath)
      const res = await fetch(`/api/tftp/targets/${encodeURIComponent(tftpTargetId)}/files?${q.toString()}`, { credentials: 'include' })
      if (!res.ok) {
        const err = await res.json().catch(() => ({ message: res.statusText }))
        throw new Error(err.message || '一覧の取得に失敗しました')
      }
      const entries = await res.json()
      renderList(entries || [])
    } catch (e) {
      setError(e.message || '一覧の取得に失敗しました')
      renderList([])
    } finally {
      loading = false
    }
  }

  function renderList(entries) {
    const listEl = container.querySelector('#tftp-files-list')
    if (!listEl) return
    if (!entries || !entries.length) {
      listEl.innerHTML = '<div class="py-6 text-center text-slate-500 text-sm">このディレクトリにはファイルがありません</div>'
      return
    }
    const rows = entries
      .slice()
      .sort((a, b) => {
        if (a.is_dir && !b.is_dir) return -1
        if (!a.is_dir && b.is_dir) return 1
        return (a.name || '').localeCompare(b.name || '', undefined, { sensitivity: 'base' })
      })
      .map((e) => {
        const isDir = !!e.is_dir
        const size = isDir ? '—' : formatSize(e.size || 0)
        const modTime = e.mod_time || '—'
        const pathNext = currentPath === '/' ? '/' + e.name : currentPath + '/' + e.name
        return `
          <div class="tftp-row flex items-center gap-3 px-3 py-2 border-b border-slate-100 hover:bg-slate-50 ${isDir ? 'cursor-pointer' : ''}" data-path="${escapeHtml(pathNext)}" data-dir="${isDir ? '1' : '0'}" data-name="${escapeHtml(e.name)}">
            <div class="min-w-0 flex-1">
              <div class="font-medium text-[13px] text-slate-800 truncate">${escapeHtml(e.name)}${isDir ? '/' : ''}</div>
              <div class="text-[11px] text-slate-500 truncate">${escapeHtml(size)} · ${escapeHtml(modTime)}</div>
            </div>
            ${!isDir ? `
            <div class="flex items-center gap-1 flex-shrink-0">
              <button type="button" class="tftp-download text-[11px] px-2 py-1 rounded border border-slate-300 bg-white hover:bg-slate-100" data-path="${escapeHtml(pathNext)}">ダウンロード</button>
              <button type="button" class="tftp-copy-name text-[11px] px-2 py-1 rounded border border-slate-200 bg-slate-50 hover:bg-slate-100" data-name="${escapeHtml(e.name)}">名前をコピー</button>
              <button type="button" class="tftp-delete text-[11px] px-2 py-1 rounded border border-red-200 bg-white hover:bg-red-50 text-red-700" data-path="${escapeHtml(pathNext)}" data-name="${escapeHtml(e.name)}">削除</button>
            </div>
            ` : ''}
          </div>
        `
      })
      .join('')
    listEl.innerHTML = rows
  }

  function renderLayout() {
    container.innerHTML = `
      <div class="h-screen w-full flex flex-col bg-slate-100 overflow-hidden">
        <header class="shrink-0 bg-sky-800 text-white">
          <div class="px-4 py-3 flex items-center justify-between gap-3">
            <div class="min-w-0 flex flex-col">
              <div class="text-xs text-sky-100">TFTP ファイル + コンソール</div>
              <div class="text-sm font-semibold truncate">${escapeHtml(targetName || '')}</div>
            </div>
            <div class="flex items-center gap-3">
              <div class="flex items-center gap-2 text-xs">
                <span class="opacity-80">TFTP サーバー:</span>
                <span id="tftp-server-addr" class="font-mono text-[11px] bg-sky-900/40 rounded px-2 py-0.5">${escapeHtml(tftpServerAddr || '(不明)')}</span>
                <button type="button" id="tftp-copy-addr" class="rounded border border-white/40 bg-white/10 px-2 py-0.5 text-[11px] hover:bg-white/20">コピー</button>
              </div>
              <button type="button" id="tftp-back" class="rounded border border-white/30 bg-white/10 px-3 py-1.5 text-xs font-semibold text-white hover:bg-white/20 shadow-sm">戻る</button>
              <button type="button" id="tftp-disconnect" class="rounded border border-red-300 bg-red-500/90 px-3 py-1.5 text-xs font-semibold text-white hover:bg-red-600 shadow-sm">切断</button>
            </div>
          </div>
        </header>

        <main class="flex-1 min-h-0 flex flex-col divide-y divide-slate-200 overflow-hidden">
          <section class="flex-1 min-h-0 min-h-[30vh] flex flex-col bg-slate-50 overflow-hidden">
            <div class="flex items-center justify-between px-4 pt-3 pb-2">
              <div>
                <h2 class="text-xs font-semibold text-slate-700">TFTP ディレクトリ</h2>
                <p class="text-[11px] text-slate-500 mt-0.5">上でアップロードしたファイル名をコピーし、下のコンソールから TFTP コマンドを実行します。</p>
              </div>
              <div class="flex items-center gap-2">
                <button type="button" id="tftp-refresh" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-[11px] font-medium text-slate-700 hover:bg-slate-50 shadow-sm">更新</button>
                <label class="inline-flex items-center gap-2 px-3 py-1.5 rounded border border-slate-300 bg-white text-[11px] font-medium text-slate-700 hover:bg-slate-50 shadow-sm cursor-pointer">
                  アップロード
                  <input type="file" id="tftp-upload-input" class="sr-only" />
                </label>
              </div>
            </div>
            <p id="tftp-error" class="px-4 text-xs text-red-600 hidden"></p>
            <div id="tftp-files-list" class="flex-1 min-h-0 overflow-auto bg-white border-t border-slate-200"></div>
          </section>

          <section class="flex-1 min-h-0 flex flex-col bg-slate-900 shrink-0">
            <div class="shrink-0 px-4 py-2 flex items-center justify-between">
              <h2 class="text-xs font-semibold text-slate-100">コンソール</h2>
              ${sshTargetId
    ? '<span class="text-[11px] text-slate-400">対象機器側で TFTP コマンドを実行してください。</span>'
    : '<span class="text-[11px] text-red-300">SSH ターゲット ID が指定されていません。</span>'}
            </div>
            <div id="tftp-xterm-wrap" class="flex-1 min-h-0 bg-black flex flex-col">
              ${sshTargetId
    ? '<div id="tftp-xterm" class="flex-1 min-h-0 w-full" style="height: 100%;"></div>'
    : '<div class="h-full flex items-center justify-center text-xs text-slate-300">SSH ターゲットが指定されていないため、コンソールを表示できません。</div>'}
            </div>
          </section>
        </main>
      </div>
    `

    const backBtn = container.querySelector('#tftp-back')
    backBtn?.addEventListener('click', () => {
      // セッションは維持したままホームに戻る。
      window.location.href = '/'
    })

    const disconnectBtn = container.querySelector('#tftp-disconnect')
    disconnectBtn?.addEventListener('click', async () => {
      // SSH セッションを明示的に終了してからホームへ戻る。
      try {
        if (currentSessionId) {
          await API.terminalSessionDelete(currentSessionId)
        }
      } catch {
        // エラー時も一旦戻る（詳細はコンソールで確認）
      }
      try {
        sshWs?.close()
      } catch {
        // ignore
      }
      window.location.href = '/'
    })

    const copyBtn = container.querySelector('#tftp-copy-addr')
    copyBtn?.addEventListener('click', async () => {
      if (!tftpServerAddr) return
      try {
        await navigator.clipboard.writeText(tftpServerAddr)
      } catch {
        // ignore
      }
    })

    container.querySelector('#tftp-refresh')?.addEventListener('click', () => loadList())

    const uploadInput = container.querySelector('#tftp-upload-input')
    if (uploadInput) {
      uploadInput.addEventListener('change', async () => {
        const file = uploadInput.files && uploadInput.files[0]
        if (!file || !tftpTargetId) return
        setError('')
        let remotePath = currentPath === '/' ? '/' + file.name : currentPath + '/' + file.name
        try {
          const form = new FormData()
          form.append('path', remotePath)
          form.append('file', file)
          const res = await fetch(`/api/tftp/targets/${encodeURIComponent(tftpTargetId)}/files/upload`, {
            method: 'POST',
            credentials: 'include',
            body: form,
          })
          if (!res.ok) {
            const err = await res.json().catch(() => ({ message: res.statusText }))
            throw new Error(err.message || 'アップロードに失敗しました')
          }
          uploadInput.value = ''
          await loadList()
        } catch (e) {
          setError(e.message || 'アップロードに失敗しました')
        }
      })
    }

    container.addEventListener('click', async (e) => {
      const row = e.target.closest('.tftp-row')
      const dl = e.target.closest('.tftp-download')
      const copyName = e.target.closest('.tftp-copy-name')
      const delBtn = e.target.closest('.tftp-delete')
      if (row && row.dataset.dir === '1') {
        e.preventDefault()
        e.stopPropagation()
        currentPath = row.dataset.path || '/'
        await loadList()
      } else if (dl) {
        e.preventDefault()
        const path = dl.dataset.path || ''
        if (!path || !tftpTargetId) return
        setError('')
        try {
          const q = new URLSearchParams({ path })
          const res = await fetch(`/api/tftp/targets/${encodeURIComponent(tftpTargetId)}/files/download?${q.toString()}`, {
            credentials: 'include',
          })
          if (!res.ok) {
            const err = await res.json().catch(() => ({ message: res.statusText }))
            throw new Error(err.message || 'ダウンロードに失敗しました')
          }
          const blob = await res.blob()
          const a = document.createElement('a')
          const url = URL.createObjectURL(blob)
          a.href = url
          const filename = (row && row.dataset.name) || 'download'
          a.download = filename
          a.click()
          URL.revokeObjectURL(url)
        } catch (err) {
          setError(err.message || 'ダウンロードに失敗しました')
        }
      } else if (copyName) {
        e.preventDefault()
        const name = copyName.dataset.name || ''
        if (!name) return
        try {
          await navigator.clipboard.writeText(name)
        } catch {
          // ignore
        }
      } else if (delBtn) {
        e.preventDefault()
        e.stopPropagation()
        const path = delBtn.dataset.path || ''
        const name = delBtn.dataset.name || ''
        if (!path || !tftpTargetId) return
        if (!confirm(`「${name}」を削除しますか？`)) return
        setError('')
        try {
          const q = new URLSearchParams({ path })
          const res = await fetch(`/api/tftp/targets/${encodeURIComponent(tftpTargetId)}/files?${q.toString()}`, {
            method: 'DELETE',
            credentials: 'include',
          })
          if (!res.ok) {
            const err = await res.json().catch(() => ({ message: res.statusText }))
            throw new Error(err.message || '削除に失敗しました')
          }
          await loadList()
        } catch (err) {
          setError(err.message || '削除に失敗しました')
        }
      }
    })

    loadList()

    // 下半分の SSH コンソール（xterm + WebSocket）
    if (sshTargetId) {
      const xtermEl = container.querySelector('#tftp-xterm')
      if (xtermEl) {
        // 初期化
        term = new Terminal({
          fontFamily: '"SFMono-Regular", Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace',
          fontSize: 12,
          convertEol: true,
          scrollback: 2000,
          theme: {
            background: '#000000',
            foreground: '#e5e7eb',
          },
        })
        fitAddon = new FitAddon()
        const webLinksAddon = new WebLinksAddon()
        term.loadAddon(fitAddon)
        term.loadAddon(webLinksAddon)
        term.open(xtermEl)
        try { fitAddon.fit() } catch { /* ignore */ }

        function getWsUrl() {
          const cols = term?.cols || 80
          const rows = term?.rows || 24
          return `${wsProtocol}//${window.location.host}/ws/ssh?target_id=${encodeURIComponent(sshTargetId)}&cols=${cols}&rows=${rows}`
        }

        function sendResize(ws) {
          if (!term || !ws || ws.readyState !== WebSocket.OPEN) return
          const cols = term.cols || 0
          const rows = term.rows || 0
          if (cols <= 0 || rows <= 0) return
          try {
            ws.send(JSON.stringify({ type: 'resize', cols, rows }))
          } catch {
            /* ignore */
          }
        }

        const resizeObserver = new ResizeObserver(() => {
          try { fitAddon.fit() } catch { /* ignore */ }
          if (sshWs && sshWs.readyState === WebSocket.OPEN) sendResize(sshWs)
        })
        resizeObserver.observe(xtermEl)
        // レイアウト確定後に再 fit して下半分いっぱいに表示する
        requestAnimationFrame(() => {
          requestAnimationFrame(() => {
            try { fitAddon.fit() } catch { /* ignore */ }
          })
          setTimeout(() => { try { fitAddon.fit() } catch { /* ignore */ } }, 200)
        })

        function connectSSH() {
          const ws = new WebSocket(getWsUrl())
          ws.binaryType = 'arraybuffer'
          sshWs = ws
          ws.onopen = () => {
            // TFTP コンソールでは保存済み認証情報を使って SSH に接続する。
            const name = `${targetName || sshTargetId || 'TFTP コンソール'}`
            const description = `TFTP_CONSOLE:tftp_target_id=${tftpTargetId || ''}`
            const payload = { use_stored_credentials: true, name, description }
            try {
              ws.send(JSON.stringify(payload))
            } catch {
              // ignore
            }
            sendResize(ws)
            term.focus()
          }
          ws.onmessage = (ev) => {
            if (typeof ev.data === 'string') {
              // 最初に送られる {"session_id":"..."} はターミナルに表示しない。
              if (!currentSessionId && ev.data.trim().startsWith('{')) {
                try {
                  const obj = JSON.parse(ev.data)
                  if (obj && typeof obj.session_id === 'string' && obj.session_id) {
                    currentSessionId = obj.session_id
                    return
                  }
                } catch {
                  // JSON でなければそのまま表示
                }
              }
              term.write(ev.data)
            } else {
              term.write(new Uint8Array(ev.data))
            }
          }
          ws.onclose = () => {
            term.write('\r\n[接続が閉じられました]\r\n')
          }
          ws.onerror = () => {
            term.write('\r\n[WebSocket エラー]\r\n')
          }
          term.onData((data) => {
            if (ws.readyState === WebSocket.OPEN) {
              ws.send(new TextEncoder().encode(data))
            }
          })
          window.addEventListener('beforeunload', () => {
            try { ws.close() } catch { /* ignore */ }
            try { resizeObserver.disconnect() } catch { /* ignore */ }
          }, { once: true })
        }

        connectSSH()
      }
    }
  }

  renderLayout()
}

