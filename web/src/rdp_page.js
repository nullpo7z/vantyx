/**
 * RDP connection page.
 * Browser-based RDP via noVNC (FreeRDP→Xvfb→x11vnc on server side).
 * Connects immediately on page load using stored target credentials.
 */
import RFB from '@novnc/novnc'
import API from './api.js'
import { t } from './i18n.js'
import { initViewOnlySharingUI } from './sharing_ui.js'
function escapeHtml(s) {
  if (s == null) return ''
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}

export async function renderRdpPage(container) {
  const params = new URLSearchParams(window.location.search)
  const targetId = params.get('target_id') || ''
  const targetName = params.get('target_name') || targetId || 'RDP'
  const sessionId = params.get('session_id') || ''
  const parentToken = params.get('parent_token') || ''
  const sharingMode = (params.get('mode') || 'writer').toLowerCase() === 'viewer' ? 'viewer' : 'writer'
  const inviteToken = params.get('invite') || ''

  if (!targetId && !sessionId) {
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
    <div data-sharing-root="1" class="h-screen w-screen flex flex-col bg-slate-100 font-sans text-slate-900 overflow-hidden">
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
            <button id="rdp-fullscreen" type="button" class="vantyx-page-btn hidden">${t('rdp.fullscreen')}</button>
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
        <div id="rdp-screen" class="relative flex-1 min-h-0 w-full overflow-hidden"></div>
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
  let activeSessionId = sessionId || ''
  let sharingCtl = null

  function showKicked() {
    disconnect()
    container.innerHTML = `
      <div class="min-h-screen flex flex-col items-center justify-center p-8 text-center">
        <h2 class="text-lg font-semibold text-slate-800">${escapeHtml(t('sharing.youWereKicked'))}</h2>
        <p class="text-sm text-slate-600 mt-2">${escapeHtml(t('sharing.youWereKickedHint'))}</p>
      </div>`
  }

  async function ensureSharingUI() {
    await resolveActiveSessionId()
    if (!activeSessionId) return
    sharingCtl?.stop?.()
    sharingCtl = initViewOnlySharingUI({
      container,
      sessionKind: 'rdp',
      sessionId: activeSessionId,
      sharingMode,
      targetName,
      escapeHtml,
      onKicked: showKicked,
    })
  }

  async function resolveActiveSessionId() {
    if (activeSessionId) return activeSessionId
    if (!targetId || typeof API?.rdpSessions !== 'function') return ''
    try {
      const res = await API.rdpSessions()
      const hit = (res.items || []).find((s) => s.target_id === targetId)
      if (hit?.session_id) {
        activeSessionId = hit.session_id
        const u = new URL(window.location.href)
        if (u.searchParams.get('session_id') !== activeSessionId) {
          u.searchParams.set('session_id', activeSessionId)
          window.history.replaceState({}, '', u.toString())
        }
      }
    } catch {
      /* ignore */
    }
    return activeSessionId
  }

  async function endRdpSession() {
    await resolveActiveSessionId()
    if (!activeSessionId || typeof API?.rdpSessionDelete !== 'function') return
    try {
      await API.rdpSessionDelete(activeSessionId)
    } catch {
      /* UI still closes locally */
    }
    activeSessionId = ''
  }

  function showConnecting() {
    connectingEl.classList.remove('hidden')
    errorEl.classList.add('hidden')
    screenWrap.classList.add('hidden')
    disconnectBtn.classList.add('hidden')
    fullscreenBtn.classList.add('hidden')
  }

  function showError(msg) {
    connectingEl.classList.add('hidden')
    errorMsgEl.textContent = msg
    errorEl.classList.remove('hidden')
    screenWrap.classList.add('hidden')
    disconnectBtn.classList.add('hidden')
    fullscreenBtn.classList.add('hidden')
  }

  function showScreen() {
    connectingEl.classList.add('hidden')
    errorEl.classList.add('hidden')
    screenWrap.classList.remove('hidden')
    disconnectBtn.classList.remove('hidden')
    fullscreenBtn.classList.remove('hidden')
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

  async function startConnection() {
    disconnect()
    screenEl.innerHTML = ''
    showConnecting()

    if (sharingMode === 'viewer' && activeSessionId && inviteToken) {
      try {
        await API.joinRDPSession(activeSessionId, { invitationToken: inviteToken })
      } catch (err) {
        showError(t('sharing.joinFailed', { error: err.message || String(err) }))
        return
      }
    }

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
    let wsUrl
    if (activeSessionId && sharingMode === 'viewer') {
      wsUrl = `${wsScheme}//${window.location.host}/ws/rdp/browser?target_id=${encodeURIComponent(targetId)}&session_id=${encodeURIComponent(activeSessionId)}&mode=viewer&w=${w}&h=${h}`
    } else {
      wsUrl = `${wsScheme}//${window.location.host}/ws/rdp/browser?target_id=${encodeURIComponent(targetId)}&w=${w}&h=${h}`
    }

    try {
      rfb = new RFB(screenEl, wsUrl, { shared: true })
      rfb.scaleViewport = true
      // mstsc の「ウィンドウ内に収める」挙動に寄せる。必要に応じてスクロールも許容。
      rfb.clipViewport = false
      rfb.resizeSession = false

      rfb.addEventListener('connect', () => {
        showScreen()
        void ensureSharingUI()
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
    await endRdpSession()
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

  // Update the button label and nudge noVNC to recompute its layout once
  // the fullscreen transition actually completes -- a manual
  // window.dispatchEvent('resize') right after requestFullscreen()/
  // exitFullscreen() can race the browser's own layout change, leaving
  // the remote screen scaled to its pre-transition size.
  document.addEventListener('fullscreenchange', () => {
    const isFullscreen = !!document.fullscreenElement
    fullscreenBtn.textContent = t(isFullscreen ? 'rdp.exitFullscreen' : 'rdp.fullscreen')
    onResize()
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

  void startConnection()
  if (activeSessionId && sharingMode !== 'viewer') {
    void ensureSharingUI()
  }
}
