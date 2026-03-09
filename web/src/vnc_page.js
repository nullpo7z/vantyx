/**
 * VNC viewer page: connects to /ws/vnc?target_id=... via noVNC (RFB over WebSocket).
 * noVNC core is loaded from CDN (ESM).
 */
function escapeHtml(s) {
  if (s == null) return ''
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}

export function renderVncPage(container) {
  const params = new URLSearchParams(window.location.search)
  const targetId = params.get('target_id') || ''
  const targetName = params.get('target_name') || targetId || 'VNC'

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
  const wsUrl = `${wsScheme}//${window.location.host}/ws/vnc?target_id=${encodeURIComponent(targetId)}`

  container.innerHTML = `
    <div class="min-h-screen w-screen flex flex-col bg-slate-950">
      <header class="shrink-0 px-4 sm:px-6 py-3 bg-slate-900 border-b border-slate-800 flex items-center justify-between">
        <div class="min-w-0">
          <div class="text-xs text-slate-400">Vantyx VNC</div>
          <div class="text-sm sm:text-base font-semibold text-slate-100 truncate">${escapeHtml(targetName)}</div>
        </div>
        <div class="flex items-center gap-2">
          <button id="vnc-back" type="button" class="rounded border border-slate-700 bg-slate-900 px-3 py-1.5 text-xs font-semibold text-slate-200 hover:bg-slate-800">戻る</button>
          <button id="vnc-disconnect" type="button" class="rounded bg-slate-800 px-3 py-1.5 text-xs font-semibold text-slate-100 hover:bg-slate-700 hidden">切断</button>
        </div>
      </header>
      <div id="vnc-connecting" class="flex-1 flex flex-col items-center justify-center p-4 gap-4 text-slate-300">
        <p>VNC に接続しています…</p>
        <div class="animate-spin h-8 w-8 border-2 border-sky-500 border-t-transparent rounded-full"></div>
      </div>
      <div id="vnc-error" class="hidden flex-1 flex flex-col items-center justify-center p-4 gap-4 text-red-400">
        <p id="vnc-error-msg"></p>
        <a href="/" class="rounded border border-slate-600 px-4 py-2 text-sm text-slate-300 hover:bg-slate-800">ホームに戻る</a>
      </div>
      <div id="vnc-screen-wrap" class="hidden flex-1 min-h-0 flex flex-col bg-slate-900">
        <div id="vnc-screen" class="flex-1 min-h-0 w-full"></div>
      </div>
    </div>
  `

  const backBtn = container.querySelector('#vnc-back')
  const disconnectBtn = container.querySelector('#vnc-disconnect')
  const connectingEl = container.querySelector('#vnc-connecting')
  const errorEl = container.querySelector('#vnc-error')
  const errorMsgEl = container.querySelector('#vnc-error-msg')
  const screenWrap = container.querySelector('#vnc-screen-wrap')
  const screenEl = container.querySelector('#vnc-screen')

  let rfb = null

  function showError(msg) {
    connectingEl.classList.add('hidden')
    screenWrap.classList.add('hidden')
    errorMsgEl.textContent = msg
    errorEl.classList.remove('hidden')
  }

  function showScreen() {
    connectingEl.classList.add('hidden')
    errorEl.classList.add('hidden')
    screenWrap.classList.remove('hidden')
    disconnectBtn.classList.remove('hidden')
  }

  backBtn.addEventListener('click', () => {
    if (rfb) {
      rfb.disconnect()
      rfb = null
    }
    window.location.href = '/'
  })

  disconnectBtn.addEventListener('click', () => {
    if (rfb) {
      rfb.disconnect()
      rfb = null
    }
    disconnectBtn.classList.add('hidden')
    screenWrap.classList.add('hidden')
    connectingEl.classList.remove('hidden')
    connectingEl.querySelector('p').textContent = '切断しました。'
  })

  // Load noVNC core from CDN and connect
  import('https://cdn.jsdelivr.net/npm/@novnc/novnc@1.4.0/core/rfb.js')
    .then((module) => {
      const RFB = module.default
      rfb = new RFB(screenEl, wsUrl, { shared: true })
      rfb.addEventListener('connect', () => {
        showScreen()
      })
      rfb.addEventListener('disconnect', (e) => {
        if (e.detail && !e.detail.clean) {
          showError('接続が切断されました。')
        }
      })
      rfb.addEventListener('securityfailure', (e) => {
        const reason = (e.detail && e.detail.reason) ? e.detail.reason : 'セキュリティネゴシエーションに失敗しました。'
        showError(reason)
      })
      rfb.addEventListener('credentialsrequired', () => {
        // VNC server asked for password; noVNC shows its own prompt
      })
    })
    .catch((err) => {
      showError('noVNC の読み込みに失敗しました: ' + (err.message || String(err)))
    })
}
