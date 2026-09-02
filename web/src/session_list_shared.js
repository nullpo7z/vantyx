import API from './api.js'
import { safeUrl } from './dom_helpers.js'
import { t as tr } from './i18n.js'
import { uiAlert, uiConfirm } from './ui_dialog.js'

/** Relative last-activity label for display. */
export function formatLastSeen(value) {
  if (!value) return '—'
  const t = new Date(value).getTime()
  if (Number.isNaN(t)) return '—'
  const sec = Math.floor((Date.now() - t) / 1000)
  if (sec < 60) return tr('sessions.relativeJustNow')
  if (sec < 3600) return tr('sessions.relativeMinutesAgo', { n: Math.floor(sec / 60) })
  if (sec < 86400) return tr('sessions.relativeHoursAgo', { n: Math.floor(sec / 3600) })
  return tr('sessions.relativeDaysAgo', { n: Math.floor(sec / 86400) })
}

function sessionEndLabel(name, targetName, targetId) {
  const title = (name || '').trim() || tr('sessions.untitled')
  const target = (targetName || '').trim() || targetId || ''
  return target ? `${title}（${target}）` : title
}

function sessionEndModalContext(s, { kind = 'terminal' } = {}) {
  const name =
    kind === 'rdp'
      ? (s.target_name || s.target_id || '').trim() || tr('sessions.untitled')
      : (s.name || '').trim() || tr('sessions.untitled')
  const fullPath = targetFullPathForDisplay(s)
  const descRaw = sessionDescriptionForDisplay(s)
  return {
    name,
    fullPath,
    description: descRaw === '—' ? '' : descRaw,
    label: sessionEndLabel(s.name || s.target_name, s.target_name, s.target_id),
  }
}

function sessionEndButtonAttrs(s, escapeHtml, { kind = 'terminal' } = {}) {
  const ctx = sessionEndModalContext(s, { kind })
  return [
    'data-session-end="1"',
    `data-session-id="${escapeHtml(s.session_id)}"`,
    `data-session-kind="${kind}"`,
    `data-session-label="${escapeHtml(ctx.label)}"`,
    `data-session-name="${escapeHtml(ctx.name)}"`,
    `data-session-fullpath="${escapeHtml(ctx.fullPath)}"`,
    `data-session-description="${escapeHtml(ctx.description)}"`,
  ].join(' ')
}

function isTftpConsoleSession(s) {
  return typeof s.description === 'string' && s.description.startsWith('TFTP_CONSOLE:')
}

function tftpTargetFromDescription(desc) {
  if (!desc || !desc.startsWith('TFTP_CONSOLE:')) return ''
  const m = desc.match(/tftp_target_id=([^;]+)/)
  return m && m[1] ? m[1] : ''
}

/** 一覧の「種別」列用ラベル（SSH / Telnet / TFTP コンソール / RDP） */
export function sessionProtocolLabel(s, { kind = 'terminal' } = {}) {
  if (kind === 'rdp') return 'RDP'
  if (isTftpConsoleSession(s)) return tr('tftp.title')
  const p = String(s.protocol || '').toLowerCase()
  if (p === 'ssh') return 'SSH'
  if (p === 'telnet') return 'Telnet'
  if (p === 'vnc') return 'VNC'
  if (p === 'tftp') return 'TFTP'
  if (p === 'rdp') return 'RDP'
  if (s.protocol) return String(s.protocol).toUpperCase()
  return '—'
}

/** ターゲットのフルパス（階層 path + 名前。例: prod/network/router1） */
export function targetFullPathForDisplay(s) {
  const name = (s.target_name || s.target_id || '').trim()
  const path = (s.target_path || '').trim().replace(/\/+$/, '')
  if (path && name) return `${path}/${name}`
  if (path) return path
  return name || '—'
}

/** ユーザー向け説明（TFTP 内部メタデータは表示しない） */
export function sessionDescriptionForDisplay(s) {
  if (!s.description) return '—'
  if (isTftpConsoleSession(s)) return '—'
  return s.description
}

