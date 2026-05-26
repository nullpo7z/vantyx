/**
 * RDP connection page.
 * Browser-based RDP via noVNC (FreeRDP→Xvfb→x11vnc on server side).
 * Connects immediately on page load using stored target credentials.
 */
import RFB from '@novnc/novnc'
import API from './api.js'
import { t } from './i18n.js'
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
  const sessionId = params.get('session_id') || ''
  const parentToken = params.get('parent_token') || ''

  if (!targetId) {
    container.innerHTML = `
      <div class="min-h-screen flex flex-col bg-slate-100 font-sans text-slate-900 items-center justify-center p-6">
        <div class="text-center text-slate-600">
          <p class="mb-4">${t('rdp.noTargetId')}</p>
          <a href="/" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm">${t('rdp.backHome')}</a>
        </div>
      </div>
    `
    return
  }

  const wsScheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'

  container.innerHTML = `
    <div class="h-screen w-screen flex flex-col bg-slate-100 font-sans text-slate-900 overflow-hidden">
      <header class="shrink-0 shadow z-10 text-white">
        <div class="vantyx-header-inner">
          <div class="vantyx-header-start">
            <h1 class="vantyx-brand">Vantyx</h1>
            <div class="vantyx-page-context">
              <span class="vantyx-page-context-label">${t('rdp.pageLabel')}</span>
              <span class="vantyx-page-context-target">${escapeHtml(targetName)}</span>
            </div>
          </div>
          <div class="vantyx-header-end">
            <button id="rdp-back" type="button" class="vantyx-page-btn">${t('rdp.back')}</button>
            <button id="rdp-disconnect" type="button" class="vantyx-page-btn vantyx-page-btn-danger hidden">${t('rdp.disconnect')}</button>
          </div>
        </div>
      </header>
      <!-- Connecting spinner -->
      <div id="rdp-connecting" class="flex-1 flex flex-col items-center justify-center p-4 gap-4 text-slate-600">
        <p class="text-sm">${t('rdp.connecting')}</p>
        <div class="animate-spin h-8 w-8 border-2 border-sky-500 border-t-transparent rounded-full"></div>
        <p class="text-xs text-slate-400">${t('rdp.connectingHint')}</p>
      </div>

      <!-- Error display -->
      <div id="rdp-error" class="hidden flex-1 flex flex-col items-center justify-center p-4 gap-4 text-red-600">
        <p id="rdp-error-msg" class="text-sm"></p>
        <div class="flex gap-2">
          <button id="rdp-retry" type="button" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm">${t('rdp.retry')}</button>
          <button id="rdp-error-close" type="button" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm">${t('rdp.close')}</button>
        </div>
      </div>

      <!-- noVNC screen -->
      <div id="rdp-screen-wrap" class="hidden flex-1 min-h-0 flex flex-col bg-white border-t border-slate-200 overflow-auto">
        <div id="rdp-screen" class="relative flex-1 min-h-0 w-full overflow-hidden">
          <button id="rdp-fullscreen" type="button"
            class="absolute right-3 bottom-3 z-10 rounded bg-black/60 px-2 py-1 text-[11px] text-white hover:bg-black/80">
            ${t('rdp.fullscreen')}
          </button>
        </div>
      </div>
    </div>
  `

  const connectingEl = container.querySelector('#rdp-connecting')
  const backBtn = container.querySelector('#rdp-back')
  const disconnectBtn = container.querySelector('#rdp-disconnect')
  const errorEl = container.querySelector('#rdp-error')
  const errorMsgEl = container.querySelector('#rdp-error-msg')
  const screenWrap = container.querySelector('#rdp-screen-wrap')
  const screenEl = container.querySelector('#rdp-screen')
  const retryBtn = container.querySelector('#rdp-retry')
  const errorCloseBtn = container.querySelector('#rdp-error-close')
  const fullscreenBtn = container.querySelector('#rdp-fullscreen')

  let rfb = null
  let resizeRaf = 0
  // no explicit scaling here; rely on noVNC's scaleViewport so input coordinates stay correct.

  function showConnecting() {
    connectingEl.classList.remove('hidden')
    errorEl.classList.add('hidden')
    screenWrap.classList.add('hidden')
    disconnectBtn.classList.add('hidden')
  }

  function showError(msg) {
    connectingEl.classList.add('hidden')
    errorMsgEl.textContent = msg
    errorEl.classList.remove('hidden')
    screenWrap.classList.add('hidden')
    disconnectBtn.classList.add('hidden')
  }

  function showScreen() {
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

  function closeWindow() {
    if (parentToken) {
      try {
        const bc = new BroadcastChannel(`vantyx-terminal-parent-${parentToken}`)
        try { bc.postMessage({ type: 'focus', refresh: 'active_sessions' }) } finally { bc.close() }
      } catch { /* ignore */ }
    }
    try { window.close() } catch { /* ignore */ }
  }

  function startConnection() {
    disconnect()
    screenEl.innerHTML = ''
    showConnecting()

    // 基本は 1920x1080 で扱い、rw/rh クエリが指定されていればそれを優先する。
    // ブラウザのウィンドウサイズとは独立した「RDP セッション解像度」として扱う。
    let baseW = 1920
    let baseH = 1080
    const prefW = parseInt(params.get('rw') || '', 10)
    const prefH = parseInt(params.get('rh') || '', 10)
    if (Number.isFinite(prefW) && prefW >= 640 && prefW <= 3840) baseW = prefW
    if (Number.isFinite(prefH) && prefH >= 480 && prefH <= 2160) baseH = prefH
    let w = baseW
    let h = baseH
    // バックエンド側とRDPの制約に合わせて安全な範囲にクリップ
    if (w < 640) w = 640
    if (w > 3840) w = 3840
    if (h < 480) h = 480
    if (h > 2160) h = 2160
    const wsUrl = `${wsScheme}//${window.location.host}/ws/rdp/browser?target_id=${encodeURIComponent(targetId)}&w=${w}&h=${h}`

    try {
      rfb = new RFB(screenEl, wsUrl, { shared: true })
      rfb.scaleViewport = true
      // mstsc の「ウィンドウ内に収める」挙動に寄せる。必要に応じてスクロールも許容。
      rfb.clipViewport = false
      rfb.resizeSession = false

      rfb.addEventListener('connect', () => {
        showScreen()
        // 初回だけ軽くリサイズイベントを投げて noVNC に再計算させる
        setTimeout(() => {
          try { window.dispatchEvent(new window.Event('resize')) } catch { /* ignore */ }
          try { rfb.focus() } catch { /* ignore */ }
        }, 100)
      })
      rfb.addEventListener('disconnect', (e) => {
        if (e.detail && !e.detail.clean) {
          showError(t('rdp.connectionLost'))
        } else {
          showError(t('rdp.disconnectedClean'))
        }
        rfb = null
      })
      rfb.addEventListener('securityfailure', (e) => {
        const reason = (e.detail && e.detail.reason) ? e.detail.reason : t('rdp.securityFailure')
        showError(reason)
        rfb = null
      })
    } catch (err) {
      showError(t('rdp.initFailed', { error: err.message || String(err) }))
    }
  }

  retryBtn.addEventListener('click', startConnection)

  errorCloseBtn.addEventListener('click', () => {
    disconnect()
    window.removeEventListener('resize', onResize)
    closeWindow()
  })

  backBtn.addEventListener('click', () => {
    window.removeEventListener('resize', onResize)
    closeWindow()
  })

  disconnectBtn.addEventListener('click', async () => {
    try {
      if (sessionId && typeof API?.rdpSessionDelete === 'function') {
        await API.rdpSessionDelete(sessionId)
      }
    } catch {
      // エラー時もローカル側は切断しておく（UI 優先）。
    }
    disconnect()
    window.removeEventListener('resize', onResize)
    closeWindow()
  })

  fullscreenBtn.addEventListener('click', () => {
    const el = screenWrap || document.documentElement
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => {})
      return
    }
    if (el.requestFullscreen) el.requestFullscreen().catch(() => {})
  })

  function onResize() {
    if (!rfb) return
    if (resizeRaf) window.cancelAnimationFrame(resizeRaf)
    resizeRaf = window.requestAnimationFrame(() => {
      resizeRaf = 0
      // noVNC が内部で scaleViewport に応じて再レイアウトするので、追加処理は不要。
      try { window.dispatchEvent(new window.Event('resize')) } catch { /* ignore */ }
    })
  }
  window.addEventListener('resize', onResize)

  startConnection()
}
