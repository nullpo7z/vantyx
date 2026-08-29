/**
 * @file System settings (admin only): server-wide configuration that is
 * not tied to the signed-in user -- currently audit-log forwarding.
 */

import API from './api.js'
import { formatDateTime } from './datetime.js'
import { t } from './i18n.js'
import { setActiveNav } from './nav.js'

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