const SESSIONS_CELL = 'px-4 py-3 align-middle'
const SESSIONS_CELL_SHRINK = 'sessions-col-shrink px-2 py-3 align-middle text-sm'
const SESSIONS_CELL_ACTIONS = 'sessions-col-actions px-2 py-3 align-middle'
const SESSIONS_ACTION_BTNS =
  'inline-flex flex-nowrap items-center justify-start gap-1.5 shrink-0'
const SESSIONS_BTN =
  'rounded px-2 py-1 text-xs font-medium whitespace-nowrap'

// keepBadge renders a small "kept" pill shown next to a pinned session's
// title so it is clear the idle warning is intentionally suppressed.
function keepBadge(kept) {
  return kept
    ? `<span class="ml-1 inline-flex items-center rounded bg-sky-100 px-1.5 py-0.5 text-[10px] font-medium uppercase text-sky-800 align-middle" title="${tr('sessions.keepTitle')}">${tr('sessions.keepBadge')}</span>`
    : ''
}

// keepButton renders the pin / unpin toggle for a session the caller owns.
function keepButton(s, escapeHtml, kind) {
  const kept = !!s.keep
  const label = kept ? tr('sessions.keepUnmark') : tr('sessions.keepMark')
  const cls = kept
    ? `${SESSIONS_BTN} border border-sky-300 bg-sky-50 text-sky-800 hover:bg-sky-100`
    : `${SESSIONS_BTN} border border-slate-300 bg-white text-slate-700 hover:bg-slate-50`
  return `<button type="button" data-session-keep="1" data-session-id="${escapeHtml(s.session_id)}" data-session-kind="${kind}" data-keep="${kept ? '1' : '0'}" class="${cls}" title="${tr('sessions.keepTitle')}">${label}</button>`
}

function sessionTableRowClass(idle) {
  return idle
    ? 'border-b border-amber-200/80 bg-amber-50 hover:bg-amber-100/80'
    : 'border-b border-slate-200 hover:bg-slate-50'
}

function sessionListItemClass(idle) {
  return idle
    ? 'flex items-start justify-between gap-3 py-2 px-3 rounded border border-amber-200 bg-amber-50 hover:bg-amber-100/80'
    : 'flex items-start justify-between gap-3 py-2 px-3 rounded border border-slate-100 hover:bg-slate-50'
}

/**
 * Renders session rows grouped by target (for modal). Returns HTML string.
 */
export function buildGroupedSessionListHTML(sessions, rdpSessions, escapeHtml) {
  const parts = []
  if (sessions.length > 0) {
    const byTarget = {}
    sessions.forEach((s) => {
      const id = s.target_id
      if (!byTarget[id]) byTarget[id] = []
      byTarget[id].push(s)
    })
    const targetIds = Object.keys(byTarget).sort((a, b) => {
      const na = targetFullPathForDisplay(byTarget[a][0])
      const nb = targetFullPathForDisplay(byTarget[b][0])
      return na.localeCompare(nb)
    })
    parts.push(
      targetIds
        .map((targetId) => {
          const list = byTarget[targetId]
          const fullPath = targetFullPathForDisplay(list[0])
          const rows = list.map((s) => renderSessionRowHTML(s, escapeHtml, { compact: true })).join('')
          return `<div class="mb-4"><h4 class="text-xs font-semibold text-slate-600 uppercase tracking-wide mb-2 font-mono normal-case">${escapeHtml(fullPath)}</h4><ul class="space-y-2">${rows}</ul></div>`
        })
        .join(''),
    )
  }
  if (rdpSessions.length > 0) {
    const rdpHtml = rdpSessions
      .map((r) => {
        const fullPath = escapeHtml(targetFullPathForDisplay(r))
        const url = `/rdp?target_id=${encodeURIComponent(r.target_id)}&target_name=${encodeURIComponent(r.target_name || r.target_id)}&session_id=${encodeURIComponent(r.session_id)}`
        const lastSeen = formatLastSeen(r.last_seen || r.created_at)
        return `<li class="${sessionListItemClass(r.idle)}">
          <div class="min-w-0 flex-1">
            <p class="text-sm font-medium text-slate-800 font-mono break-all">${fullPath}</p>
            <p class="text-xs text-slate-500 mt-0.5">${escapeHtml(tr('sessions.rdpRowLastSeen', { when: lastSeen }))}</p>
          </div>
          <div class="flex flex-col gap-1 shrink-0 self-center">
            <a href="${safeUrl(url)}" target="_blank" rel="noopener noreferrer" data-rdp-reconnect="1" data-rdp-target-id="${escapeHtml(r.target_id)}" data-rdp-href="${escapeHtml(url)}" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 text-center">${tr('sessions.reconnect')}</a>
            <button type="button" ${sessionEndButtonAttrs(r, escapeHtml, { kind: 'rdp' })} class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50">${tr('sessions.end')}</button>
          </div>
        </li>`
      })
      .join('')
    parts.push(`<div class="mb-4"><h4 class="text-xs font-semibold text-slate-600 uppercase tracking-wide mb-2">${tr('sessions.rdpBrowserGroup')}</h4><ul class="space-y-2">${rdpHtml}</ul></div>`)
  }
  return parts.join('')
}

