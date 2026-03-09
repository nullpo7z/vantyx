import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'

function escapeHtml(s) {
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}

export function renderTerminalPage(container) {
  const params = new URLSearchParams(window.location.search)
  const targetId = params.get('target_id') || ''
  const targetName = params.get('target_name') || targetId || 'terminal'
  const channelToken = params.get('channel') || ''
  const resumeSessionId = params.get('session_id') || ''
  const useStoredCredentials = params.get('use_stored_credentials') === '1'
  const needsPassword = params.get('needs_password') === '1'
  const needsPassphrase = params.get('needs_passphrase') === '1'
  const urlSessionName = params.get('session_name') ?? ''
  const urlSessionDesc = params.get('session_description') ?? ''
  const hasSessionParamsFromUrl = params.has('session_name') || params.has('session_description')

  container.innerHTML = `
    <div class="min-h-screen w-screen flex flex-col bg-slate-950">
      <header class="shrink-0 px-4 sm:px-6 py-3 bg-slate-900 border-b border-slate-800 flex items-center justify-between">
        <div class="min-w-0">
          <div class="text-xs text-slate-400">Vantyx Console</div>
          <div class="text-sm sm:text-base font-semibold text-slate-100 truncate">${escapeHtml(targetName)}</div>
        </div>
        <div class="flex items-center gap-2">
          <button id="term-back" type="button" class="rounded border border-slate-700 bg-slate-900 px-3 py-1.5 text-xs font-semibold text-slate-200 hover:bg-slate-800">戻る</button>
          <button id="term-close" class="rounded bg-slate-800 px-3 py-1.5 text-xs font-semibold text-slate-100 hover:bg-slate-700">閉じる</button>
        </div>
      </header>

      <div id="term-disconnected" class="hidden flex-1 flex flex-col items-center justify-center p-4 gap-4 bg-slate-950">
        <p class="text-slate-300">セッションはバックエンドで継続しています。</p>
        <button id="term-reconnect" type="button" class="rounded bg-sky-600 px-4 py-2 text-sm font-medium text-white hover:bg-sky-700">再接続</button>
        <button id="term-back-from-disconnect" type="button" class="rounded border border-slate-600 px-4 py-2 text-sm text-slate-300 hover:bg-slate-800">ホームに戻る</button>
      </div>
      <div id="term-session-ended" class="hidden flex-1 flex flex-col items-center justify-center p-4 gap-4 bg-slate-950">
        <p class="text-slate-300">セッションが終了しました。サーバー側でログアウトしたため、再接続はできません。</p>
        <button id="term-back-from-ended" type="button" class="rounded border border-slate-600 px-4 py-2 text-sm text-slate-300 hover:bg-slate-800">ホームに戻る</button>
      </div>
      <div id="term-credentials" class="flex-1 flex items-center justify-center p-4">
        <form class="w-full max-w-md bg-white rounded-lg shadow-xl border border-slate-200 overflow-hidden">
          <div class="px-5 py-5 space-y-5">
            <p id="term-auth-prompt" class="text-sm text-slate-600">ターゲットの SSH 認証情報を入力してください。</p>
            <p id="term-stored-cred-hint" class="text-sm text-slate-600 hidden">セッション名と説明を入力してください（任意）。接続で保存済み認証を使って接続します。</p>
            <p id="term-needs-password-hint" class="text-sm text-slate-600 hidden">ユーザー名は保存済みです。パスワードを入力してください。</p>
            <p id="term-needs-passphrase-hint" class="text-sm text-slate-600 hidden">秘密鍵は保存済みです。パスフレーズを入力してください。</p>
            <div id="term-auth-fields" class="space-y-5">
              <div id="term-username-wrap">
                <label class="block text-xs font-medium text-slate-600 mb-1.5">SSH ユーザー名</label>
                <input type="text" id="ssh-username" autocomplete="username" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: root" />
              </div>
              <div id="term-password-wrap">
                <label class="block text-xs font-medium text-slate-600 mb-1.5">SSH パスワード</label>
                <input type="password" id="ssh-password" autocomplete="current-password" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" />
              </div>
              <div id="term-passphrase-wrap" class="hidden">
                <label class="block text-xs font-medium text-slate-600 mb-1.5">秘密鍵のパスフレーズ</label>
                <input type="password" id="ssh-passphrase" autocomplete="off" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="暗号化された秘密鍵のパスフレーズ" />
              </div>
              <div id="term-passphrase-optional-wrap">
                <label class="block text-xs font-medium text-slate-600 mb-1.5">秘密鍵のパスフレーズ（任意）</label>
                <input type="password" id="ssh-passphrase-optional" autocomplete="off" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="保存済み鍵が暗号化されている場合のみ入力" />
              </div>
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">セッション名（任意）</label>
              <input type="text" id="ssh-session-name" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: 本番デプロイ用" />
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">説明（任意）</label>
              <input type="text" id="ssh-session-desc" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: リリース作業用" />
            </div>
            <p id="term-error" class="text-sm text-red-600 hidden"></p>
          </div>
          <div class="px-5 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
            <button type="button" id="term-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50">キャンセル</button>
            <button type="submit" id="term-connect" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700">接続</button>
          </div>
        </form>
      </div>

      <div id="term-shell" class="hidden flex-1 min-h-0 flex flex-col">
        <div id="xterm" class="flex-1 min-h-0"></div>
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
  let currentSessionId = resumeSessionId || null
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
    }
    try { window.close() } catch { /* ignore */ }
  }

  // 戻る: セッションは維持したままホームへ（WebSocket はページ離脱で切断され、バックエンドのセッションは継続）
  backBtn.addEventListener('click', () => {
    teardown()
    window.location.href = '/'
  })

  // 閉じる: セッションを終了してタブを閉じ、元のタブにフォーカスを戻す
  closeBtn.addEventListener('click', async () => {
    if (currentSessionId) {
      try {
        const API = (await import('./api.js')).default
        await API.terminalSessionDelete(currentSessionId)
      } catch {
        /* 失敗してもタブは閉じる */
      }
    }
    closeWindow()
  })
  cancelBtn.addEventListener('click', () => { window.location.href = '/' })
  backFromDisconnectBtn.addEventListener('click', () => { window.location.href = '/' })
  backFromEndedBtn.addEventListener('click', () => { window.location.href = '/' })

  function connectResume(sessionId) {
    if (!sessionId) return
    credsWrap.classList.add('hidden')
    shellWrap.classList.remove('hidden')
    errorEl.classList.add('hidden')
    disconnectedWrap.classList.add('hidden')
    const ws = new WebSocket(wsUrlResume(sessionId))
    ws.binaryType = 'arraybuffer'
    ws.onmessage = (ev) => {
      if (typeof ev.data === 'string' && ev.data.startsWith('session_ended:')) {
        const msg = ev.data.slice('session_ended:'.length).trim() || 'セッションが終了しました'
        if (term) term.write('\r\n\n[セッション終了] ' + msg + '\r\n')
        currentSessionId = null
        shellWrap.classList.add('hidden')
        sessionEndedWrap.classList.remove('hidden')
        try { ws.close() } catch { /* ignore */ }
        return
      }
      if (typeof ev.data === 'string' && ev.data.startsWith('error:')) {
        errorEl.textContent = ev.data.slice(6).trim()
        errorEl.classList.remove('hidden')
        shellWrap.classList.add('hidden')
        credsWrap.classList.remove('hidden')
        try { ws.close() } catch { /* ignore */ }
        return
      }
      if (!ws._vantyxAttached) {
        startXterm(ws)
        ws._vantyxAttached = true
      }
      if (typeof ev.data === 'string') {
        term.write(ev.data)
      } else {
        term.write(new Uint8Array(ev.data))
      }
    }
    ws.onerror = () => {
      errorEl.textContent = 'WebSocket 接続に失敗しました。'
      errorEl.classList.remove('hidden')
    }
    ws.onclose = () => {
      if (term) term.write('\r\n\n[接続が閉じられました]\r\n')
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
      errorEl.textContent = 'target_id が指定されていません。ホーム画面から開き直してください。'
      errorEl.classList.remove('hidden')
      return
    }
    if (!username || !username.trim()) {
      errorEl.textContent = 'ユーザー名を入力してください'
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
      errorEl.textContent = '接続がタイムアウトしました。ターゲットに到達できるか、認証情報を確認してください。'
      errorEl.classList.remove('hidden')
      connectBtn.disabled = false
      credsWrap.classList.remove('hidden')
      shellWrap.classList.add('hidden')
      try { ws.close() } catch { /* ignore */ }
    }, 20000)

    ws.onopen = () => {
      const payload = { username: user, password: password || '', name, description }
      if (privateKeyPassphrase != null && privateKeyPassphrase !== '') payload.private_key_passphrase = privateKeyPassphrase
      ws.send(JSON.stringify(payload))
    }

    ws.onmessage = (ev) => {
      sawFirstMessage = true
      window.clearTimeout(connectTimeout)
      if (typeof ev.data === 'string' && ev.data.startsWith('session_ended:')) {
        const msg = ev.data.slice('session_ended:'.length).trim() || 'セッションが終了しました'
        if (term) term.write('\r\n\n[セッション終了] ' + msg + '\r\n')
        currentSessionId = null
        shellWrap.classList.add('hidden')
        sessionEndedWrap.classList.remove('hidden')
        try { ws.close() } catch { /* ignore */ }
        return
      }
      if (typeof ev.data === 'string' && ev.data.startsWith('error:')) {
        const msg = ev.data.slice(6).trim()
        sawError = true
        errorEl.textContent = msg
        errorEl.classList.remove('hidden')
        connectBtn.disabled = false
        // ターミナル表示に切り替わった後でもエラーを見せるため、認証パネルを再表示する
        credsWrap.classList.remove('hidden')
        shellWrap.classList.add('hidden')
        if (term) term.write('\r\n\n[エラー] ' + msg + '\r\n')
        try { ws.close() } catch { /* ignore */ }
        return
      }
      // Backend sends session_id as JSON after connect (for resume).
      if (typeof ev.data === 'string' && ev.data.trim().startsWith('{')) {
        try {
          const o = JSON.parse(ev.data)
          if (o && typeof o.session_id === 'string') {
            currentSessionId = o.session_id
            return
          }
        } catch {
          /* not JSON, fall through to term.write */
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
    }

    ws.onerror = () => {
      sawError = true
      errorEl.textContent = 'WebSocket 接続に失敗しました。バックエンドが起動しているか確認してください。'
      errorEl.classList.remove('hidden')
      connectBtn.disabled = false
      credsWrap.classList.remove('hidden')
      shellWrap.classList.add('hidden')
    }

    ws.onclose = () => {
      window.clearTimeout(connectTimeout)
      if (term) term.write('\r\n\n[接続が閉じられました]\r\n')
      connectBtn.disabled = false
      if (!sawFirstMessage && !sawError) {
        sawError = true
        errorEl.textContent = '接続が閉じられました。ターゲット・ネットワーク・認証情報を確認してください。'
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
    if (privateKeyPassphrase != null && privateKeyPassphrase !== '') payload.private_key_passphrase = privateKeyPassphrase

    const ws = new WebSocket(getWsUrlNew())
    ws.binaryType = 'arraybuffer'
    let sawError = false
    let sawFirstMessage = false
    const connectTimeout = window.setTimeout(() => {
      if (sawFirstMessage) return
      sawError = true
      errorEl.textContent = '接続がタイムアウトしました。ターゲットに到達できるか、保存済み認証情報を確認してください。'
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
      if (typeof ev.data === 'string' && ev.data.startsWith('session_ended:')) {
        const msg = ev.data.slice('session_ended:'.length).trim() || 'セッションが終了しました'
        if (term) term.write('\r\n\n[セッション終了] ' + msg + '\r\n')
        currentSessionId = null
        shellWrap.classList.add('hidden')
        sessionEndedWrap.classList.remove('hidden')
        try { ws.close() } catch { /* ignore */ }
        return
      }
      if (typeof ev.data === 'string' && ev.data.startsWith('error:')) {
        const msg = ev.data.slice(6).trim()
        sawError = true
        errorEl.textContent = msg
        errorEl.classList.remove('hidden')
        credsWrap.classList.remove('hidden')
        shellWrap.classList.add('hidden')
        if (term) term.write('\r\n\n[エラー] ' + msg + '\r\n')
        try { ws.close() } catch { /* ignore */ }
        return
      }
      if (typeof ev.data === 'string' && ev.data.trim().startsWith('{')) {
        try {
          const o = JSON.parse(ev.data)
          if (o && typeof o.session_id === 'string') {
            currentSessionId = o.session_id
            return
          }
        } catch {
          /* ignore */
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
    }

    ws.onerror = () => {
      sawError = true
      errorEl.textContent = 'WebSocket 接続に失敗しました。保存済み認証で接続できない場合は、認証情報の入力をお試しください。'
      errorEl.classList.remove('hidden')
      credsWrap.classList.remove('hidden')
      shellWrap.classList.add('hidden')
    }

    ws.onclose = () => {
      window.clearTimeout(connectTimeout)
      if (term) term.write('\r\n\n[接続が閉じられました]\r\n')
      if (!sawFirstMessage && !sawError) {
        sawError = true
        errorEl.textContent = '接続が閉じられました。ターゲット・ネットワーク・保存済み認証情報を確認してください。'
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
      errorEl.textContent = '認証情報を受信中です。少し待ってください。'
      errorEl.classList.remove('hidden')
      return
    }
    const sessionName = sessionNameInput?.value?.trim() ?? ''
    const sessionDesc = sessionDescInput?.value?.trim() ?? ''
    if (useStoredCredentials && targetId) {
      const password = needsPassword ? (passwordInput?.value ?? '') : ''
      const passphrase = needsPassphrase ? (passphraseInput?.value ?? '') : ''
      if (needsPassword && !password) {
        errorEl.textContent = 'パスワードを入力してください。'
        errorEl.classList.remove('hidden')
        return
      }
      if (needsPassphrase && !passphrase) {
        errorEl.textContent = '秘密鍵のパスフレーズを入力してください。'
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
    const passphraseOptional = passphraseOptionalInput?.value ?? ''
    connectWithCredentials(username, password, sessionName, sessionDesc, passphraseOptional)
  })

  // session_id のみで開いた場合（レジューム用リンク）は認証なしで再接続
  if (resumeSessionId && !targetId) {
    credsWrap.classList.add('hidden')
    shellWrap.classList.remove('hidden')
    connectResume(resumeSessionId)
  }

  // 保存済み認証: モーダルから渡されたパスワード/パスフレーズがあれば即接続
  let usedPendingCreds = false
  if (useStoredCredentials && targetId) {
    try {
      const key = `vantyx_terminal_pending_${targetId}`
      const raw = localStorage.getItem(key)
      if (raw) {
        localStorage.removeItem(key)
        const pending = JSON.parse(raw)
        if (pending && (pending.password !== undefined || pending.private_key_passphrase !== undefined)) {
          credsWrap.classList.add('hidden')
          shellWrap.classList.remove('hidden')
          connectWithStoredCredentials(
            urlSessionName,
            urlSessionDesc,
            pending.password || '',
            pending.private_key_passphrase || ''
          )
          usedPendingCreds = true
        }
      }
    } catch (_) { /* ignore */ }
  }

  // 保存済み認証: 上で即接続していない場合、パスワード/パスフレーズが必要な場合はフォーム表示。不要かつ URL でセッション名・説明があれば即接続
  const needsExtraCreds = useStoredCredentials && targetId && (needsPassword || needsPassphrase)
  if (usedPendingCreds) {
    // すでに connectWithStoredCredentials を呼んだ
  } else if (useStoredCredentials && targetId && hasSessionParamsFromUrl && !needsPassword && !needsPassphrase) {
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
  if (channelToken && targetId && !useStoredCredentials) {
    const infoEl = document.createElement('p')
    infoEl.className = 'text-xs text-slate-500'
    infoEl.textContent = '親タブから認証情報を受信中…（数秒かかる場合があります）'
    container.querySelector('#term-credentials .px-5')?.appendChild(infoEl)

    // 認証情報受信中は「送信」させない（Enter 送信やブラウザの自動入力で誤接続しないようにする）
    waitingBroadcastCreds = true
    connectBtn.disabled = true
    usernameInput.value = ''
    passwordInput.value = ''
    usernameInput.readOnly = true
    passwordInput.readOnly = true

    const bc = new BroadcastChannel(`vantyx-terminal-${channelToken}`)
    const timeoutId = window.setTimeout(() => {
      try { bc.close() } catch { /* ignore */ }
      waitingBroadcastCreds = false
      connectBtn.disabled = false
      usernameInput.readOnly = false
      passwordInput.readOnly = false
      infoEl.textContent = '認証情報を受信できませんでした。必要ならこの画面で入力して接続してください。'
    }, 10_000)

    bc.onmessage = (ev) => {
      if (ev?.data?.type !== 'credentials') return
      window.clearTimeout(timeoutId)
      try { bc.close() } catch { /* ignore */ }
      const u = ev.data.username
      const p = ev.data.password != null ? ev.data.password : ''
      waitingBroadcastCreds = false
      usernameInput.readOnly = false
      passwordInput.readOnly = false

      usernameInput.value = typeof u === 'string' ? u : ''
      passwordInput.value = typeof p === 'string' ? p : ''
      const passphrase = typeof ev.data.private_key_passphrase === 'string' ? ev.data.private_key_passphrase : ''
      if (passphraseOptionalInput) passphraseOptionalInput.value = passphrase
      const name = typeof ev.data.name === 'string' ? ev.data.name : ''
      const desc = typeof ev.data.description === 'string' ? ev.data.description : ''
      if (sessionNameInput) sessionNameInput.value = name
      if (sessionDescInput) sessionDescInput.value = desc

      if (!usernameInput.value.trim()) {
        connectBtn.disabled = false
        infoEl.textContent = '親タブから認証情報を受信しましたが、ユーザー名が空でした。入力して接続してください。'
        return
      }
      if (!passwordInput.value) {
        connectBtn.disabled = false
        infoEl.textContent = `親タブからユーザー名「${usernameInput.value.trim()}」を受信しました。パスワードが空のため、この画面で入力して接続してください。`
        return
      }

      infoEl.textContent = `親タブから認証情報を受信しました。接続中…`
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
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data))
      }
    })
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

