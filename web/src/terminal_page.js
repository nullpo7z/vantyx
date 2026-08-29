import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import API from './api.js'
import { t } from './i18n.js'
import {
  createRealtimeWatcher,
  POLL_MS,
  shouldRefreshTerminalSharing,
} from './sharing_events.js'
import { classifyTerminalWsFrameSync } from './terminal_ws_protocol.js'
import { createHostKeyDialogController } from './host_key_dialog.js'
import { uiAlert, uiConfirm } from './ui_dialog.js'
import { setupTerminalKeyboard } from './xterm_input.js'
import { refreshParticipantsDialog } from './participants_dialog.js'
import { isSameOriginBroadcast } from './dom_helpers.js'

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
  // Whether the target already has a private key stored server-side.
  // Gates the ad-hoc credential form's optional passphrase field --
  // without a key, entering a passphrase here can never do anything,
  // and showing it anyway reads as if the target used key auth.
  const hasSshKey = !isTelnet && params.get('has_ssh_key') === '1'
  const urlSessionName = params.get('session_name') ?? ''
  const urlSessionDesc = params.get('session_description') ?? ''
  const hasSessionParamsFromUrl = params.has('session_name') || params.has('session_description')
  // Collaborative session attach mode. The URL ?mode=viewer flips the
  // page into a read-only attach: stdin is dropped, a red banner is
  // shown, and the user can request control via the sharing API. The
  // optional ?invite=<token> consumes a link invitation before the
  // WebSocket is opened.
  const sharingMode = (params.get('mode') || 'writer').toLowerCase() === 'viewer' ? 'viewer' : 'writer'
  const inviteToken = params.get('invite') || ''
  let writerUserId = '' // populated from /participants once we have the session id
  let myUserId = ''
  const myUserIdReady = API.me()
    .then((me) => {
      myUserId = me?.user_id || ''
    })
    .catch(() => {})
  let viewerOwnerName = ''
  let writerDisplayName = ''
  let sessionOwnerId = ''
  let participantsLoaded = false
  let shownWriteRequestId = ''
  /** Pending write request awaiting owner review (banner → modal on click). */
  let pendingWriteRequestApproval = null
  let myPendingWriteRequest = false
  let cachedParticipants = []
  // User IDs the owner removed and who are blocked from rejoining until
  // "Allow rejoin" (or a new named invitation) lifts the block.
  let cachedKicked = []
  let stopSharingWatch = null
  let sharingEventsSessionId = ''

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
            <button id="term-participants-manage" type="button" class="vantyx-page-btn hidden" title="${escapeHtml(t('sharing.participantsTitle'))}">${t('terminal.actionParticipants')} (0)</button>
            <button id="term-invite-manage" type="button" class="vantyx-page-btn hidden" title="${escapeHtml(t('sharing.inviteTitle'))}">${t('terminal.inviteManage')}</button>
            <button id="term-back" type="button" class="vantyx-page-btn">${t('terminal.back')}</button>
            <button id="term-leave" type="button" class="vantyx-page-btn${sharingMode === 'viewer' ? '' : ' hidden'}">${t('terminal.actionLeave')}</button>
            <button id="term-close" type="button" class="vantyx-page-btn${sharingMode === 'viewer' ? ' hidden' : ''}">${t('terminal.endSessionBtn')}</button>
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
              ${isTelnet ? '' : `<div id="term-passphrase-optional-wrap" class="${hasSshKey ? '' : 'hidden'}">
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
  const inviteManageBtn = container.querySelector('#term-invite-manage')
  const participantsManageBtn = container.querySelector('#term-participants-manage')
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
  const wsUrlResume = (sid, mode = sharingMode) => {
    const m = mode === 'viewer' ? 'viewer' : 'writer'
    return `${protocol}//${window.location.host}/ws/ssh?session_id=${encodeURIComponent(sid)}&mode=${m}`
  }

  let term = null
  let fitAddon = null
  let resizeObserver = null
  let termStdinAttached = false
  let termBeforeUnloadAttached = false
  // Cached creds + form values from the last connectWithCredentials /
  // connectWithStoredCredentials call so the host-key TOFU adopt /
  // mismatch dialogs can transparently retry the connection after
  // adopting the new fingerprint. Cleared on session_ended.
  let lastCredentials = null
  let hostKeyCtrl = null

  function handleTerminalMetaObject(ws, o) {
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
    if (hostKeyCtrl?.handleHostKeyMeta(ws, o)) return true
    return false
  }

  /** WebSocket メッセージ共通処理。制御フレームは xterm に書き込まない。 */
  function handleTerminalWsMessage(ws, ev, { onError, onSessionEnded, onData } = {}) {
    if (typeof ev.data === 'string' && ev.data.startsWith('session_ended:')) {
      const raw = ev.data.slice('session_ended:'.length).trim()
      if (raw === 'kicked') {
        // The owner removed us. The bridge says so on this socket right
        // before closing it, so this is observed before onclose (R-5):
        // mark the close as handled and show the dedicated screen (no
        // Reconnect -- the server would refuse it anyway).
        onSessionEnded?.()
        showKickedSessionEnded()
        try { ws.close() } catch { /* ignore */ }
        return true
      }
      const msg = raw || t('terminal.sessionEndedSuffix')
      if (term) term.write(`\r\n\n${t('terminal.sessionEndedPrefix')} ${msg}\r\n`)
      currentSessionId = null
      syncInviteManageButton()
      detachSharingEvents()
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

    const frameKind = classifyTerminalWsFrameSync(ev.data)
    if (frameKind.kind === 'empty' || frameKind.kind === 'ready' || frameKind.kind === 'swallow') {
      return true
    }
    if (frameKind.kind === 'meta') {
      handleTerminalMetaObject(ws, frameKind.object)
      return true
    }
    if (frameKind.kind !== 'terminal' && frameKind.kind !== 'binary') {
      return true
    }

    // Real shell output. Unlike the session_id meta frame (sent up front,
    // before the bridge even attempts to authenticate against the
    // target), this can only happen once the bridge is genuinely up and
    // running -- callers use it as the reliable signal that there was
    // ever an actual live session, independent of whether an "error: "
    // frame sent right after a failed connect happens to arrive before
    // the socket closes.
    onData?.()

    credsWrap.classList.add('hidden')
    shellWrap.classList.remove('hidden')
    if (!ws._vantyxAttached) {
      startXterm(ws)
      ws._vantyxAttached = true
    }
    if (frameKind.kind === 'terminal' && frameKind.text) {
      term.write(frameKind.text)
    } else if (frameKind.kind === 'binary') {
      term.write(new Uint8Array(ev.data))
    }
    return false
  }

  /**
   * Hide all the post-handshake wraps so the host-key TOFU / mismatch
   * dialog is the only thing the user sees. Without this, the SPA
   * may have already swapped to shellWrap or — worse — fallen back to
   * the misleading "session is still running on the backend" banner
   * via the ws.onclose path. The bridge actually failed before any
   * session existed, so those wraps would lie about the state.
   */
  function hideTransientWrapsForHostKeyDialog() {
    try { credsWrap.classList.add('hidden') } catch { /* ignore */ }
    try { shellWrap.classList.add('hidden') } catch { /* ignore */ }
    try { disconnectedWrap?.classList.add('hidden') } catch { /* ignore */ }
    try { sessionEndedWrap?.classList.add('hidden') } catch { /* ignore */ }
    try { errorEl.classList.add('hidden') } catch { /* ignore */ }
  }

  /**
   * Replays the most recent credentials-based connect so the user is
   * not asked to re-enter their password after adopting a host key.
   * Falls back to re-showing the credentials form when nothing is
   * cached (e.g. resume flow).
   */
  function reconnectWithLastCredentials() {
    if (!lastCredentials) {
      credsWrap.classList.remove('hidden')
      shellWrap.classList.add('hidden')
      return
    }
    if (lastCredentials.kind === 'stored') {
      const { sessionName, sessionDescription, password, privateKeyPassphrase } = lastCredentials
      connectWithStoredCredentials(sessionName, sessionDescription, password, privateKeyPassphrase)
      return
    }
    const { username, password, sessionName, sessionDescription, privateKeyPassphrase } = lastCredentials
    connectWithCredentials(username, password, sessionName, sessionDescription, privateKeyPassphrase)
  }

  hostKeyCtrl = createHostKeyDialogController({
    targetId,
    onReconnect: reconnectWithLastCredentials,
    onBeforeDialog: hideTransientWrapsForHostKeyDialog,
    onCancelled: showHostKeyCancelledState,
  })

  /**
   * Renders the post-cancel state for a host-key TOFU / mismatch
   * dialog. Hides every other transient wrap and surfaces a concise
   * explanation in the page-level error banner. We deliberately do
   * NOT show disconnectedWrap (it advertises "session continuing on
   * backend", which is wrong here — the bridge died on host-key
   * verification) nor credsWrap on its own (re-submitting the same
   * credentials would just reproduce the same error).
   */
  function showHostKeyCancelledState(mode) {
    hostKeyCtrl?.closeHostKeyDialog()
    try { shellWrap.classList.add('hidden') } catch { /* ignore */ }
    try { disconnectedWrap?.classList.add('hidden') } catch { /* ignore */ }
    try { sessionEndedWrap?.classList.add('hidden') } catch { /* ignore */ }
    // Wipe the stale "received credentials, connecting..." status
    // line that the BroadcastChannel auto-connect path leaves in
    // place; without this it would still claim a connect was in
    // progress under the new error banner.
    try {
      const stale = container.querySelector('#term-broadcast-info')
      if (stale) stale.textContent = ''
    } catch { /* ignore */ }
    const title = t('hostKey.cancelledTitle')
    const body = mode === 'mismatch'
      ? t('hostKey.cancelledMismatch')
      : t('hostKey.cancelledUnknown')
    errorEl.textContent = `${title} — ${body}`
    errorEl.classList.remove('hidden')
    try { credsWrap.classList.remove('hidden') } catch { /* ignore */ }
    try { connectBtn.disabled = false } catch { /* ignore */ }
  }

  let currentSessionId = resumeSessionId || null
  // Set once the parent-tab-credential-relay info line (#term-broadcast-info,
  // "received credentials, connecting…") is created. showConnectError()
  // clears it on failure so it doesn't linger next to the real error --
  // otherwise a failed auth attempt shows both "connecting…" and the
  // error at the same time, which reads as nonsense.
  let broadcastInfoEl = null

  /**
   * Shows a connection error and undoes any state that assumed the
   * connection would succeed:
   *  - clears the stale "connecting…" status line from the parent-tab
   *    credential relay, if any.
   *  - hides the "enter a session name, we'll use the stored credentials"
   *    hint, which is only relevant while a connection attempt hasn't
   *    failed yet.
   *  - re-enables the Connect button and clears any BroadcastChannel
   *    wait-lock, both of which are left in their "attempt in
   *    progress" state (disabled / readOnly) by the parent-tab
   *    credential relay and never reset on their own. Without this,
   *    re-showing credsWrap after a failed attempt looks like nothing
   *    can be retried: the fields may be visible but Connect stays
   *    dead.
   *  - forgets currentSessionId and tears down the sharing banner /
   *    participant polling. The backend sends the session_id meta frame
   *    *before* it attempts to authenticate against the target (so the
   *    client can resume even if the bridge dies moments later), which
   *    means setCurrentSessionId() can already have fired -- creating a
   *    "you hold the write token" banner and starting participant
   *    polling -- by the time an auth failure arrives. None of that is
   *    true once we're showing a real error, so it must not linger.
   */
  function showConnectError(msg) {
    errorEl.textContent = msg
    errorEl.classList.remove('hidden')
    if (broadcastInfoEl) broadcastInfoEl.textContent = ''
    const storedCredHint = container.querySelector('#term-stored-cred-hint')
    if (storedCredHint) storedCredHint.classList.add('hidden')
    connectBtn.disabled = false
    if (currentSessionId) {
      currentSessionId = null
      syncInviteManageButton()
      detachSharingEvents()
    }
    const banner = container.querySelector('#sharing-status-banner')
    if (banner) banner.remove()
  }
  const currentSessionIdReady = (() => {
    if (currentSessionId) return Promise.resolve(currentSessionId)
    let resolve
    const p = new Promise((r) => { resolve = r })
    p._resolve = resolve
    return p
  })()
  function syncInviteManageButton() {
    if (!inviteManageBtn) return
    const show = !!currentSessionId && sharingMode !== 'viewer'
    inviteManageBtn.classList.toggle('hidden', !show)
    inviteManageBtn.disabled = !show
    if (participantsManageBtn) {
      participantsManageBtn.classList.toggle('hidden', !show)
      participantsManageBtn.disabled = !show
    }
  }

  function setCurrentSessionId(v) {
    if (!v) return
    currentSessionId = v
    syncInviteManageButton()
    ensureSharingBanner()
    attachSharingEvents(v)
    void refreshParticipants(v)
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

  if (inviteManageBtn) {
    inviteManageBtn.addEventListener('click', async () => {
      const sid = currentSessionId
      if (!sid || sharingMode === 'viewer') return
      const { openInviteDialog } = await import('./invite_dialog.js')
      openInviteDialog({ sessionId: sid, targetName, escapeHtml })
    })
  }
  if (participantsManageBtn) {
    participantsManageBtn.addEventListener('click', async () => {
      const sid = currentSessionId
      if (!sid || sharingMode === 'viewer') return
      const { openParticipantsDialog } = await import('./participants_dialog.js')
      openParticipantsDialog({
        sessionId: sid,
        targetName,
        escapeHtml,
        getState: () => ({ participants: cachedParticipants, kicked: cachedKicked }),
        onKick: (uid, name) => kickParticipant(sid, uid, name),
      })
    })
  }
  syncInviteManageButton()

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
  cancelBtn.addEventListener('click', goHomeOrCloseToOpener)
  // Viewers get "Leave" instead of the owner-only "End session" (E-14):
  // leaving just closes this tab; the owner's session keeps running.
  container.querySelector('#term-leave')?.addEventListener('click', goHomeOrCloseToOpener)

  function connectResume(sessionId) {
    if (!sessionId) return
    hostKeyCtrl?.resetHostKeyError()
    credsWrap.classList.add('hidden')
    shellWrap.classList.remove('hidden')
    errorEl.classList.add('hidden')
    disconnectedWrap.classList.add('hidden')
    // currentSessionId is pre-seeded from the ?session_id= URL param (see
    // its declaration) so it stays truthy even when THIS resume attempt
    // never actually re-establishes anything -- it must not be used on
    // its own to decide whether to show the "session is still running on
    // the backend" recovery UI. sawFirstMessage / sawError track whether
    // *this* attempt got anywhere, mirroring connectWithCredentials /
    // connectWithStoredCredentials below. sawRealSession additionally
    // requires actual shell output (not just the session_id meta frame,
    // which the server sends before it even knows whether the bridge is
    // still reachable) -- see handleTerminalWsMessage's onData.
    let sawError = false
    let sawFirstMessage = false
    let sawRealSession = false
    // See connectWithCredentials for why this guard is needed: onclose
    // fires right after handleTerminalWsMessage handles a clean
    // session_ended frame, by which point currentSessionId has already
    // been nulled, so without this flag onclose would misread the clean
    // shutdown as a failed resume attempt.
    let sawSessionEnded = false
    const ws = new WebSocket(wsUrlResume(sessionId))
    ws.binaryType = 'arraybuffer'
    ws.onmessage = (ev) => {
      sawFirstMessage = true
      handleTerminalWsMessage(ws, ev, {
        onData: () => { sawRealSession = true },
        onSessionEnded: () => { sawSessionEnded = true },
        onError: (msg) => {
          sawError = true
          if (sharingMode === 'viewer') {
            showViewerRejoinFailed()
            return
          }
          showConnectError(msg)
          shellWrap.classList.add('hidden')
          credsWrap.classList.remove('hidden')
        },
      })
    }
    // onerror deliberately does NOT touch the UI. Whether it fires before
    // or after onmessage has processed a server-sent "error: ..." frame
    // is not reliably ordered across browsers (both are commonly
    // dispatched for the same abrupt-looking closure), so anything it
    // shows here can race with, and clobber, a real message. onclose
    // always fires last and is the single place that decides what to
    // show, using sawError as the record of what already happened.
    ws.onerror = () => {}
    ws.onclose = () => {
      if (leavingPage) return
      if (term) term.write(`\r\n\n${t('terminal.connectionClosed')}\r\n`)
      // host_key_unknown / host_key_mismatch closed the WS itself
      // and is showing its own dialog (or the post-cancel UI). Don't
      // pretend the session is "still running on the backend" in
      // that case — there is no surviving session.
      if (hostKeyCtrl?.getSawHostKeyError()) return
      if (sawError) return // already shown via onmessage's onError above
      if (sawSessionEnded) return // already shown via onmessage's session_ended handling above
      if (!sawFirstMessage) {
        // This attempt never actually re-attached to a live session
        // (connectivity failure before the server said anything) --
        // don't claim the session is still alive on the backend.
        if (sharingMode === 'viewer') {
          showViewerRejoinFailed()
          return
        }
        showConnectError(t('terminal.wsConnectFailedShort'))
        shellWrap.classList.add('hidden')
        credsWrap.classList.remove('hidden')
        return
      }
      if (currentSessionId && sawRealSession) {
        shellWrap.classList.add('hidden')
        disconnectedWrap.classList.remove('hidden')
        return
      }
      // Got a session_id but never any actual shell output before the
      // close -- the resume never really came up this time (e.g. the
      // backend session was already gone). Don't claim it's "still
      // running on the backend". Viewers get the dedicated dead end
      // rather than the owner's credentials form.
      if (sharingMode === 'viewer') {
        showViewerRejoinFailed()
        return
      }
      showConnectError(t('terminal.wsConnectFailedShort'))
      shellWrap.classList.add('hidden')
      credsWrap.classList.remove('hidden')
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
      showConnectError(t('terminal.targetIdMissingFromHome'))
      return
    }
    if (!username || !username.trim()) {
      showConnectError(t('terminal.usernameRequiredShort'))
      return
    }
    const user = username.trim()
    const name = typeof sessionName === 'string' ? sessionName.trim() : ''
    const description = typeof sessionDescription === 'string' ? sessionDescription.trim() : ''
    lastCredentials = {
      kind: 'credentials',
      username: user,
      password: password || '',
      sessionName: name,
      sessionDescription: description,
      privateKeyPassphrase: privateKeyPassphrase || '',
    }
    errorEl.classList.add('hidden')
    connectBtn.disabled = true
    hostKeyCtrl?.resetHostKeyError()
    credsWrap.classList.add('hidden')
    shellWrap.classList.remove('hidden')

    const ws = new WebSocket(getWsUrlNew())
    ws.binaryType = 'arraybuffer'
    let sawError = false
    let sawFirstMessage = false
    // Requires actual shell output, not just the session_id meta frame
    // (sent before the bridge even attempts to authenticate) -- see
    // handleTerminalWsMessage's onData and the comment on its use below.
    let sawRealSession = false
    // Set when handleTerminalWsMessage already handled a clean
    // session_ended frame (and shown sessionEndedWrap) on this socket.
    // onclose fires right after (the handler calls ws.close()) with
    // currentSessionId already nulled out by that same handler, so
    // without this guard the checks below would misread the clean
    // shutdown as "closed before any real session" and pop the connect
    // form + error text on top of the session-ended overlay.
    let sawSessionEnded = false
    const connectTimeout = window.setTimeout(() => {
      if (sawFirstMessage) return
      sawError = true
      showConnectError(t('terminal.connectTimedOutNew'))
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
        onData: () => { sawRealSession = true },
        onSessionEnded: () => { sawSessionEnded = true },
        onError: (msg) => {
          sawError = true
          showConnectError(msg)
          connectBtn.disabled = false
          credsWrap.classList.remove('hidden')
          shellWrap.classList.add('hidden')
        },
      })
    }

    // onerror deliberately does NOT touch the UI -- see the matching
    // comment in connectResume for why (its firing order relative to
    // onmessage is not reliably guaranteed, so anything it shows here
    // can race with, and clobber, a real "error: ..." message that
    // onmessage already surfaced). onclose always fires last and is the
    // single place that decides what to show.
    ws.onerror = () => {}

    ws.onclose = () => {
      if (leavingPage) return
      window.clearTimeout(connectTimeout)
      if (term) term.write(`\r\n\n${t('terminal.connectionClosed')}\r\n`)
      connectBtn.disabled = false
      // Host-key TOFU / mismatch already presented a dialog and
      // (post-cancel) the explanatory error banner. Don't fall back
      // to the generic "session continuing on backend" UI or auto-
      // close the tab — the user needs to interact with the dialog.
      if (hostKeyCtrl?.getSawHostKeyError()) return
      if (sawError) return // already shown via onmessage's onError above
      if (sawSessionEnded) return // already shown via onmessage's session_ended handling above
      if (currentSessionId && sawRealSession) {
        shellWrap.classList.add('hidden')
        disconnectedWrap.classList.remove('hidden')
        return
      }
      if (channelToken && !currentSessionId) {
        // Never got far enough to even receive a session_id -- likely
        // this popup's only purpose was the credential relay, so just
        // close it rather than leave a dead form behind.
        window.setTimeout(() => closeWindow(), 400)
        return
      }
      // Either never received anything at all, or got a session_id but
      // the bridge died before any real shell output arrived (e.g. an
      // auth failure whose "error: ..." frame didn't make it before the
      // socket closed) -- don't claim a session is still alive.
      showConnectError(t('terminal.closedNoFirstNew'))
      credsWrap.classList.remove('hidden')
      shellWrap.classList.add('hidden')
    }
  }

  function connectWithStoredCredentials(sessionName, sessionDescription, password, privateKeyPassphrase) {
    if (!targetId) return
    errorEl.classList.add('hidden')
    connectBtn.disabled = true
    hostKeyCtrl?.resetHostKeyError()
    credsWrap.classList.add('hidden')
    shellWrap.classList.remove('hidden')
    const name = typeof sessionName === 'string' ? sessionName.trim() : ''
    const description = typeof sessionDescription === 'string' ? sessionDescription.trim() : ''
    lastCredentials = {
      kind: 'stored',
      sessionName: name,
      sessionDescription: description,
      password: password || '',
      privateKeyPassphrase: privateKeyPassphrase || '',
    }
    const payload = { use_stored_credentials: true, name, description }
    if (password != null && password !== '') payload.password = password
    if (!isTelnet && privateKeyPassphrase != null && privateKeyPassphrase !== '') payload.private_key_passphrase = privateKeyPassphrase

    const ws = new WebSocket(getWsUrlNew())
    ws.binaryType = 'arraybuffer'
    let sawError = false
    let sawFirstMessage = false
    // Requires actual shell output, not just the session_id meta frame
    // (sent before the bridge even attempts to authenticate) -- see
    // handleTerminalWsMessage's onData and the comment on its use below.
    let sawRealSession = false
    // See connectWithCredentials for why this guard is needed: onclose
    // fires right after handleTerminalWsMessage handles a clean
    // session_ended frame, by which point currentSessionId has already
    // been nulled, so without this flag onclose would misread the clean
    // shutdown as a failed connection attempt.
    let sawSessionEnded = false
    const connectTimeout = window.setTimeout(() => {
      if (sawFirstMessage) return
      sawError = true
      showConnectError(t('terminal.connectTimedOutStored'))
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
        onData: () => { sawRealSession = true },
        onSessionEnded: () => { sawSessionEnded = true },
        onError: (msg) => {
          sawError = true
          showConnectError(msg)
          credsWrap.classList.remove('hidden')
          shellWrap.classList.add('hidden')
        },
      })
    }

    // onerror deliberately does NOT touch the UI -- see the matching
    // comment in connectResume for why. onclose always fires last and is
    // the single place that decides what to show.
    ws.onerror = () => {}

    ws.onclose = () => {
      if (leavingPage) return
      window.clearTimeout(connectTimeout)
      if (term) term.write(`\r\n\n${t('terminal.connectionClosed')}\r\n`)
      // See connectWithCredentials for why the host-key path
      // bypasses these recovery branches.
      if (hostKeyCtrl?.getSawHostKeyError()) return
      if (sawError) return // already shown via onmessage's onError above
      if (sawSessionEnded) return // already shown via onmessage's session_ended handling above
      if (currentSessionId && sawRealSession) {
        shellWrap.classList.add('hidden')
        disconnectedWrap.classList.remove('hidden')
        return
      }
      // Either never received anything at all, or got a session_id but
      // the bridge died before any real shell output arrived (e.g. an
      // auth failure whose "error: ..." frame didn't make it before the
      // socket closed) -- don't claim a session is still alive.
      showConnectError(t('terminal.closedNoFirstStored'))
      credsWrap.classList.remove('hidden')
      shellWrap.classList.add('hidden')
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
    // The sharing banner (write-token / viewer status) is intentionally
    // NOT shown here, before the WebSocket even attempts to attach:
    // setCurrentSessionId() already creates and populates it once the
    // resume genuinely succeeds (a real session_id meta frame arrives).
    // Showing it eagerly meant a failed resume (expired session, auth
    // rejected, etc.) left a stale "you hold the write token" banner on
    // screen for a session that was never actually attached.
    if (sharingMode === 'viewer') {
      // (Optionally) consume the invite before opening the WebSocket.
      // Failing to consume the invite is non-fatal: the user may
      // already have joined via the sessions page, in which case the
      // WebSocket attach succeeds.
      if (inviteToken) {
        joinAsViewer(resumeSessionId, inviteToken).finally(() => {
          connectResume(resumeSessionId)
          attachSharingEvents(resumeSessionId)
          refreshParticipants(resumeSessionId)
        })
      } else {
        connectResume(resumeSessionId)
        attachSharingEvents(resumeSessionId)
        refreshParticipants(resumeSessionId)
      }
    } else {
      connectResume(resumeSessionId)
      attachSharingEvents(resumeSessionId)
      void refreshParticipants(resumeSessionId)
    }
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
    infoEl.id = 'term-broadcast-info'
    infoEl.className = 'text-xs text-slate-500'
    infoEl.textContent = t('terminal.parentTabReceiving')
    container.querySelector('#term-credentials .px-5')?.appendChild(infoEl)
    broadcastInfoEl = infoEl

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
      if (!isSameOriginBroadcast(ev)) return
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
    setupTerminalKeyboard(term, xtermEl)
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
        // Drop input unless we currently hold the write token.
        // The bridge also enforces this server-side via SetWriter(),
        // but doing it client-side makes UX predictable even when the
        // viewer URL stays `mode=viewer` after token transfer.
        if (!holdsWriteToken()) return
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
    if (!termBeforeUnloadAttached) {
      termBeforeUnloadAttached = true
      // Close over currentWs (not the ws param) so this single listener,
      // registered once, always closes whichever socket is live at unload
      // time instead of accumulating a new listener on every reconnect.
      window.addEventListener('beforeunload', () => {
        try { currentWs && currentWs.close() } catch { /* ignore */ }
      })
    }
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

  // ---- Collaborative session (sharing) banner ----------------------

  function removeLegacySharingBanners() {
    try { container.querySelector('#sharing-viewer-banner')?.remove() } catch { /* ignore */ }
    try { container.querySelector('#sharing-writer-banner')?.remove() } catch { /* ignore */ }
    try { container.querySelector('#sharing-demoted-banner')?.remove() } catch { /* ignore */ }
    try { container.querySelector('#sharing-write-request-banner')?.remove() } catch { /* ignore */ }
  }

  function ensureSharingBanner() {
    removeLegacySharingBanners()
    if (container.querySelector('#sharing-status-banner')) return
    const root = container.querySelector('.terminal-page-root')
    if (!root) return
    const banner = document.createElement('div')
    banner.id = 'sharing-status-banner'
    // Color is toggled by renderSharingBanner().
    banner.className = 'sharing-status-banner shrink-0 text-white text-xs px-3 py-1.5 flex items-center gap-3 justify-between flex-nowrap overflow-hidden'
    banner.innerHTML = `
      <span data-banner-text="1" class="min-w-0 flex-1 truncate"></span>
      <div class="flex items-center gap-2 shrink-0">
        <button type="button" id="sharing-review-write" class="hidden rounded bg-amber-300 px-2 py-0.5 text-xs font-semibold text-amber-950 hover:bg-amber-200 whitespace-nowrap">${t('sharing.reviewWriteRequest')}</button>
        <button type="button" id="sharing-request-write" class="rounded bg-white px-2 py-0.5 text-xs font-semibold whitespace-nowrap">${t('sharing.requestWrite')}</button>
        <button type="button" id="sharing-release-write" class="rounded bg-white px-2 py-0.5 text-xs font-semibold whitespace-nowrap">${t('sharing.releaseToken')}</button>
      </div>
    `
    const header = root.querySelector('header')
    if (header && header.parentNode === root) header.insertAdjacentElement('afterend', banner)
    else root.insertBefore(banner, root.firstChild)
    banner.querySelector('#sharing-request-write')?.addEventListener('click', requestWriteToken)
    banner.querySelector('#sharing-release-write')?.addEventListener('click', releaseWriteToken)
    banner.querySelector('#sharing-review-write')?.addEventListener('click', () => {
      if (pendingWriteRequestApproval) {
        showIncomingWriteRequestModal(pendingWriteRequestApproval)
      }
    })
    renderSharingBanner()
  }

  function renderSharingBanner() {
    const banner = container.querySelector('#sharing-status-banner')
    if (!banner) return
    const span = banner.querySelector('[data-banner-text="1"]')
    const btnRequest = banner.querySelector('#sharing-request-write')
    const btnRelease = banner.querySelector('#sharing-release-write')
    const btnReview = banner.querySelector('#sharing-review-write')

    const canWrite = holdsWriteToken()
    const holderLabel = writerDisplayName || viewerOwnerName || '—'
    const awaitingApproval = !!(pendingWriteRequestApproval && canWrite)
    const isOwner = !!(sessionOwnerId && myUserId && myUserId === sessionOwnerId)

    // Red = write token holder, blue = view-only, amber tint when approval needed.
    banner.classList.toggle('bg-red-600', canWrite && !awaitingApproval)
    banner.classList.toggle('bg-amber-600', awaitingApproval)
    banner.classList.toggle('bg-sky-700', !canWrite && !awaitingApproval)

    if (span) {
      if (awaitingApproval) {
        const name = pendingWriteRequestApproval.username || pendingWriteRequestApproval.user_id || ''
        span.textContent = t('sharing.pendingWriteRequestInline', { name })
      } else if (canWrite) {
        span.textContent = t('sharing.youAreWriter')
      } else if (myPendingWriteRequest) {
        span.textContent = t('sharing.youAreViewerPending')
      } else {
        span.textContent = t('sharing.youAreViewer', { holder: holderLabel })
      }
    }

    if (btnReview) {
      btnReview.classList.toggle('hidden', !awaitingApproval)
    }

    if (btnRequest) {
      const showRequest = !canWrite && !awaitingApproval
      btnRequest.classList.toggle('hidden', !showRequest)
      btnRequest.classList.toggle('text-sky-900', showRequest)
      if (showRequest) {
        if (myPendingWriteRequest) {
          btnRequest.textContent = t('sharing.requestWriteSent')
          btnRequest.disabled = true
        } else {
          btnRequest.textContent = t('sharing.requestWrite')
          btnRequest.disabled = false
        }
      }
    }

    if (btnRelease) {
      const showRelease = canWrite && !awaitingApproval && !isOwner && sessionOwnerId
      btnRelease.classList.toggle('hidden', !showRelease)
      btnRelease.classList.toggle('text-red-700', showRelease)
    }
  }

  async function joinAsViewer(sessionId, token) {
    try {
      const res = await API.joinSession(sessionId, { invitationToken: token })
      if (res?.role) {
        // Best-effort: a returning viewer may already have been a
        // participant; the response is purely informational.
      }
    } catch (err) {
      ensureSharingBanner()
      const banner = container.querySelector('#sharing-status-banner [data-banner-text="1"]')
      if (banner) banner.textContent = t('sharing.joinFailed', { error: err?.message || '' })
    }
  }

  async function requestWriteToken() {
    const sid = currentSessionId || resumeSessionId
    if (!sid) return
    const btn = container.querySelector('#sharing-request-write')
    if (btn) btn.disabled = true
    try {
      await API.createSessionWriteRequest(sid)
      myPendingWriteRequest = true
      renderSharingBanner()
    } catch (err) {
      await uiAlert(t('sharing.requestWriteFailed', { error: err?.message || '' }))
      if (btn) btn.disabled = false
    }
  }

  async function releaseWriteToken() {
    const sid = currentSessionId || resumeSessionId
    if (!sid) return
    // Release is only meaningful if this tab currently holds the token.
    if (!holdsWriteToken()) return
    if (!(await uiConfirm(t('sharing.releaseTokenConfirm'), { danger: true }))) return
    const btn = container.querySelector('#sharing-release-write')
    if (btn) btn.disabled = true
    try {
      await API.releaseSessionWriteToken(sid)
      // Best-effort: sync UI immediately instead of waiting for SSE/poll.
      void refreshParticipants(sid)
    } catch {
      await uiAlert(t('sharing.releaseFailed'))
    } finally {
      if (btn) btn.disabled = false
    }
  }

  function attachSharingEvents(sessionId) {
    if (!sessionId) return
    if (sharingEventsSessionId === sessionId && stopSharingWatch) return
    detachSharingEvents()
    sharingEventsSessionId = sessionId
    stopSharingWatch = createRealtimeWatcher({
      shouldRefresh: (payload) => {
        const sid = currentSessionId || sharingEventsSessionId
        return shouldRefreshTerminalSharing(payload, sid)
      },
      onEvent: (payload) => handleSharingEvent(payload),
      onRefresh: () => {
        const sid = currentSessionId || sharingEventsSessionId
        if (!sid) return
        void refreshParticipants(sid)
      },
      pollMs: POLL_MS.terminalSharing,
    })
  }

  function detachSharingEvents() {
    sharingEventsSessionId = ''
    if (!stopSharingWatch) return
    try {
      stopSharingWatch()
    } catch {
      /* ignore */
    }
    stopSharingWatch = null
  }

  /** True when this tab holds the session write token (from server state). */
  function holdsWriteToken() {
    if (!writerUserId) {
      // Before /participants loads (and before /api/me may have resolved),
      // only the initial writer attach may send stdin. This must not depend
      // on myUserId being populated yet, or the owner's own keystrokes get
      // silently dropped for the brief window before API.me() resolves.
      return !participantsLoaded && sharingMode === 'writer'
    }
    return !!myUserId && myUserId === writerUserId
  }

  function handleSharingEvent(payload) {
    switch (payload.type) {
      case 'write_token_transferred':
        handleWriteTokenTransferred(payload)
        return
      case 'write_request_pending':
        if (payload.user_id && myUserId && payload.user_id === myUserId) return
        void (async () => {
          const sid = payload.session_id || currentSessionId || sharingEventsSessionId
          await refreshParticipants(sid)
          if (holdsWriteToken()) queueWriteRequestApproval(payload)
        })()
        return
      case 'write_request_decided':
        shownWriteRequestId = ''
        myPendingWriteRequest = false
        clearWriteRequestApproval()
        renderSharingBanner()
        return
      case 'session_change':
      case 'participant_joined':
        void refreshParticipants(payload.session_id || currentSessionId || sharingEventsSessionId)
        return
      case 'participant_left':
        if (
          payload.extra?.reason === 'kicked' &&
          payload.user_id &&
          myUserId &&
          payload.user_id === myUserId
        ) {
          showKickedSessionEnded()
          return
        }
        void refreshParticipants(payload.session_id || currentSessionId || sharingEventsSessionId)
        return
      case 'invitation_revoked':
      case 'invitation_updated':
      case 'invitation_consumed':
        return
      default:
        return
    }
  }

  function handleWriteTokenTransferred(payload) {
    const newWriter = payload.user_id || ''
    writerUserId = newWriter
    participantsLoaded = true
    // A transfer ends any "waiting for approval" state for viewers who
    // did not receive the token (grant consumed, deny, or release).
    if (myUserId && newWriter !== myUserId) {
      myPendingWriteRequest = false
      clearWriteRequestApproval()
    }
    ensureSharingBanner()
    renderSharingBanner()
    if (sharingMode === 'viewer') {
      // Keep best-effort: reload to writer mode for future reattach.
      refreshSelfRole(payload.session_id, newWriter)
    }
  }

  async function refreshSelfRole(sessionId, newWriter) {
    if (!sessionId) return
    try {
      const me = await API.me()
      if (me?.user_id && newWriter && me.user_id === newWriter) {
        // We are now the writer. Reloading rejoins as writer.
        const url = new URL(window.location.href)
        url.searchParams.set('mode', 'writer')
        url.searchParams.delete('invite')
        window.location.replace(url.toString())
      }
    } catch {
      /* ignore */
    }
  }

  async function refreshParticipants(sessionId) {
    if (!sessionId) return
    await myUserIdReady
    ensureSharingBanner()
    try {
      const res = await API.listSessionParticipants(sessionId)
      writerUserId = res?.writer_id || ''
      sessionOwnerId = res?.owner_id || ''
      participantsLoaded = true
      const owner = (res?.items || []).find((p) => p.role === 'owner')
      if (owner?.username) viewerOwnerName = owner.username
      const writer = (res?.items || []).find((p) => p.is_writer)
      writerDisplayName = res?.writer_username || writer?.username || writer?.user_id || ''

      cachedParticipants = Array.isArray(res?.items) ? res.items : []
      cachedKicked = Array.isArray(res?.kicked) ? res.kicked : []
      const pending = Array.isArray(res?.pending_requests) ? res.pending_requests : []
      myPendingWriteRequest = pending.some(
        (wr) => wr && wr.status === 'pending' && myUserId && wr.requester_id === myUserId,
      )
      const first = pending.find((wr) => wr && wr.status === 'pending')
      if (first && holdsWriteToken()) {
        queueWriteRequestApproval({
          session_id: sessionId,
          user_id: first.requester_id,
          username: first.requester_name,
          extra: { request_id: first.id },
        })
      } else {
        clearWriteRequestApproval()
      }
    } catch {
      /* ignore */
    } finally {
      renderSharingBanner()
      renderParticipantsPanel(sessionId)
    }
  }

  // Replace the whole page with a terminal-state message plus a single
  // "Back to home" button. Used for the two viewer-side dead ends where
  // neither a credentials form nor a Reconnect button makes sense: being
  // kicked by the owner, and a viewer resume that the server refused.
  function showViewerDeadEnd(titleKey, hintKey) {
    try {
      if (currentWs && currentWs.readyState === WebSocket.OPEN) currentWs.close()
    } catch { /* ignore */ }
    const root = container.querySelector('.terminal-page-root')
    if (!root) return
    root.innerHTML = `
      <div class="flex flex-1 items-center justify-center p-8 text-center">
        <div class="max-w-md space-y-4">
          <h2 class="text-lg font-semibold text-slate-800">${escapeHtml(t(titleKey))}</h2>
          <p class="text-sm text-slate-600">${escapeHtml(t(hintKey))}</p>
          <button id="term-back-from-dead-end" type="button" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm">${escapeHtml(t('terminal.backHome'))}</button>
        </div>
      </div>
    `
    root.querySelector('#term-back-from-dead-end')?.addEventListener('click', goHomeOrCloseToOpener)
  }

  function showKickedSessionEnded() {
    showViewerDeadEnd('sharing.youWereKicked', 'sharing.youWereKickedHint')
  }

  // A viewer whose resume the server refused (kicked earlier, invitation
  // revoked/expired, session gone) must not be dropped onto the owner's
  // SSH credentials form (E-15): show a dedicated dead end instead.
  function showViewerRejoinFailed() {
    showViewerDeadEnd('terminal.viewerRejoinFailed', 'terminal.viewerRejoinFailedHint')
  }

  async function kickParticipant(sessionId, userId, displayName) {
    if (!sessionId || !userId) return
    const ok = await uiConfirm(t('sharing.kickConfirm', { name: displayName || userId }))
    if (!ok) return
    try {
      await API.kickSessionParticipant(sessionId, userId)
      await refreshParticipants(sessionId)
    } catch (err) {
      await uiAlert(t('sharing.kickFailed') + (err?.message ? `: ${err.message}` : ''))
    }
  }

  // The participants list lives in a modal (participants_dialog.js) opened
  // from the "Participants (N)" header button, so the terminal area no
  // longer shrinks as viewers join (F-1). This just keeps the button's
  // count current and re-renders the dialog if it is open.
  function renderParticipantsPanel(sessionId) {
    const root = container.querySelector('.terminal-page-root')
    if (!root || !sessionId) return
    // Drop any legacy inline panel left over from an older bundle.
    root.querySelector('#sharing-participants-panel')?.remove()
    const isOwner = !!(sessionOwnerId && myUserId && myUserId === sessionOwnerId)
    if (participantsManageBtn) {
      const viewers = cachedParticipants.filter((p) => p && p.role === 'viewer')
      participantsManageBtn.textContent = `${t('terminal.actionParticipants')} (${viewers.length})`
      const show = isOwner && !!currentSessionId && sharingMode !== 'viewer'
      participantsManageBtn.classList.toggle('hidden', !show)
      participantsManageBtn.disabled = !show
    }
    refreshParticipantsDialog()
  }


  function clearWriteRequestApproval() {
    pendingWriteRequestApproval = null
    try { document.getElementById('sharing-write-request-modal')?.remove() } catch { /* ignore */ }
  }

  function queueWriteRequestApproval(payload) {
    const reqId = payload?.extra?.request_id || ''
    if (!reqId) return
    pendingWriteRequestApproval = payload
    renderSharingBanner()
  }

  function showIncomingWriteRequestModal(payload) {
    const reqId = payload.extra?.request_id || ''
    if (reqId && reqId === shownWriteRequestId && document.getElementById('sharing-write-request-modal')) {
      return
    }
    if (reqId) shownWriteRequestId = reqId
    const existing = document.getElementById('sharing-write-request-modal')
    if (existing) existing.remove()
    const wrap = document.createElement('div')
    wrap.id = 'sharing-write-request-modal'
    wrap.className = 'fixed inset-0 z-[210] flex items-center justify-center bg-black/40 backdrop-blur-sm p-4'
    wrap.innerHTML = `
      <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
        <div class="px-5 py-4 border-b border-slate-200 bg-slate-50">
          <h3 class="font-semibold text-slate-800">${t('sharing.incomingRequestTitle')}</h3>
        </div>
        <div class="px-6 py-5 space-y-4">
          <p class="text-sm text-slate-700">${t('sharing.incomingRequestBody', { name: escapeHtml(payload.username || payload.user_id || '') })}</p>
        </div>
        <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
          <button type="button" data-deny="1" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm">${t('sharing.denyRequest')}</button>
          <button type="button" data-grant="1" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm">${t('sharing.grantRequest')}</button>
        </div>
      </div>
    `
    document.body.appendChild(wrap)
    const close = () => {
      shownWriteRequestId = ''
      try { wrap.remove() } catch { /* ignore */ }
    }
    wrap.querySelector('[data-deny="1"]').addEventListener('click', async () => {
      if (!reqId) return close()
      try { await API.denySessionWriteRequest(payload.session_id, reqId) } catch { /* ignore */ }
      close()
    })
    wrap.querySelector('[data-grant="1"]').addEventListener('click', async () => {
      if (!reqId) return close()
      try { await API.grantSessionWriteRequest(payload.session_id, reqId) } catch { await uiAlert(t('sharing.grantFailed')) }
      close()
    })
    wrap.addEventListener('click', (e) => {
      if (e.target === wrap) close()
    })
  }
}