function renderSessionRowHTML(s, escapeHtml, { compact = false }) {
  const titleText = s.name ? escapeHtml(s.name) : tr('sessions.untitled')
  const descText = sessionDescriptionForDisplay(s)
  const descHtml =
    descText !== '—'
      ? `<p class="text-sm text-slate-600 mt-1 break-words leading-normal">${escapeHtml(descText)}</p>`
      : ''
  const protoLabel = sessionProtocolLabel(s)
  const lastSeen = formatLastSeen(s.last_seen || s.created_at)
  const isTftp = isTftpConsoleSession(s)
  const reconnectLabel = isTftp ? tr('sessions.reconnectTftp') : tr('sessions.reconnect')
  const tftpTargetId = isTftp ? tftpTargetFromDescription(s.description) : ''
  const reconnectAttrs = isTftp
    ? `data-terminal-reconnect="1" data-session-id="${escapeHtml(s.session_id)}" data-reconnect-mode="tftp" data-target-id="${escapeHtml(s.target_id)}" data-tftp-target-id="${escapeHtml(tftpTargetId)}"`
    : `data-terminal-reconnect="1" data-session-id="${escapeHtml(s.session_id)}" data-target-id="${escapeHtml(s.target_id)}" data-target-name="${escapeHtml(s.target_name || '')}"`
  const meta = compact
    ? `<p class="text-sm text-slate-500 mt-1">${escapeHtml(tr('sessions.metaProtoLastSeen', { proto: protoLabel, when: lastSeen }))}</p>`
    : ''
  return `<li class="${sessionListItemClass(s.idle)}">
    <div class="min-w-0 flex-1">
      <p class="text-sm font-medium text-slate-800">${titleText}</p>
      ${descHtml}
      ${meta}
    </div>
    <div class="flex flex-col gap-1 shrink-0 self-center">
      <button type="button" ${reconnectAttrs} class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700">${reconnectLabel}</button>
      <button type="button" ${sessionEndButtonAttrs(s, escapeHtml, { kind: 'terminal' })} class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50">${tr('sessions.end')}</button>
    </div>
  </li>`
}

/**
 * Table body rows for the full sessions page.
 */
