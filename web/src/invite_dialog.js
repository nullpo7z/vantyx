// Invitation dialog for collaborative sessions.
//
// Supports named (user picker), group (all members), and link
// invitations with single / limited / unlimited join caps.

import API from './api.js'
import { t } from './i18n.js'
import { uiAlert, uiConfirm } from './ui_dialog.js'
import { createRealtimeWatcher, POLL_MS, shouldRefreshInviteList } from './sharing_events.js'

let openDialogEl = null

const INVITE_URL_CACHE_KEY = 'vantyx_invite_url_cache'

function readInviteUrlCache(sessionId) {
  try {
    const raw = sessionStorage.getItem(INVITE_URL_CACHE_KEY)
    if (!raw) return {}
    const all = JSON.parse(raw)
    return all[sessionId] && typeof all[sessionId] === 'object' ? all[sessionId] : {}
  } catch {
    return {}
  }
}

function writeInviteUrlCache(sessionId, invitationId, url) {
  if (!sessionId || !invitationId || !url) return
  try {
    const raw = sessionStorage.getItem(INVITE_URL_CACHE_KEY)
    const all = raw ? JSON.parse(raw) : {}
    if (!all[sessionId] || typeof all[sessionId] !== 'object') all[sessionId] = {}
    all[sessionId][invitationId] = url
    sessionStorage.setItem(INVITE_URL_CACHE_KEY, JSON.stringify(all))
  } catch {
    /* ignore */
  }
}

function removeInviteUrlCache(sessionId, invitationId) {
  try {
    const raw = sessionStorage.getItem(INVITE_URL_CACHE_KEY)
    if (!raw) return
    const all = JSON.parse(raw)
    if (all[sessionId] && typeof all[sessionId] === 'object') {
      delete all[sessionId][invitationId]
      sessionStorage.setItem(INVITE_URL_CACHE_KEY, JSON.stringify(all))
    }
  } catch {
    /* ignore */
  }
}

function getCachedInviteUrl(sessionId, invitationId) {
  return readInviteUrlCache(sessionId)[invitationId] || ''
}

function parseApiTime(value) {
  if (value == null || value === '') return null
  if (typeof value === 'number') {
    if (value === 0) return null
    const ms = value < 1e12 ? value * 1000 : value
    const d = new Date(ms)
    return Number.isNaN(d.getTime()) ? null : d
  }
  try {
    const d = new Date(value)
    return Number.isNaN(d.getTime()) ? null : d
  } catch {
    return null
  }
}

function fmtDate(value) {
  const d = parseApiTime(value)
  if (!d) return '—'
  return d.toLocaleString()
}

function isInvitationExpired(inv) {
  const exp = parseApiTime(inv.expires_at)
  return exp != null && exp.getTime() < Date.now()
}

function isInvitationExhausted(inv) {
  if (inv.revoked_at) return true
  if (inv.used_at) return true
  if (isInvitationExpired(inv)) return true
  if (inv.is_link && !inv.link_unlimited && inv.max_uses != null) {
    return (inv.use_count || 0) >= inv.max_uses
  }
  return false
}

function inviteTargetLabel(inv) {
  if (inv.is_link) {
    if (inv.link_unlimited) return t('sharing.inviteLinkUnlimitedLabel')
    if (inv.max_uses === 1) return t('sharing.inviteLinkSingleLabel')
    return t('sharing.inviteLinkLimitedLabel', {
      max: inv.max_uses ?? '?',
      used: inv.use_count || 0,
    })
  }
  if (inv.invite_tag) {
    return t('sharing.inviteTagBatchLabel', {
      tag: inv.invite_tag,
      user: inv.invitee_username || inv.invitee_user_id || '',
    })
  }
  return inv.invitee_username || inv.invitee_user_id || '—'
}

