/**
 * View-only collaborative session UI (VNC / RDP browser).
 * Owner: invite button + participants panel with kick.
 * Viewer: read-only banner; kicked users see a message.
 */

import API from './api.js'
import { t } from './i18n.js'
import { uiAlert, uiConfirm } from './ui_dialog.js'
import {
  createRealtimeWatcher,
  POLL_MS,
  shouldRefreshTerminalSharing,
} from './sharing_events.js'

/**
 * @param {object} options
 * @param {HTMLElement} options.container
 * @param {'vnc'|'rdp'} options.sessionKind
 * @param {string} options.sessionId
 * @param {'viewer'|'writer'} options.sharingMode
 * @param {string} [options.targetName]
 * @param {(root: ParentNode) => HTMLElement|null} [options.insertAfter]
 * @param {() => void} [options.onKicked]
 * @param {(el: HTMLElement) => string} options.escapeHtml
 */
export function initViewOnlySharingUI({
  container,
  sessionKind,
  sessionId,
  sharingMode,
  targetName = '',
  insertAfter,
  onKicked,
  escapeHtml,
}) {
  if (!container || !sessionId) {
    return { refresh: async () => {}, stop: () => {} }
  }

  let myUserId = ''
  let sessionOwnerId = ''
  let cachedParticipants = []
  let stopWatch = null
  let sharingEventsSessionId = ''

  const meReady = API.me()
    .then((me) => {
      myUserId = me?.user_id || ''
    })
    .catch(() => {})

  function rootEl() {
    return container.querySelector('[data-sharing-root="1"]') || container
  }

  function ensureViewerBanner() {
    if (sharingMode !== 'viewer') return
    const root = rootEl()
    if (root.querySelector('#sharing-viewer-banner')) return
    const banner = document.createElement('div')
    banner.id = 'sharing-viewer-banner'
    banner.className = 'shrink-0 bg-sky-700 text-white text-xs px-3 py-1.5'
    banner.textContent = t('sharing.viewOnlyBanner')
    const anchor = insertAfter?.(root) || root.querySelector('header') || root.firstElementChild
    if (anchor?.parentNode) {
      anchor.insertAdjacentElement('afterend', banner)
    } else {
      root.prepend(banner)
    }
  }

  function ensureInviteButton() {
    if (sharingMode === 'viewer') return null
    const root = rootEl()
    let btn = root.querySelector('#sharing-invite-manage')
    if (btn) return btn
    const headerEnd = root.querySelector('.vantyx-header-end')
    if (!headerEnd) return null
    btn = document.createElement('button')
    btn.id = 'sharing-invite-manage'
    btn.type = 'button'
    btn.className = 'vantyx-page-btn'
    btn.textContent = t('terminal.inviteManage')
    btn.title = t('sharing.inviteTitle')
    headerEnd.insertBefore(btn, headerEnd.firstChild)
    btn.addEventListener('click', async () => {
      if (!sessionId || sharingMode === 'viewer') return
      const { openInviteDialog } = await import('./invite_dialog.js')
      openInviteDialog({ sessionId, sessionKind, targetName, escapeHtml })
    })
    return btn
  }

  function renderParticipantsPanel() {
    const root = rootEl()
    if (!root || !sessionId) return
    const isOwner = !!(sessionOwnerId && myUserId && myUserId === sessionOwnerId)
    let panel = root.querySelector('#sharing-participants-panel')
    if (!isOwner) {
      panel?.remove()
      return
    }
    const viewers = cachedParticipants.filter((p) => p && p.role === 'viewer')
    if (!panel) {
      panel = document.createElement('div')
      panel.id = 'sharing-participants-panel'
      panel.className = 'shrink-0 border-b border-slate-200 bg-slate-50 px-3 py-2 text-xs'
      const banner = root.querySelector('#sharing-viewer-banner')
      const header = root.querySelector('header')
      if (banner) banner.insertAdjacentElement('afterend', panel)
      else if (header) header.insertAdjacentElement('afterend', panel)
      else root.prepend(panel)
    }
    if (viewers.length === 0) {
      panel.innerHTML = `<div class="text-slate-600"><span class="font-semibold text-slate-800">${escapeHtml(t('sharing.participantsTitle'))}</span> — ${escapeHtml(t('sharing.participantsEmpty'))}</div>`
      return
    }
    const rows = viewers
      .map((p) => {
        const label = escapeHtml(p.username || p.user_id || '')
        return `<tr>
          <td class="py-1 pr-3">${label}</td>
          <td class="py-1 pr-3 text-slate-600">${escapeHtml(t('sharing.roleViewer'))}</td>
          <td class="py-1 text-right">
            <button type="button" class="sharing-kick-btn rounded border border-red-300 bg-white px-2 py-0.5 text-red-700 hover:bg-red-50" data-user-id="${escapeHtml(p.user_id)}">${escapeHtml(t('sharing.kick'))}</button>
          </td>
        </tr>`
      })
      .join('')
    panel.innerHTML = `
      <div class="font-semibold text-slate-800 mb-1">${escapeHtml(t('sharing.participantsTitle'))}</div>
      <table class="w-full text-left"><thead><tr>
        <th class="pb-1 pr-3">${escapeHtml(t('sharing.headerUsername'))}</th>
        <th class="pb-1 pr-3">${escapeHtml(t('sharing.headerStatus'))}</th>
        <th class="pb-1 text-right">${escapeHtml(t('sharing.headerActions'))}</th>
      </tr></thead><tbody>${rows}</tbody></table>`
    panel.querySelectorAll('.sharing-kick-btn').forEach((btn) => {
      btn.addEventListener('click', async () => {
        const uid = btn.getAttribute('data-user-id') || ''
        const p = viewers.find((x) => x.user_id === uid)
        const ok = await uiConfirm(t('sharing.kickConfirm', { name: p?.username || uid }))
        if (!ok) return
        try {
          await API.kickSessionParticipant(sessionId, uid, { kind: sessionKind })
          await refreshParticipants()
        } catch (err) {
          await uiAlert(t('sharing.kickFailed') + (err?.message ? `: ${err.message}` : ''))
        }
      })
    })
  }

  async function refreshParticipants() {
    if (!sessionId) return
    await meReady
    try {
      const res = await API.listSessionParticipants(sessionId, { kind: sessionKind })
      sessionOwnerId = res?.owner_id || ''
      cachedParticipants = Array.isArray(res?.items) ? res.items : []
    } catch {
      /* ignore */
    } finally {
      renderParticipantsPanel()
    }
  }

  function handleSharingEvent(payload) {
    if (payload?.type === 'participant_left') {
      if (
        payload.extra?.reason === 'kicked' &&
        payload.user_id &&
        myUserId &&
        payload.user_id === myUserId
      ) {
        if (typeof onKicked === 'function') onKicked()
        return
      }
    }
    if (payload?.session_id === sessionId || payload?.type === 'session_change') {
      void refreshParticipants()
    }
  }

  function attachSharingEvents() {
    if (!sessionId || sharingEventsSessionId === sessionId) return
    stopWatch?.()
    sharingEventsSessionId = sessionId
    stopWatch = createRealtimeWatcher({
      shouldRefresh: (payload) => shouldRefreshTerminalSharing(payload, sessionId),
      onEvent: (payload) => handleSharingEvent(payload),
      onRefresh: () => refreshParticipants(),
      pollMs: POLL_MS.terminalSharing,
    })
  }

  ensureViewerBanner()
  ensureInviteButton()
  attachSharingEvents()
  void refreshParticipants()

  return {
    refresh: refreshParticipants,
    stop: () => {
      stopWatch?.()
      stopWatch = null
      sharingEventsSessionId = ''
    },
  }
}
