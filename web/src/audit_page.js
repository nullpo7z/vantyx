import API from './api.js'
import { t } from './i18n.js'

function escapeHtml(s) {
  const div = document.createElement('div')
  div.textContent = String(s ?? '')
  return div.innerHTML
}

function fmtTime(t) {
  try {
    return new Date(t).toLocaleString()
  } catch {
    return String(t || '')
  }
}

function fieldsToText(fields) {
  if (!fields || typeof fields !== 'object') return ''
  const keys = Object.keys(fields).sort()
  return keys
    .filter((k) => k !== 'event')
    .map((k) => `${k}=${String(fields[k])}`)
    .join(' ')
}

function auditEventLabel(event) {
  if (!event) return '—'
  const k = `audit.eventLabels.${event}`
  const localized = t(k)
  return localized === k ? event : localized
}

function auditUserId(it) {
  const f = it.fields || {}
  return String(f.user_id || '').trim()
}

function formatAuditSummary(it) {
  const ev = it.event || (it.fields && it.fields.event) || ''
  const f = it.fields || {}
  if (ev === 'http_request') {
    const method = String(f.method || 'GET')
    const path = String(f.path || '')
    const status = f.status != null ? String(f.status) : ''
    const ms = f.duration_ms != null ? `${f.duration_ms}ms` : ''
    const query = f.query ? `?${f.query}` : ''
    const tail = [status, ms].filter(Boolean).join(' · ')
    return tail ? `${method} ${path}${query} → ${tail}` : `${method} ${path}${query}`
  }
  if (ev === 'login_success') {
    return t('audit.loggedIn', { user: auditUserId(it) || t('audit.fallbackUser') })
  }
  if (ev === 'login_failed' || ev === 'login_rate_limited') {
    const remote = f.remote ? ` (${f.remote})` : ''
    return `${auditUserId(it) || t('audit.fallbackUser')}${remote}`
  }
  if (ev.startsWith('terminal_')) {
    const parts = []
    if (f.target_id) parts.push(`target=${f.target_id}`)
    if (f.session_id) parts.push(`session=${f.session_id}`)
    if (f.reason) parts.push(String(f.reason))
    if (f.error) parts.push(String(f.error))
    return parts.length ? parts.join(' · ') : auditEventLabel(ev)
  }
  if (ev.startsWith('files_')) {
    const parts = []
    if (f.target_id) parts.push(`target=${f.target_id}`)
    if (f.path) parts.push(String(f.path))
    if (f.error) parts.push(String(f.error))
    return parts.join(' · ') || auditEventLabel(ev)
  }
  const text = fieldsToText(f)
  return text.length > 120 ? text.slice(0, 117) + '…' : text
}

function httpStatusClass(status) {
  const n = Number(status)
  if (n >= 500) return 'text-red-700 bg-red-50'
  if (n >= 400) return 'text-amber-800 bg-amber-50'
  if (n >= 200 && n < 300) return 'text-emerald-800 bg-emerald-50'
  return 'text-slate-700 bg-slate-100'
}

function defaultDateRange() {
  const to = new Date()
  const from = new Date(to)
  from.setDate(from.getDate() - 30)
  return {
    from: from.toISOString().slice(0, 10),
    to: to.toISOString().slice(0, 10),
  }
}

function flattenTargets(groups) {
  const out = []
  for (const g of groups || []) {
    for (const t of g.targets || []) {
      out.push({ id: t.id, name: t.name || t.id })
    }
  }
  return out.sort((a, b) => String(a.name).localeCompare(String(b.name)))
}

const TAB_BTN_ACTIVE =
  'border-sky-600 text-sky-700 font-semibold'
const TAB_BTN_INACTIVE =
  'border-transparent text-slate-600 hover:text-slate-800 hover:border-slate-300'

