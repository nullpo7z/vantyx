import API from './api.js'
import { setActiveNav } from './nav.js'
import { getLocale, setLocale, SUPPORTED_LOCALES, t } from './i18n.js'

function esc(s) {
  return String(s ?? '').replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;')
}

export async function renderSettingsPage(container, { meData } = {}) {
  if (typeof setActiveNav === 'function') setActiveNav('settings')
  // Language is a per-user preference and is shown to everyone (E-8);
  // the audit-forwarder section is admin-only. The display timezone is
  // fixed server-wide by VANTYX_TIMEZONE and is not a UI setting.
  const isAdmin = !!(meData && meData.role === 'admin')
  const current = getLocale()
  const langOptions = SUPPORTED_LOCALES.map(
    (l) =>
      `<option value="${l.code}"${l.code === current ? ' selected' : ''}>${t(l.labelKey)}</option>`,
  ).join('')

  container.innerHTML = `
    <div class="w-full max-w-5xl flex-1 flex flex-col">
      <div class="flex items-center justify-between">
        <h2 class="text-lg font-semibold text-slate-800">${t('settings.title')}</h2>
      </div>

      <div class="mt-4 rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
        <h3 class="text-sm font-semibold text-slate-800 mb-2">${t('settings.sectionLanguage')}</h3>
        <p class="text-xs text-slate-500 mb-3">${t('settings.languageHint')}</p>
        <select id="settings-language" class="w-full sm:w-64 rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 bg-white">
          ${langOptions}
        </select>
      </div>

      ${
        isAdmin
          ? `<div class="mt-4 rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
        <h3 class="text-sm font-semibold text-slate-800 mb-3">${t('settings.sectionAudit')}</h3>
        <div class="flex items-center gap-2">
          <input id="audit-fwd-enabled" type="checkbox" class="h-4 w-4" />
          <label for="audit-fwd-enabled" class="text-sm text-slate-800">${t('settings.auditEnable')}</label>
        </div>
        <div class="mt-4 grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('settings.proto')}</label>
            <select id="audit-fwd-proto" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 bg-white">
              <option value="udp">${t('settings.protoUdp')}</option>
              <option value="tcp">${t('settings.protoTcp')}</option>
              <option value="unix">${t('settings.protoUnix')}</option>
              <option value="unixgram">${t('settings.protoUnixgram')}</option>
            </select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('settings.addr')}</label>
            <input id="audit-fwd-addr" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 bg-white font-mono" placeholder="${t('settings.addrPlaceholder')}" />
            <p class="mt-1 text-[11px] text-slate-500">${t('settings.addrHint')}</p>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('settings.appName')}</label>
            <input id="audit-fwd-app" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 bg-white font-mono" placeholder="${t('settings.appNamePlaceholder')}" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">${t('settings.buffer')}</label>
            <input id="audit-fwd-buffer" type="number" min="0" max="200000" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 bg-white font-mono" placeholder="${t('settings.bufferPlaceholder')}" />
          </div>
        </div>

        <div class="mt-5 flex items-center gap-3">
          <button id="audit-fwd-save" class="rounded bg-sky-700 px-4 py-2 text-sm font-medium text-white hover:bg-sky-800">${t('settings.save')}</button>
          <span id="audit-fwd-status" class="text-sm text-slate-600"></span>
        </div>
      </div>`
          : ''
      }
    </div>
  `

  const languageEl = container.querySelector('#settings-language')
  if (languageEl) {
    languageEl.addEventListener('change', () => {
      setLocale(languageEl.value)
    })
  }

  if (!isAdmin) return

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