function statusLabel(inv) {
  if (inv.revoked_at) return t('sharing.inviteRevoked')
  if (isInvitationExpired(inv)) return t('sharing.inviteExpired')
  if (inv.is_link && inv.link_unlimited && (inv.use_count || 0) > 0) {
    return t('sharing.inviteLinkActive', { used: inv.use_count })
  }
  if (inv.used_at || isInvitationExhausted(inv)) return t('sharing.inviteUsed')
  return t('sharing.invitePendingNamed')
}

/** Normalize POST /invitations response into invitation rows for the table. */
function invitationsFromCreateResponse(res, method) {
  if (!res || typeof res !== 'object') return []
  if (method === 'tag' && Array.isArray(res.items)) return res.items
  if (res.id) return [res]
  return []
}

function mergeInvitationLists(existing, created) {
  const byId = new Map()
  for (const inv of Array.isArray(existing) ? existing : []) {
    if (inv?.id) byId.set(inv.id, inv)
  }
  for (const inv of created) {
    if (inv?.id) byId.set(inv.id, inv)
  }
  return [...byId.values()]
}

function renderInvitationsTable(items, escapeHtml) {
  if (!items || items.length === 0) {
    return `<p class="text-xs text-slate-500 px-3 py-2">${t('sharing.inviteListEmpty')}</p>`
  }
  const rows = items
    .map((inv) => {
      const target = inviteTargetLabel(inv)
      const isPending = !isInvitationExhausted(inv)
      const linkActions = isPending && inv.is_link
        ? `<button type="button" data-show-link-id="${escapeHtml(inv.id)}" class="rounded border border-slate-300 bg-white px-2 py-0.5 text-xs text-slate-700 hover:bg-slate-50">${t('sharing.inviteShowLink')}</button>
            <button type="button" data-regenerate-link-id="${escapeHtml(inv.id)}" class="rounded border border-slate-300 bg-white px-2 py-0.5 text-xs text-slate-600 hover:bg-slate-50" title="${escapeHtml(t('sharing.inviteRegenerateLink'))}">${t('sharing.inviteRegenerateLinkShort')}</button>`
        : ''
      const deleteBtn = isPending
        ? `<button type="button" data-delete-id="${escapeHtml(inv.id)}" class="rounded border border-rose-200 bg-white px-2 py-0.5 text-xs text-rose-700 hover:bg-rose-50">${t('sharing.inviteDelete')}</button>`
        : ''
      const actionBtns = (linkActions || deleteBtn)
        ? `<div class="flex flex-wrap gap-1 justify-end">${linkActions}${deleteBtn}</div>`
        : ''
      return `<tr class="border-b border-slate-100 last:border-0">
        <td class="px-3 py-2 text-xs text-slate-700 break-all">${escapeHtml(target)}</td>
        <td class="px-3 py-2 text-xs text-slate-700">${escapeHtml(inv.mode || '')}</td>
        <td class="px-3 py-2 text-xs text-slate-500 whitespace-nowrap">${escapeHtml(fmtDate(inv.expires_at))}</td>
        <td class="px-3 py-2 text-xs text-slate-700 whitespace-nowrap">${escapeHtml(statusLabel(inv))}</td>
        <td class="px-3 py-2 text-xs whitespace-nowrap text-right">${actionBtns}</td>
      </tr>`
    })
    .join('')
  return `<table class="w-full text-left text-xs"><thead class="bg-slate-50 border-b border-slate-200"><tr>
      <th class="px-3 py-2 font-semibold text-slate-700">${t('sharing.headerUsername')}</th>
      <th class="px-3 py-2 font-semibold text-slate-700">${t('sharing.headerRole')}</th>
      <th class="px-3 py-2 font-semibold text-slate-700">${t('sharing.inviteExpiresAtLabel')}</th>
      <th class="px-3 py-2 font-semibold text-slate-700">${t('sharing.headerStatus')}</th>
      <th class="px-3 py-2 font-semibold text-slate-700">${t('sharing.headerActions')}</th>
    </tr></thead><tbody>${rows}</tbody></table>`
}

