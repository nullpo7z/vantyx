// Participants dialog for collaborative sessions (owner side).
//
// Replaces the always-visible participants panel that used to sit under
// the sharing banner and shrink the terminal area as viewers joined.
// Opened from the "Participants (N)" header button, like Invitations.
// Lists current viewers with Remove, and -- for information only -- the
// users the owner removed. There is no "allow rejoin" control: issuing a
// new named invitation to a removed user is the one way to let them back
// in (F-2 / E-16).

import { t } from './i18n.js'

let openDialogEl = null
let rerender = null

/**
 * Re-render the dialog body from fresh state if it is currently open.
 * Called by the host page whenever its participant cache changes (SSE
 * participant_joined / participant_left / session_change, or after a
 * kick round-trip).
 */
export function refreshParticipantsDialog() {
  if (openDialogEl && typeof rerender === 'function') {
    try {
      rerender()
    } catch {
      /* ignore */
    }
  }
}

export function closeParticipantsDialog() {
  if (!openDialogEl) return
  try {
    openDialogEl.remove()
  } catch {
    /* ignore */
  }
  openDialogEl = null
  rerender = null
}

/**
 * @param {object} opts
 * @param {string} opts.sessionId
 * @param {string} [opts.targetName]
 * @param {(s: unknown) => string} opts.escapeHtml
 * @param {() => {participants: Array<object>, kicked: Array<string>}} opts.getState
 *   Returns the host page's current participant cache; `participants`
 *   are the rows from GET .../participants (user_id, username, role,
 *   is_writer), `kicked` the user IDs blocked from rejoining.
 * @param {(userId: string, displayName: string) => Promise<void>} opts.onKick
 */
export function openParticipantsDialog({ sessionId, targetName, escapeHtml, getState, onKick }) {
  if (!sessionId) return
  closeParticipantsDialog()

  const wrap = document.createElement('div')
  wrap.className = 'fixed inset-0 z-[200] flex items-center justify-center bg-black/40 backdrop-blur-sm p-4'
  wrap.innerHTML = `
    <div class="bg-white rounded-lg shadow-xl w-full max-w-2xl mx-4 overflow-hidden border border-slate-200/50 flex flex-col max-h-[85vh]">
      <div class="px-5 py-4 border-b border-slate-200 bg-slate-50 flex items-center justify-between shrink-0">
        <div>
          <h3 class="font-semibold text-slate-800">${t('sharing.participantsTitle')}</h3>
          <p class="text-xs text-slate-500 mt-0.5">${escapeHtml(targetName || '')}</p>
        </div>
        <button type="button" data-close="1" class="text-slate-500 hover:text-slate-700 text-sm">${t('sharing.closeDialog')}</button>
      </div>
      <div data-body="1" class="px-6 py-5 space-y-5 overflow-y-auto"></div>
    </div>
  `
  document.body.appendChild(wrap)
  openDialogEl = wrap

  const body = wrap.querySelector('[data-body="1"]')

  function render() {
    const state = (typeof getState === 'function' ? getState() : null) || {}
    const viewers = (Array.isArray(state.participants) ? state.participants : []).filter((p) => p && p.role === 'viewer')
    const kicked = Array.isArray(state.kicked) ? state.kicked : []

    const viewerRows = viewers
      .map((p) => {
        const label = escapeHtml(p.username || p.user_id || '')
        const status = p.is_writer ? t('sharing.roleWriter') : t('sharing.roleViewer')
        return `<tr class="border-b border-slate-100 last:border-0">
          <td class="px-3 py-2 text-sm text-slate-800">${label}</td>
          <td class="px-3 py-2 text-sm text-slate-600">${escapeHtml(status)}</td>
          <td class="px-3 py-2 text-right">
            <button type="button" data-kick="${escapeHtml(p.user_id)}" data-name="${escapeHtml(p.username || p.user_id || '')}" class="rounded border border-red-300 bg-white px-2 py-0.5 text-xs text-red-700 hover:bg-red-50">${t('sharing.kick')}</button>
          </td>
        </tr>`
      })
      .join('')
    const viewersHtml = viewers.length
      ? `<table class="w-full text-left"><thead class="bg-slate-50 border-b border-slate-200"><tr class="text-xs text-slate-500">
            <th class="px-3 py-2 font-semibold">${t('sharing.headerUsername')}</th>
            <th class="px-3 py-2 font-semibold">${t('sharing.headerStatus')}</th>
            <th class="px-3 py-2 font-semibold text-right">${t('sharing.headerActions')}</th>
          </tr></thead><tbody>${viewerRows}</tbody></table>`
      : `<p class="text-xs text-slate-500 px-3 py-2">${t('sharing.participantsEmpty')}</p>`

    const kickedHtml = kicked.length
      ? `<ul class="divide-y divide-slate-100">${kicked
          .map((uid) => `<li class="px-3 py-2 text-sm text-slate-800">${escapeHtml(uid)}</li>`)
          .join('')}</ul>`
      : ''

    body.innerHTML = `
      <div class="space-y-2">
        <h4 class="text-xs font-semibold text-slate-700">${t('sharing.participantsTitle')} (${viewers.length})</h4>
        <div class="rounded border border-slate-200 overflow-hidden">${viewersHtml}</div>
      </div>
      ${
        kicked.length
          ? `<div class="space-y-2">
        <h4 class="text-xs font-semibold text-slate-700">${t('sharing.removedUsersTitle')}</h4>
        <p class="text-xs text-slate-500">${t('sharing.removedUsersHint')}</p>
        <div class="rounded border border-slate-200 overflow-hidden">${kickedHtml}</div>
      </div>`
          : ''
      }
    `
    body.querySelectorAll('[data-kick]').forEach((btn) => {
      btn.addEventListener('click', async () => {
        btn.disabled = true
        try {
          await onKick?.(btn.getAttribute('data-kick') || '', btn.getAttribute('data-name') || '')
        } finally {
          btn.disabled = false
        }
      })
    })
  }

  rerender = render
  render()

  wrap.querySelector('[data-close="1"]')?.addEventListener('click', closeParticipantsDialog)
  wrap.addEventListener('click', (e) => {
    if (e.target === wrap) closeParticipantsDialog()
  })
  const onKey = (e) => {
    if (e.key === 'Escape') {
      document.removeEventListener('keydown', onKey)
      closeParticipantsDialog()
    }
  }
  document.addEventListener('keydown', onKey)
}
