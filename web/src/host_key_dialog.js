import API from './api.js'
import { t } from './i18n.js'

/**
 * SSH host-key TOFU / mismatch dialogs shared by terminal and TFTP-console pages.
 *
 * @param {object} options
 * @param {string} options.targetId - default target id when the frame omits target_id
 * @param {() => void} options.onReconnect - called after the user adopts a fingerprint
 * @param {(mode: 'unknown' | 'mismatch') => void} [options.onCancelled]
 * @param {() => void} [options.onBeforeDialog] - hide transient UI before showing the modal
 */
export function createHostKeyDialogController({ targetId, onReconnect, onCancelled, onBeforeDialog }) {
  let hostKeyDialogEl = null
  let sawHostKeyError = false

  function closeHostKeyDialog() {
    if (!hostKeyDialogEl) return
    try { hostKeyDialogEl.remove() } catch { /* ignore */ }
    hostKeyDialogEl = null
  }

  function silenceWebSocket(ws) {
    try { ws.onmessage = null } catch { /* ignore */ }
    try { ws.onclose = null } catch { /* ignore */ }
    try { ws.onerror = null } catch { /* ignore */ }
    try { ws.onopen = null } catch { /* ignore */ }
  }

  function renderHostKeyDialog({ title, bodyText, acceptLabel, requireCheckbox, onAccept, mode }) {
    closeHostKeyDialog()
    const wrap = document.createElement('div')
    wrap.className = 'fixed inset-0 z-[200] flex items-center justify-center bg-black/40 backdrop-blur-sm p-4'
    const banner = requireCheckbox
      ? '<div class="px-5 py-3 border-b border-rose-200 bg-rose-50 text-sm font-semibold text-rose-700 flex items-center gap-2"><span aria-hidden="true">⚠</span><span></span></div>'
      : ''
    wrap.innerHTML = `
      <div class="bg-white rounded-lg shadow-xl w-full max-w-lg mx-4 overflow-hidden border border-slate-200/50">
        <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
          <h3 class="font-semibold text-slate-800" data-host-key-title="1"></h3>
          <button type="button" data-host-key-close="1" class="text-slate-500 hover:text-slate-700 text-2xl leading-none">&times;</button>
        </div>
        ${banner}
        <div class="px-6 py-5 space-y-4">
          <pre data-host-key-body="1" class="whitespace-pre-wrap text-sm text-slate-800 font-sans"></pre>
          ${requireCheckbox ? `<label class="flex items-start gap-2 text-sm text-slate-800"><input type="checkbox" data-host-key-confirm="1" class="mt-0.5 rounded border-slate-300 text-rose-600 focus:ring-rose-500" /><span data-host-key-confirm-label="1"></span></label>` : ''}
          <p data-host-key-error="1" class="text-sm text-red-600 hidden"></p>
        </div>
        <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
          <button type="button" data-host-key-cancel="1" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm"></button>
          <button type="button" data-host-key-accept="1" class="rounded ${requireCheckbox ? 'bg-rose-600 hover:bg-rose-700' : 'bg-sky-600 hover:bg-sky-700'} px-3 py-1.5 text-xs font-medium text-white shadow-sm" disabled></button>
        </div>
      </div>
    `
    wrap.querySelector('[data-host-key-title="1"]').textContent = title
    wrap.querySelector('[data-host-key-body="1"]').textContent = bodyText
    wrap.querySelector('[data-host-key-cancel="1"]').textContent = t('hostKey.confirmCancel')
    const acceptBtn = wrap.querySelector('[data-host-key-accept="1"]')
    acceptBtn.textContent = acceptLabel
    if (requireCheckbox) {
      const checkboxLabel = wrap.querySelector('[data-host-key-confirm-label="1"]')
      if (checkboxLabel) checkboxLabel.textContent = t('hostKey.mismatchConfirmCheckbox')
      const cb = wrap.querySelector('[data-host-key-confirm="1"]')
      cb.addEventListener('change', () => {
        acceptBtn.disabled = !cb.checked
      })
      const bannerSpan = wrap.querySelector('.bg-rose-50 span:last-child')
      if (bannerSpan) bannerSpan.textContent = title
    } else {
      acceptBtn.disabled = false
    }

    document.body.appendChild(wrap)
    hostKeyDialogEl = wrap

    const close = () => {
      closeHostKeyDialog()
      onCancelled?.(mode)
    }
    wrap.addEventListener('click', (e) => { if (e.target === wrap) close() })
    wrap.querySelector('[data-host-key-close="1"]').addEventListener('click', close)
    wrap.querySelector('[data-host-key-cancel="1"]').addEventListener('click', close)
    acceptBtn.addEventListener('click', async () => {
      acceptBtn.disabled = true
      const errEl = wrap.querySelector('[data-host-key-error="1"]')
      errEl.classList.add('hidden')
      try {
        await onAccept()
        closeHostKeyDialog()
        onReconnect()
      } catch (err) {
        errEl.textContent = t('hostKey.updateFailed', { error: err.message || String(err) })
        errEl.classList.remove('hidden')
        acceptBtn.disabled = false
      }
    })
  }

  function showHostKeyAdoptDialog(o) {
    const fp = o.fingerprint || ''
    const host = o.host || ''
    const port = o.port || 22
    const tid = o.target_id || targetId
    renderHostKeyDialog({
      title: t('hostKey.adoptTitle'),
      bodyText: t('hostKey.adoptBody', { host, port, fp }),
      acceptLabel: t('hostKey.adoptAccept'),
      requireCheckbox: false,
      onAccept: () => API.updateTargetHostKey(tid, fp),
      mode: 'unknown',
    })
  }

  function showHostKeyMismatchDialog(o) {
    const expected = o.expected || ''
    const offered = o.offered || ''
    const host = o.host || ''
    const port = o.port || 22
    const tid = o.target_id || targetId
    renderHostKeyDialog({
      title: t('hostKey.mismatchTitle'),
      bodyText: t('hostKey.mismatchBody', { host, port, expected, offered }),
      acceptLabel: t('hostKey.mismatchAccept'),
      requireCheckbox: true,
      onAccept: () => API.updateTargetHostKey(tid, offered),
      mode: 'mismatch',
    })
  }

  function handleHostKeyMeta(ws, o) {
    if (o && o.type === 'host_key_unknown') {
      sawHostKeyError = true
      silenceWebSocket(ws)
      try { ws.close() } catch { /* ignore */ }
      onBeforeDialog?.()
      showHostKeyAdoptDialog(o)
      return true
    }
    if (o && o.type === 'host_key_mismatch') {
      sawHostKeyError = true
      silenceWebSocket(ws)
      try { ws.close() } catch { /* ignore */ }
      onBeforeDialog?.()
      showHostKeyMismatchDialog(o)
      return true
    }
    return false
  }

  return {
    handleHostKeyMeta,
    closeHostKeyDialog,
    getSawHostKeyError: () => sawHostKeyError,
    resetHostKeyError: () => { sawHostKeyError = false },
  }
}