function userDisplayLabel(u) {
  if (!u) return ''
  return u.username ? `${u.username} (${u.id})` : (u.id || '')
}

function filterUsers(users, { query = '', tag = '' } = {}) {
  const q = query.trim().toLowerCase()
  return (Array.isArray(users) ? users : []).filter((u) => {
    if (tag) {
      const tags = Array.isArray(u.tags) ? u.tags : []
      if (!tags.includes(tag)) return false
    }
    if (!q) return true
    const id = (u.id || '').toLowerCase()
    const name = (u.username || '').toLowerCase()
    return id.includes(q) || name.includes(q)
  })
}

function filterTags(tags, query = '') {
  const q = query.trim().toLowerCase()
  return (Array.isArray(tags) ? tags : []).filter((item) => {
    if (!q) return true
    return (item.tag || '').toLowerCase().includes(q)
  })
}

function pickerItemClass(active) {
  return `invite-picker-item w-full text-left px-3 py-2 text-sm border-b border-slate-100 last:border-0${active ? ' invite-picker-item--active' : ''}`
}

/** @param {HTMLElement} root */
function mountUserPicker(root, { users, tags, escapeHtml }) {
  const hidden = root.querySelector('[data-invitee-value]')
  const searchInput = root.querySelector('[data-user-search]')
  const tagFilter = root.querySelector('[data-user-tag-filter]')
  const listEl = root.querySelector('[data-user-list]')
  const selectedLabel = root.querySelector('[data-user-selected-label]')
  const clearBtn = root.querySelector('[data-user-clear]')
  let selectedId = ''

  const clearSelection = () => {
    selectedId = ''
    if (hidden) hidden.value = ''
    if (selectedLabel) selectedLabel.textContent = ''
    if (clearBtn) clearBtn.classList.add('hidden')
    renderList()
  }

  const applySelection = (id) => {
    selectedId = id || ''
    if (hidden) hidden.value = selectedId
    const u = users.find((x) => x.id === selectedId)
    if (selectedLabel) {
      selectedLabel.textContent = selectedId
        ? t('sharing.inviteUserSelected', { label: userDisplayLabel(u) })
        : ''
    }
    if (clearBtn) clearBtn.classList.toggle('hidden', !selectedId)
    renderList()
  }

  const renderTagFilter = () => {
    const ts = Array.isArray(tags) ? [...tags] : []
    ts.sort((a, b) => (a.tag || '').localeCompare(b.tag || ''))
    tagFilter.innerHTML = `<option value="">${escapeHtml(t('sharing.inviteUserFilterAllTags'))}</option>${ts
      .map((item) => {
        const tag = escapeHtml(item.tag || '')
        const n = item.user_count ?? ''
        const label = n !== '' ? `${tag} (${n})` : tag
        return `<option value="${tag}">${label}</option>`
      })
      .join('')}`
  }

  const renderList = () => {
    const filtered = filterUsers(users, {
      query: searchInput?.value || '',
      tag: tagFilter?.value || '',
    })
    if (filtered.length === 0) {
      listEl.innerHTML = `<p class="px-3 py-4 text-xs text-slate-500 text-center">${escapeHtml(t('sharing.inviteUserEmpty'))}</p>`
      return
    }
    listEl.innerHTML = filtered
      .map((u) => {
        const id = escapeHtml(u.id || '')
        const label = escapeHtml(userDisplayLabel(u))
        const active = id === selectedId
        return `<button type="button" data-pick-user="${id}" class="${pickerItemClass(active)}">${label}</button>`
      })
      .join('')
    listEl.querySelectorAll('[data-pick-user]').forEach((btn) => {
      btn.addEventListener('click', () => {
        const id = btn.getAttribute('data-pick-user') || ''
        if (id && id === selectedId) {
          clearSelection()
        } else {
          applySelection(id)
        }
      })
    })
  }

  renderTagFilter()
  searchInput?.addEventListener('input', renderList)
  tagFilter?.addEventListener('change', renderList)
  clearBtn?.addEventListener('click', clearSelection)
  renderList()

  return {
    getValue: () => selectedId || (hidden?.value || '').trim(),
    clear: clearSelection,
  }
}