export function buildSessionsTableHTML(sessions, rdpSessions, escapeHtml) {
  const rows = []
  sessions.forEach((s) => {
    const titleText = s.name ? escapeHtml(s.name) : tr('sessions.untitled')
    const descText = sessionDescriptionForDisplay(s)
    const descCell =
      descText === '—'
        ? '<span class="text-slate-400">—</span>'
        : `<span class="break-words">${escapeHtml(descText)}</span>`
    const isTftp = isTftpConsoleSession(s)
    const tftpTargetId = isTftp ? tftpTargetFromDescription(s.description) : ''
    const role = (s.role || 'owner').toLowerCase()
    const isOwner = role === 'owner'
    // Viewer participants always reattach in viewer mode. The
    // terminal page picks up `mode=viewer` from the URL and disables
    // stdin client-side; the bridge enforces it server-side too.
    const reconnectModeAttr = isOwner ? '' : ` data-reconnect-attach-mode="viewer"`
    const reconnectAttrs = isTftp
      ? `data-terminal-reconnect="1" data-session-id="${escapeHtml(s.session_id)}" data-reconnect-mode="tftp" data-target-id="${escapeHtml(s.target_id)}" data-tftp-target-id="${escapeHtml(tftpTargetId)}"`
      : `data-terminal-reconnect="1" data-session-id="${escapeHtml(s.session_id)}" data-target-id="${escapeHtml(s.target_id)}" data-target-name="${escapeHtml(s.target_name || '')}"${reconnectModeAttr}`
    const inviteBtn = isOwner
      ? `<button type="button" data-session-invite="1" data-session-id="${escapeHtml(s.session_id)}" data-target-id="${escapeHtml(s.target_id)}" data-target-name="${escapeHtml(s.target_name || '')}" class="${SESSIONS_BTN} border border-slate-300 bg-white text-slate-700 hover:bg-slate-50" title="${tr('sharing.inviteTitle')}">${tr('sharing.invite')}</button>`
      : ''
    const roleBadge = isOwner
      ? ''
      : `<span class="ml-1 inline-flex items-center rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium uppercase text-amber-800 align-middle">${tr('sharing.roleViewer')}</span>`
    const ownerLine = !isOwner && s.owner_username
      ? `<div class="text-xs text-slate-500 mt-0.5">${escapeHtml(tr('sharing.ownedBy', { name: s.owner_username }))}</div>`
      : ''
    rows.push(`<tr class="${sessionTableRowClass(s.idle)}">
      <td class="${SESSIONS_CELL} text-sm font-medium text-slate-900 whitespace-nowrap">${titleText}${roleBadge}${keepBadge(s.keep)}${ownerLine}</td>
      <td class="${SESSIONS_CELL} text-sm text-slate-600 min-w-[8rem] max-w-md">${descCell}</td>
      <td class="${SESSIONS_CELL} text-sm text-slate-700 break-all font-mono">${escapeHtml(targetFullPathForDisplay(s))}</td>
      <td class="${SESSIONS_CELL_SHRINK} text-slate-800 font-medium">${escapeHtml(sessionProtocolLabel(s))}</td>
      <td class="${SESSIONS_CELL_SHRINK} text-slate-600">${escapeHtml(formatLastSeen(s.last_seen || s.created_at))}</td>
      <td class="${SESSIONS_CELL_ACTIONS}">
        <div class="${SESSIONS_ACTION_BTNS}">
          <button type="button" ${reconnectAttrs} class="${SESSIONS_BTN} bg-sky-600 text-white hover:bg-sky-700">${tr('sessions.reconnect')}</button>
          ${inviteBtn}
          ${isOwner ? keepButton(s, escapeHtml, 'terminal') : ''}
          ${isOwner ? `<button type="button" ${sessionEndButtonAttrs(s, escapeHtml, { kind: 'terminal' })} class="${SESSIONS_BTN} border border-slate-300 bg-white text-slate-700 hover:bg-slate-50">${tr('sessions.end')}</button>` : ''}
        </div>
      </td>
    </tr>`)
  })
  rdpSessions.forEach((r) => {
    const url = `/rdp?target_id=${encodeURIComponent(r.target_id)}&target_name=${encodeURIComponent(r.target_name || r.target_id)}&session_id=${encodeURIComponent(r.session_id)}`
    rows.push(`<tr class="${sessionTableRowClass(r.idle)}">
      <td class="${SESSIONS_CELL} text-sm font-medium text-slate-900 break-words">${escapeHtml(r.target_name || r.target_id)}${keepBadge(r.keep)}</td>
      <td class="${SESSIONS_CELL} text-sm text-slate-400">—</td>
      <td class="${SESSIONS_CELL} text-sm text-slate-700 break-all font-mono">${escapeHtml(targetFullPathForDisplay(r))}</td>
      <td class="${SESSIONS_CELL_SHRINK} text-slate-800 font-medium">RDP</td>
      <td class="${SESSIONS_CELL_SHRINK} text-slate-600">${escapeHtml(formatLastSeen(r.last_seen || r.created_at))}</td>
      <td class="${SESSIONS_CELL_ACTIONS}">
        <div class="${SESSIONS_ACTION_BTNS}">
          <a href="${safeUrl(url)}" target="_blank" rel="noopener noreferrer" data-rdp-reconnect="1" data-rdp-target-id="${escapeHtml(r.target_id)}" data-rdp-href="${escapeHtml(url)}" class="${SESSIONS_BTN} bg-sky-600 text-white hover:bg-sky-700">${tr('sessions.reconnect')}</a>
          ${keepButton(r, escapeHtml, 'rdp')}
          <button type="button" ${sessionEndButtonAttrs(r, escapeHtml, { kind: 'rdp' })} class="${SESSIONS_BTN} border border-slate-300 bg-white text-slate-700 hover:bg-slate-50">${tr('sessions.end')}</button>
        </div>
      </td>
    </tr>`)
  })
  return { body: rows.join('') }
}

