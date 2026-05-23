import API from './api.js'
import { setActiveNav } from './nav.js'

function esc(s) {
  return String(s ?? '').replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;')
}

export async function renderSettingsPage(container) {
  if (typeof setActiveNav === 'function') setActiveNav('settings')
  container.innerHTML = `
    <div class="w-full max-w-5xl flex-1 flex flex-col">
      <div class="flex items-center justify-between">
        <h2 class="text-lg font-semibold text-slate-800">設定</h2>
      </div>
      <div class="mt-4 rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
        <div class="flex items-center gap-2">
          <input id="audit-fwd-enabled" type="checkbox" class="h-4 w-4" />
          <label for="audit-fwd-enabled" class="text-sm text-slate-800">監査ログを syslog / SIEM に転送する</label>
        </div>
        <div class="mt-4 grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">プロトコル</label>
            <select id="audit-fwd-proto" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 bg-white">
              <option value="udp">udp（syslog over UDP）</option>
              <option value="tcp">tcp（syslog over TCP）</option>
              <option value="unix">unix（/dev/log stream）</option>
              <option value="unixgram">unixgram（/dev/log datagram）</option>
            </select>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">宛先（host:port または /dev/log）</label>
            <input id="audit-fwd-addr" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 bg-white font-mono" placeholder="例: siem.example.com:514 / /dev/log" />
            <p class="mt-1 text-[11px] text-slate-500">unix/unixgram の場合、空なら自動で <span class="font-mono">/dev/log</span> を使います。</p>
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">app-name</label>
            <input id="audit-fwd-app" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 bg-white font-mono" placeholder="vantyx" />
          </div>
          <div>
            <label class="block text-xs font-medium text-slate-600 mb-1">送信バッファ（ドロップ防止）</label>
            <input id="audit-fwd-buffer" type="number" min="0" max="200000" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 bg-white font-mono" placeholder="2000" />
          </div>
        </div>

        <div class="mt-5 flex items-center gap-3">
          <button id="audit-fwd-save" class="rounded bg-sky-700 px-4 py-2 text-sm font-medium text-white hover:bg-sky-800">保存</button>
          <span id="audit-fwd-status" class="text-sm text-slate-600"></span>
        </div>
      </div>
    </div>
  `

  const enabledEl = container.querySelector('#audit-fwd-enabled')
  const protoEl = container.querySelector('#audit-fwd-proto')
  const addrEl = container.querySelector('#audit-fwd-addr')
  const appEl = container.querySelector('#audit-fwd-app')
  const bufferEl = container.querySelector('#audit-fwd-buffer')
  const saveBtn = container.querySelector('#audit-fwd-save')
  const statusEl = container.querySelector('#audit-fwd-status')

  async function load() {
    statusEl.textContent = '読み込み中…'
    try {
      const res = await API.auditForwarderSettingsGet()
      const cfg = res?.config || {}
      enabledEl.checked = !!cfg.enabled
      protoEl.value = cfg.proto || 'udp'
      addrEl.value = cfg.addr || ''
      appEl.value = cfg.app || 'vantyx'
      bufferEl.value = cfg.buffer != null ? String(cfg.buffer) : '2000'
      statusEl.textContent = res?.source === 'env' ? '現在: 環境変数設定（未保存）' : '現在: アプリ設定（保存済み）'
    } catch (e) {
      statusEl.textContent = `読み込みに失敗: ${esc(e?.message || e)}`
    }
  }

  saveBtn.addEventListener('click', async () => {
    statusEl.textContent = '保存中…'
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
      statusEl.textContent = '保存しました（即時反映）'
    } catch (e) {
      statusEl.textContent = `保存に失敗: ${esc(e?.message || e)}`
    } finally {
      saveBtn.disabled = false
    }
  })

  await load()
}

