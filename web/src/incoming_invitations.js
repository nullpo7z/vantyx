import API from './api.js'
import { t } from './i18n.js'
import { escapeHtml } from './dom_helpers.js'
import { uiAlert } from './ui_dialog.js'

function renderInvitationCard(inv) {
  const owner = escapeHtml(inv.owner_username || inv.owner_user_id || '')
  const target = escapeHtml(inv.target_name || inv.target_id || '')
  const id = escapeHtml(inv.id || '')
  const sessionId = escapeHtml(inv.session_id || '')
  const targetId = escapeHtml(inv.target_id || '')
  return `
    <div class="incoming-invitation-card rounded-md border border-sky-200 bg-sky-50 px-4 py-3 text-sm text-slate-800 flex flex-wrap items-center justify-between gap-3" data-incoming-invitation-id="${id}">
      <p class="incoming-invitation-message min-w-0 flex-1">${t('app.incomingInviteMessage', { owner, target })}</p>
      <button
        type="button"
        class="shrink-0 rounded-md bg-sky-700 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-800 disabled:opacity-50"
        data-join-incoming-invitation
        data-session-id="${sessionId}"
        data-invitation-id="${id}"
        data-target-id="${targetId}"
      >${t('app.incomingInviteJoin')}</button>
    </div>
  `
}

/**
 * ホーム画面の指名招待バナーを更新する。
 * @param {HTMLElement|null} container - #incoming-invitations-banner
 * @param {{ onJoin?: (inv: { session_id: string, target_id?: string }) => void | Promise<void> }} [opts]
 */
export async function refreshIncomingInvitationsBanner(container, { onJoin } = {}) {
  if (!container) return
  try {
    const res = await API.listIncomingInvitations()
    const items = Array.isArray(res?.items) ? res.items : []
    if (items.length === 0) {
      container.classList.add('hidden')
      container.innerHTML = ''
      return
    }
    container.classList.remove('hidden')
    container.innerHTML = items.map((inv) => renderInvitationCard(inv)).join('')
    container.querySelectorAll('[data-join-incoming-invitation]').forEach((btn) => {
      btn.addEventListener('click', async () => {
        const sessionId = btn.dataset.sessionId || ''
        const invitationId = btn.dataset.invitationId || ''
        const targetId = btn.dataset.targetId || ''
        if (!sessionId || !invitationId) return
        btn.disabled = true
        try {
          await API.joinSession(sessionId, { invitationId })
          if (typeof onJoin === 'function') {
            await onJoin({ session_id: sessionId, target_id: targetId })
          }
          await refreshIncomingInvitationsBanner(container, { onJoin })
        } catch (err) {
          await uiAlert(err?.message || t('app.incomingInviteJoinFailed'))
        } finally {
          btn.disabled = false
        }
      })
    })
  } catch (e) {
    if (e?.message !== 'Unauthorized' && e?.message !== 'unauthorized') {
      console.error('Failed to load incoming invitations', e)
    }
    container.classList.add('hidden')
    container.innerHTML = ''
  }
}