export async function renderAuditPage({ mainContent, meData, setActiveNav }) {
  if (!meData || meData.role !== 'admin') {
    mainContent.innerHTML = `<p class="text-sm text-red-600">${t('audit.forbidden')}</p>`
    return
  }
  if (typeof setActiveNav === 'function') setActiveNav('audit')

  const dates = defaultDateRange()
  let users = []
  let targets = []
  try {
    const [usersRes, groupsRes] = await Promise.all([API.users(), API.groups()])
    users = (usersRes && usersRes.items) || []
    targets = flattenTargets(groupsRes)
  } catch {
    /* dropdowns stay empty */
  }

  const allOpt = `<option value="">${t('audit.allOptionParen')}</option>`
  const userOptions =
    allOpt +
    users.map((u) => `<option value="${escapeHtml(u.id)}">${escapeHtml(u.id)}</option>`).join('')
  const targetOptions =
    allOpt +
    targets
      .map((tg) => `<option value="${escapeHtml(tg.id)}">${escapeHtml(tg.name)} (${escapeHtml(tg.id)})</option>`)
      .join('')

  mainContent.innerHTML = `
    <div class="w-full max-w-6xl">
      <h2 class="text-lg font-semibold text-slate-800 mb-4">${t('audit.title')}</h2>

      <div class="flex gap-1 border-b border-slate-200 mb-0" role="tablist" aria-label="${t('audit.tabsAria')}">
        <button type="button" id="audit-tab-btn-audit" role="tab" aria-selected="true" aria-controls="audit-panel-audit" data-tab="audit"
          class="px-4 py-2.5 text-sm border-b-2 -mb-px transition-colors ${TAB_BTN_ACTIVE}">
          ${t('audit.tabAudit')}
        </button>
        <button type="button" id="audit-tab-btn-cmd" role="tab" aria-selected="false" aria-controls="audit-panel-cmd" data-tab="cmd"
          class="px-4 py-2.5 text-sm border-b-2 -mb-px transition-colors ${TAB_BTN_INACTIVE}">
          ${t('audit.tabCmd')}
        </button>
        <button type="button" id="audit-tab-btn-ft" role="tab" aria-selected="false" aria-controls="audit-panel-ft" data-tab="ft"
          class="px-4 py-2.5 text-sm border-b-2 -mb-px transition-colors ${TAB_BTN_INACTIVE}">
          ${t('audit.tabFt')}
        </button>
      </div>

      <div id="audit-panel-audit" role="tabpanel" aria-labelledby="audit-tab-btn-audit" class="bg-white rounded-b-lg rounded-tr-lg border border-t-0 border-slate-200 shadow-sm overflow-hidden">
        <div class="px-4 py-3 border-b border-slate-200 flex items-center justify-between gap-3 flex-wrap bg-slate-50">
          <p class="text-xs text-slate-500">${t('audit.auditCardHint')}</p>
          <button id="audit-refresh" type="button" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('audit.refresh')}</button>
        </div>
        <div class="p-4 border-b border-slate-200 flex gap-3 flex-wrap items-end">
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.rangeFrom')}</label>
            <input id="audit-filter-from" type="date" value="${escapeHtml(dates.from)}" class="rounded border border-slate-300 px-3 py-2 text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.rangeTo')}</label>
            <input id="audit-filter-to" type="date" value="${escapeHtml(dates.to)}" class="rounded border border-slate-300 px-3 py-2 text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.eventKind')}</label>
            <select id="audit-filter-event-preset" class="rounded border border-slate-300 px-3 py-2 text-sm bg-white">
              <option value="">${t('audit.allOption')}</option>
              <option value="login_">${t('audit.eventLogin')}</option>
              <option value="terminal_">${t('audit.eventTerminal')}</option>
              <option value="files_">${t('audit.eventFiles')}</option>
            </select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">event</label>
            <input id="audit-filter-event" class="w-36 rounded border border-slate-300 px-3 py-2 text-sm" placeholder="${t('audit.eventPlaceholder')}" />
          </div>
          <label class="flex items-center gap-2 text-sm text-slate-700 pb-2 cursor-pointer select-none">
            <input id="audit-filter-http" type="checkbox" class="rounded border-slate-300" />
            <span class="text-xs">${t('audit.includeHttp')}</span>
          </label>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.userIdLabel')}</label>
            <input id="audit-filter-user" class="w-40 rounded border border-slate-300 px-3 py-2 text-sm" placeholder="${t('audit.userIdPlaceholder')}" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.limit')}</label>
            <input id="audit-filter-limit" type="number" min="1" max="1000" value="200" class="w-28 rounded border border-slate-300 px-3 py-2 text-sm" />
          </div>
          <button id="audit-apply" type="button" class="rounded bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('audit.apply')}</button>
        </div>
        <div id="audit-error" class="px-4 py-3 text-sm text-red-600 hidden"></div>
        <div class="overflow-x-auto">
          <table class="min-w-full text-left text-sm">
            <thead class="bg-slate-50 border-b border-slate-200">
              <tr>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.headerTime')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.headerKind')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.headerWho')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.headerSummary')}</th>
              </tr>
            </thead>
            <tbody id="audit-rows">
              <tr><td colspan="4" class="px-4 py-6 text-center text-slate-500">${t('audit.loading')}</td></tr>
            </tbody>
          </table>
        </div>
        <div id="audit-footer" class="px-4 py-3 border-t border-slate-200 flex items-center justify-between gap-3 flex-wrap bg-slate-50">
          <span id="audit-count" class="text-xs text-slate-500"></span>
          <button type="button" id="audit-load-more" class="hidden rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('audit.loadMore')}</button>
        </div>
      </div>

      <div id="audit-panel-cmd" role="tabpanel" aria-labelledby="audit-tab-btn-cmd" class="hidden bg-white rounded-b-lg rounded-tr-lg border border-t-0 border-slate-200 shadow-sm overflow-hidden">
        <div class="px-4 py-3 border-b border-slate-200 flex items-center justify-between gap-3 flex-wrap bg-slate-50">
          <p class="text-xs text-slate-500">${t('audit.cmdHint')}</p>
          <button id="cmd-refresh" type="button" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('audit.cmdResearch')}</button>
        </div>
        <div class="p-4 border-b border-slate-200 flex gap-3 flex-wrap items-end">
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.rangeFrom')}</label>
            <input id="cmd-filter-from" type="date" value="${escapeHtml(dates.from)}" class="rounded border border-slate-300 px-3 py-2 text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.rangeTo')}</label>
            <input id="cmd-filter-to" type="date" value="${escapeHtml(dates.to)}" class="rounded border border-slate-300 px-3 py-2 text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.cmdQueryLabel')}</label>
            <input id="cmd-filter-query" class="w-48 rounded border border-slate-300 px-3 py-2 text-sm" placeholder="${t('audit.cmdQueryPlaceholder')}" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.cmdUserLabel')}</label>
            <select id="cmd-filter-user" class="w-40 rounded border border-slate-300 px-3 py-2 text-sm bg-white">${userOptions}</select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.cmdTargetLabel')}</label>
            <select id="cmd-filter-target" class="w-56 rounded border border-slate-300 px-3 py-2 text-sm bg-white">${targetOptions}</select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.limit')}</label>
            <input id="cmd-filter-limit" type="number" min="1" max="500" value="200" class="w-24 rounded border border-slate-300 px-3 py-2 text-sm" />
          </div>
          <button id="cmd-apply" type="button" class="rounded bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('audit.apply')}</button>
        </div>
        <div id="cmd-error" class="px-4 py-3 text-sm text-red-600 hidden"></div>
        <div class="overflow-x-auto">
          <table class="min-w-full text-left text-sm">
            <thead class="bg-slate-50 border-b border-slate-200">
              <tr>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.headerTime')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.cmdHeaderUser')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.cmdHeaderTarget')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.cmdHeaderSession')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.cmdHeaderInput')}</th>
              </tr>
            </thead>
            <tbody id="cmd-rows">
              <tr><td colspan="5" class="px-4 py-6 text-center text-slate-500">${t('audit.cmdInitialPrompt')}</td></tr>
            </tbody>
          </table>
        </div>
        <div id="cmd-footer" class="px-4 py-3 border-t border-slate-200 flex items-center justify-between gap-3 flex-wrap bg-slate-50">
          <span id="cmd-count" class="text-xs text-slate-500"></span>
          <button type="button" id="cmd-load-more" class="hidden rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('audit.loadMore')}</button>
        </div>
      </div>

      <div id="audit-panel-ft" role="tabpanel" aria-labelledby="audit-tab-btn-ft" class="hidden bg-white rounded-b-lg rounded-tr-lg border border-t-0 border-slate-200 shadow-sm overflow-hidden">
        <div class="px-4 py-3 border-b border-slate-200 flex items-center justify-between gap-3 flex-wrap bg-slate-50">
          <p class="text-xs text-slate-500">${t('audit.ftHint')}</p>
          <button id="ft-refresh" type="button" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('audit.cmdResearch')}</button>
        </div>
        <div class="p-4 border-b border-slate-200 flex gap-3 flex-wrap items-end">
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.rangeFrom')}</label>
            <input id="ft-filter-from" type="date" value="${escapeHtml(dates.from)}" class="rounded border border-slate-300 px-3 py-2 text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.rangeTo')}</label>
            <input id="ft-filter-to" type="date" value="${escapeHtml(dates.to)}" class="rounded border border-slate-300 px-3 py-2 text-sm" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.ftStateLabel')}</label>
            <select id="ft-filter-state" class="rounded border border-slate-300 px-3 py-2 text-sm bg-white">
              <option value="">${t('audit.allOptionParen')}</option>
              <option value="completed">${t('audit.ftStateCompleted')}</option>
              <option value="failed">${t('audit.ftStateFailed')}</option>
              <option value="cancelled">${t('audit.ftStateCancelled')}</option>
              <option value="running">${t('audit.ftStateRunning')}</option>
              <option value="receiving">${t('audit.ftStateReceiving')}</option>
            </select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.ftDirectionLabel')}</label>
            <select id="ft-filter-direction" class="rounded border border-slate-300 px-3 py-2 text-sm bg-white">
              <option value="">${t('audit.allOptionParen')}</option>
              <option value="upload">${t('audit.ftDirectionUpload')}</option>
              <option value="download">${t('audit.ftDirectionDownload')}</option>
            </select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.ftBackendLabel')}</label>
            <select id="ft-filter-backend" class="rounded border border-slate-300 px-3 py-2 text-sm bg-white">
              <option value="">${t('audit.allOptionParen')}</option>
              <option value="remote">remote</option>
              <option value="tftp_server">tftp_server</option>
            </select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.cmdUserLabel')}</label>
            <select id="ft-filter-user" class="w-40 rounded border border-slate-300 px-3 py-2 text-sm bg-white">${userOptions}</select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.cmdTargetLabel')}</label>
            <select id="ft-filter-target" class="w-56 rounded border border-slate-300 px-3 py-2 text-sm bg-white">${targetOptions}</select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.ftSearchLabel')}</label>
            <input id="ft-filter-query" class="w-56 rounded border border-slate-300 px-3 py-2 text-sm" placeholder="${t('audit.ftSearchPlaceholder')}" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('audit.limit')}</label>
            <input id="ft-filter-limit" type="number" min="1" max="500" value="100" class="w-24 rounded border border-slate-300 px-3 py-2 text-sm" />
          </div>
          <button id="ft-apply" type="button" class="rounded bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('audit.apply')}</button>
        </div>
        <div id="ft-error" class="px-4 py-3 text-sm text-red-600 hidden"></div>
        <div class="overflow-x-auto">
          <table class="min-w-full text-left text-sm">
            <thead class="bg-slate-50 border-b border-slate-200">
              <tr>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.headerTime')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.cmdHeaderUser')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.ftHeaderDirection')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.ftHeaderBackend')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.ftHeaderTarget')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.ftHeaderFile')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.ftHeaderState')}</th>
                <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('audit.ftHeaderProgress')}</th>
              </tr>
            </thead>
            <tbody id="ft-rows">
              <tr><td colspan="8" class="px-4 py-6 text-center text-slate-500">${t('audit.ftInitialPrompt')}</td></tr>
            </tbody>
          </table>
        </div>
        <div id="ft-footer" class="px-4 py-3 border-t border-slate-200 flex items-center justify-between gap-3 flex-wrap bg-slate-50">
          <span id="ft-count" class="text-xs text-slate-500"></span>
          <button type="button" id="ft-load-more" class="hidden rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('audit.loadMore')}</button>
        </div>
      </div>

      <p class="text-xs text-slate-500 mt-3">${t('audit.cmdNote')}</p>
    </div>
  `

  const tabBtnAudit = mainContent.querySelector('#audit-tab-btn-audit')
  const tabBtnCmd = mainContent.querySelector('#audit-tab-btn-cmd')
  const tabBtnFt = mainContent.querySelector('#audit-tab-btn-ft')
  const panelAudit = mainContent.querySelector('#audit-panel-audit')
  const panelCmd = mainContent.querySelector('#audit-panel-cmd')
  const panelFt = mainContent.querySelector('#audit-panel-ft')

  let ftLoadedOnce = false

  function setActiveTab(tab) {
    const tabs = [
      { name: 'audit', btn: tabBtnAudit, panel: panelAudit },
      { name: 'cmd', btn: tabBtnCmd, panel: panelCmd },
      { name: 'ft', btn: tabBtnFt, panel: panelFt },
    ]
    for (const tb of tabs) {
      const active = tb.name === tab
      tb.panel.classList.toggle('hidden', !active)
      tb.btn.setAttribute('aria-selected', active ? 'true' : 'false')
      tb.btn.className = `px-4 py-2.5 text-sm border-b-2 -mb-px transition-colors ${active ? TAB_BTN_ACTIVE : TAB_BTN_INACTIVE}`
    }
    if (tab === 'ft' && !ftLoadedOnce) {
      ftLoadedOnce = true
      loadFt(false)
    }
  }

  tabBtnAudit.addEventListener('click', () => setActiveTab('audit'))
  tabBtnCmd.addEventListener('click', () => setActiveTab('cmd'))
  tabBtnFt.addEventListener('click', () => setActiveTab('ft'))

  const errEl = mainContent.querySelector('#audit-error')
  const rowsEl = mainContent.querySelector('#audit-rows')
  const auditFromEl = mainContent.querySelector('#audit-filter-from')
  const auditToEl = mainContent.querySelector('#audit-filter-to')
  const eventEl = mainContent.querySelector('#audit-filter-event')
  const userEl = mainContent.querySelector('#audit-filter-user')
  const limitEl = mainContent.querySelector('#audit-filter-limit')
  const includeHttpEl = mainContent.querySelector('#audit-filter-http')
  const eventPresetEl = mainContent.querySelector('#audit-filter-event-preset')
  const countEl = mainContent.querySelector('#audit-count')
  const loadMoreEl = mainContent.querySelector('#audit-load-more')

  let auditNextCursor = ''
  let auditRowCount = 0

  function auditQueryParams() {
    return {
      limit: Number(limitEl.value || 200) || 200,
      event: String(eventEl.value || '').trim(),
      user_id: String(userEl.value || '').trim(),
      from: String(auditFromEl.value || '').trim(),
      to: String(auditToEl.value || '').trim(),
      exclude_event: includeHttpEl && includeHttpEl.checked ? '' : 'http_request',
    }
  }

  function renderAuditRow(it) {
    const time = fmtTime(it.time)
    const ev = it.event || (it.fields && it.fields.event) || ''
    const label = auditEventLabel(ev)
    const uid = auditUserId(it)
    const summary = formatAuditSummary(it)
    const status = it.fields && it.fields.status
    const statusBadge =
      ev === 'http_request' && status != null
        ? `<span class="inline-block rounded px-1.5 py-0.5 text-[10px] font-medium ${httpStatusClass(status)}">${escapeHtml(String(status))}</span> `
        : ''
    return `<tr class="border-b border-slate-200 hover:bg-slate-50">
      <td class="px-4 py-2 text-xs text-slate-600 whitespace-nowrap">${escapeHtml(time)}</td>
      <td class="px-4 py-2 text-xs text-slate-800 whitespace-nowrap" title="${escapeHtml(ev)}">${escapeHtml(label)}</td>
      <td class="px-4 py-2 text-xs text-slate-700 whitespace-nowrap font-mono">${escapeHtml(uid || '—')}</td>
      <td class="px-4 py-2 text-xs text-slate-800">${statusBadge}<span class="font-mono text-[11px] break-all">${escapeHtml(summary)}</span></td>
    </tr>`
  }

  function updateAuditFooter() {
    const hasMore = Boolean(auditNextCursor)
    countEl.textContent = hasMore
      ? t('audit.visibleCountMore', { n: auditRowCount })
      : t('audit.visibleCount', { n: auditRowCount })
    loadMoreEl.classList.toggle('hidden', !hasMore)
  }

  async function loadAudit(append) {
    errEl.classList.add('hidden')
    if (!append) {
      auditNextCursor = ''
      auditRowCount = 0
      rowsEl.innerHTML = `<tr><td colspan="4" class="px-4 py-6 text-center text-slate-500">${t('audit.loading')}</td></tr>`
      loadMoreEl.classList.add('hidden')
    } else {
      loadMoreEl.disabled = true
      loadMoreEl.textContent = t('audit.loading')
    }
    const params = auditQueryParams()
    try {
      const res = await API.auditLogs({ ...params, after_id: append ? auditNextCursor : '' })
      const items = (res && res.items) || []
      auditNextCursor = (res && res.next_cursor) || ''
      if (!append && !items.length) {
        rowsEl.innerHTML = `<tr><td colspan="4" class="px-4 py-6 text-center text-slate-500">${t('audit.noLogs')}</td></tr>`
        countEl.textContent = ''
        loadMoreEl.classList.add('hidden')
        return
      }
      const html = items.map(renderAuditRow).join('')
      if (append) {
        rowsEl.insertAdjacentHTML('beforeend', html)
      } else {
        rowsEl.innerHTML = html
      }
      auditRowCount += items.length
      updateAuditFooter()
    } catch (e) {
      errEl.textContent = e.message || t('audit.fetchFailed')
      errEl.classList.remove('hidden')
      if (!append) {
        rowsEl.innerHTML = `<tr><td colspan="4" class="px-4 py-6 text-center text-slate-500">${t('audit.fetchFailedRow')}</td></tr>`
        countEl.textContent = ''
      }
      loadMoreEl.classList.add('hidden')
    } finally {
      loadMoreEl.disabled = false
      loadMoreEl.textContent = t('audit.loadMore')
    }
  }

  eventPresetEl?.addEventListener('change', () => {
    const v = eventPresetEl.value || ''
    eventEl.value = v
  })

  mainContent.querySelector('#audit-refresh').addEventListener('click', () => loadAudit(false))
  mainContent.querySelector('#audit-apply').addEventListener('click', () => loadAudit(false))
  loadMoreEl.addEventListener('click', () => loadAudit(true))

  await loadAudit(false)

  const cmdErrEl = mainContent.querySelector('#cmd-error')
  const cmdRowsEl = mainContent.querySelector('#cmd-rows')
  const cmdQueryEl = mainContent.querySelector('#cmd-filter-query')
  const cmdUserEl = mainContent.querySelector('#cmd-filter-user')
  const cmdTargetEl = mainContent.querySelector('#cmd-filter-target')
  const cmdFromEl = mainContent.querySelector('#cmd-filter-from')
  const cmdToEl = mainContent.querySelector('#cmd-filter-to')
  const cmdLimitEl = mainContent.querySelector('#cmd-filter-limit')
  const cmdCountEl = mainContent.querySelector('#cmd-count')
  const cmdLoadMoreEl = mainContent.querySelector('#cmd-load-more')

  let cmdNextCursor = ''
  let cmdRowCount = 0

  function cmdQueryParams() {
    return {
      query: String(cmdQueryEl.value || '').trim(),
      user_id: String(cmdUserEl.value || '').trim(),
      target_id: String(cmdTargetEl.value || '').trim(),
      from: String(cmdFromEl.value || '').trim(),
      to: String(cmdToEl.value || '').trim(),
      limit: Number(cmdLimitEl.value || 200) || 200,
    }
  }

  function renderCmdRow(it) {
    const time = fmtTime(it.time)
    return `<tr class="border-b border-slate-200 hover:bg-slate-50">
      <td class="px-4 py-2 text-xs text-slate-600 whitespace-nowrap">${escapeHtml(time)}</td>
      <td class="px-4 py-2 text-xs text-slate-700 whitespace-nowrap">${escapeHtml(it.user_id || '')}</td>
      <td class="px-4 py-2 text-xs text-slate-700 whitespace-nowrap font-mono">${escapeHtml(it.target_id || '')}</td>
      <td class="px-4 py-2 text-xs text-slate-500 whitespace-nowrap font-mono max-w-[8rem] truncate" title="${escapeHtml(it.session_id || '')}">${escapeHtml(it.session_id || '')}</td>
      <td class="px-4 py-2 text-xs text-slate-800 font-mono text-[11px] break-all">${escapeHtml(it.line_text || '')}</td>
    </tr>`
  }

  function updateCmdFooter() {
    const hasMore = Boolean(cmdNextCursor)
    cmdCountEl.textContent = hasMore
      ? t('audit.visibleCountMore', { n: cmdRowCount })
      : cmdRowCount > 0
        ? t('audit.visibleCount', { n: cmdRowCount })
        : ''
    cmdLoadMoreEl.classList.toggle('hidden', !hasMore)
  }

  async function loadCmd(append) {
    cmdErrEl.classList.add('hidden')
    if (!append) {
      cmdNextCursor = ''
      cmdRowCount = 0
      cmdRowsEl.innerHTML = `<tr><td colspan="5" class="px-4 py-6 text-center text-slate-500">${t('audit.cmdSearching')}</td></tr>`
      cmdLoadMoreEl.classList.add('hidden')
      cmdCountEl.textContent = ''
    } else {
      cmdLoadMoreEl.disabled = true
      cmdLoadMoreEl.textContent = t('audit.loading')
    }
    const params = cmdQueryParams()
    try {
      const res = await API.commandLogs({ ...params, after_id: append ? cmdNextCursor : '' })
      const items = (res && res.items) || []
      cmdNextCursor = (res && res.next_cursor) || ''
      if (!append && !items.length) {
        cmdRowsEl.innerHTML = `<tr><td colspan="5" class="px-4 py-6 text-center text-slate-500">${t('audit.cmdNoResults')}</td></tr>`
        cmdCountEl.textContent = ''
        cmdLoadMoreEl.classList.add('hidden')
        return
      }
      const html = items.map(renderCmdRow).join('')
      if (append) {
        cmdRowsEl.insertAdjacentHTML('beforeend', html)
      } else {
        cmdRowsEl.innerHTML = html
      }
      cmdRowCount += items.length
      updateCmdFooter()
    } catch (e) {
      cmdErrEl.textContent = e.message || t('audit.fetchFailed')
      cmdErrEl.classList.remove('hidden')
      if (!append) {
        cmdRowsEl.innerHTML = `<tr><td colspan="5" class="px-4 py-6 text-center text-slate-500">${t('audit.fetchFailedRow')}</td></tr>`
        cmdCountEl.textContent = ''
      }
      cmdLoadMoreEl.classList.add('hidden')
    } finally {
      cmdLoadMoreEl.disabled = false
      cmdLoadMoreEl.textContent = t('audit.loadMore')
    }
  }

  mainContent.querySelector('#cmd-refresh').addEventListener('click', () => loadCmd(false))
  mainContent.querySelector('#cmd-apply').addEventListener('click', () => loadCmd(false))
  cmdLoadMoreEl.addEventListener('click', () => loadCmd(true))

  const ftErrEl = mainContent.querySelector('#ft-error')
  const ftRowsEl = mainContent.querySelector('#ft-rows')
  const ftFromEl = mainContent.querySelector('#ft-filter-from')
  const ftToEl = mainContent.querySelector('#ft-filter-to')
  const ftStateEl = mainContent.querySelector('#ft-filter-state')
  const ftDirectionEl = mainContent.querySelector('#ft-filter-direction')
  const ftBackendEl = mainContent.querySelector('#ft-filter-backend')
  const ftUserEl = mainContent.querySelector('#ft-filter-user')
  const ftTargetEl = mainContent.querySelector('#ft-filter-target')
  const ftQueryEl = mainContent.querySelector('#ft-filter-query')
  const ftLimitEl = mainContent.querySelector('#ft-filter-limit')
  const ftCountEl = mainContent.querySelector('#ft-count')
  const ftLoadMoreEl = mainContent.querySelector('#ft-load-more')

  let ftNextCursor = ''
  let ftRowCount = 0

  const FT_STATE_KEYS = {
    completed: 'audit.ftStateCompleted',
    failed: 'audit.ftStateFailed',
    cancelled: 'audit.ftStateCancelled',
    running: 'audit.ftStateRunning',
    receiving: 'audit.ftStateReceiving',
  }
  const FT_DIRECTION_KEYS = {
    upload: 'audit.ftDirectionUpload',
    download: 'audit.ftDirectionDownload',
  }

  function ftStateBadge(state) {
    const label = FT_STATE_KEYS[state] ? t(FT_STATE_KEYS[state]) : state || '—'
    let cls = 'text-slate-700 bg-slate-100'
    if (state === 'completed') cls = 'text-emerald-800 bg-emerald-50'
    else if (state === 'failed') cls = 'text-red-700 bg-red-50'
    else if (state === 'cancelled') cls = 'text-amber-800 bg-amber-50'
    else if (state === 'running' || state === 'receiving') cls = 'text-sky-700 bg-sky-50'
    return `<span class="inline-block rounded px-1.5 py-0.5 text-[10px] font-medium ${cls}">${escapeHtml(label)}</span>`
  }

  function formatBytes(n) {
    const v = Number(n || 0)
    if (!Number.isFinite(v) || v <= 0) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    let i = 0
    let x = v
    while (x >= 1024 && i < units.length - 1) {
      x /= 1024
      i++
    }
    return (i === 0 ? x.toFixed(0) : x.toFixed(x < 10 ? 2 : 1)) + ' ' + units[i]
  }

  function ftProgress(it) {
    const total = Number(it.total || 0)
    let prog = Number(it.progress || 0)
    const state = String(it.state || '')
    // 古いデータ（throttle で進捗が DB に書かれないまま完了したもの）の救済:
    // 状態が completed かつ total が分かっていれば 100% として扱う。
    if (state === 'completed' && total > 0 && prog < total) {
      prog = total
    }
    if (total > 0) {
      const pct = Math.min(100, Math.round((prog / total) * 100))
      return t('audit.ftProgressFormat', {
        pct,
        prog: formatBytes(prog),
        total: formatBytes(total),
      })
    }
    return prog > 0 ? formatBytes(prog) : '—'
  }

  function ftQueryParams() {
    return {
      from: String(ftFromEl.value || '').trim(),
      to: String(ftToEl.value || '').trim(),
      state: String(ftStateEl.value || '').trim(),
      direction: String(ftDirectionEl.value || '').trim(),
      backend: String(ftBackendEl.value || '').trim(),
      userId: String(ftUserEl.value || '').trim(),
      targetId: String(ftTargetEl.value || '').trim(),
      query: String(ftQueryEl.value || '').trim(),
      limit: Number(ftLimitEl.value || 100) || 100,
    }
  }

  function renderFtRow(it) {
    const time = fmtTime(it.updated_at || it.created_at)
    const dirLabel = FT_DIRECTION_KEYS[it.direction] ? t(FT_DIRECTION_KEYS[it.direction]) : it.direction || ''
    const file = it.file_name || (it.remote_path ? it.remote_path.split('/').pop() : '—')
    const path = it.remote_path || ''
    const targetCell = it.target_name
      ? `<span title="${escapeHtml(it.target_id || '')}">${escapeHtml(it.target_name)}</span>`
      : escapeHtml(it.target_id || '')
    const err = it.error ? `<div class="text-[11px] text-red-600 mt-0.5">${escapeHtml(it.error)}</div>` : ''
    return `<tr class="border-b border-slate-200 hover:bg-slate-50">
      <td class="px-4 py-2 text-xs text-slate-600 whitespace-nowrap">${escapeHtml(time)}</td>
      <td class="px-4 py-2 text-xs text-slate-700 whitespace-nowrap font-mono">${escapeHtml(it.user_id || '—')}</td>
      <td class="px-4 py-2 text-xs text-slate-800 whitespace-nowrap">${escapeHtml(dirLabel)}</td>
      <td class="px-4 py-2 text-xs text-slate-700 whitespace-nowrap font-mono">${escapeHtml(it.backend || '')}</td>
      <td class="px-4 py-2 text-xs text-slate-700 whitespace-nowrap">${targetCell}</td>
      <td class="px-4 py-2 text-xs text-slate-800">
        <div class="font-mono text-[11px] break-all">${escapeHtml(file)}</div>
        <div class="text-[11px] text-slate-500 break-all">${escapeHtml(path)}</div>
        ${err}
      </td>
      <td class="px-4 py-2 text-xs whitespace-nowrap">${ftStateBadge(it.state)}</td>
      <td class="px-4 py-2 text-xs text-slate-700 whitespace-nowrap font-mono">${escapeHtml(ftProgress(it))}</td>
    </tr>`
  }

  function updateFtFooter() {
    const hasMore = Boolean(ftNextCursor)
    ftCountEl.textContent = hasMore
      ? t('audit.visibleCountMore', { n: ftRowCount })
      : ftRowCount > 0
        ? t('audit.visibleCount', { n: ftRowCount })
        : ''
    ftLoadMoreEl.classList.toggle('hidden', !hasMore)
  }

  async function loadFt(append) {
    ftErrEl.classList.add('hidden')
    if (!append) {
      ftNextCursor = ''
      ftRowCount = 0
      ftRowsEl.innerHTML = `<tr><td colspan="8" class="px-4 py-6 text-center text-slate-500">${t('audit.loading')}</td></tr>`
      ftLoadMoreEl.classList.add('hidden')
      ftCountEl.textContent = ''
    } else {
      ftLoadMoreEl.disabled = true
      ftLoadMoreEl.textContent = t('audit.loading')
    }
    const params = ftQueryParams()
    try {
      const res = await API.fileTransfers({ ...params, afterCursor: append ? ftNextCursor : '' })
      const items = (res && res.items) || []
      ftNextCursor = (res && res.next_cursor) || ''
      if (!append && !items.length) {
        ftRowsEl.innerHTML = `<tr><td colspan="8" class="px-4 py-6 text-center text-slate-500">${t('audit.ftNoResults')}</td></tr>`
        ftCountEl.textContent = ''
        ftLoadMoreEl.classList.add('hidden')
        return
      }
      const html = items.map(renderFtRow).join('')
      if (append) {
        ftRowsEl.insertAdjacentHTML('beforeend', html)
      } else {
        ftRowsEl.innerHTML = html
      }
      ftRowCount += items.length
      updateFtFooter()
    } catch (e) {
      ftErrEl.textContent = e.message || t('audit.fetchFailed')
      ftErrEl.classList.remove('hidden')
      if (!append) {
        ftRowsEl.innerHTML = `<tr><td colspan="8" class="px-4 py-6 text-center text-slate-500">${t('audit.fetchFailedRow')}</td></tr>`
        ftCountEl.textContent = ''
      }
      ftLoadMoreEl.classList.add('hidden')
    } finally {
      ftLoadMoreEl.disabled = false
      ftLoadMoreEl.textContent = t('audit.loadMore')
    }
  }

  mainContent.querySelector('#ft-refresh').addEventListener('click', () => loadFt(false))
  mainContent.querySelector('#ft-apply').addEventListener('click', () => loadFt(false))
  ftLoadMoreEl.addEventListener('click', () => loadFt(true))
}