export function countIdleSessions(sessions, rdpSessions) {
  let n = 0
  for (const s of sessions) {
    if (s.idle) n++
  }
  for (const r of rdpSessions) {
    if (r.idle) n++
  }
  return n
}

/** Confirmation modal before ending a session. */
export function showSessionEndConfirmModal(
  modalEl,
  escapeHtml,
  { kind, name, fullPath, description, label },
  onConfirm,
) {
  if (!modalEl) return
  const kindLabel = kind === 'rdp' ? tr('sessions.rdpKind') : tr('sessions.sshTelnet')
  const displayName = (name || label || '').trim() || tr('sessions.untitled')
  const displayPath = (fullPath || '').trim() || '—'
  const displayDesc = (description || '').trim()
  const descBlock = displayDesc
    ? `<div>
            <dt class="text-xs font-medium text-slate-500 mb-0.5">${tr('sessions.fieldDescription')}</dt>
            <dd class="text-sm text-slate-800 break-words">${escapeHtml(displayDesc)}</dd>
          </div>`
    : ''
  modalEl.classList.remove('hidden')
  modalEl.innerHTML = `
    <div id="session-end-backdrop" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
      <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50" role="dialog" aria-labelledby="session-end-title">
        <div class="px-5 py-4 border-b border-slate-200 bg-slate-50">
          <h3 id="session-end-title" class="font-semibold text-slate-800">${tr('sessions.endTitle')}</h3>
        </div>
        <div class="px-6 py-5">
          <p class="text-sm text-slate-700">${escapeHtml(tr('sessions.endQuestion', { kind: kindLabel }))}</p>
          <dl class="mt-3 space-y-2.5 rounded-md border border-slate-200 bg-slate-50 px-3 py-3">
            <div>
              <dt class="text-xs font-medium text-slate-500 mb-0.5">${tr('sessions.fieldName')}</dt>
              <dd class="text-sm font-medium text-slate-900 break-words">${escapeHtml(displayName)}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-slate-500 mb-0.5">${tr('sessions.fieldTarget')}</dt>
              <dd class="text-sm text-slate-800 font-mono break-all">${escapeHtml(displayPath)}</dd>
            </div>
            ${descBlock}
          </dl>
          <p class="mt-3 text-xs text-slate-500">${tr('sessions.endHint')}</p>
        </div>
        <div class="px-6 py-4 border-t border-slate-200 flex justify-end gap-2 bg-slate-50">
          <button type="button" id="session-end-cancel" class="rounded border border-slate-300 bg-white px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50">${tr('sessions.endCancel')}</button>
          <button type="button" id="session-end-confirm" class="rounded bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700">${tr('sessions.endConfirm')}</button>
        </div>
      </div>
    </div>
  `
  const close = () => {
    modalEl.classList.add('hidden')
    modalEl.innerHTML = ''
  }
  modalEl.querySelector('#session-end-cancel')?.addEventListener('click', close)
  modalEl.querySelector('#session-end-backdrop')?.addEventListener('click', (e) => {
    if (e.target.id === 'session-end-backdrop') close()
  })
  modalEl.querySelector('#session-end-confirm')?.addEventListener('click', async () => {
    const confirmBtn = modalEl.querySelector('#session-end-confirm')
    if (confirmBtn) confirmBtn.disabled = true
    try {
      await onConfirm()
      close()
    } catch (err) {
      await uiAlert(err.message || tr('sessions.endFailed'))
      if (confirmBtn) confirmBtn.disabled = false
    }
  })
}

/**
 * Wire reconnect/end buttons inside container.
 */
