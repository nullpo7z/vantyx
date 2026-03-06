import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'

/* global URLSearchParams */

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

      <div id="term-credentials" class="flex-1 flex items-center justify-center p-4">
        <form class="w-full max-w-md bg-white rounded-lg shadow-xl border border-slate-200 overflow-hidden">
          <div class="px-5 py-5 space-y-5">
            <p class="text-sm text-slate-600">ターゲットの SSH 認証情報を入力してください。</p>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">SSH ユーザー名</label>
              <input type="text" id="ssh-username" autocomplete="username" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: root" />
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">SSH パスワード</label>
              <input type="password" id="ssh-password" autocomplete="current-password" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" />
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

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const wsUrl = `${protocol}//${window.location.host}/ws/ssh?target_id=${encodeURIComponent(targetId)}`

  let term = null
  let fitAddon = null
  let resizeObserver = null

  function teardown() {
    try { resizeObserver?.disconnect() } catch { /* ignore */ }
    resizeObserver = null
    try { term?.dispose() } catch { /* ignore */ }
    term = null
    fitAddon = null
  }

  function closeWindow() {
    teardown()
    // Works when opened by window.open; if blocked, user can use the tab close button.
    try { window.close() } catch { /* ignore */ }
  }

  closeBtn.addEventListener('click', closeWindow)
  backBtn.addEventListener('click', () => {
    // ホームの「接続」ボタン（別タブ）から開いた場合は、元タブへ戻すためにタブを閉じる
    if (channelToken) {
      closeWindow()
      return
    }
    window.location.href = '/'
  })
  cancelBtn.addEventListener('click', () => { window.location.href = '/' })

  function connectWithCredentials(username, password) {
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
    errorEl.classList.add('hidden')
    connectBtn.disabled = true

    const ws = new WebSocket(wsUrl)
    ws.binaryType = 'arraybuffer'
    let sawError = false

    ws.onopen = () => {
      ws.send(JSON.stringify({ username: user, password: password || '' }))
    }

    ws.onmessage = (ev) => {
      // Backend may send "error: ..." as a string (e.g. SSH connection failed).
      if (typeof ev.data === 'string' && ev.data.startsWith('error:')) {
        const msg = ev.data.slice(6).trim()
        sawError = true
        errorEl.textContent = msg
        errorEl.classList.remove('hidden')
        if (term) term.write('\r\n\n[エラー] ' + msg + '\r\n')
        connectBtn.disabled = false
        try { ws.close() } catch { /* ignore */ }
        return
      }

      if (!term) {
        credsWrap.classList.add('hidden')
        shellWrap.classList.remove('hidden')
        startXterm(ws)
      }

      if (typeof ev.data === 'string') {
        term.write(ev.data)
      } else {
        term.write(new Uint8Array(ev.data))
      }
    }

    ws.onerror = () => {
      errorEl.textContent = 'WebSocket 接続に失敗しました。バックエンドが起動しているか確認してください。'
      errorEl.classList.remove('hidden')
      connectBtn.disabled = false
    }

    ws.onclose = () => {
      if (term) term.write('\r\n\n[接続が閉じられました]\r\n')
      connectBtn.disabled = false
      // エラーで閉じた場合は内容を見せるため自動で閉じない。
      if (channelToken && !sawError) {
        // 少し待ってから閉じる（文言を描画するため）
        window.setTimeout(() => closeWindow(), 400)
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
    const username = usernameInput.value.trim()
    const password = passwordInput.value
    connectWithCredentials(username, password)
  })

  // 親タブから開かれた場合、BroadcastChannel 経由で認証情報を受け取り自動接続する（noopener でも動く）
  if (channelToken && targetId) {
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

      infoEl.textContent = `親タブからユーザー名「${usernameInput.value.trim()}」を受信しました。接続中…`
      connectWithCredentials(usernameInput.value, passwordInput.value)
    }

    try {
      bc.postMessage({ type: 'ready', target_id: targetId })
    } catch {
      window.clearTimeout(timeoutId)
      try { bc.close() } catch { /* ignore */ }
      connectBtn.disabled = false
    }
  }

  function startXterm(ws) {
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
    sendResize(ws)
    term.focus()

    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data))
      }
    })

    resizeObserver = new ResizeObserver(() => {
      try { fitAddon.fit() } catch { /* ignore */ }
      sendResize(ws)
    })
    resizeObserver.observe(xtermEl)

    window.addEventListener('beforeunload', () => {
      try { ws.close() } catch { /* ignore */ }
    }, { once: true })
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

