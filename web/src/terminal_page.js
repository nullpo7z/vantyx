import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import API from './api.js'
import { t } from './i18n.js'

function escapeHtml(s) {
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}

export function renderTerminalPage(container) {
  const params = new URLSearchParams(window.location.search)
  const targetId = params.get('target_id') || ''
  const targetName = params.get('target_name') || targetId || 'terminal'
  const terminalProtocol = params.get('protocol') || 'ssh'
  const isTelnet = terminalProtocol === 'telnet'
  const authLabel = isTelnet ? 'Telnet' : 'SSH'
  const channelToken = params.get('channel') || ''
  const resumeSessionId = params.get('session_id') || ''
  const parentToken = params.get('parent_token') || ''
  const useStoredCredentials = params.get('use_stored_credentials') === '1'
  const needsPassword = params.get('needs_password') === '1'
  const needsPassphrase = !isTelnet && params.get('needs_passphrase') === '1'
  const urlSessionName = params.get('session_name') ?? ''
  const urlSessionDesc = params.get('session_description') ?? ''
  const hasSessionParamsFromUrl = params.has('session_name') || params.has('session_description')

  document.documentElement.classList.add('terminal-standalone')
  document.body.classList.add('terminal-standalone')

  container.innerHTML = `
    <div class="terminal-page-root flex flex-1 min-h-0 w-full flex-col overflow-hidden font-sans text-slate-900">
      <header class="relative z-30 shrink-0 shadow text-white">
        <div class="vantyx-header-inner">
          <div class="vantyx-header-start">
            <h1 class="vantyx-brand">Vantyx</h1>
            <div class="vantyx-page-context">
              <span class="vantyx-page-context-label">${isTelnet ? t('terminal.pageLabelTelnet') : t('terminal.pageLabelSsh')}</span>
              <span class="vantyx-page-context-target">${escapeHtml(targetName)}</span>
            </div>
          </div>
          <div class="vantyx-header-end">
            <button id="term-back" type="button" class="vantyx-page-btn">${t('terminal.back')}</button>
            <button id="term-close" type="button" class="vantyx-page-btn">${t('terminal.endSessionBtn')}</button>
          </div>
        </div>
      </header>

      <div id="term-disconnected" class="hidden flex-1 flex flex-col items-center justify-center p-4 gap-4 bg-slate-100">
        <p class="text-sm text-slate-600">${t('terminal.sessionContinues')}</p>
        <div class="flex gap-3">
          <button id="term-reconnect" type="button" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm">${t('terminal.reconnect')}</button>
          <button id="term-back-from-disconnect" type="button" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm">${t('terminal.backHome')}</button>
        </div>
      </div>
      <div id="term-session-ended" class="hidden flex-1 flex flex-col items-center justify-center p-4 gap-4 bg-slate-100">
        <p class="text-sm text-slate-600">${t('terminal.sessionEnded')}</p>
        <button id="term-back-from-ended" type="button" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm">${t('terminal.backHome')}</button>
      </div>
      <div id="term-credentials" class="flex-1 flex items-center justify-center p-4 bg-slate-100">
        <form class="w-full max-w-md bg-white rounded-lg shadow-sm border border-slate-200 overflow-hidden">
          <div class="px-5 py-5 space-y-5">
            <p id="term-auth-prompt" class="text-sm text-slate-600">${t('terminal.authPrompt', { auth: authLabel })}</p>
            <p id="term-stored-cred-hint" class="text-sm text-slate-600 hidden">${t('terminal.storedCredHint')}</p>
            <p id="term-needs-password-hint" class="text-sm text-slate-600 hidden">${t('terminal.needsPasswordHint')}</p>
            <p id="term-needs-passphrase-hint" class="text-sm text-slate-600 hidden">${t('terminal.needsPassphraseHint')}</p>
            <div id="term-auth-fields" class="space-y-5">
              <div id="term-username-wrap">
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('terminal.fieldUsername', { auth: authLabel })}</label>
                <input type="text" id="ssh-username" autocomplete="username" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('terminal.fieldUsernamePlaceholder')}" />
              </div>
              <div id="term-password-wrap">
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('terminal.fieldPassword', { auth: authLabel })}</label>
                <input type="password" id="ssh-password" autocomplete="current-password" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" />
              </div>
              <div id="term-passphrase-wrap" class="hidden">
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('terminal.fieldPassphrase')}</label>
                <input type="password" id="ssh-passphrase" autocomplete="off" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('terminal.fieldPassphrasePlaceholder')}" />
              </div>
              ${isTelnet ? '' : `<div id="term-passphrase-optional-wrap">
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('terminal.fieldPassphraseOptional')}</label>
                <input type="password" id="ssh-passphrase-optional" autocomplete="off" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('terminal.fieldPassphraseOptionalPlaceholder')}" />
              </div>`}
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('terminal.fieldSessionName')}</label>
              <input type="text" id="ssh-session-name" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('terminal.fieldSessionNamePlaceholder')}" />
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('terminal.fieldSessionDesc')}</label>
              <input type="text" id="ssh-session-desc" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('terminal.fieldSessionDescPlaceholder')}" />
            </div>
            <p id="term-error" class="text-sm text-red-600 hidden"></p>
          </div>
          <div class="px-5 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
            <button type="button" id="term-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm">${t('terminal.cancel')}</button>
            <button type="submit" id="term-connect" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm">${t('terminal.connect')}</button>
          </div>
        </form>
      </div>

      <div id="term-shell" class="hidden relative z-0 flex-1 min-h-0 flex flex-col overflow-hidden bg-[#020617]">
        <div id="xterm" class="terminal-xterm-host flex-1 min-h-0 w-full"></div>
      </div>
    </div>
  `

  const closeBtn = container.querySelector('#term-close')
  const backBtn = container.querySelector('#term-back')
  const cancelBtn = container.querySelector('#term-cancel')
  const connectBtn = container.querySelector('#term-connect')
  const errorEl = container.querySelector('#term-error')
  const credsWrap = container.querySelector('#term-credentials')
  const shellWrap = container.querySelector('#term-shell')
  const xtermEl = container.querySelector('#xterm')
  const usernameInput = container.querySelector('#ssh-username')
  const passwordInput = container.querySelector('#ssh-password')
  const passphraseInput = container.querySelector('#ssh-passphrase')
  const passphraseOptionalInput = container.querySelector('#ssh-passphrase-optional')
  const sessionNameInput = container.querySelector('#ssh-session-name')
  const sessionDescInput = container.querySelector('#ssh-session-desc')

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  /** 新規接続用 WebSocket URL。録画・PTY のサイズに使うため、ensureTerm() 実行後に呼ぶこと。 */
  function getWsUrlNew() {
    ensureTerm()
    const c = term?.cols || 80
    const r = term?.rows || 24
    return `${protocol}//${window.location.host}/ws/ssh?target_id=${encodeURIComponent(targetId)}&cols=${c}&rows=${r}`
  }
  const wsUrlResume = (sid) => `${protocol}//${window.location.host}/ws/ssh?session_id=${encodeURIComponent(sid)}`

  let term = null
  let fitAddon = null
  let resizeObserver = null
  let termStdinAttached = false

  /** WebSocket メッセージ共通処理。session_id 受信時も xterm を必ず接続する。 */
  function handleTerminalWsMessage(ws, ev, { onError, onSessionEnded } = {}) {
    if (typeof ev.data === 'string' && ev.data.startsWith('session_ended:')) {
      const msg = ev.data.slice('session_ended:'.length).trim() || t('terminal.sessionEndedSuffix')
      if (term) term.write(`\r\n\n${t('terminal.sessionEndedPrefix')} ${msg}\r\n`)
      currentSessionId = null
      shellWrap.classList.add('hidden')
      sessionEndedWrap.classList.remove('hidden')
      try { ws.close() } catch { /* ignore */ }
      onSessionEnded?.()
      return true
    }
    if (typeof ev.data === 'string' && ev.data.startsWith('error:')) {
      const msg = ev.data.slice(6).trim()
      onError?.(msg)
      if (term) term.write(`\r\n\n${t('terminal.errorPrefix')} ${msg}\r\n`)
      try { ws.close() } catch { /* ignore */ }
      return true
    }
    if (typeof ev.data === 'string' && ev.data.trim().startsWith('{')) {
      try {
        const o = JSON.parse(ev.data)
        if (o && typeof o.session_id === 'string') {
          setCurrentSessionId(o.session_id)
          credsWrap.classList.add('hidden')
          shellWrap.classList.remove('hidden')
          if (!ws._vantyxAttached) {
            startXterm(ws)
            ws._vantyxAttached = true
          }
          return true
        }
      } catch {
        /* not JSON */
      }
    }
    credsWrap.classList.add('hidden')
    shellWrap.classList.remove('hidden')
    if (!ws._vantyxAttached) {
      startXterm(ws)
      ws._vantyxAttached = true
    }
    if (typeof ev.data === 'string') {
      term.write(ev.data)
    } else {
      term.write(new Uint8Array(ev.data))
    }
    return false
  }
  let currentSessionId = resumeSessionId || null
  const currentSessionIdReady = (() => {
    if (currentSessionId) return Promise.resolve(currentSessionId)
    let resolve
    const p = new Promise((r) => { resolve = r })
    p._resolve = resolve
    return p
  })()
  function setCurrentSessionId(v) {
    if (!v) return
    currentSessionId = v
    if (typeof currentSessionIdReady?._resolve === 'function') {
      try { currentSessionIdReady._resolve(v) } catch { /* ignore */ }
      currentSessionIdReady._resolve = null
    }
  }
  let leavingPage = false
  let endSessionModalEl = null
  const disconnectedWrap = container.querySelector('#term-disconnected')
  const reconnectBtn = container.querySelector('#term-reconnect')
  const backFromDisconnectBtn = container.querySelector('#term-back-from-disconnect')
  const sessionEndedWrap = container.querySelector('#term-session-ended')
  const backFromEndedBtn = container.querySelector('#term-back-from-ended')

  function teardown() {
    try { resizeObserver?.disconnect() } catch { /* ignore */ }
    resizeObserver = null
    try { term?.dispose() } catch { /* ignore */ }
    term = null
    fitAddon = null
  }

  function closeWindow() {
    teardown()
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

  function closeEndSessionModal() {
    if (!endSessionModalEl) return
    try { endSessionModalEl.remove() } catch { /* ignore */ }
    endSessionModalEl = null
  }

  function showEndSessionConfirmModal() {
    closeEndSessionModal()
    const wrap = document.createElement('div')
    wrap.className = 'fixed inset-0 z-[200] flex items-center justify-center bg-black/40 backdrop-blur-sm p-4'
    wrap.innerHTML = `
      <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
        <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
          <h3 class="font-semibold text-slate-800">${t('terminal.confirmEndTitle')}</h3>
          <button type="button" data-end-session-close="1" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
        </div>
        <div class="px-6 py-5 space-y-4">
          <p class="text-sm text-slate-700">${t('terminal.confirmEndQuestion')}</p>
          <p class="text-xs text-slate-500">${t('terminal.confirmEndHint')}</p>
          <p data-end-session-error="1" class="text-sm text-red-600 hidden"></p>
        </div>
        <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
          <button type="button" data-end-session-cancel="1" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm">${t('terminal.cancel')}</button>
          <button type="button" data-end-session-confirm="1" class="rounded bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700 shadow-sm">${t('terminal.endSessionBtn')}</button>
        </div>
      </div>
    `
    document.body.appendChild(wrap)
    endSessionModalEl = wrap

    const close = () => {
      leavingPage = false
      closeEndSessionModal()
    }
    wrap.addEventListener('click', (e) => {
      if (e.target === wrap) close()
    })
    wrap.querySelector('[data-end-session-close="1"]').addEventListener('click', close)
    wrap.querySelector('[data-end-session-cancel="1"]').addEventListener('click', close)
    wrap.querySelector('[data-end-session-confirm="1"]').addEventListener('click', async () => {
      const errEl = wrap.querySelector('[data-end-session-error="1"]')
      errEl.classList.add('hidden')
      const btn = wrap.querySelector('[data-end-session-confirm="1"]')
      btn.disabled = true
      try {
        if (!currentSessionId) {
          await Promise.race([
            currentSessionIdReady,
            new Promise((_, rej) => setTimeout(() => rej(new Error(t('terminal.sessionIdTimeout'))), 5000)),
          ])
        }
        if (!currentSessionId) throw new Error(t('terminal.sessionIdMissing'))
        leavingPage = true
        await API.terminalSessionDelete(currentSessionId)
        closeWindow()
      } catch (err) {
        leavingPage = false
        errEl.textContent = err?.message || t('terminal.endSessionFailedRetry')
        errEl.classList.remove('hidden')
        btn.disabled = false
      }
    })
  }

  // 戻る: セッションは維持したままホームへ（親タブがあれば戻してこのタブを閉じる）
  backBtn.addEventListener('click', () => {
    leavingPage = true
    if (window.opener && !window.opener.closed) {
      closeWindow()
      return
    }
    if (parentToken) {
      closeWindow()
      return
    }
    teardown()
    window.location.href = '/'
  })

  // セッション終了: 確認モーダルを表示し、OK のときだけセッションを終了して閉じる
  closeBtn.addEventListener('click', () => {
    showEndSessionConfirmModal()
  })
  cancelBtn.addEventListener('click', () => { window.location.href = '/' })
  // ホームに戻る: 親タブから開かれていればそのタブにフォーカスしてこのタブを閉じる。そうでなければこのタブでホームへ遷移する。
  function goHomeOrCloseToOpener() {
    leavingPage = true
    teardown()
    if (window.opener && !window.opener.closed) {
      try { window.opener.focus() } catch { /* ignore */ }
      try { window.close() } catch { /* ignore */ }
    } else if (parentToken) {
      try {
        const bc = new BroadcastChannel(`vantyx-terminal-parent-${parentToken}`)
        try { bc.postMessage({ type: 'focus', refresh: 'active_sessions' }) } finally { bc.close() }
      } catch { /* ignore */ }
      try { window.close() } catch { /* ignore */ }
    } else {
      window.location.href = '/'
    }
  }
  backFromDisconnectBtn.addEventListener('click', goHomeOrCloseToOpener)
  backFromEndedBtn.addEventListener('click', goHomeOrCloseToOpener)

  function connectResume(sessionId) {
    if (!sessionId) return
    credsWrap.classList.add('hidden')
    shellWrap.classList.remove('hidden')
    errorEl.classList.add('hidden')
    disconnectedWrap.classList.add('hidden')
    const ws = new WebSocket(wsUrlResume(sessionId))
    ws.binaryType = 'arraybuffer'
    ws.onmessage = (ev) => {
      handleTerminalWsMessage(ws, ev, {
        onError: (msg) => {
          errorEl.textContent = msg
          errorEl.classList.remove('hidden')
          shellWrap.classList.add('hidden')
          credsWrap.classList.remove('hidden')
        },
      })
    }
    ws.onerror = () => {
      errorEl.textContent = t('terminal.wsConnectFailedShort')
      errorEl.classList.remove('hidden')
    }
    ws.onclose = () => {
      if (leavingPage) return
      if (term) term.write(`\r\n\n${t('terminal.connectionClosed')}\r\n`)
      if (currentSessionId) {
        shellWrap.classList.add('hidden')
        disconnectedWrap.classList.remove('hidden')
      }
    }
  }

  reconnectBtn.addEventListener('click', () => {
    if (!currentSessionId) return
    disconnectedWrap.classList.add('hidden')
    shellWrap.classList.remove('hidden')
    connectResume(currentSessionId)
  })

  function connectWithCredentials(username, password, sessionName, sessionDescription, privateKeyPassphrase) {
    if (!targetId) {
      errorEl.textContent = t('terminal.targetIdMissingFromHome')
      errorEl.classList.remove('hidden')
      return
    }
    if (!username || !username.trim()) {
      errorEl.textContent = t('terminal.usernameRequiredShort')
      errorEl.classList.remove('hidden')
      return
    }
    const user = username.trim()
    const name = typeof sessionName === 'string' ? sessionName.trim() : ''
    const description = typeof sessionDescription === 'string' ? sessionDescription.trim() : ''
    errorEl.classList.add('hidden')
    connectBtn.disabled = true
    credsWrap.classList.add('hidden')
    shellWrap.classList.remove('hidden')

    const ws = new WebSocket(getWsUrlNew())
    ws.binaryType = 'arraybuffer'
    let sawError = false
    let sawFirstMessage = false
    const connectTimeout = window.setTimeout(() => {
      if (sawFirstMessage) return
      sawError = true
      errorEl.textContent = t('terminal.connectTimedOutNew')
      errorEl.classList.remove('hidden')
      connectBtn.disabled = false
      credsWrap.classList.remove('hidden')
      shellWrap.classList.add('hidden')
      try { ws.close() } catch { /* ignore */ }
    }, 20000)

    ws.onopen = () => {
      const payload = { username: user, password: password || '', name, description }
      if (!isTelnet && privateKeyPassphrase != null && privateKeyPassphrase !== '') payload.private_key_passphrase = privateKeyPassphrase
      ws.send(JSON.stringify(payload))
    }

    ws.onmessage = (ev) => {
      sawFirstMessage = true
      window.clearTimeout(connectTimeout)
      handleTerminalWsMessage(ws, ev, {
        onError: (msg) => {
          sawError = true
          errorEl.textContent = msg
          errorEl.classList.remove('hidden')
          connectBtn.disabled = false
          credsWrap.classList.remove('hidden')
          shellWrap.classList.add('hidden')
        },
      })
    }

    ws.onerror = () => {
      sawError = true
      errorEl.textContent = t('terminal.wsConnectFailed')
      errorEl.classList.remove('hidden')
      connectBtn.disabled = false
      credsWrap.classList.remove('hidden')
      shellWrap.classList.add('hidden')
    }

    ws.onclose = () => {
      if (leavingPage) return
      window.clearTimeout(connectTimeout)
      if (term) term.write(`\r\n\n${t('terminal.connectionClosed')}\r\n`)
      connectBtn.disabled = false
      if (!sawFirstMessage && !sawError) {
        sawError = true
        errorEl.textContent = t('terminal.closedNoFirstNew')
        errorEl.classList.remove('hidden')
        credsWrap.classList.remove('hidden')
        shellWrap.classList.add('hidden')
      } else if (currentSessionId) {
        shellWrap.classList.add('hidden')
        disconnectedWrap.classList.remove('hidden')
      } else if (channelToken && !sawError) {
        window.setTimeout(() => closeWindow(), 400)
      }
    }
  }

  function connectWithStoredCredentials(sessionName, sessionDescription, password, privateKeyPassphrase) {
    if (!targetId) return
    errorEl.classList.add('hidden')
    credsWrap.classList.add('hidden')
    shellWrap.classList.remove('hidden')
    const name = typeof sessionName === 'string' ? sessionName.trim() : ''
    const description = typeof sessionDescription === 'string' ? sessionDescription.trim() : ''
    const payload = { use_stored_credentials: true, name, description }
    if (password != null && password !== '') payload.password = password
    if (!isTelnet && privateKeyPassphrase != null && privateKeyPassphrase !== '') payload.private_key_passphrase = privateKeyPassphrase

    const ws = new WebSocket(getWsUrlNew())
    ws.binaryType = 'arraybuffer'
    let sawError = false
    let sawFirstMessage = false
    const connectTimeout = window.setTimeout(() => {
      if (sawFirstMessage) return
      sawError = true
      errorEl.textContent = t('terminal.connectTimedOutStored')
      errorEl.classList.remove('hidden')
      credsWrap.classList.remove('hidden')
      shellWrap.classList.add('hidden')
      try { ws.close() } catch { /* ignore */ }
    }, 20000)

    ws.onopen = () => {
      ws.send(JSON.stringify(payload))
    }

    ws.onmessage = (ev) => {
      sawFirstMessage = true
      window.clearTimeout(connectTimeout)
      handleTerminalWsMessage(ws, ev, {
        onError: (msg) => {
          sawError = true
          errorEl.textContent = msg
          errorEl.classList.remove('hidden')
          credsWrap.classList.remove('hidden')
          shellWrap.classList.add('hidden')
        },
      })
    }

    ws.onerror = () => {
      sawError = true
      errorEl.textContent = t('terminal.wsConnectFailedStored')
      errorEl.classList.remove('hidden')
      credsWrap.classList.remove('hidden')
      shellWrap.classList.add('hidden')
    }

    ws.onclose = () => {
      if (leavingPage) return
      window.clearTimeout(connectTimeout)
      if (term) term.write(`\r\n\n${t('terminal.connectionClosed')}\r\n`)
      if (!sawFirstMessage && !sawError) {
        sawError = true
        errorEl.textContent = t('terminal.closedNoFirstStored')
        errorEl.classList.remove('hidden')
        credsWrap.classList.remove('hidden')
        shellWrap.classList.add('hidden')
      } else if (currentSessionId) {
        shellWrap.classList.add('hidden')
        disconnectedWrap.classList.remove('hidden')
      }
    }
  }

  let waitingBroadcastCreds = false

  container.querySelector('form').addEventListener('submit', (e) => {
    e.preventDefault()
    if (waitingBroadcastCreds) {
      errorEl.textContent = t('terminal.waitingCredsBlocked')
      errorEl.classList.remove('hidden')
      return
    }
    const sessionName = sessionNameInput?.value?.trim() ?? ''
    const sessionDesc = sessionDescInput?.value?.trim() ?? ''
    if (useStoredCredentials && targetId) {
      const password = needsPassword ? (passwordInput?.value ?? '') : ''
      const passphrase = needsPassphrase ? (passphraseInput?.value ?? '') : ''
      if (needsPassword && !password) {
        errorEl.textContent = t('terminal.enterPasswordPrompt')
        errorEl.classList.remove('hidden')
        return
      }
      if (needsPassphrase && !passphrase) {
        errorEl.textContent = t('terminal.enterPassphrasePrompt')
        errorEl.classList.remove('hidden')
        return
      }
      credsWrap.classList.add('hidden')
      shellWrap.classList.remove('hidden')
      connectWithStoredCredentials(sessionName, sessionDesc, password, passphrase)
      return
    }
    const username = usernameInput.value.trim()
    const password = passwordInput.value
      const passphraseOptional = isTelnet ? '' : (passphraseOptionalInput?.value ?? '')
      connectWithCredentials(username, password, sessionName, sessionDesc, passphraseOptional)
  })

  // session_id がある場合は既存セッションへアタッチ（target_id はヘッダ表示用に付くことがある）
  if (resumeSessionId) {
    credsWrap.classList.add('hidden')
    shellWrap.classList.remove('hidden')
    connectResume(resumeSessionId)
  } else {
    // 保存済み認証: 以前は localStorage で不足分（パスワード/パスフレーズ）を受け渡ししていたが、
    // 機密情報をブラウザ永続ストレージに残さないため BroadcastChannel に統一した。
    let usedPendingCreds = false

    // 保存済み認証: 上で即接続していない場合、パスワード/パスフレーズが必要な場合はフォーム表示。不要かつ URL でセッション名・説明があれば即接続
    const needsExtraCreds = useStoredCredentials && targetId && (needsPassword || needsPassphrase)
    if (usedPendingCreds) {
      // すでに connectWithStoredCredentials を呼んだ
    } else if (useStoredCredentials && targetId && hasSessionParamsFromUrl && !needsPassword && !needsPassphrase && !channelToken) {
      credsWrap.classList.add('hidden')
      shellWrap.classList.remove('hidden')
      connectWithStoredCredentials(urlSessionName, urlSessionDesc)
    } else if (needsExtraCreds) {
      const authPrompt = container.querySelector('#term-auth-prompt')
      const storedCredHint = container.querySelector('#term-stored-cred-hint')
      const needsPasswordHint = container.querySelector('#term-needs-password-hint')
      const needsPassphraseHint = container.querySelector('#term-needs-passphrase-hint')
      const usernameWrap = container.querySelector('#term-username-wrap')
      const passwordWrap = container.querySelector('#term-password-wrap')
      const passphraseWrap = container.querySelector('#term-passphrase-wrap')
      const passphraseOptionalWrap = container.querySelector('#term-passphrase-optional-wrap')
      if (authPrompt) authPrompt.classList.add('hidden')
      if (storedCredHint) storedCredHint.classList.remove('hidden')
      if (needsPassword && needsPasswordHint) needsPasswordHint.classList.remove('hidden')
      if (needsPassphrase && needsPassphraseHint) needsPassphraseHint.classList.remove('hidden')
      if (usernameWrap) usernameWrap.classList.add('hidden')
      if (passwordWrap) passwordWrap.classList.toggle('hidden', !needsPassword)
      if (passphraseWrap) passphraseWrap.classList.toggle('hidden', !needsPassphrase)
      if (passphraseOptionalWrap) passphraseOptionalWrap.classList.add('hidden')
    } else if (useStoredCredentials && targetId) {
      const authPrompt = container.querySelector('#term-auth-prompt')
      const authFields = container.querySelector('#term-auth-fields')
      const storedCredHint = container.querySelector('#term-stored-cred-hint')
      if (authPrompt) authPrompt.classList.add('hidden')
      if (authFields) authFields.classList.add('hidden')
      if (storedCredHint) storedCredHint.classList.remove('hidden')
    }

    // 親タブから開かれた場合、BroadcastChannel 経由で認証情報を受け取り自動接続する（noopener でも動く）
    if (channelToken && targetId) {
    const infoEl = document.createElement('p')
    infoEl.className = 'text-xs text-slate-500'
    infoEl.textContent = t('terminal.parentTabReceiving')
    container.querySelector('#term-credentials .px-5')?.appendChild(infoEl)

    // 認証情報受信中は「送信」させない（Enter 送信やブラウザの自動入力で誤接続しないようにする）
    waitingBroadcastCreds = true
    connectBtn.disabled = true
    if (!useStoredCredentials) {
      usernameInput.value = ''
      passwordInput.value = ''
      usernameInput.readOnly = true
      passwordInput.readOnly = true
    }

    const bc = new BroadcastChannel(`vantyx-terminal-${channelToken}`)
    const timeoutId = window.setTimeout(() => {
      try { bc.close() } catch { /* ignore */ }
      waitingBroadcastCreds = false
      connectBtn.disabled = false
      if (!useStoredCredentials) {
        usernameInput.readOnly = false
        passwordInput.readOnly = false
      }
      infoEl.textContent = t('terminal.parentTabTimeout')
    }, 10_000)

    bc.onmessage = (ev) => {
      const typ = ev?.data?.type
      if (typ !== 'credentials' && typ !== 'stored_credentials') return
      window.clearTimeout(timeoutId)
      try { bc.close() } catch { /* ignore */ }
      waitingBroadcastCreds = false
      const name = typeof ev.data.name === 'string' ? ev.data.name : ''
      const desc = typeof ev.data.description === 'string' ? ev.data.description : ''
      if (sessionNameInput) sessionNameInput.value = name
      if (sessionDescInput) sessionDescInput.value = desc

      if (typ === 'stored_credentials') {
        const p = ev.data.password != null ? ev.data.password : ''
        const passphrase = typeof ev.data.private_key_passphrase === 'string' ? ev.data.private_key_passphrase : ''
        infoEl.textContent = t('terminal.parentTabReceived')
        usedPendingCreds = true
        credsWrap.classList.add('hidden')
        shellWrap.classList.remove('hidden')
        connectWithStoredCredentials(name, desc, typeof p === 'string' ? p : '', isTelnet ? '' : passphrase)
        return
      }

      // typ === 'credentials' (username/password required)
      const u = ev.data.username
      const p = ev.data.password != null ? ev.data.password : ''
      usernameInput.readOnly = false
      passwordInput.readOnly = false
      usernameInput.value = typeof u === 'string' ? u : ''
      passwordInput.value = typeof p === 'string' ? p : ''
      const passphrase = typeof ev.data.private_key_passphrase === 'string' ? ev.data.private_key_passphrase : ''
      if (!isTelnet && passphraseOptionalInput) passphraseOptionalInput.value = passphrase

      if (!usernameInput.value.trim()) {
        connectBtn.disabled = false
        infoEl.textContent = t('terminal.parentTabReceivedNoUser')
        return
      }
      if (!passwordInput.value) {
        connectBtn.disabled = false
        infoEl.textContent = t('terminal.parentTabReceivedNoPassword', { user: usernameInput.value.trim() })
        return
      }

      infoEl.textContent = t('terminal.parentTabReceived')
      connectWithCredentials(usernameInput.value, passwordInput.value, name, desc, passphrase)
    }

    try {
      bc.postMessage({ type: 'ready', target_id: targetId })
    } catch {
      window.clearTimeout(timeoutId)
      try { bc.close() } catch { /* ignore */ }
      connectBtn.disabled = false
    }
    }
  }

  /** ターミナルが無ければ作成して fit。録画・PTY の初期サイズに必要なので、WebSocket 接続前に呼ぶ。 */
  function ensureTerm() {
    if (term) return
    term = new Terminal({
      cursorBlink: true,
      theme: { background: '#020617', foreground: '#e2e8f0' },
      fontFamily: 'ui-monospace, monospace',
      allowProposedApi: false,
    })
    fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.loadAddon(new WebLinksAddon())
    term.open(xtermEl)
    fitAddon.fit()
  }

  /** 既存の term に WebSocket を接続（onData, resize 送信）。ensureTerm の後に呼ぶ。 */
  let currentWs = null
  function attachWsToTerm(ws) {
    currentWs = ws
    term.focus()
    sendResize(ws)
    if (!termStdinAttached) {
      termStdinAttached = true
      term.onData((data) => {
        if (currentWs && currentWs.readyState === WebSocket.OPEN) {
          currentWs.send(new TextEncoder().encode(data))
        }
      })
    }
    if (!resizeObserver) {
      resizeObserver = new ResizeObserver(() => {
        try { fitAddon.fit() } catch { /* ignore */ }
        if (currentWs && currentWs.readyState === WebSocket.OPEN) sendResize(currentWs)
      })
      resizeObserver.observe(xtermEl)
    }
    window.addEventListener('beforeunload', () => {
      try { ws.close() } catch { /* ignore */ }
    }, { once: true })
  }

  function startXterm(ws) {
    ensureTerm()
    attachWsToTerm(ws)
    if (isTelnet && term && !term._vantyxTelnetHint) {
      term._vantyxTelnetHint = true
      term.write(`\r\n\x1b[33m${t('terminal.telnetHint')}\x1b[0m\r\n`)
    }
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
}

