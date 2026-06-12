/**
 * VNC viewer page: connects to /ws/vnc?target_id=... or shared session attach.
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

export async function renderVncPage(container) {
  const params = new URLSearchParams(window.location.search)
  const targetId = params.get('target_id') || ''
  const sessionId = params.get('session_id') || ''
  const targetName = params.get('target_name') || targetId || 'VNC'
  const sharingMode = (params.get('mode') || 'writer').toLowerCase() === 'viewer' ? 'viewer' : 'writer'
  const inviteToken = params.get('invite') || ''

  if (!targetId && !sessionId) {
    container.innerHTML = `
      <div class="min-h-screen flex flex-col bg-slate-100 font-sans text-slate-900 items-center justify-center p-6">
        <div class="text-center text-slate-600">
          <p class="mb-4">${t('vnc.noTargetId')}</p>
          <a href="/" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm">${t('vnc.backHome')}</a>
        </div>
      </div>
    `
    return
  }

  let activeSessionId = sessionId || ''
  let sharingCtl = null

  container.innerHTML = `
    <div data-sharing-root="1" class="h-screen w-screen flex flex-col bg-black font-sans text-slate-900 overflow-hidden">
      <header class="shrink-0 shadow z-10 text-white">
        <div class="vantyx-header-inner">
          <div class="vantyx-header-start">
            <h1 class="vantyx-brand">Vantyx</h1>
            <div class="vantyx-page-context">
              <span class="vantyx-page-context-label">VNC</span>
              <span class="vantyx-page-context-target">${escapeHtml(targetName)}</span>
            </div>
          </div>
          <div class="vantyx-header-end">
            <button id="vnc-back" type="button" class="vantyx-page-btn">${t('vnc.backHome')}</button>
          </div>
        </div>
      </header>
      <div id="vnc-connecting" class="flex-1 flex flex-col items-center justify-center p-4 gap-4 text-slate-200">
        <p class="text-sm">${t('vnc.connecting')}</p>
        <div class="animate-spin h-8 w-8 border-2 border-sky-500 border-t-transparent rounded-full"></div>
      </div>
      <div id="vnc-error" class="hidden flex-1 flex flex-col items-center justify-center p-4 gap-4 text-red-200">
        <p id="vnc-error-msg" class="text-sm"></p>
        <button id="vnc-error-close" type="button" class="rounded border border-slate-500 bg-slate-800 px-3 py-1.5 text-xs font-semibold text-slate-100 hover:bg-slate-700 shadow-sm">${t('vnc.closeWindow')}</button>
      </div>
      <div id="vnc-screen-wrap" class="hidden flex-1 min-h-0 flex flex-col bg-black">
        <div id="vnc-screen" class="relative flex-1 min-h-0 w-full">
          <button id="vnc-fullscreen" type="button"
            class="absolute right-3 bottom-3 z-10 rounded bg-black/60 px-2 py-1 text-[11px] text-white hover:bg-black/80">
            ${t('vnc.fullscreen')}
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
  const backBtn = container.querySelector('#vnc-back')

  let rfb = null

  function showKicked() {
    sharingCtl?.stop?.()
    if (rfb) {
      try { rfb.disconnect() } catch { /* ignore */ }
      rfb = null
    }
    container.innerHTML = `
      <div class="min-h-screen flex flex-col items-center justify-center p-8 text-center bg-slate-100">
        <h2 class="text-lg font-semibold text-slate-800">${escapeHtml(t('sharing.youWereKicked'))}</h2>
        <p class="text-sm text-slate-600 mt-2">${escapeHtml(t('sharing.youWereKickedHint'))}</p>
      </div>`
  }

  function syncSessionIdInUrl() {
    if (!activeSessionId) return
    const u = new URL(window.location.href)
    if (u.searchParams.get('session_id') === activeSessionId) return
    u.searchParams.set('session_id', activeSessionId)
    window.history.replaceState({}, '', u.toString())
  }

  async function resolveActiveSessionId(retries = 10) {
    if (activeSessionId) return activeSessionId
    if (sessionId) {
      activeSessionId = sessionId
      return activeSessionId
    }
    if (!targetId || typeof API?.vncSessions !== 'function') return ''
    for (let i = 0; i < retries; i++) {
      try {
        const res = await API.vncSessions()
        const items = res.items || []
        const matches = items.filter((s) => s && s.target_id === targetId)
        const hit = matches.length === 1
          ? matches[0]
          : matches.sort((a, b) => {
            const ta = Date.parse(a.last_seen || a.created_at || '') || 0
            const tb = Date.parse(b.last_seen || b.created_at || '') || 0
            return tb - ta
          })[0]
        if (hit?.session_id) {
          activeSessionId = hit.session_id
          syncSessionIdInUrl()
          return activeSessionId
        }
      } catch {
        /* retry */
      }
      if (i < retries - 1) {
        await new Promise((r) => setTimeout(r, 150))
      }
    }
    return ''
  }

  async function ensureSharingUI() {
    await resolveActiveSessionId()
    if (!activeSessionId) return
    sharingCtl?.stop?.()
    sharingCtl = initViewOnlySharingUI({
      container,
      sessionKind: 'vnc',
      sessionId: activeSessionId,
      sharingMode,
      targetName,
      escapeHtml,
      onKicked: showKicked,
    })
  }

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

  backBtn?.addEventListener('click', () => {
    sharingCtl?.stop?.()
    if (rfb) {
      try { rfb.disconnect() } catch { /* ignore */ }
      rfb = null
    }
    try { window.close() } catch { /* ignore */ }
    window.location.href = '/'
  })

  errorCloseBtn.addEventListener('click', () => {
    sharingCtl?.stop?.()
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

  async function startConnection() {
    if (sharingMode === 'viewer' && activeSessionId && inviteToken) {
      try {
        await API.joinVNCSession(activeSessionId, { invitationToken: inviteToken })
      } catch (err) {
        showError(t('sharing.joinFailed', { error: err.message || String(err) }))
        return
      }
    }

    const wsScheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    let wsUrl
    if (activeSessionId && (sharingMode === 'viewer' || sessionId)) {
      wsUrl = `${wsScheme}//${window.location.host}/ws/vnc?session_id=${encodeURIComponent(activeSessionId)}&mode=${encodeURIComponent(sharingMode)}`
    } else if (targetId) {
      wsUrl = `${wsScheme}//${window.location.host}/ws/vnc?target_id=${encodeURIComponent(targetId)}`
    } else {
      showError(t('vnc.noTargetId'))
      return
    }

    try {
      rfb = new RFB(screenEl, wsUrl, { shared: true })
      rfb.scaleViewport = true
      rfb.clipViewport = true
      rfb.resizeSession = false
      rfb.addEventListener('connect', () => {
        showScreen()
        void ensureSharingUI()
        setTimeout(() => {
          try { rfb.scaleViewport = false } catch { /* ignore */ }
          try { rfb.scaleViewport = true } catch { /* ignore */ }
          try { window.dispatchEvent(new window.Event('resize')) } catch { /* ignore */ }
        }, 100)
      })
      rfb.addEventListener('disconnect', (e) => {
        if (e.detail && !e.detail.clean) {
          showError(t('vnc.connectionLost'))
        }
      })
      rfb.addEventListener('securityfailure', (e) => {
        const reason = (e.detail && e.detail.reason) ? e.detail.reason : t('vnc.securityFailure')
        showError(reason)
      })
      rfb.addEventListener('credentialsrequired', () => {
      })
    } catch (err) {
      showError(t('vnc.initFailed', { error: err.message || String(err) }))
    }
  }

  if (sharingMode === 'viewer' && sessionId && inviteToken) {
    try {
      await API.joinVNCSession(sessionId, { invitationToken: inviteToken })
    } catch (err) {
      container.innerHTML = `<div class="p-8 text-center text-red-700">${escapeHtml(t('sharing.joinFailed', { error: err.message || String(err) }))}</div>`
      return
    }
  }

  if (activeSessionId && sharingMode !== 'viewer') {
    void ensureSharingUI()
  }

  void startConnection()
}
