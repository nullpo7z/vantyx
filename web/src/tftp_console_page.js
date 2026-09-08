import API from './api.js'
import {
  initFileTransferManager,
  startBackgroundDownload,
  startBackgroundUpload,
} from './file_transfer_manager.js'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import { t } from './i18n.js'
import { createHostKeyDialogController } from './host_key_dialog.js'
import { uiConfirm } from './ui_dialog.js'
import { classifyTerminalWsFrameSync } from './terminal_ws_protocol.js'
import { setupTerminalKeyboard } from './xterm_input.js'

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
  const resumeSessionId = params.get('session_id') || ''
  const parentToken = params.get('parent_token') || ''
  const channelToken = params.get('channel') || ''
  const useStoredCredentials = params.get('use_stored_credentials') === '1'
  const needsPassword = params.get('needs_password') === '1'
  const needsPassphrase = params.get('needs_passphrase') === '1'
  const urlSessionName = params.get('session_name') ?? ''
  const urlSessionDesc = params.get('session_description') ?? ''

  const tftpServerHost = window.location.hostname || ''
  const tftpServerAddr = tftpServerHost || ''

  let currentPath = '/'
  let loading = false
  const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  let term = null
  let fitAddon = null
  let sshWs = null
  let sshStdinAttached = false
  let sshBeforeUnloadAttached = false
  let currentSessionId = resumeSessionId || ''
  let writeWindowExpiresAt = null
  let writeWindowTimer = null
  let writeWindowEventSource = null
  let writeWindowReconnectTimer = null

  function stopWriteWindowTimer() {
    if (writeWindowTimer) {
      clearInterval(writeWindowTimer)
      writeWindowTimer = null
    }
  }

  function stopWriteWindowEvents() {
    if (writeWindowReconnectTimer) {
      clearTimeout(writeWindowReconnectTimer)
      writeWindowReconnectTimer = null
    }
    writeWindowEventSource?.close()
    writeWindowEventSource = null
  }

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
      setError(t('tftpConsole.targetIdMissing'))
      renderList([])
      return
    }
    if (loading) return
    loading = true
    setError('')
    const listEl = container.querySelector('#tftp-files-list')
    if (listEl) {
      listEl.innerHTML = `<div class="py-6 text-center text-slate-500 text-sm">${t('tftpConsole.listLoading')}</div>`
    }
    try {
      const entries = await API.tftpServerFilesList(tftpTargetId, currentPath || '/')
      renderList(entries || [])
    } catch (e) {
      setError(e.message || t('tftpConsole.listFailed'))
      renderList([])
    } finally {
      loading = false
    }
  }

  function renderList(entries) {
    const listEl = container.querySelector('#tftp-files-list')
    if (!listEl) return
    if (!entries || !entries.length) {
      listEl.innerHTML = `<div class="py-6 text-center text-slate-500 text-sm">${t('tftpConsole.emptyDir')}</div>`
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
              <button type="button" class="tftp-download text-[11px] px-2 py-1 rounded border border-slate-300 bg-white hover:bg-slate-100" data-path="${escapeHtml(pathNext)}">${t('tftpConsole.download')}</button>
              <button type="button" class="tftp-copy-name text-[11px] px-2 py-1 rounded border border-slate-200 bg-slate-50 hover:bg-slate-100" data-name="${escapeHtml(e.name)}">${t('tftpConsole.copyName')}</button>
              <button type="button" class="tftp-delete text-[11px] px-2 py-1 rounded border border-red-200 bg-white hover:bg-red-50 text-red-700" data-path="${escapeHtml(pathNext)}" data-name="${escapeHtml(e.name)}">${t('tftpConsole.delete')}</button>
            </div>
            ` : ''}
          </div>
        `
      })
      .join('')
    listEl.innerHTML = rows
  }

  initFileTransferManager()

  function renderLayout() {
    container.innerHTML = `
      <div class="h-screen w-full flex flex-col overflow-hidden">
        <header class="shrink-0 text-white">
          <div class="vantyx-header-inner">
            <div class="vantyx-header-start">
              <h1 class="vantyx-brand">Vantyx</h1>
              <div class="vantyx-page-context">
                <span class="vantyx-page-context-label">${t('tftpConsole.pageLabel')}</span>
                <span class="vantyx-page-context-target">${escapeHtml(targetName || '')}</span>
              </div>
            </div>
            <div class="vantyx-header-end">
              <div class="flex items-center gap-2 text-xs">
                <span class="opacity-70">${t('tftpConsole.serverLabel')}</span>
                <span id="tftp-server-addr" class="font-mono text-[11px] bg-white/10 rounded px-2 py-0.5">${escapeHtml(tftpServerAddr || t('tftpConsole.serverUnknown'))}</span>
                <button type="button" id="tftp-copy-addr" class="vantyx-page-btn">${t('tftpConsole.copy')}</button>
              </div>
              <button type="button" id="tftp-back" class="vantyx-page-btn">${t('tftpConsole.back')}</button>
              <button type="button" id="tftp-disconnect" class="vantyx-page-btn vantyx-page-btn-danger">${t('tftpConsole.disconnect')}</button>
            </div>
          </div>
        </header>

        <main class="flex-1 min-h-0 flex flex-col divide-y divide-slate-200 overflow-hidden">
          <section class="flex-1 min-h-0 min-h-[30vh] flex flex-col bg-slate-50 overflow-hidden">
            <div class="flex items-center justify-between px-4 pt-3 pb-2">
              <div>
                <h2 class="text-xs font-semibold text-slate-700">${t('tftpConsole.dirHeading')}</h2>
                <p class="text-[11px] text-slate-500 mt-0.5">${t('tftpConsole.dirHint')}</p>
              </div>
              <div class="flex items-center gap-2">
                <button type="button" id="tftp-refresh" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-[11px] font-medium text-slate-700 hover:bg-slate-50 shadow-sm">${t('tftpConsole.refresh')}</button>
                <label class="inline-flex items-center gap-2 px-3 py-1.5 rounded border border-slate-300 bg-white text-[11px] font-medium text-slate-700 hover:bg-slate-50 shadow-sm cursor-pointer">
                  ${t('tftpConsole.upload')}
                  <input type="file" id="tftp-upload-input" class="sr-only" />
                </label>
              </div>
            </div>
            <div id="tftp-write-window-box" class="mx-4 mb-2 p-3 bg-amber-50 border border-amber-200 rounded-xl">
              <div class="flex items-center gap-2 flex-wrap text-[11px]">
                <span class="font-medium text-amber-800">${t('tftpConsole.writeWindowLabel')}</span>
                <span id="tftp-write-window-status" class="text-amber-700 truncate"></span>
                <div class="flex items-center gap-2 ml-auto">
                  <input type="number" min="1" id="tftp-write-window-ttl" class="rounded border border-slate-300 px-2 py-1 text-[11px] w-24 bg-white" placeholder="${t('tftpConsole.writeWindowTtlPlaceholder')}" />
                  <button type="button" id="tftp-write-window-open" class="rounded bg-amber-600 px-3 py-1.5 text-[11px] font-medium text-white hover:bg-amber-700">${t('tftpConsole.writeWindowOpenBtn')}</button>
                  <button type="button" id="tftp-write-window-close" class="rounded border border-amber-300 bg-white px-3 py-1.5 text-[11px] font-medium text-amber-800 hover:bg-amber-100 hidden">${t('tftpConsole.writeWindowCloseBtn')}</button>
                </div>
              </div>
              <p class="text-[11px] text-amber-700 mt-1">${t('tftpConsole.writeWindowHint')}</p>
            </div>
            <p id="tftp-error" class="px-4 text-xs text-red-600 hidden"></p>
            <div id="tftp-files-list" class="flex-1 min-h-0 overflow-auto bg-white border-t border-slate-200"></div>
          </section>

          <section class="flex-1 min-h-0 flex flex-col bg-slate-900 shrink-0">
            <div class="shrink-0 px-4 py-2 flex items-center justify-between">
              <h2 class="text-xs font-semibold text-slate-100">${t('tftpConsole.consoleHeading')}</h2>
              ${sshTargetId
    ? `<span class="text-[11px] text-slate-400">${t('tftpConsole.runHint')}</span>`
    : `<span class="text-[11px] text-red-300">${t('tftpConsole.noSshTargetWarn')}</span>`}
            </div>
            <div id="tftp-xterm-wrap" class="flex-1 min-h-0 bg-black flex flex-col relative">
              ${sshTargetId
    ? `
              <div id="tftp-ssh-creds" class="absolute inset-0 z-10 flex items-center justify-center bg-slate-900/95 p-4 overflow-auto">
                <form id="tftp-ssh-cred-form" class="w-full max-w-md bg-slate-800 border border-slate-600 rounded-lg shadow-lg p-5 space-y-4">
                  <p id="tftp-ssh-cred-prompt" class="text-sm text-slate-200">${t('tftpConsole.credPrompt')}</p>
                  <p id="tftp-ssh-stored-hint" class="text-sm text-slate-300 hidden">${t('tftpConsole.credStoredHint')}</p>
                  <div id="tftp-ssh-username-wrap">
                    <label class="block text-xs text-slate-400 mb-1">${t('tftpConsole.sshUsername')}</label>
                    <input type="text" id="tftp-ssh-username" autocomplete="username" class="w-full rounded border border-slate-600 bg-slate-900 px-3 py-2 text-sm text-white" />
                  </div>
                  <div id="tftp-ssh-password-wrap">
                    <label class="block text-xs text-slate-400 mb-1">${t('tftpConsole.sshPassword')}</label>
                    <input type="password" id="tftp-ssh-password" autocomplete="current-password" class="w-full rounded border border-slate-600 bg-slate-900 px-3 py-2 text-sm text-white" />
                  </div>
                  <div id="tftp-ssh-passphrase-wrap" class="hidden">
                    <label class="block text-xs text-slate-400 mb-1">${t('tftpConsole.sshPassphrase')}</label>
                    <input type="password" id="tftp-ssh-passphrase" autocomplete="off" class="w-full rounded border border-slate-600 bg-slate-900 px-3 py-2 text-sm text-white" />
                  </div>
                  <p id="tftp-ssh-cred-error" class="text-xs text-red-400 hidden"></p>
                  <button type="submit" id="tftp-ssh-connect" class="w-full rounded bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700">${t('tftpConsole.connect')}</button>
                </form>
              </div>
              <div id="tftp-xterm" class="flex-1 min-h-0 w-full hidden" style="height: 100%;"></div>
              `
    : `<div class="h-full flex items-center justify-center text-xs text-slate-300">${t('tftpConsole.noSshTargetBody')}</div>`}
            </div>
          </section>
        </main>
      </div>
    `

    function closeWindow() {
      stopWriteWindowTimer()
      stopWriteWindowEvents()
      try { sshWs?.close() } catch { /* ignore */ }
      if (window.opener && !window.opener.closed) {
        try { window.opener.focus() } catch { /* ignore */ }
      } else if (parentToken) {
        try {
          const bc = new BroadcastChannel(`vantyx-terminal-parent-${parentToken}`)
          try { bc.postMessage({ type: 'focus', refresh: 'active_sessions' }) } finally { bc.close() }
        } catch { /* ignore */ }
      }
      try { window.close() } catch { /* ignore */ }
    }

    const backBtn = container.querySelector('#tftp-back')
    backBtn?.addEventListener('click', () => {
      // セッションは維持したまま元のタブへ戻る。
      closeWindow()
    })

    const disconnectBtn = container.querySelector('#tftp-disconnect')
    disconnectBtn?.addEventListener('click', async () => {
      // SSH セッションを明示的に終了してから元のタブへ戻る。
      try {
        if (currentSessionId) {
          await API.terminalSessionDelete(currentSessionId)
        }
      } catch {
        // エラー時も一旦戻る（詳細はコンソールで確認）
      }
      closeWindow()
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
        const remotePath = currentPath === '/' ? '/' + file.name : currentPath + '/' + file.name
        uploadInput.value = ''
        try {
          await startBackgroundUpload({
            backend: 'tftp_server',
            targetId: tftpTargetId,
            path: remotePath,
            file,
          })
          setError('')
          setTimeout(() => loadList(), 2000)
        } catch (e) {
          setError(e.message || t('tftpConsole.uploadFailed'))
        }
      })
    }

    const writeWindowTtlInput = container.querySelector('#tftp-write-window-ttl')
    const writeWindowOpenBtn = container.querySelector('#tftp-write-window-open')
    const writeWindowCloseBtn = container.querySelector('#tftp-write-window-close')

    function renderWriteWindowStatus(ip) {
      const statusEl = container.querySelector('#tftp-write-window-status')
      if (!statusEl) return
      if (!writeWindowExpiresAt) {
        statusEl.textContent = ''
        writeWindowCloseBtn?.classList.add('hidden')
        return
      }
      if (writeWindowExpiresAt.getTime() - Date.now() <= 0) {
        writeWindowExpiresAt = null
        stopWriteWindowTimer()
        statusEl.textContent = ''
        writeWindowCloseBtn?.classList.add('hidden')
        return
      }
      statusEl.textContent = t('tftpConsole.writeWindowOpenUntil', {
        ip,
        time: writeWindowExpiresAt.toLocaleTimeString(),
      })
      writeWindowCloseBtn?.classList.remove('hidden')
    }

    // Sets local state from a server-reported status and (re)starts the
    // countdown timer. Shared by the initial page-load status fetch and
    // the open-button handler so both paths render identically.
    function applyWriteWindowStatus(open, ip, expiresAtIso) {
      writeWindowExpiresAt = open && expiresAtIso ? new Date(expiresAtIso) : null
      stopWriteWindowTimer()
      if (writeWindowExpiresAt) {
        writeWindowTimer = setInterval(() => renderWriteWindowStatus(ip), 1000)
      }
      renderWriteWindowStatus(ip)
    }

    // Live status via SSE instead of a one-shot GET on load: the stream's
    // first message is the current status (so this also replaces the old
    // "query on load" bootstrap), and every later open/close/reset by any
    // tab or operator arrives immediately after, with no polling.
    function connectWriteWindowEvents() {
      if (!tftpTargetId) return
      writeWindowEventSource = API.subscribeTFTPWriteWindowEvents(
        tftpTargetId,
        (status) => {
          applyWriteWindowStatus(!!(status && status.open), (status && status.client_ip) || '', status && status.expires_at)
        },
        () => {
          // Reconnect after a short delay; until then the panel just
          // keeps showing the last known state instead of erroring out.
          writeWindowEventSource = null
          writeWindowReconnectTimer = setTimeout(connectWriteWindowEvents, 5000)
        },
      )
    }
    connectWriteWindowEvents()

    writeWindowOpenBtn?.addEventListener('click', async () => {
      if (!tftpTargetId) return
      const ttlRaw = (writeWindowTtlInput?.value || '').trim()
      const ttlSeconds = ttlRaw ? Number(ttlRaw) : undefined
      setError('')
      try {
        // The authorized IP is fixed to the target's configured Host and
        // decided server-side; the response echoes it back for display.
        const resp = await API.tftpServerOpenWriteWindow(tftpTargetId, ttlSeconds)
        applyWriteWindowStatus(true, (resp && resp.client_ip) || '', resp && resp.expires_at)
      } catch (e) {
        setError(e.message || t('tftpConsole.writeWindowOpenFailed'))
      }
    })

    writeWindowCloseBtn?.addEventListener('click', async () => {
      if (!tftpTargetId) return
      setError('')
      try {
        await API.tftpServerCloseWriteWindow(tftpTargetId)
      } catch (e) {
        setError(e.message || t('tftpConsole.writeWindowCloseFailed'))
        return
      }
      writeWindowExpiresAt = null
      stopWriteWindowTimer()
      const statusEl = container.querySelector('#tftp-write-window-status')
      if (statusEl) statusEl.textContent = t('tftpConsole.writeWindowClosed')
      writeWindowCloseBtn.classList.add('hidden')
    })

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
          await startBackgroundDownload({
            backend: 'tftp_server',
            targetId: tftpTargetId,
            path,
          })
        } catch (err) {
          setError(err.message || t('tftpConsole.downloadFailed'))
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
        if (!(await uiConfirm(t('tftpConsole.confirmDelete', { name }), { danger: true }))) return
        setError('')
        try {
          await API.tftpServerDelete(tftpTargetId, path)
          await loadList()
        } catch (err) {
          setError(err.message || t('tftpConsole.deleteFailed'))
        }
      }
    })

    loadList()

    // 下半分の SSH コンソール（xterm + WebSocket）
    if (sshTargetId) {
      const xtermEl = container.querySelector('#tftp-xterm')
      const credsWrap = container.querySelector('#tftp-ssh-creds')
      const credForm = container.querySelector('#tftp-ssh-cred-form')
      const credError = container.querySelector('#tftp-ssh-cred-error')
      const credPrompt = container.querySelector('#tftp-ssh-cred-prompt')
      const storedHint = container.querySelector('#tftp-ssh-stored-hint')
      const usernameWrap = container.querySelector('#tftp-ssh-username-wrap')
      const passwordWrap = container.querySelector('#tftp-ssh-password-wrap')
      const passphraseWrap = container.querySelector('#tftp-ssh-passphrase-wrap')
      const usernameInput = container.querySelector('#tftp-ssh-username')
      const passwordInput = container.querySelector('#tftp-ssh-password')
      const passphraseInput = container.querySelector('#tftp-ssh-passphrase')

      function showCredError(msg) {
        if (!credError) return
        if (!msg) {
          credError.classList.add('hidden')
          credError.textContent = ''
          return
        }
        credError.textContent = msg
        credError.classList.remove('hidden')
      }

      function hideCredsShowTerm() {
        credsWrap?.classList.add('hidden')
        xtermEl?.classList.remove('hidden')
        try { fitAddon?.fit() } catch { /* ignore */ }
      }

      function buildAuthPayload({ useStored, username, password, passphrase, sessionName, sessionDesc }) {
        const description = `TFTP_CONSOLE:tftp_target_id=${tftpTargetId || ''}`
        const metaDesc = sessionDesc || description
        const metaName = sessionName || `${targetName || sshTargetId} TFTP`
        if (useStored) {
          const p = { use_stored_credentials: true, name: metaName, description: metaDesc }
          if (password) p.password = password
          if (passphrase) p.private_key_passphrase = passphrase
          return p
        }
        const out = { username, password: password || '', name: metaName, description: metaDesc }
        if (passphrase) out.private_key_passphrase = passphrase
        return out
      }

      if (xtermEl) {
        let lastAuthPayload = null
        const hostKeyCtrl = createHostKeyDialogController({
          targetId: sshTargetId,
          onReconnect: () => {
            if (!lastAuthPayload) {
              credsWrap?.classList.remove('hidden')
              xtermEl?.classList.add('hidden')
              return
            }
            showCredError('')
            hideCredsShowTerm()
            connectSSH(lastAuthPayload)
          },
          onBeforeDialog: () => {
            xtermEl?.classList.add('hidden')
            credsWrap?.classList.add('hidden')
          },
          onCancelled: (mode) => {
            const title = t('hostKey.cancelledTitle')
            const body = mode === 'mismatch'
              ? t('hostKey.cancelledMismatch')
              : t('hostKey.cancelledUnknown')
            showCredError(`${title} — ${body}`)
            credsWrap?.classList.remove('hidden')
            xtermEl?.classList.add('hidden')
          },
        })

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
        setupTerminalKeyboard(term, xtermEl)
        try { fitAddon.fit() } catch { /* ignore */ }

        function getWsUrlNew() {
          const cols = term?.cols || 80
          const rows = term?.rows || 24
          return `${wsProtocol}//${window.location.host}/ws/ssh?target_id=${encodeURIComponent(sshTargetId)}&cols=${cols}&rows=${rows}`
        }

        const getWsUrlResume = (sid) => `${wsProtocol}//${window.location.host}/ws/ssh?session_id=${encodeURIComponent(sid)}`

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
        window.requestAnimationFrame(() => {
          window.requestAnimationFrame(() => {
            try { fitAddon.fit() } catch { /* ignore */ }
          })
          setTimeout(() => { try { fitAddon.fit() } catch { /* ignore */ } }, 200)
        })

        function connectSSH(authPayload) {
          if (authPayload) lastAuthPayload = authPayload
          hostKeyCtrl.resetHostKeyError()
          const isResume = !!currentSessionId
          const ws = new WebSocket(isResume ? getWsUrlResume(currentSessionId) : getWsUrlNew())
          ws.binaryType = 'arraybuffer'
          sshWs = ws
          let sawError = false
          let sawFirstMessage = false
          // A resumed session already had real output at some point, so
          // a bare close shouldn't be treated as "never connected" and
          // fall back to the credential form. For a fresh connect this
          // only flips true once actual terminal/binary output arrives
          // -- the session_id meta frame alone is sent before the
          // bridge even attempts to authenticate, so it can't be used
          // as proof the login succeeded (mirrors terminal_page.js).
          let sawRealSession = isResume
          const connectTimeout = window.setTimeout(() => {
            if (sawFirstMessage) return
            sawError = true
            showCredError(t('tftpConsole.connectTimedOut'))
            credsWrap?.classList.remove('hidden')
            xtermEl?.classList.add('hidden')
            try { ws.close() } catch { /* ignore */ }
          }, 20000)
          ws.onopen = () => {
            if (!isResume && authPayload) {
              try {
                ws.send(JSON.stringify(authPayload))
              } catch {
                // ignore
              }
            }
            sendResize(ws)
            term.focus()
          }
          ws.onmessage = (ev) => {
            sawFirstMessage = true
            window.clearTimeout(connectTimeout)
            const frame = classifyTerminalWsFrameSync(ev.data)
            if (frame.kind === 'empty' || frame.kind === 'ready' || frame.kind === 'swallow') {
              return
            }
            if (frame.kind === 'meta' && frame.object) {
              const obj = frame.object
              if (hostKeyCtrl.handleHostKeyMeta(ws, obj)) return
              if (!currentSessionId && typeof obj.session_id === 'string' && obj.session_id) {
                currentSessionId = obj.session_id
              }
              return
            }
            if (typeof ev.data === 'string') {
              if (ev.data.startsWith('error:')) {
                sawError = true
                showCredError(ev.data.replace(/^error:\s*/, ''))
                credsWrap?.classList.remove('hidden')
                xtermEl?.classList.add('hidden')
                try { ws.close() } catch { /* ignore */ }
                return
              }
              if (frame.kind === 'terminal' && frame.text) {
                sawRealSession = true
                hideCredsShowTerm()
                term.write(frame.text)
              }
            } else {
              sawRealSession = true
              hideCredsShowTerm()
              term.write(new Uint8Array(ev.data))
            }
          }
          // onerror deliberately does NOT touch the UI: its firing order
          // relative to onmessage/onclose is not reliably guaranteed
          // across browsers, so anything shown here can race with, and
          // clobber, a real "error: ..." message onmessage already
          // surfaced. onclose always fires last and is the single place
          // that decides what to show (see terminal_page.js for the
          // same pattern and the incident that motivated it).
          ws.onerror = () => {}
          ws.onclose = () => {
            window.clearTimeout(connectTimeout)
            if (hostKeyCtrl.getSawHostKeyError()) return
            if (sawError) return // already shown via onmessage above
            if (sawRealSession) {
              term.write(`\r\n${t('tftpConsole.connectionClosed')}\r\n`)
              return
            }
            // Never received anything at all, or got a session_id but
            // the bridge died before any real shell output arrived
            // (e.g. an auth failure whose "error: ..." frame didn't
            // make it before the socket closed) -- don't leave the
            // user staring at a blank/hidden screen with no feedback.
            currentSessionId = ''
            showCredError(t(useStoredCredentials ? 'tftpConsole.closedNoFirstStored' : 'tftpConsole.closedNoFirstNew'))
            credsWrap?.classList.remove('hidden')
            xtermEl?.classList.add('hidden')
          }
          // Guard both registrations: connectSSH re-runs on every reconnect
          // (credential retry, host-key-adopt reconnect), and without these
          // flags each call would stack another term.onData closure and
          // another never-removed beforeunload listener.
          if (!sshStdinAttached) {
            sshStdinAttached = true
            term.onData((data) => {
              if (sshWs && sshWs.readyState === WebSocket.OPEN) {
                sshWs.send(new TextEncoder().encode(data))
              }
            })
          }
          if (!sshBeforeUnloadAttached) {
            sshBeforeUnloadAttached = true
            window.addEventListener('beforeunload', () => {
              try { sshWs && sshWs.close() } catch { /* ignore */ }
              try { resizeObserver.disconnect() } catch { /* ignore */ }
            })
          }
        }

        function startConnect(authPayload) {
          showCredError('')
          hideCredsShowTerm()
          connectSSH(authPayload)
        }

        credForm?.addEventListener('submit', (e) => {
          e.preventDefault()
          const sessionName = urlSessionName
          const sessionDesc = urlSessionDesc
          if (useStoredCredentials) {
            const password = needsPassword ? (passwordInput?.value ?? '') : ''
            const passphrase = needsPassphrase ? (passphraseInput?.value ?? '') : ''
            if (needsPassword && !password) {
              showCredError(t('tftpConsole.enterPassword'))
              return
            }
            if (needsPassphrase && !passphrase) {
              showCredError(t('tftpConsole.enterPassphrase'))
              return
            }
            startConnect(buildAuthPayload({
              useStored: true,
              password,
              passphrase,
              sessionName,
              sessionDesc,
            }))
            return
          }
          const username = (usernameInput?.value ?? '').trim()
          const password = passwordInput?.value ?? ''
          const passphrase = passphraseInput?.value ?? ''
          if (!username) {
            showCredError(t('tftpConsole.enterUsername'))
            return
          }
          startConnect(buildAuthPayload({
            useStored: false,
            username,
            password,
            passphrase,
            sessionName,
            sessionDesc,
          }))
        })

        if (resumeSessionId) {
          hideCredsShowTerm()
          connectSSH(null)
        } else if (channelToken) {
          if (credPrompt) credPrompt.classList.add('hidden')
          if (storedHint) {
            storedHint.textContent = t('tftpConsole.parentTabReceiving')
            storedHint.classList.remove('hidden')
          }
          const bc = new BroadcastChannel(`vantyx-terminal-${channelToken}`)
          const timeoutId = window.setTimeout(() => {
            try { bc.close() } catch { /* ignore */ }
            showCredError(t('tftpConsole.parentTabTimeout'))
            if (credPrompt) credPrompt.classList.remove('hidden')
          }, 10_000)
          bc.onmessage = (ev) => {
            const typ = ev?.data?.type
            if (typ !== 'credentials' && typ !== 'stored_credentials') return
            window.clearTimeout(timeoutId)
            try { bc.close() } catch { /* ignore */ }
            const name = typeof ev.data.name === 'string' ? ev.data.name : urlSessionName
            const desc = typeof ev.data.description === 'string' ? ev.data.description : urlSessionDesc
            if (typ === 'stored_credentials') {
              const p = ev.data.password != null ? ev.data.password : ''
              const passphrase = typeof ev.data.private_key_passphrase === 'string' ? ev.data.private_key_passphrase : ''
              startConnect(buildAuthPayload({
                useStored: true,
                password: typeof p === 'string' ? p : '',
                passphrase,
                sessionName: name,
                sessionDesc: desc,
              }))
              return
            }
            const u = ev.data.username
            const p = ev.data.password != null ? ev.data.password : ''
            const passphrase = typeof ev.data.private_key_passphrase === 'string' ? ev.data.private_key_passphrase : ''
            if (!u || !p) {
              if (usernameInput) usernameInput.value = typeof u === 'string' ? u : ''
              if (passwordInput) passwordInput.value = typeof p === 'string' ? p : ''
              if (passphraseInput && passphrase) passphraseInput.value = passphrase
              showCredError(t('tftpConsole.credsIncomplete'))
              if (credPrompt) credPrompt.classList.remove('hidden')
              return
            }
            startConnect(buildAuthPayload({
              useStored: false,
              username: u,
              password: p,
              passphrase,
              sessionName: name,
              sessionDesc: desc,
            }))
          }
          try {
            bc.postMessage({ type: 'ready', target_id: sshTargetId })
          } catch {
            window.clearTimeout(timeoutId)
          }
        } else if (useStoredCredentials) {
          if (credPrompt) credPrompt.classList.add('hidden')
          if (storedHint) storedHint.classList.remove('hidden')
          if (usernameWrap) usernameWrap.classList.add('hidden')
          if (passwordWrap) passwordWrap.classList.toggle('hidden', !needsPassword)
          if (passphraseWrap) passphraseWrap.classList.toggle('hidden', !needsPassphrase)
          if (!needsPassword && !needsPassphrase) {
            startConnect(buildAuthPayload({
              useStored: true,
              sessionName: urlSessionName,
              sessionDesc: urlSessionDesc,
            }))
          }
        } else {
          // 直接 URL で開いた場合など: 認証フォームを表示（自動接続しない）
          if (credPrompt) credPrompt.classList.remove('hidden')
        }
      }
    }
  }

  renderLayout()
}

