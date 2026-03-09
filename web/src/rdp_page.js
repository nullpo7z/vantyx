/**
 * RDP connection page.
 * Provides:
 *  1. Browser-based RDP via noVNC (FreeRDP→Xvfb→x11vnc on server side)
 *  2. .rdp file download for native client (mstsc, Remmina, etc.)
 *  3. WebSocket proxy endpoint (/ws/rdp) for external RDP clients.
 */
function escapeHtml(s) {
  if (s == null) return ''
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}

export function renderRdpPage(container) {
  const params = new URLSearchParams(window.location.search)
  const targetId = params.get('target_id') || ''
  const targetName = params.get('target_name') || targetId || 'RDP'

  if (!targetId) {
    container.innerHTML = `
      <div class="min-h-screen flex items-center justify-center p-6 bg-slate-950">
        <div class="text-center text-slate-300">
          <p class="mb-4">target_id が指定されていません。</p>
          <a href="/" class="rounded bg-sky-600 px-4 py-2 text-sm font-medium text-white hover:bg-sky-700">ホームに戻る</a>
        </div>
      </div>
    `
    return
  }

  const wsScheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const rdpFileUrl = `/api/rdp/file?target_id=${encodeURIComponent(targetId)}`

  container.innerHTML = `
    <div class="min-h-screen w-screen flex flex-col bg-slate-950">
      <header class="shrink-0 px-4 sm:px-6 py-3 bg-slate-900 border-b border-slate-800 flex items-center justify-between">
        <div class="min-w-0">
          <div class="text-xs text-slate-400">Vantyx RDP</div>
          <div class="text-sm sm:text-base font-semibold text-slate-100 truncate">${escapeHtml(targetName)}</div>
        </div>
        <div class="flex items-center gap-2">
          <button id="rdp-back" type="button" class="rounded border border-slate-700 bg-slate-900 px-3 py-1.5 text-xs font-semibold text-slate-200 hover:bg-slate-800">戻る</button>
          <button id="rdp-disconnect" type="button" class="rounded bg-red-700 px-3 py-1.5 text-xs font-semibold text-white hover:bg-red-600 hidden">切断</button>
        </div>
      </header>

      <!-- Landing: connection options -->
      <div id="rdp-landing" class="flex-1 flex flex-col items-center justify-center p-6 gap-8">
        <div class="w-full max-w-lg bg-slate-900 border border-slate-800 rounded-lg overflow-hidden">
          <div class="px-6 py-4 border-b border-slate-800">
            <h2 class="text-base font-semibold text-slate-100">ブラウザで接続</h2>
            <p class="text-xs text-slate-400 mt-1">ブラウザ内でリモートデスクトップを表示します（サーバー側で FreeRDP→VNC 変換）。</p>
          </div>
          <div class="px-6 py-5">
            <button id="rdp-browser-connect" type="button"
              class="w-full inline-flex items-center justify-center gap-2 rounded bg-sky-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">
              <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"/></svg>
              ブラウザで接続
            </button>
          </div>
        </div>

        <div class="w-full max-w-lg bg-slate-900 border border-slate-800 rounded-lg overflow-hidden">
          <div class="px-6 py-4 border-b border-slate-800">
            <h2 class="text-base font-semibold text-slate-100">ネイティブ RDP クライアントで接続</h2>
            <p class="text-xs text-slate-400 mt-1">Windows リモートデスクトップ (mstsc)、Remmina、FreeRDP 等のクライアントで接続できます。</p>
          </div>
          <div class="px-6 py-5">
            <a href="${escapeHtml(rdpFileUrl)}" download
              class="inline-flex items-center justify-center gap-2 rounded bg-slate-700 px-4 py-2.5 text-sm font-medium text-white hover:bg-slate-600 shadow-sm transition-colors">
              <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/></svg>
              .rdp ファイルをダウンロード
            </a>
          </div>
        </div>
      </div>

      <!-- Connecting spinner -->
      <div id="rdp-connecting" class="hidden flex-1 flex flex-col items-center justify-center p-4 gap-4 text-slate-300">
        <p>RDP に接続しています… (FreeRDP→VNC ブリッジを起動中)</p>
        <div class="animate-spin h-8 w-8 border-2 border-sky-500 border-t-transparent rounded-full"></div>
        <p class="text-xs text-slate-500">初回接続には数秒かかります。</p>
      </div>

      <!-- Error display -->
      <div id="rdp-error" class="hidden flex-1 flex flex-col items-center justify-center p-4 gap-4 text-red-400">
        <p id="rdp-error-msg"></p>
        <button id="rdp-retry" type="button" class="rounded border border-slate-600 px-4 py-2 text-sm text-slate-300 hover:bg-slate-800">再試行</button>
      </div>

      <!-- noVNC screen -->
      <div id="rdp-screen-wrap" class="hidden flex-1 min-h-0 flex flex-col bg-slate-900">
        <div id="rdp-screen" class="flex-1 min-h-0 w-full"></div>
      </div>
    </div>
  `

  const backBtn = container.querySelector('#rdp-back')
  const disconnectBtn = container.querySelector('#rdp-disconnect')
  const landingEl = container.querySelector('#rdp-landing')
  const connectingEl = container.querySelector('#rdp-connecting')
  const errorEl = container.querySelector('#rdp-error')
  const errorMsgEl = container.querySelector('#rdp-error-msg')
  const screenWrap = container.querySelector('#rdp-screen-wrap')
  const screenEl = container.querySelector('#rdp-screen')
  const connectBtn = container.querySelector('#rdp-browser-connect')
  const retryBtn = container.querySelector('#rdp-retry')

  let rfb = null

  function showLanding() {
    landingEl.classList.remove('hidden')
    connectingEl.classList.add('hidden')
    errorEl.classList.add('hidden')
    screenWrap.classList.add('hidden')
    disconnectBtn.classList.add('hidden')
  }

  function showConnecting() {
    landingEl.classList.add('hidden')
    connectingEl.classList.remove('hidden')
    errorEl.classList.add('hidden')
    screenWrap.classList.add('hidden')
    disconnectBtn.classList.add('hidden')
  }

  function showError(msg) {
    landingEl.classList.add('hidden')
    connectingEl.classList.add('hidden')
    errorMsgEl.textContent = msg
    errorEl.classList.remove('hidden')
    screenWrap.classList.add('hidden')
    disconnectBtn.classList.add('hidden')
  }

  function showScreen() {
    landingEl.classList.add('hidden')
    connectingEl.classList.add('hidden')
    errorEl.classList.add('hidden')
    screenWrap.classList.remove('hidden')
    disconnectBtn.classList.remove('hidden')
  }

  function disconnect() {
    if (rfb) {
      rfb.disconnect()
      rfb = null
    }
  }

  function startBrowserConnection() {
    showConnecting()

    const w = window.screen.width
    const h = window.screen.height
    const wsUrl = `${wsScheme}//${window.location.host}/ws/rdp/browser?target_id=${encodeURIComponent(targetId)}&w=${w}&h=${h}`

    import('https://cdn.jsdelivr.net/npm/@novnc/novnc@1.4.0/core/rfb.js')
      .then((module) => {
        const RFB = module.default
        rfb = new RFB(screenEl, wsUrl, { shared: true })
        rfb.scaleViewport = true
        rfb.resizeSession = false

        rfb.addEventListener('connect', () => {
          showScreen()
        })
        rfb.addEventListener('disconnect', (e) => {
          if (e.detail && !e.detail.clean) {
            showError('接続が切断されました。')
          } else {
            showLanding()
          }
          rfb = null
        })
        rfb.addEventListener('securityfailure', (e) => {
          const reason = (e.detail && e.detail.reason) ? e.detail.reason : 'セキュリティネゴシエーションに失敗しました。'
          showError(reason)
          rfb = null
        })
      })
      .catch((err) => {
        showError('noVNC の読み込みに失敗しました: ' + (err.message || String(err)))
      })
  }

  connectBtn.addEventListener('click', startBrowserConnection)
  retryBtn.addEventListener('click', startBrowserConnection)

  backBtn.addEventListener('click', () => {
    disconnect()
    window.location.href = '/'
  })

  disconnectBtn.addEventListener('click', () => {
    disconnect()
    showLanding()
  })
}