/** @param {HTMLElement} root */
function mountTagPicker(root, { tags, escapeHtml }) {
  const hidden = root.querySelector('[data-tag-value]')
  const searchInput = root.querySelector('[data-tag-search]')
  const listEl = root.querySelector('[data-tag-list]')
  const selectedLabel = root.querySelector('[data-tag-selected-label]')
  const clearBtn = root.querySelector('[data-tag-clear]')
  let selectedTag = ''

  const clearSelection = () => {
    selectedTag = ''
    if (hidden) hidden.value = ''
    if (selectedLabel) selectedLabel.textContent = ''
    if (clearBtn) clearBtn.classList.add('hidden')
    renderList()
  }

  const applySelection = (tag) => {
    selectedTag = tag || ''
    if (hidden) hidden.value = selectedTag
    if (selectedLabel) {
      selectedLabel.textContent = selectedTag
        ? t('sharing.inviteTagSelected', { tag: selectedTag })
        : ''
    }
    if (clearBtn) clearBtn.classList.toggle('hidden', !selectedTag)
    renderList()
  }

  const renderList = () => {
    const filtered = filterTags(tags, searchInput?.value || '')
    if (filtered.length === 0) {
      listEl.innerHTML = `<p class="px-3 py-4 text-xs text-slate-500 text-center">${escapeHtml(t('sharing.inviteUserEmpty'))}</p>`
      return
    }
    listEl.innerHTML = filtered
      .map((item) => {
        const tag = escapeHtml(item.tag || '')
        const n = item.user_count ?? 0
        const label = escapeHtml(`${item.tag} (${n})`)
        const active = tag === selectedTag
        return `<button type="button" data-pick-tag="${tag}" class="${pickerItemClass(active)}">${label}</button>`
      })
      .join('')
    listEl.querySelectorAll('[data-pick-tag]').forEach((btn) => {
      btn.addEventListener('click', () => {
        const tag = btn.getAttribute('data-pick-tag') || ''
        if (tag && tag === selectedTag) {
          clearSelection()
        } else {
          applySelection(tag)
        }
      })
    })
  }

  searchInput?.addEventListener('input', renderList)
  clearBtn?.addEventListener('click', clearSelection)
  renderList()

  return {
    getValue: () => selectedTag || (hidden?.value || '').trim(),
    clear: clearSelection,
  }
}

