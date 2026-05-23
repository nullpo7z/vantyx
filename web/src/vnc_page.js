/**
 * VNC viewer page: connects to /ws/vnc?target_id=... via noVNC (RFB over WebSocket).
 */
import RFB from '@novnc/novnc/lib/rfb.js'

export function renderVncPage(container) {
  const params = new URLSearchParams(window.location.search)
  const targetId = params.get('target_id') || ''

  if (!targetId) {
    container.innerHTML = `
      <div class="min-h-screen flex flex-col bg-slate-100 font-sans text-slate-900 items-center justify-center p-6">
        <div class="text-center text-slate-600">
          <p class="mb-4">target_id が指定されていません。</p>
          <a href="/" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm">ホームに戻る</a>
        </div>
      </div>
    `
    return
  }

  const wsScheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const wsUrl = `${wsScheme}//${window.location.host}/ws/vnc?target_id=${encodeURIComponent(targetId)}`

  container.innerHTML = `
    <div class="h-screen w-screen flex flex-col bg-black font-sans text-slate-900 overflow-hidden">
      <div id="vnc-connecting" class="flex-1 flex flex-col items-center justify-center p-4 gap-4 text-slate-200">
        <p class="text-sm">VNC に接続しています…</p>
        <div class="animate-spin h-8 w-8 border-2 border-sky-500 border-t-transparent rounded-full"></div>
      </div>
      <div id="vnc-error" class="hidden flex-1 flex flex-col items-center justify-center p-4 gap-4 text-red-200">
        <p id="vnc-error-msg" class="text-sm"></p>
        <button id="vnc-error-close" type="button" class="rounded border border-slate-500 bg-slate-800 px-3 py-1.5 text-xs font-semibold text-slate-100 hover:bg-slate-700 shadow-sm">ウィンドウを閉じる</button>
      </div>
      <div id="vnc-screen-wrap" class="hidden flex-1 min-h-0 flex flex-col bg-black">
        <div id="vnc-screen" class="relative flex-1 min-h-0 w-full">
          <button id="vnc-fullscreen" type="button"
            class="absolute right-3 bottom-3 z-10 rounded bg-black/60 px-2 py-1 text-[11px] text-white hover:bg-black/80">
            全画面
          </button>
        </div>
      </div>
    </div>
  `

  const connectingEl = container.querySelector('#vnc-connecting')
  const errorEl = container.querySelector('#vnc-error')
  const errorMsgEl = container.querySelector('#vnc-error-msg')
  const errorCloseBtn = container.querySelector('#vnc-error-close')
  const screenWrap = container.querySelector('#vnc-screen-wrap')
  const screenEl = container.querySelector('#vnc-screen')
  const fullscreenBtn = container.querySelector('#vnc-fullscreen')

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
  }

  errorCloseBtn.addEventListener('click', () => {
    if (rfb) {
      rfb.disconnect()
      rfb = null
    }
    try { window.close() } catch { /* ignore */ }
  })

  fullscreenBtn.addEventListener('click', () => {
    const el = screenWrap || document.documentElement
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => {})
      return
    }
    if (el.requestFullscreen) el.requestFullscreen().catch(() => {})
  })

  try {
    rfb = new RFB(screenEl, wsUrl, { shared: true })
    rfb.scaleViewport = true
    // 見切れゼロ最優先: 必ずビューポートに収める（スクロールではなく縮小）
    rfb.clipViewport = true
    rfb.resizeSession = false
    rfb.addEventListener('connect', () => {
      showScreen()
      // scaleViewport を効かせるため、一度 resize イベントを飛ばす
      setTimeout(() => {
        // 端数丸めで下端が見切れるケースがあるためトグルで再計算させる
        try { rfb.scaleViewport = false } catch { /* ignore */ }
        try { rfb.scaleViewport = true } catch { /* ignore */ }
        try { window.dispatchEvent(new window.Event('resize')) } catch { /* ignore */ }
      }, 100)
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
    })
  } catch (err) {
    showError('noVNC の初期化に失敗しました: ' + (err.message || String(err)))
  }
}
