/**
 * @file Access requests: a user asks for (time-limited) membership of a
 * group from the home page; admins decide on the "Access requests" page.
 */

import API from './api.js'
import { formatDateTime } from './datetime.js'
import { t } from './i18n.js'
import { setActiveNav } from './nav.js'
import { uiAlert, uiConfirm } from './ui_dialog.js'

function esc(s) {
  return String(s ?? '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
}

const INPUT =
  'w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white'
const BTN_PRIMARY =
  'rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors disabled:opacity-50'
const BTN_SECONDARY =
  'rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors disabled:opacity-50'
const BTN_DANGER =
  'rounded border border-red-200 bg-white px-3 py-1.5 text-xs font-medium text-red-700 hover:bg-red-50 shadow-sm transition-colors disabled:opacity-50'

/** Duration presets in seconds; 0 = permanent. */
const DURATIONS = [
  [3600, 'access.dur1h'],
  [8 * 3600, 'access.dur8h'],
  [24 * 3600, 'access.dur1d'],
  [7 * 24 * 3600, 'access.dur7d'],
  [30 * 24 * 3600, 'access.dur30d'],
  [0, 'access.durPermanent'],
]

function durationOptions(selected) {
  return DURATIONS.map(
    ([secs, key]) => `<option value="${secs}"${Number(selected) === secs ? ' selected' : ''}>${t(key)}</option>`,
  ).join('')
}

function durationLabel(secs) {
  const hit = DURATIONS.find(([s]) => s === Number(secs))
  if (hit) return t(hit[1])
  const h = Math.round(Number(secs) / 3600)
  return t('access.durHours', { n: h })
}

function statusPill(status) {
  const cls = {
    pending: 'bg-amber-100 text-amber-800',
    approved: 'bg-emerald-100 text-emerald-800',
    denied: 'bg-red-100 text-red-700',
    cancelled: 'bg-slate-100 text-slate-600',
  }[status] || 'bg-slate-100 text-slate-600'
  return `<span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${cls}">${t('access.status_' + status)}</span>`
}

/**
 * Modal for a user to request access to a group.
 * @param {{ onCreated?: () => void }} opts
 */
export async function showAccessRequestModal({ onCreated } = {}) {
  const modal = document.getElementById('change-password-modal')
  if (!modal) return
  const close = () => {
    modal.classList.add('hidden')
    modal.innerHTML = ''
  }
  modal.classList.remove('hidden')
  modal.innerHTML = `
    <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
      <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
        <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
          <h3 class="font-semibold text-slate-800">${t('access.modalTitle')}</h3>
          <button id="ar-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
        </div>
        <form id="ar-form">
          <div class="px-6 py-5 space-y-4">
            <p class="text-xs text-slate-500">${t('access.modalIntro')}</p>
            <div>
              <label for="ar-group" class="block text-xs font-medium text-slate-600 mb-1.5">${t('access.fieldGroup')}</label>
              <select id="ar-group" required class="${INPUT}"><option value="">${t('common.loading')}</option></select>
            </div>
            <div>
              <label for="ar-duration" class="block text-xs font-medium text-slate-600 mb-1.5">${t('access.fieldDuration')}</label>
              <select id="ar-duration" class="${INPUT}">${durationOptions(24 * 3600)}</select>
            </div>
            <div>
              <label for="ar-reason" class="block text-xs font-medium text-slate-600 mb-1.5">${t('access.fieldReason')}</label>
              <textarea id="ar-reason" rows="3" maxlength="500" class="${INPUT}" placeholder="${t('access.reasonPlaceholder')}"></textarea>
            </div>
            <p id="ar-error" class="text-sm text-red-600 hidden"></p>
          </div>
          <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
            <button type="button" id="ar-cancel" class="${BTN_SECONDARY}">${t('common.cancel')}</button>
            <button type="submit" id="ar-submit" class="${BTN_PRIMARY}">${t('access.submit')}</button>
          </div>
        </form>
      </div>
    </div>`
  modal.querySelector('#ar-close').addEventListener('click', close)
  modal.querySelector('#ar-cancel').addEventListener('click', close)
  const groupEl = modal.querySelector('#ar-group')
  try {
    const groups = (await API.accessRequestGroups()) || []
    const requestable = groups.filter((g) => !g.has_access)
    groupEl.innerHTML =
      `<option value="">${t('access.pickGroup')}</option>` +
      requestable
        .map((g) => `<option value="${esc(g.id)}">${esc(g.name && g.name !== g.id ? `${g.id} (${g.name})` : g.id)}</option>`)
        .join('')
    if (requestable.length === 0) {
      groupEl.innerHTML = `<option value="">${t('access.noRequestableGroups')}</option>`
      groupEl.disabled = true
    }
  } catch (err) {
    groupEl.innerHTML = `<option value="">${esc(err.message || t('common.errorOccurred'))}</option>`
    groupEl.disabled = true
  }
  modal.querySelector('#ar-form').addEventListener('submit', async (e) => {
    e.preventDefault()
    const errorEl = modal.querySelector('#ar-error')
    const submit = modal.querySelector('#ar-submit')
    const groupId = groupEl.value
    if (!groupId) return
    errorEl.classList.add('hidden')
    submit.disabled = true
    try {
      await API.createAccessRequest({
        group_id: groupId,
        reason: modal.querySelector('#ar-reason').value.trim(),
        duration_seconds: modal.querySelector('#ar-duration').value,
      })
      close()
      if (typeof onCreated === 'function') onCreated()
    } catch (err) {
      errorEl.textContent = err.message || t('common.errorOccurred')
      errorEl.classList.remove('hidden')
    } finally {
      submit.disabled = false
    }
  })
}

/**
 * "My requests" card on the home page (hidden when there are none).
 * @param {HTMLElement} container
 * @param {{ onChanged?: () => void }} opts
 */
export async function renderMyAccessRequests(container, { onChanged } = {}) {
  if (!container) return
  let list
  try {
    list = (await API.accessRequests({ mine: true })) || []
  } catch {
    container.classList.add('hidden')
    return
  }
  const recent = list.slice(0, 8)
  if (recent.length === 0) {
    container.classList.add('hidden')
    container.innerHTML = ''
    return
  }
  container.classList.remove('hidden')
  container.innerHTML = `
    <h3 class="text-sm font-semibold text-slate-800">${t('access.myRequestsTitle')}</h3>
    <ul class="divide-y divide-slate-100">
      ${recent
        .map(
          (r) => `<li class="py-2 flex flex-wrap items-center justify-between gap-2">
            <div class="min-w-0 text-sm text-slate-700">
              <span class="font-medium text-slate-900">${esc(r.group_name && r.group_name !== r.group_id ? `${r.group_id} (${r.group_name})` : r.group_id)}</span>
              <span class="text-xs text-slate-500 ml-2">${esc(durationLabel(r.duration_seconds))} · ${esc(formatDateTime(r.created_at))}</span>
              ${r.status === 'approved' && r.expires_at ? `<span class="text-xs text-slate-500 ml-2">${t('access.until', { date: esc(formatDateTime(r.expires_at)) })}</span>` : ''}
              ${r.decision_note ? `<div class="text-xs text-slate-500">${t('access.noteFromAdmin')}: ${esc(r.decision_note)}</div>` : ''}
            </div>
            <div class="flex items-center gap-2">
              ${statusPill(r.status)}
              ${r.status === 'pending' ? `<button type="button" class="ar-cancel-btn ${BTN_SECONDARY}" data-id="${esc(r.id)}">${t('access.withdraw')}</button>` : ''}
            </div>
          </li>`,
        )
        .join('')}
    </ul>`
  container.querySelectorAll('.ar-cancel-btn').forEach((btn) => {
    btn.addEventListener('click', async () => {
      if (!(await uiConfirm(t('access.confirmWithdraw')))) return
      btn.disabled = true
      try {
        await API.cancelAccessRequest(btn.dataset.id)
        await renderMyAccessRequests(container, { onChanged })
        if (typeof onChanged === 'function') onChanged()
      } catch (err) {
        btn.disabled = false
        await uiAlert(err.message || t('common.errorOccurred'))
      }
    })
  })
}

/**
 * Admin page: pending requests with approve / deny, plus recent history.
 * @param {HTMLElement} container
 * @param {{ onDecided?: () => void }} opts
 */
export async function renderAccessRequestsAdminPage(container, { onDecided } = {}) {
  if (typeof setActiveNav === 'function') setActiveNav('accessRequests')
  container.innerHTML = `
    <div class="w-full flex-1 flex flex-col">
      <h2 class="text-lg font-semibold text-slate-800">${t('access.adminTitle')}</h2>
      <p class="mt-1 text-xs text-slate-500">${t('access.adminIntro')}</p>
      <section class="mt-4 rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="px-5 pt-4 pb-3 border-b border-slate-100"><h3 class="text-sm font-semibold text-slate-800">${t('access.pendingTitle')}</h3></div>
        <div id="ar-pending" class="px-5 py-4 text-sm text-slate-500">${t('common.loading')}</div>
      </section>
      <section class="mt-4 rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="px-5 pt-4 pb-3 border-b border-slate-100"><h3 class="text-sm font-semibold text-slate-800">${t('access.historyTitle')}</h3></div>
        <div id="ar-history" class="px-5 py-4 text-sm text-slate-500">${t('common.loading')}</div>
      </section>
    </div>`
  const pendingEl = container.querySelector('#ar-pending')
  const historyEl = container.querySelector('#ar-history')
  const reload = () => renderAccessRequestsAdminPage(container, { onDecided })

  let all
  try {
    all = (await API.accessRequests()) || []
  } catch (err) {
    pendingEl.innerHTML = `<p class="text-sm text-red-600">${esc(err.message || t('common.errorOccurred'))}</p>`
    historyEl.innerHTML = ''
    return
  }
  const pending = all.filter((r) => r.status === 'pending')
  const history = all.filter((r) => r.status !== 'pending').slice(0, 50)

  pendingEl.innerHTML = pending.length
    ? `<ul class="divide-y divide-slate-100">${pending
        .map(
          (r) => `<li class="py-3" data-id="${esc(r.id)}">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div class="min-w-0 text-sm">
                <div><span class="font-medium text-slate-900">${esc(r.username || r.user_id)}</span> <span class="text-slate-500">→</span> <span class="font-medium text-slate-900">${esc(r.group_name && r.group_name !== r.group_id ? `${r.group_id} (${r.group_name})` : r.group_id)}</span></div>
                <div class="text-xs text-slate-500 mt-0.5">${t('access.requested')}: ${esc(formatDateTime(r.created_at))} · ${t('access.fieldDuration')}: ${esc(durationLabel(r.duration_seconds))}</div>
                ${r.reason ? `<div class="mt-1 text-sm text-slate-700 whitespace-pre-wrap">${esc(r.reason)}</div>` : `<div class="mt-1 text-xs text-slate-400">${t('access.noReason')}</div>`}
              </div>
              <form class="ar-decide flex flex-wrap items-end gap-2">
                <div>
                  <label class="block text-[11px] font-medium text-slate-600 mb-1">${t('access.grantFor')}</label>
                  <select name="duration" class="${INPUT} w-40">${durationOptions(r.duration_seconds)}</select>
                </div>
                <div>
                  <label class="block text-[11px] font-medium text-slate-600 mb-1">${t('access.noteLabel')}</label>
                  <input name="note" maxlength="500" class="${INPUT} w-56" placeholder="${t('access.notePlaceholder')}" />
                </div>
                <button type="button" class="ar-approve ${BTN_PRIMARY}">${t('access.approve')}</button>
                <button type="button" class="ar-deny ${BTN_DANGER}">${t('access.deny')}</button>
              </form>
            </div>
            <p class="ar-error mt-1 text-sm text-red-600 hidden"></p>
          </li>`,
        )
        .join('')}</ul>`
    : `<p class="text-sm text-slate-500">${t('access.noPending')}</p>`

  pendingEl.querySelectorAll('li[data-id]').forEach((li) => {
    const id = li.dataset.id
    const form = li.querySelector('.ar-decide')
    const errorEl = li.querySelector('.ar-error')
    const decide = async (decision) => {
      errorEl.classList.add('hidden')
      li.querySelectorAll('button').forEach((b) => (b.disabled = true))
      try {
        await API.decideAccessRequest(id, decision, {
          duration_seconds: decision === 'approve' ? form.elements.duration.value : undefined,
          note: form.elements.note.value.trim(),
        })
        if (typeof onDecided === 'function') onDecided()
        await reload()
      } catch (err) {
        errorEl.textContent = err.message || t('common.errorOccurred')
        errorEl.classList.remove('hidden')
        li.querySelectorAll('button').forEach((b) => (b.disabled = false))
      }
    }
    li.querySelector('.ar-approve').addEventListener('click', () => decide('approve'))
    li.querySelector('.ar-deny').addEventListener('click', () => decide('deny'))
  })

  historyEl.innerHTML = history.length
    ? `<div class="overflow-x-auto"><table class="min-w-full text-left text-sm">
        <thead class="bg-slate-50 border-b border-slate-200"><tr>
          <th class="px-3 py-2 text-xs font-semibold text-slate-700">${t('access.requester')}</th>
          <th class="px-3 py-2 text-xs font-semibold text-slate-700">${t('access.fieldGroup')}</th>
          <th class="px-3 py-2 text-xs font-semibold text-slate-700">${t('access.requested')}</th>
          <th class="px-3 py-2 text-xs font-semibold text-slate-700">${t('access.decision')}</th>
          <th class="px-3 py-2 text-xs font-semibold text-slate-700">${t('access.decidedBy')}</th>
          <th class="px-3 py-2 text-xs font-semibold text-slate-700">${t('access.noteLabel')}</th>
        </tr></thead>
        <tbody>${history
          .map(
            (r) => `<tr class="border-b border-slate-100">
              <td class="px-3 py-2">${esc(r.username || r.user_id)}</td>
              <td class="px-3 py-2">${esc(r.group_id)}</td>
              <td class="px-3 py-2 text-xs text-slate-600 whitespace-nowrap">${esc(formatDateTime(r.created_at))}</td>
              <td class="px-3 py-2">${statusPill(r.status)}${r.status === 'approved' ? `<div class="text-xs text-slate-500 mt-0.5">${r.expires_at ? t('access.until', { date: esc(formatDateTime(r.expires_at)) }) : t('access.durPermanent')}</div>` : ''}</td>
              <td class="px-3 py-2 text-xs text-slate-600">${esc(r.decided_by || '')}<div class="text-slate-400">${esc(formatDateTime(r.decided_at, undefined, ''))}</div></td>
              <td class="px-3 py-2 text-xs text-slate-600">${esc(r.decision_note || '')}</td>
            </tr>`,
          )
          .join('')}</tbody>
      </table></div>`
    : `<p class="text-sm text-slate-500">${t('access.noHistory')}</p>`
}