export function openInviteDialog({ sessionId, targetName, escapeHtml }) {
  if (!sessionId) return
  if (openDialogEl) {
    try { openDialogEl.remove() } catch { /* ignore */ }
    openDialogEl = null
  }
  const wrap = document.createElement('div')
  wrap.className = 'fixed inset-0 z-[200] flex items-center justify-center bg-black/40 backdrop-blur-sm p-4'
  wrap.innerHTML = `
    <div class="bg-white rounded-lg shadow-xl w-full max-w-2xl mx-4 overflow-hidden border border-slate-200/50">
      <div class="px-5 py-4 border-b border-slate-200 bg-slate-50 flex items-center justify-between">
        <div>
          <h3 class="font-semibold text-slate-800">${t('sharing.inviteTitle')}</h3>
          <p class="text-xs text-slate-500 mt-0.5">${escapeHtml(targetName || '')}</p>
        </div>
        <button type="button" data-close="1" class="text-slate-500 hover:text-slate-700 text-sm">${t('sharing.closeDialog')}</button>
      </div>
      <div class="px-6 py-5 space-y-4">
        <p class="text-sm text-slate-700">${t('sharing.inviteIntro')}</p>
        <fieldset class="flex flex-wrap items-center gap-4 text-sm text-slate-700">
          <legend class="sr-only">${t('sharing.inviteMethod')}</legend>
          <label class="flex items-center gap-1.5">
            <input type="radio" name="invite-method" value="named" checked />
            <span>${t('sharing.inviteMethodNamed')}</span>
          </label>
          <label class="flex items-center gap-1.5">
            <input type="radio" name="invite-method" value="tag" />
            <span>${t('sharing.inviteMethodTag')}</span>
          </label>
          <label class="flex items-center gap-1.5">
            <input type="radio" name="invite-method" value="link" />
            <span>${t('sharing.inviteMethodLink')}</span>
          </label>
        </fieldset>
        <div data-named="1" class="space-y-2" data-user-picker-root="1">
          <label class="block text-xs font-medium text-slate-600">${t('sharing.inviteUserLabel')}</label>
          <input type="hidden" data-invitee-value="1" value="" />
          <div class="flex flex-wrap gap-2">
            <input type="search" data-user-search="1" placeholder="${t('sharing.inviteUserSearchPlaceholder')}" class="flex-1 min-w-[12rem] rounded border border-slate-300 px-2 py-1.5 text-sm" autocomplete="off" />
            <select data-user-tag-filter="1" class="rounded border border-slate-300 px-2 py-1.5 text-sm min-w-[10rem]" title="${t('sharing.inviteUserFilterTag')}">
              <option value="">${t('common.loading')}</option>
            </select>
          </div>
          <div class="flex flex-wrap items-center gap-2 min-h-[1.25rem]">
            <p data-user-selected-label="1" class="text-xs text-sky-800 flex-1 min-w-0"></p>
            <button type="button" data-user-clear="1" class="hidden shrink-0 text-xs text-slate-600 hover:text-slate-800 underline">${t('sharing.inviteClearSelection')}</button>
          </div>
          <div data-user-list="1" class="max-h-48 overflow-y-auto rounded border border-slate-200 bg-white">
            <p class="text-xs text-slate-500 px-3 py-4 text-center">${t('common.loading')}</p>
          </div>
        </div>
        <div data-tag="1" class="hidden space-y-2" data-tag-picker-root="1">
          <label class="block text-xs font-medium text-slate-600">${t('sharing.inviteTagLabel')}</label>
          <input type="hidden" data-tag-value="1" value="" />
          <input type="search" data-tag-search="1" placeholder="${t('sharing.inviteTagSearchPlaceholder')}" class="w-full rounded border border-slate-300 px-2 py-1.5 text-sm" autocomplete="off" />
          <div class="flex flex-wrap items-center gap-2 min-h-[1.25rem]">
            <p data-tag-selected-label="1" class="text-xs text-sky-800 flex-1 min-w-0"></p>
            <button type="button" data-tag-clear="1" class="hidden shrink-0 text-xs text-slate-600 hover:text-slate-800 underline">${t('sharing.inviteClearSelection')}</button>
          </div>
          <div data-tag-list="1" class="max-h-40 overflow-y-auto rounded border border-slate-200 bg-white">
            <p class="text-xs text-slate-500 px-3 py-4 text-center">${t('common.loading')}</p>
          </div>
          <p class="text-xs text-slate-500">${t('sharing.inviteTagHint')}</p>
        </div>
        <div data-link="1" class="hidden space-y-2">
          <span class="block text-xs font-medium text-slate-600">${t('sharing.inviteLinkUsageLabel')}</span>
          <fieldset class="flex flex-wrap gap-3 text-sm text-slate-700">
            <label class="flex items-center gap-1.5">
              <input type="radio" name="link-usage" value="single" checked />
              <span>${t('sharing.inviteLinkSingle')}</span>
            </label>
            <label class="flex items-center gap-1.5">
              <input type="radio" name="link-usage" value="limited" />
              <span>${t('sharing.inviteLinkLimited')}</span>
            </label>
            <label class="flex items-center gap-1.5">
              <input type="radio" name="link-usage" value="unlimited" />
              <span>${t('sharing.inviteLinkUnlimited')}</span>
            </label>
          </fieldset>
          <div data-link-max-wrap="1" class="hidden space-y-1">
            <label class="block text-xs font-medium text-slate-600">${t('sharing.inviteLinkMaxUsesLabel')}</label>
            <input type="number" data-link-max="1" min="2" max="999" value="5" class="w-32 rounded border border-slate-300 px-2 py-1.5 text-sm" />
          </div>
        </div>
        <div class="space-y-1">
          <label class="block text-xs font-medium text-slate-600">${t('sharing.inviteTTLLabel')}</label>
          <select data-ttl="1" class="rounded border border-slate-300 px-2 py-1.5 text-sm">
            <option value="900">${t('sharing.inviteTTL15')}</option>
            <option value="3600" selected>${t('sharing.inviteTTL60')}</option>
            <option value="14400">${t('sharing.inviteTTL240')}</option>
          </select>
        </div>
        <div class="flex items-center gap-2">
          <button type="button" data-issue="1" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700">${t('sharing.inviteSubmit')}</button>
          <span data-status="1" class="text-xs text-slate-500"></span>
        </div>
        <div data-token-row="1" class="hidden space-y-1.5 rounded border border-emerald-200 bg-emerald-50 px-3 py-2">
          <p class="text-xs text-emerald-800">${t('sharing.inviteUrlHelp')}</p>
          <div class="flex items-center gap-2">
            <input type="text" data-token-url="1" readonly class="flex-1 rounded border border-emerald-300 bg-white px-2 py-1 text-xs font-mono" />
            <button type="button" data-copy="1" class="rounded bg-emerald-600 px-2 py-1 text-xs font-medium text-white hover:bg-emerald-700">${t('sharing.inviteCopy')}</button>
          </div>
        </div>
        <div class="space-y-2">
          <h4 class="text-xs font-semibold text-slate-700">${t('sharing.inviteList')}</h4>
          <div data-list="1" class="rounded border border-slate-200 overflow-hidden">
            <p class="text-xs text-slate-500 px-3 py-2">${t('common.loading')}</p>
          </div>
        </div>
      </div>
    </div>
  `
  document.body.appendChild(wrap)
  openDialogEl = wrap

  let stopInviteListWatch = null

  const close = () => {
    if (stopInviteListWatch) {
      try { stopInviteListWatch() } catch { /* ignore */ }
      stopInviteListWatch = null
    }
    try { wrap.remove() } catch { /* ignore */ }
    if (openDialogEl === wrap) openDialogEl = null
  }

  const status = wrap.querySelector('[data-status="1"]')
  const namedBox = wrap.querySelector('[data-named="1"]')
  const tagBox = wrap.querySelector('[data-tag="1"]')
  const linkBox = wrap.querySelector('[data-link="1"]')
  const linkMaxWrap = wrap.querySelector('[data-link-max-wrap="1"]')
  const userPickerRoot = wrap.querySelector('[data-user-picker-root="1"]')
  const tagPickerRoot = wrap.querySelector('[data-tag-picker-root="1"]')
  let userPicker = null
  let tagPicker = null
  const linkMaxInput = wrap.querySelector('[data-link-max="1"]')
  const ttlSelect = wrap.querySelector('[data-ttl="1"]')
  const issueBtn = wrap.querySelector('[data-issue="1"]')
  const tokenRow = wrap.querySelector('[data-token-row="1"]')
  const tokenInput = wrap.querySelector('[data-token-url="1"]')
  const listBox = wrap.querySelector('[data-list="1"]')

  function syncMethodPanels() {
    const method = wrap.querySelector('input[name="invite-method"]:checked')?.value || 'named'
    namedBox.classList.toggle('hidden', method !== 'named')
    tagBox.classList.toggle('hidden', method !== 'tag')
    linkBox.classList.toggle('hidden', method !== 'link')
  }

  wrap.addEventListener('click', (e) => {
    if (e.target === wrap) close()
  })
  wrap.querySelector('[data-close="1"]').addEventListener('click', close)
  wrap.querySelectorAll('input[name="invite-method"]').forEach((r) => {
    r.addEventListener('change', syncMethodPanels)
  })
  wrap.querySelectorAll('input[name="link-usage"]').forEach((r) => {
    r.addEventListener('change', () => {
      const usage = wrap.querySelector('input[name="link-usage"]:checked')?.value || 'single'
      linkMaxWrap.classList.toggle('hidden', usage !== 'limited')
    })
  })

  void API.sessionInvitationOptions(sessionId)
    .then((opts) => {
      const users = Array.isArray(opts?.users) ? opts.users : []
      const tags = Array.isArray(opts?.tags) ? opts.tags : []
      userPicker = mountUserPicker(userPickerRoot, { users, tags, escapeHtml })
      tagPicker = mountTagPicker(tagPickerRoot, { tags, escapeHtml })
    })
    .catch(() => {
      userPicker = mountUserPicker(userPickerRoot, { users: [], tags: [], escapeHtml })
      tagPicker = mountTagPicker(tagPickerRoot, { tags: [], escapeHtml })
    })

  function revealJoinUrl(fullUrl, { regenerated = false } = {}) {
    if (!fullUrl) return
    tokenRow.classList.remove('hidden')
    tokenInput.value = fullUrl
    status.textContent = regenerated ? t('sharing.inviteLinkRegenerated') : ''
    try {
      tokenRow.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    } catch {
      /* ignore */
    }
  }

  async function fetchJoinUrl(invitationId, { forceRegenerate = false } = {}) {
    if (!forceRegenerate) {
      const cached = getCachedInviteUrl(sessionId, invitationId)
      if (cached) return { url: cached, regenerated: false }
    }
    const res = await API.regenerateSessionInvitationJoinUrl(sessionId, invitationId)
    const path = res?.join_url || ''
    const fullUrl = path ? new URL(path, window.location.origin).toString() : ''
    if (fullUrl) writeInviteUrlCache(sessionId, invitationId, fullUrl)
    return { url: fullUrl, regenerated: true }
  }

  let cachedInvitations = []

  async function refreshList({ mergeItems } = {}) {
    try {
      const res = await API.listSessionInvitations(sessionId)
      let items = Array.isArray(res?.items) ? res.items : []
      if (mergeItems?.length) {
        items = mergeInvitationLists(items, mergeItems)
      }
      cachedInvitations = items
      listBox.innerHTML = renderInvitationsTable(items, escapeHtml)
      listBox.querySelectorAll('[data-show-link-id]').forEach((btn) => {
        btn.addEventListener('click', async () => {
          const id = btn.getAttribute('data-show-link-id') || ''
          if (!id) return
          btn.disabled = true
          try {
            const { url } = await fetchJoinUrl(id, { forceRegenerate: false })
            revealJoinUrl(url, { regenerated: false })
          } catch (err) {
            await uiAlert(err?.message || t('sharing.inviteFailed', { error: '' }))
          } finally {
            btn.disabled = false
          }
        })
      })
      listBox.querySelectorAll('[data-regenerate-link-id]').forEach((btn) => {
        btn.addEventListener('click', async () => {
          const id = btn.getAttribute('data-regenerate-link-id') || ''
          if (!id) return
          if (!(await uiConfirm(t('sharing.inviteRegenerateConfirm'), { danger: true }))) return
          btn.disabled = true
          try {
            const { url } = await fetchJoinUrl(id, { forceRegenerate: true })
            revealJoinUrl(url, { regenerated: true })
          } catch (err) {
            await uiAlert(err?.message || t('sharing.inviteFailed', { error: '' }))
          } finally {
            btn.disabled = false
          }
        })
      })
      listBox.querySelectorAll('[data-delete-id]').forEach((btn) => {
        btn.addEventListener('click', async () => {
          const id = btn.getAttribute('data-delete-id') || ''
          if (!id) return
          if (!(await uiConfirm(t('sharing.inviteDeleteConfirm'), { danger: true }))) return
          try {
            await API.revokeSessionInvitation(sessionId, id)
            removeInviteUrlCache(sessionId, id)
            await refreshList()
          } catch (err) {
            await uiAlert(err?.message || t('sharing.inviteFailed', { error: '' }))
          }
        })
      })
    } catch (err) {
      listBox.innerHTML = `<p class="text-xs text-red-600 px-3 py-2">${escapeHtml(err?.message || '')}</p>`
    }
  }

  issueBtn.addEventListener('click', async () => {
    const method = wrap.querySelector('input[name="invite-method"]:checked')?.value || 'named'
    const ttlSeconds = Number.parseInt(ttlSelect.value, 10) || 900
    const payload = { mode: 'viewer', ttl_seconds: ttlSeconds }

    if (method === 'named') {
      const invitee = (userPicker?.getValue() || '').trim()
      if (!invitee) {
        status.textContent = t('sharing.inviteUserNotFound')
        return
      }
      payload.invitee_user_id = invitee
    } else if (method === 'tag') {
      const tag = (tagPicker?.getValue() || '').trim()
      if (!tag) {
        status.textContent = t('sharing.inviteTagNotForTarget')
        return
      }
      payload.invite_tag = tag
    } else {
      const usage = wrap.querySelector('input[name="link-usage"]:checked')?.value || 'single'
      if (usage === 'unlimited') {
        payload.link_unlimited = true
      } else if (usage === 'limited') {
        const n = Number.parseInt(linkMaxInput?.value, 10)
        if (!Number.isFinite(n) || n < 2) {
          status.textContent = t('sharing.linkMaxUsesInvalid')
          return
        }
        payload.link_max_uses = n
      } else {
        payload.link_max_uses = 1
      }
    }

    issueBtn.disabled = true
    status.textContent = ''
    try {
      const res = await API.createSessionInvitation(sessionId, payload)
      if (method === 'link') {
        const url = res?.join_url || ''
        const fullUrl = url ? new URL(url, window.location.origin).toString() : ''
        if (fullUrl && res?.id) writeInviteUrlCache(sessionId, res.id, fullUrl)
        if (fullUrl) revealJoinUrl(fullUrl, { regenerated: false })
        else tokenRow.classList.add('hidden')
      } else {
        tokenRow.classList.add('hidden')
      }
      if (method === 'tag' && res?.created != null) {
        status.textContent = t('sharing.inviteTagCreated', { n: res.created })
      } else {
        status.textContent = t('sharing.inviteCreated')
      }
      userPicker?.clear()
      tagPicker?.clear()
      const createdItems = invitationsFromCreateResponse(res, method)
      if (createdItems.length > 0) {
        cachedInvitations = mergeInvitationLists(cachedInvitations, createdItems)
        listBox.innerHTML = renderInvitationsTable(cachedInvitations, escapeHtml)
      }
      await refreshList({ mergeItems: createdItems })
      if (listBox) {
        try {
          listBox.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
        } catch {
          /* ignore */
        }
      }
    } catch (err) {
      status.textContent = t('sharing.inviteFailed', { error: err?.message || '' })
    } finally {
      issueBtn.disabled = false
    }
  })

  wrap.querySelector('[data-copy="1"]').addEventListener('click', async () => {
    const url = tokenInput.value || ''
    if (!url) return
    try {
      await navigator.clipboard.writeText(url)
      status.textContent = t('sharing.inviteCopied')
    } catch {
      tokenInput.select()
    }
  })

  stopInviteListWatch = createRealtimeWatcher({
    shouldRefresh: (payload) => shouldRefreshInviteList(payload, sessionId),
    onRefresh: () => refreshList(),
    pollMs: POLL_MS.inviteList,
  })

  syncMethodPanels()
  refreshList()
}