export function bindSessionListActions(container, { openTerminalTab, getRdpResolutionForTarget, onEnded, escapeHtml, sessionEndModal }) {
  if (!container) return
  container.querySelectorAll('[data-terminal-reconnect]').forEach((btn) => {
    btn.addEventListener('click', () => {
      const sid = btn.getAttribute('data-session-id') || ''
      if (!sid) return
      const mode = btn.getAttribute('data-reconnect-mode') || ''
      const targetId = btn.getAttribute('data-target-id') || ''
      if (mode === 'tftp' && targetId) {
        const tftpTargetId = btn.getAttribute('data-tftp-target-id') || ''
        if (!tftpTargetId) {
          void uiAlert(tr('sessions.tftpParseFailed'))
          return
        }
        const name = `${targetId} (TFTP)`
        const url = `/tftp-console?tftp_target_id=${encodeURIComponent(tftpTargetId)}&ssh_target_id=${encodeURIComponent(targetId)}&target_name=${encodeURIComponent(name)}&session_id=${encodeURIComponent(sid)}`
        openTerminalTab(url)
        return
      }
      const targetName = btn.getAttribute('data-target-name') || ''
      const params = new URLSearchParams()
      if (targetId) params.set('target_id', targetId)
      if (targetName) params.set('target_name', targetName)
      params.set('session_id', sid)
      const attachMode = btn.getAttribute('data-reconnect-attach-mode') || ''
      if (attachMode === 'viewer') params.set('mode', 'viewer')
      openTerminalTab(`/terminal?${params.toString()}`)
    })
  })
  container.querySelectorAll('[data-session-invite]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const sid = btn.getAttribute('data-session-id') || ''
      if (!sid) return
      const targetName = btn.getAttribute('data-target-name') || ''
      const { openInviteDialog } = await import('./invite_dialog.js')
      openInviteDialog({ sessionId: sid, targetName, escapeHtml })
    })
  })
  container.querySelectorAll('[data-rdp-reconnect]').forEach((el) => {
    el.addEventListener('click', (e) => {
      e.preventDefault()
      const id = el.getAttribute('data-rdp-target-id') || ''
      const href = el.getAttribute('data-rdp-href') || el.href
      const u = new URL(href, window.location.origin)
      if (getRdpResolutionForTarget) {
        const { w, h } = getRdpResolutionForTarget(id)
        if (w) u.searchParams.set('rw', String(w))
        if (h) u.searchParams.set('rh', String(h))
      }
      openTerminalTab(u.toString())
    })
  })
  container.querySelectorAll('[data-session-keep]').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const sid = btn.getAttribute('data-session-id') || ''
      const kind = btn.getAttribute('data-session-kind') || 'terminal'
      if (!sid) return
      const next = btn.getAttribute('data-keep') !== '1'
      btn.disabled = true
      try {
        await API.setSessionKeep(sid, next, { kind })
        if (typeof onEnded === 'function') onEnded()
      } catch (err) {
        btn.disabled = false
        await uiAlert((err && err.message) || tr('common.errorOccurred'))
      }
    })
  })
  container.querySelectorAll('[data-session-end]').forEach((btn) => {
    btn.addEventListener('click', () => {
      const sid = btn.getAttribute('data-session-id') || ''
      const kind = btn.getAttribute('data-session-kind') || 'terminal'
      const label = btn.getAttribute('data-session-label') || ''
      if (!sid) return
      const doEnd = async () => {
        if (kind === 'rdp') {
          await API.rdpSessionDelete(sid)
        } else {
          await API.terminalSessionDelete(sid)
        }
        if (typeof onEnded === 'function') onEnded()
      }
      if (sessionEndModal && typeof escapeHtml === 'function') {
        showSessionEndConfirmModal(sessionEndModal, escapeHtml, {
          kind,
          name: btn.getAttribute('data-session-name') || '',
          fullPath: btn.getAttribute('data-session-fullpath') || '',
          description: btn.getAttribute('data-session-description') || '',
          label,
        }, doEnd)
        return
      }
      void (async () => {
        if (!(await uiConfirm(tr('sessions.confirmEnd'), { danger: true }))) return
        try {
          await doEnd()
        } catch (err) {
          await uiAlert(err.message || tr('sessions.endFailed'))
        }
      })()
    })
  })
}
