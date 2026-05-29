/**
 * @file Shared SSE fan-out and refresh predicates for collaborative sessions.
 *
 * All UI surfaces subscribe to the same `/api/events/sessions` stream via
 * {@link subscribeSharingEvents}. Use {@link createRealtimeWatcher} for the
 * standard SSE + visibility + polling fallback pattern.
 */

/** @type {EventSource | null} */
let eventSource = null
/** @type {Set<(payload: object) => void>} */
const listeners = new Set()

const SSE_RECONNECT_MS = 1000

/** Fallback poll intervals (ms) per surface. */
export const POLL_MS = {
  incoming: 5000,
  inviteList: 2000,
  sessionList: 5000,
  terminalSharing: 1500,
}

/** Mirrors internal/sharing/events.go */
export const SharingEventType = {
  sessionChange: 'session_change',
  participantJoined: 'participant_joined',
  participantLeft: 'participant_left',
  writeRequestPending: 'write_request_pending',
  writeRequestDecided: 'write_request_decided',
  writeTokenTransferred: 'write_token_transferred',
  invitationRevoked: 'invitation_revoked',
  invitationCreated: 'invitation_created',
  invitationReceived: 'invitation_received',
  invitationUpdated: 'invitation_updated',
  invitationConsumed: 'invitation_consumed',
}

const INVITE_LIST_REFRESH_EVENTS = new Set([
  SharingEventType.invitationCreated,
  SharingEventType.invitationRevoked,
  SharingEventType.invitationUpdated,
  SharingEventType.invitationConsumed,
  SharingEventType.participantJoined,
])

const INCOMING_INVITATION_EVENTS = new Set([
  SharingEventType.invitationReceived,
  SharingEventType.invitationRevoked,
  SharingEventType.invitationConsumed,
])

const TERMINAL_SHARING_REFRESH_EVENTS = new Set([
  SharingEventType.participantJoined,
  SharingEventType.participantLeft,
  SharingEventType.invitationRevoked,
  SharingEventType.invitationUpdated,
  SharingEventType.invitationConsumed,
  SharingEventType.writeRequestPending,
  SharingEventType.writeRequestDecided,
  SharingEventType.writeTokenTransferred,
])

export function eventType(payload) {
  return (payload?.type || '').trim()
}

export function payloadSessionId(payload) {
  return (payload?.session_id || '').trim()
}

export function isSessionChange(payload) {
  return eventType(payload) === SharingEventType.sessionChange
}

/** Session list / active-session counts (global). */
export function shouldRefreshSessionList(payload) {
  return isSessionChange(payload)
}

/** Home incoming named-invitation banner. */
export function shouldRefreshIncomingInvitationsBanner(payload) {
  const type = eventType(payload)
  return type === SharingEventType.sessionChange || INCOMING_INVITATION_EVENTS.has(type)
}

/** Issued-invitations table in the invite dialog (session-scoped). */
export function shouldRefreshInviteList(payload, sessionId) {
  if (!payload || !sessionId) return false
  if (isSessionChange(payload)) return true
  if (payloadSessionId(payload) !== sessionId) return false
  return INVITE_LIST_REFRESH_EVENTS.has(eventType(payload))
}

/** Terminal sharing UI: participants, write token, invitations (session-scoped). */
export function shouldRefreshTerminalSharing(payload, sessionId) {
  if (!payload || !sessionId) return false
  if (isSessionChange(payload)) return true
  if (payloadSessionId(payload) !== sessionId) return false
  return TERMINAL_SHARING_REFRESH_EVENTS.has(eventType(payload))
}

function ensureEventSource() {
  if (eventSource) return
  try {
    const url = new URL('/api/events/sessions', window.location.origin).toString()
    eventSource = new EventSource(url)
    eventSource.onmessage = (ev) => {
      let payload
      try {
        payload = JSON.parse(ev.data || '{}')
      } catch {
        return
      }
      if (!payload || typeof payload !== 'object') return
      for (const fn of listeners) {
        try {
          fn(payload)
        } catch {
          /* ignore subscriber errors */
        }
      }
    }
    eventSource.onerror = () => {
      try {
        eventSource?.close()
      } catch {
        /* ignore */
      }
      eventSource = null
      if (listeners.size > 0) {
        window.setTimeout(() => {
          if (listeners.size > 0 && !eventSource) ensureEventSource()
        }, SSE_RECONNECT_MS)
      }
    }
  } catch {
    eventSource = null
  }
}

function maybeCloseEventSource() {
  if (listeners.size > 0) return
  try {
    eventSource?.close()
  } catch {
    /* ignore */
  }
  eventSource = null
}

/**
 * Subscribe to session/sharing SSE payloads.
 *
 * @param {(payload: object) => void} handler
 * @returns {() => void} Unsubscribe function.
 */
export function subscribeSharingEvents(handler) {
  if (typeof handler !== 'function') return () => {}
  listeners.add(handler)
  ensureEventSource()
  return () => {
    listeners.delete(handler)
    maybeCloseEventSource()
  }
}

/**
 * SSE + tab visibility + optional polling fallback.
 *
 * @param {{
 *   shouldRefresh: (payload: object) => boolean,
 *   onRefresh: () => void | Promise<void>,
 *   onEvent?: (payload: object) => void,
 *   pollMs?: number,
 * }} options
 * @returns {() => void} Stop watching.
 */
export function createRealtimeWatcher({ shouldRefresh, onRefresh, onEvent, pollMs = 0 }) {
  let pollTimer = null
  let visibilityHandler = null
  let unsub = null

  const stop = () => {
    if (pollTimer != null) {
      window.clearInterval(pollTimer)
      pollTimer = null
    }
    if (visibilityHandler) {
      document.removeEventListener('visibilitychange', visibilityHandler)
      visibilityHandler = null
    }
    if (unsub) {
      try {
        unsub()
      } catch {
        /* ignore */
      }
      unsub = null
    }
  }

  if (typeof onRefresh !== 'function' || typeof shouldRefresh !== 'function') {
    return stop
  }

  const runRefresh = () => {
    void onRefresh()
  }

  unsub = subscribeSharingEvents((data) => {
    if (!shouldRefresh(data)) return
    if (typeof onEvent === 'function') {
      try {
        onEvent(data)
      } catch {
        /* ignore */
      }
    }
    runRefresh()
  })

  visibilityHandler = () => {
    if (document.visibilityState === 'visible') runRefresh()
  }
  document.addEventListener('visibilitychange', visibilityHandler)

  if (pollMs > 0) {
    pollTimer = window.setInterval(runRefresh, pollMs)
  }

  return stop
}
