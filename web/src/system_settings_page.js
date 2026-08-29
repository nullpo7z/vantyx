/**
 * @file System settings (admin only): server-wide configuration that is
 * not tied to the signed-in user -- currently audit-log forwarding.
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

export async function renderSystemSettingsPage(container) {
  if (typeof setActiveNav === 'function') setActiveNav('system')
  container.innerHTML = `
    <div class="w-full flex-1 flex flex-col">
      <h2 class="text-lg font-semibold text-slate-800">${t('settings.title')}</h2>
      <p class="mt-1 text-xs text-slate-500">${t('settings.intro')}</p>

      <section class="mt-4 rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="px-5 pt-4 pb-3 border-b border-slate-100">
          <h3 class="text-sm font-semibold text-slate-800">${t('settings.sectionRetention')}</h3>
          <p class="mt-0.5 text-xs text-slate-500">${t('settings.retentionHint')}</p>
        </div>
        <div id="retention-body" class="px-5 py-4 text-sm text-slate-500">${t('common.loading')}</div>
      </section>

      <section class="mt-4 rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="px-5 pt-4 pb-3 border-b border-slate-100">
          <h3 class="text-sm font-semibold text-slate-800">${t('settings.sectionBackups')}</h3>
          <p class="mt-0.5 text-xs text-slate-500">${t('settings.backupsHint')}</p>
        </div>
        <div id="backups-body" class="px-5 py-4 text-sm text-slate-500">${t('common.loading')}</div>
      </section>

      <section class="mt-4 rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="px-5 pt-4 pb-3 border-b border-slate-100">
          <h3 class="text-sm font-semibold text-slate-800">${t('settings.sectionWebhooks')}</h3>
          <p class="mt-0.5 text-xs text-slate-500">${t('settings.webhooksHint')}</p>
        </div>
        <div id="webhooks-body" class="px-5 py-4 text-sm text-slate-500">${t('common.loading')}</div>
      </section>

      <section class="mt-4 rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="px-5 pt-4 pb-3 border-b border-slate-100">
          <h3 class="text-sm font-semibold text-slate-800">${t('settings.sectionAudit')}</h3>
          <p class="mt-0.5 text-xs text-slate-500">${t('settings.auditHint')}</p>
        </div>
        <div class="px-5 py-4">
          <div class="flex items-center gap-2">
            <input id="audit-fwd-enabled" type="checkbox" class="h-4 w-4" />
            <label for="audit-fwd-enabled" class="text-sm text-slate-800">${t('settings.auditEnable')}</label>
          </div>
          <div class="mt-4 grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1">${t('settings.proto')}</label>
              <select id="audit-fwd-proto" class="${INPUT}">
                <option value="udp">${t('settings.protoUdp')}</option>
                <option value="tcp">${t('settings.protoTcp')}</option>
                <option value="unix">${t('settings.protoUnix')}</option>
                <option value="unixgram">${t('settings.protoUnixgram')}</option>
              </select>
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1">${t('settings.addr')}</label>
              <input id="audit-fwd-addr" class="${INPUT} font-mono" placeholder="${t('settings.addrPlaceholder')}" />
              <p class="mt-1 text-[11px] text-slate-500">${t('settings.addrHint')}</p>
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1">${t('settings.appName')}</label>
              <input id="audit-fwd-app" class="${INPUT} font-mono" placeholder="${t('settings.appNamePlaceholder')}" />
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1">${t('settings.buffer')}</label>
              <input id="audit-fwd-buffer" type="number" min="0" max="200000" class="${INPUT} font-mono" placeholder="${t('settings.bufferPlaceholder')}" />
            </div>
          </div>
          <div class="mt-5 flex items-center gap-3">
            <button id="audit-fwd-save" class="rounded bg-sky-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors disabled:opacity-50">${t('settings.save')}</button>
            <span id="audit-fwd-status" class="text-sm text-slate-600"></span>
          </div>
        </div>
      </section>
    </div>
  `

  renderRetention(container.querySelector('#retention-body'))
  renderWebhooks(container.querySelector('#webhooks-body'))
  renderBackups(container.querySelector('#backups-body'))

  const enabledEl = container.querySelector('#audit-fwd-enabled')
  const protoEl = container.querySelector('#audit-fwd-proto')
  const addrEl = container.querySelector('#audit-fwd-addr')
  const appEl = container.querySelector('#audit-fwd-app')
  const bufferEl = container.querySelector('#audit-fwd-buffer')
  const saveBtn = container.querySelector('#audit-fwd-save')
  const statusEl = container.querySelector('#audit-fwd-status')

  async function load() {
    statusEl.textContent = t('settings.loading')
    try {
      const res = await API.auditForwarderSettingsGet()
      const cfg = res?.config || {}
      enabledEl.checked = !!cfg.enabled
      protoEl.value = cfg.proto || 'udp'
      addrEl.value = cfg.addr || ''
      appEl.value = cfg.app || 'vantyx'
      bufferEl.value = cfg.buffer != null ? String(cfg.buffer) : '2000'
      statusEl.textContent = res?.source === 'env' ? t('settings.sourceEnv') : t('settings.sourceApp')
    } catch (e) {
      statusEl.textContent = t('settings.loadFailed', { error: esc(e?.message || e) })
    }
  }

  saveBtn.addEventListener('click', async () => {
    statusEl.textContent = t('settings.saving')
    saveBtn.disabled = true
    try {
      const cfg = {
        enabled: !!enabledEl.checked,
        proto: String(protoEl.value || 'udp'),
        addr: String(addrEl.value || '').trim(),
        app: String(appEl.value || '').trim(),
        buffer: Number(bufferEl.value || '0'),
      }
      await API.auditForwarderSettingsPut({ config: cfg })
      statusEl.textContent = t('settings.saved')
    } catch (e) {
      statusEl.textContent = t('settings.saveFailed', { error: esc(e?.message || e) })
    } finally {
      saveBtn.disabled = false
    }
  })

  await load()
}

function humanDuration(goDuration) {
  if (!goDuration) return t('settings.retentionDisabled')
  const m = /^(\d+)h/.exec(goDuration)
  if (m) {
    const h = Number(m[1])
    if (h % 24 === 0) return t('settings.retentionDays', { n: h / 24 })
    return t('settings.retentionHours', { n: h })
  }
  return goDuration
}

async function renderRetention(body) {
  if (!body) return
  let p
  try {
    p = await API.retentionGet()
  } catch (e) {
    body.innerHTML = `<p class="text-sm text-red-600">${esc(e?.message || e)}</p>`
    return
  }
  const row = (label, val) => `<dt class="text-slate-500">${label}</dt><dd class="text-slate-800">${esc(humanDuration(val))}</dd>`
  const last = p.last_run
  body.innerHTML = `
    <dl class="grid grid-cols-1 sm:grid-cols-[14rem_1fr] gap-x-4 gap-y-2 text-sm">
      ${row(t('settings.retentionRecordings'), p.recordings)}
      ${row(t('settings.retentionAudit'), p.audit)}
      ${row(t('settings.retentionCommands'), p.command_logs)}
      ${row(t('settings.retentionMemberships'), p.memberships_expired)}
    </dl>
    <p class="mt-3 text-xs text-slate-500">${t('settings.retentionEnvHint')}</p>
    <div class="mt-3 flex flex-wrap items-center gap-3">
      <button type="button" id="retention-run" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors disabled:opacity-50"${p.enabled ? '' : ' disabled'}>${t('settings.retentionRunNow')}</button>
      <span id="retention-status" class="text-xs text-slate-600">${
        last
          ? t('settings.retentionLastRun', {
              at: esc(formatDateTime(last.ran_at)),
              rec: last.recordings_deleted,
              audit: last.audit_rows_deleted,
              cmd: last.command_rows_deleted,
              mem: last.memberships_purged,
            }) + (last.errors && last.errors.length ? ` · ${esc(last.errors.join('; '))}` : '')
          : t('settings.retentionNeverRun')
      }</span>
    </div>`
  body.querySelector('#retention-run')?.addEventListener('click', async (e) => {
    const btn = e.currentTarget
    btn.disabled = true
    try {
      await API.retentionRun()
      await renderRetention(body)
    } catch (err) {
      body.querySelector('#retention-status').textContent = err?.message || String(err)
      btn.disabled = false
    }
  })
}

const WEBHOOK_PRESETS = [
  'login_failed',
  'login_rate_limited',
  'oidc_login_failed',
  'totp_reset_by_admin',
  'user_role_update',
  'access_request_*',
  'session_terminated_by_admin',
  'retention_purge',
  '*',
]

async function renderWebhooks(body) {
  if (!body) return
  let endpoints
  try {
    endpoints = ((await API.webhooksGet()) || {}).endpoints || []
  } catch (e) {
    body.innerHTML = `<p class="text-sm text-red-600">${esc(e?.message || e)}</p>`
    return
  }
  const rows = endpoints
    .map((ep) => {
      const st = ep.stats || {}
      const last = st.last_at
        ? `${esc(formatDateTime(st.last_at))} · ${st.last_error ? `<span class="text-red-600">${esc(st.last_error)}</span>` : `HTTP ${st.last_status}`}`
        : t('settings.webhookNeverSent')
      return `<tr class="border-b border-slate-100" data-id="${esc(ep.id)}">
        <td class="px-3 py-2 text-sm text-slate-900">${esc(ep.name)}${ep.enabled ? '' : ` <span class="text-xs text-slate-400">(${t('common.disabled')})</span>`}</td>
        <td class="px-3 py-2 text-xs font-mono text-slate-700 break-all">${esc(ep.url)}<div class="text-slate-400">${esc(ep.format)}${ep.has_secret ? ' · HMAC' : ''}</div></td>
        <td class="px-3 py-2 text-xs text-slate-700">${ep.events.map((e) => `<code class="rounded bg-slate-100 px-1">${esc(e)}</code>`).join(' ')}</td>
        <td class="px-3 py-2 text-xs text-slate-600 whitespace-nowrap">${t('settings.webhookSentFailed', { sent: st.sent || 0, failed: st.failed || 0 })}<div>${last}</div></td>
        <td class="px-3 py-2 whitespace-nowrap">
          <button type="button" class="wh-test rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-700 hover:bg-slate-50">${t('settings.webhookTest')}</button>
          <button type="button" class="wh-edit rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-700 hover:bg-slate-50">${t('common.edit')}</button>
          <button type="button" class="wh-delete rounded border border-red-200 bg-white px-2 py-1 text-xs font-medium text-red-700 hover:bg-red-50">${t('common.delete')}</button>
        </td>
      </tr>`
    })
    .join('')
  body.innerHTML = `
    ${
      rows
        ? `<div class="overflow-x-auto"><table class="min-w-full text-left text-sm"><thead class="bg-slate-50 border-b border-slate-200"><tr>
            <th class="px-3 py-2 text-xs font-semibold text-slate-700">${t('common.name')}</th>
            <th class="px-3 py-2 text-xs font-semibold text-slate-700">URL</th>
            <th class="px-3 py-2 text-xs font-semibold text-slate-700">${t('settings.webhookEvents')}</th>
            <th class="px-3 py-2 text-xs font-semibold text-slate-700">${t('settings.webhookDelivery')}</th>
            <th class="px-3 py-2"></th></tr></thead><tbody>${rows}</tbody></table></div>`
        : `<p class="text-sm text-slate-500">${t('settings.webhookNone')}</p>`
    }
    <div class="mt-3 flex items-center gap-3">
      <button type="button" id="wh-add" class="rounded bg-sky-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-sky-700 shadow-sm">${t('settings.webhookAdd')}</button>
      <span id="wh-status" class="text-xs text-slate-600"></span>
    </div>
    <div id="wh-form-wrap" class="hidden mt-4 rounded border border-slate-200 bg-slate-50 p-4"></div>`

  const statusEl = body.querySelector('#wh-status')
  const formWrap = body.querySelector('#wh-form-wrap')
  const save = async (list) => {
    statusEl.textContent = t('settings.saving')
    await API.webhooksPut(list.map((ep) => ({ ...ep, stats: undefined, has_secret: undefined })))
    await renderWebhooks(body)
  }
  const openForm = (ep) => {
    const cur = ep || { id: '', name: '', url: '', secret: '', format: 'generic', events: ['login_failed', 'access_request_*'], enabled: true }
    formWrap.classList.remove('hidden')
    formWrap.innerHTML = `
      <form id="wh-form" class="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div><label class="block text-xs font-medium text-slate-600 mb-1">${t('common.name')}</label><input name="name" required maxlength="100" class="${INPUT}" value="${esc(cur.name)}" /></div>
        <div><label class="block text-xs font-medium text-slate-600 mb-1">URL</label><input name="url" required class="${INPUT} font-mono" placeholder="https://hooks.example.com/…" value="${esc(cur.url)}" /></div>
        <div><label class="block text-xs font-medium text-slate-600 mb-1">${t('settings.webhookSecret')}</label><input name="secret" class="${INPUT} font-mono" placeholder="${cur.id && cur.has_secret ? t('settings.webhookSecretKeep') : t('settings.webhookSecretOptional')}" /></div>
        <div><label class="block text-xs font-medium text-slate-600 mb-1">${t('settings.webhookFormat')}</label>
          <select name="format" class="${INPUT}"><option value="generic"${cur.format !== 'slack' ? ' selected' : ''}>${t('settings.webhookFormatGeneric')}</option><option value="slack"${cur.format === 'slack' ? ' selected' : ''}>Slack / Mattermost (text)</option></select></div>
        <div class="md:col-span-2"><label class="block text-xs font-medium text-slate-600 mb-1">${t('settings.webhookEvents')}</label>
          <input name="events" required class="${INPUT} font-mono" value="${esc(cur.events.join(', '))}" />
          <div class="mt-1 flex flex-wrap gap-1">${WEBHOOK_PRESETS.map((p) => `<button type="button" class="wh-preset rounded border border-slate-300 bg-white px-1.5 py-0.5 text-[11px] font-mono text-slate-700 hover:bg-slate-100" data-p="${esc(p)}">${esc(p)}</button>`).join('')}</div>
          <p class="mt-1 text-[11px] text-slate-500">${t('settings.webhookEventsHint')}</p></div>
        <div class="md:col-span-2 flex items-center gap-2"><input type="checkbox" id="wh-enabled" name="enabled" class="h-4 w-4"${cur.enabled ? ' checked' : ''} /><label for="wh-enabled" class="text-sm text-slate-800">${t('common.enabled')}</label></div>
        <p id="wh-form-error" class="md:col-span-2 text-sm text-red-600 hidden"></p>
        <div class="md:col-span-2 flex gap-2"><button type="submit" class="rounded bg-sky-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-sky-700">${t('common.save')}</button><button type="button" id="wh-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50">${t('common.cancel')}</button></div>
      </form>`
    const form = formWrap.querySelector('#wh-form')
    formWrap.querySelectorAll('.wh-preset').forEach((b) => {
      b.addEventListener('click', () => {
        const cur = form.elements.events.value.split(',').map((x) => x.trim()).filter(Boolean)
        if (!cur.includes(b.dataset.p)) cur.push(b.dataset.p)
        form.elements.events.value = cur.join(', ')
      })
    })
    formWrap.querySelector('#wh-cancel').addEventListener('click', () => { formWrap.classList.add('hidden'); formWrap.innerHTML = '' })
    form.addEventListener('submit', async (e) => {
      e.preventDefault()
      const errEl = form.querySelector('#wh-form-error')
      errEl.classList.add('hidden')
      const next = {
        id: cur.id,
        name: form.elements.name.value.trim(),
        url: form.elements.url.value.trim(),
        secret: form.elements.secret.value,
        format: form.elements.format.value,
        events: form.elements.events.value.split(',').map((x) => x.trim()).filter(Boolean),
        enabled: form.elements.enabled.checked,
      }
      const list = endpoints.filter((x) => x.id !== cur.id).map((x) => ({ id: x.id, name: x.name, url: x.url, format: x.format, events: x.events, enabled: x.enabled }))
      list.push(next)
      try {
        await save(list)
      } catch (err) {
        errEl.textContent = err?.message || String(err)
        errEl.classList.remove('hidden')
      }
    })
  }
  body.querySelector('#wh-add').addEventListener('click', () => openForm(null))
  body.querySelectorAll('tr[data-id]').forEach((tr) => {
    const ep = endpoints.find((x) => x.id === tr.dataset.id)
    tr.querySelector('.wh-edit').addEventListener('click', () => openForm(ep))
    tr.querySelector('.wh-delete').addEventListener('click', async () => {
      if (!(await uiConfirm(t('settings.webhookConfirmDelete', { name: ep.name }), { danger: true }))) return
      try {
        await save(endpoints.filter((x) => x.id !== ep.id).map((x) => ({ id: x.id, name: x.name, url: x.url, format: x.format, events: x.events, enabled: x.enabled })))
      } catch (err) {
        await uiAlert(err?.message || String(err))
      }
    })
    tr.querySelector('.wh-test').addEventListener('click', async (e) => {
      e.currentTarget.disabled = true
      statusEl.textContent = t('settings.webhookTesting')
      try {
        const res = await API.webhookTest(ep.id)
        statusEl.textContent = res.ok ? t('settings.webhookTestOk', { status: res.status }) : t('settings.webhookTestFailed', { error: res.error || res.status })
      } catch (err) {
        statusEl.textContent = err?.message || String(err)
      }
      await renderWebhooks(body)
    })
  })
}

function humanBytes(n) {
  const v = Number(n) || 0
  if (v < 1024) return `${v} B`
  if (v < 1024 * 1024) return `${(v / 1024).toFixed(1)} KB`
  if (v < 1024 * 1024 * 1024) return `${(v / 1024 / 1024).toFixed(1)} MB`
  return `${(v / 1024 / 1024 / 1024).toFixed(2)} GB`
}

async function renderBackups(body) {
  if (!body) return
  let data
  try {
    data = await API.backupsGet()
  } catch (e) {
    body.innerHTML = `<p class="text-sm text-red-600">${esc(e?.message || e)}</p>`
    return
  }
  const p = data.policy || {}
  const list = data.backups || []
  body.innerHTML = `
    ${
      p.restore_pending
        ? `<div class="mb-3 rounded border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900 flex flex-wrap items-center justify-between gap-2"><span>${t('settings.backupRestorePending')}</span><button type="button" id="bk-cancel-restore" class="rounded border border-amber-300 bg-white px-2 py-1 text-xs font-medium text-amber-900 hover:bg-amber-100">${t('common.cancel')}</button></div>`
        : ''
    }
    <dl class="grid grid-cols-1 sm:grid-cols-[14rem_1fr] gap-x-4 gap-y-1 text-sm">
      <dt class="text-slate-500">${t('settings.backupDir')}</dt><dd class="font-mono text-xs text-slate-800">${esc(p.dir || '')}</dd>
      <dt class="text-slate-500">${t('settings.backupSchedule')}</dt><dd class="text-slate-800">${p.interval ? esc(humanDuration(p.interval)) : t('settings.backupManualOnly')} · ${t('settings.backupKeep', { n: p.keep })}</dd>
    </dl>
    ${p.last_error ? `<p class="mt-2 text-sm text-red-600">${esc(p.last_error)}</p>` : ''}
    <div class="mt-3 flex flex-wrap items-center gap-3">
      <button type="button" id="bk-create" class="rounded bg-sky-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-sky-700 shadow-sm disabled:opacity-50">${t('settings.backupNow')}</button>
      <label class="text-sm text-slate-700 flex items-center gap-2">${t('settings.backupRestoreUpload')}
        <input type="file" id="bk-upload" accept=".db,application/vnd.sqlite3,application/octet-stream" class="text-xs" />
      </label>
      <span id="bk-status" class="text-xs text-slate-600"></span>
    </div>
    ${
      list.length
        ? `<div class="mt-4 overflow-x-auto"><table class="min-w-full text-left text-sm"><thead class="bg-slate-50 border-b border-slate-200"><tr>
            <th class="px-3 py-2 text-xs font-semibold text-slate-700">${t('common.name')}</th>
            <th class="px-3 py-2 text-xs font-semibold text-slate-700">${t('common.size')}</th>
            <th class="px-3 py-2 text-xs font-semibold text-slate-700">${t('common.createdAt')}</th>
            <th class="px-3 py-2"></th></tr></thead><tbody>
            ${list
              .map(
                (b) => `<tr class="border-b border-slate-100" data-name="${esc(b.name)}">
                  <td class="px-3 py-2 font-mono text-xs text-slate-800">${esc(b.name)}</td>
                  <td class="px-3 py-2 text-xs text-slate-600 whitespace-nowrap">${esc(humanBytes(b.size_bytes))}</td>
                  <td class="px-3 py-2 text-xs text-slate-600 whitespace-nowrap">${esc(formatDateTime(b.created_at))}</td>
                  <td class="px-3 py-2 whitespace-nowrap">
                    <a href="/api/settings/backups/${encodeURIComponent(b.name)}" class="rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-700 hover:bg-slate-50">${t('common.download')}</a>
                    <button type="button" class="bk-restore rounded border border-amber-300 bg-white px-2 py-1 text-xs font-medium text-amber-800 hover:bg-amber-50">${t('settings.backupRestore')}</button>
                    <button type="button" class="bk-delete rounded border border-red-200 bg-white px-2 py-1 text-xs font-medium text-red-700 hover:bg-red-50">${t('common.delete')}</button>
                  </td></tr>`,
              )
              .join('')}
          </tbody></table></div>`
        : `<p class="mt-3 text-sm text-slate-500">${t('settings.backupNone')}</p>`
    }
    <p class="mt-3 text-xs text-slate-500">${t('settings.backupScopeHint')}</p>`
  const statusEl = body.querySelector('#bk-status')
  body.querySelector('#bk-create').addEventListener('click', async (e) => {
    e.currentTarget.disabled = true
    statusEl.textContent = t('settings.backupRunning')
    try {
      const info = await API.backupCreate()
      statusEl.textContent = t('settings.backupDone', { name: info.name })
      await renderBackups(body)
    } catch (err) {
      statusEl.textContent = err?.message || String(err)
      e.currentTarget.disabled = false
    }
  })
  body.querySelector('#bk-cancel-restore')?.addEventListener('click', async () => {
    try {
      await API.backupRestoreCancel()
      await renderBackups(body)
    } catch (err) {
      await uiAlert(err?.message || String(err))
    }
  })
  body.querySelector('#bk-upload').addEventListener('change', async (e) => {
    const file = e.target.files && e.target.files[0]
    if (!file) return
    if (!(await uiConfirm(t('settings.backupConfirmRestore', { name: file.name }), { danger: true }))) {
      e.target.value = ''
      return
    }
    statusEl.textContent = t('settings.backupUploading')
    try {
      await API.backupRestore({ file })
      await renderBackups(body)
    } catch (err) {
      statusEl.textContent = err?.message || String(err)
    }
    e.target.value = ''
  })
  body.querySelectorAll('tr[data-name]').forEach((tr) => {
    const name = tr.dataset.name
    tr.querySelector('.bk-delete').addEventListener('click', async () => {
      if (!(await uiConfirm(t('settings.backupConfirmDelete', { name }), { danger: true }))) return
      try {
        await API.backupDelete(name)
        await renderBackups(body)
      } catch (err) {
        await uiAlert(err?.message || String(err))
      }
    })
    tr.querySelector('.bk-restore').addEventListener('click', async () => {
      if (!(await uiConfirm(t('settings.backupConfirmRestore', { name }), { danger: true }))) return
      try {
        await API.backupRestore({ name })
        await renderBackups(body)
      } catch (err) {
        await uiAlert(err?.message || String(err))
      }
    })
  })
}
