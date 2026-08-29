import { t } from './i18n.js'

function esc(s) {
  const div = document.createElement('div')
  div.textContent = s == null ? '' : String(s)
  return div.innerHTML
}

function removeEl(el) {
  try { el?.remove() } catch { /* ignore */ }
}

function buildDialog({ title, body, kind, buttons }) {
  const overlay = document.createElement('div')
  overlay.className = 'fixed inset-0 z-[220] flex items-center justify-center bg-black/40 backdrop-blur-sm p-4'

  const icon =
    kind === 'danger'
      ? '<div class="h-10 w-10 rounded-full bg-rose-100 text-rose-700 flex items-center justify-center text-lg font-semibold">!</div>'
      : kind === 'info'
        ? '<div class="h-10 w-10 rounded-full bg-sky-100 text-sky-700 flex items-center justify-center text-lg font-semibold">i</div>'
        : ''

  overlay.innerHTML = `
    <div class="bg-white rounded-lg shadow-xl w-full max-w-lg mx-4 overflow-hidden border border-slate-200/50">
      <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
        <h3 class="font-semibold text-slate-800">${esc(title || '')}</h3>
        <button type="button" data-ui-dialog-close="1" class="text-slate-500 hover:text-slate-700 text-2xl leading-none">&times;</button>
      </div>
      <div class="px-6 py-5">
        <div class="flex items-start gap-4">
          ${icon}
          <div class="min-w-0 flex-1">
            <div class="text-sm text-slate-800 whitespace-pre-wrap break-words">${esc(body || '')}</div>
          </div>
        </div>
      </div>
      <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200" data-ui-dialog-actions="1"></div>
    </div>
  `

  const actions = overlay.querySelector('[data-ui-dialog-actions="1"]')
  for (const b of buttons || []) {
    const btn = document.createElement('button')
    btn.type = 'button'
    btn.textContent = b.label
    btn.className =
      b.variant === 'primary'
        ? 'rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm'
        : b.variant === 'danger'
          ? 'rounded bg-rose-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-rose-700 shadow-sm'
          : 'rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm'
    btn.dataset.uiDialogResult = b.result
    actions.appendChild(btn)
  }

  return overlay
}

function showDialog({ title, body, kind, buttons, closeResult }) {
  return new Promise((resolve) => {
    const overlay = buildDialog({ title, body, kind, buttons })
    const onKey = (e) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        close(closeResult)
      }
    }
    const close = (v) => {
      document.removeEventListener('keydown', onKey, true)
      removeEl(overlay)
      resolve(v)
    }
    // Escape = the same outcome as clicking outside / the × button (E-2).
    document.addEventListener('keydown', onKey, true)
    overlay.addEventListener('click', (e) => {
      if (e.target === overlay) close(closeResult)
    })
    overlay.querySelector('[data-ui-dialog-close="1"]').addEventListener('click', () => close(closeResult))
    overlay.querySelectorAll('button[data-ui-dialog-result]').forEach((btn) => {
      btn.addEventListener('click', () => close(btn.dataset.uiDialogResult))
    })
    document.body.appendChild(overlay)
  })
}

// Escape closes the topmost page modal (E-2). Page modals are either a
// pre-rendered `#…-modal` container toggled with the `hidden` class, or a
// dynamically appended full-screen wrap (invite / participants dialogs).
// Each exposes its own close/cancel control; we just click it so the
// modal's own teardown logic runs. The uiConfirm/uiAlert overlays handle
// Escape themselves (capture phase, stopPropagation) so they win when
// stacked on top of a page modal.
const MODAL_CLOSE_SELECTOR = [
  '[data-close="1"]',
  '[data-cancel="1"]',
  'button[id$="-cancel"]',
  'button[id$="-close"]',
].join(', ')

function topmostOpenModal() {
  const candidates = []
  for (const el of document.querySelectorAll('[id$="-modal"]')) {
    if (!el.classList.contains('hidden') && el.childElementCount > 0) candidates.push(el)
  }
  for (const el of document.querySelectorAll('body > div.fixed.inset-0')) {
    if (!el.classList.contains('hidden') && el.querySelector(MODAL_CLOSE_SELECTOR)) candidates.push(el)
  }
  return candidates.length ? candidates[candidates.length - 1] : null
}

if (typeof document !== 'undefined' && !document.__vantyxModalEscapeInstalled) {
  document.__vantyxModalEscapeInstalled = true
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape' || e.defaultPrevented) return
    const modal = topmostOpenModal()
    if (!modal) return
    const closer = modal.querySelector(MODAL_CLOSE_SELECTOR)
    if (closer) {
      e.preventDefault()
      closer.click()
    }
  })
}

export async function uiAlert(message, { title } = {}) {
  await showDialog({
    title: title || t('common.confirm') || 'Notice',
    body: message || '',
    kind: 'info',
    buttons: [{ label: t('common.ok') || 'OK', variant: 'primary', result: 'ok' }],
    closeResult: 'ok',
  })
}

export async function uiConfirm(message, { title, danger = false, okLabel, cancelLabel } = {}) {
  const r = await showDialog({
    title: title || (t('common.confirm') || 'Confirm'),
    body: message || '',
    kind: danger ? 'danger' : 'info',
    buttons: [
      { label: cancelLabel || t('common.cancel') || 'Cancel', variant: 'secondary', result: 'cancel' },
      { label: okLabel || (t('common.ok') || 'OK'), variant: danger ? 'danger' : 'primary', result: 'ok' },
    ],
    closeResult: 'cancel',
  })
  return r === 'ok'
}

/**
 * Non-blocking progress overlay for long operations (e.g. downloads).
 *
 * @param {{ title?: string, message?: string }} [opts]
 * @returns {{ setMessage: (msg: string) => void, setProgress: (percent: number | null) => void, close: () => void }}
 */
export function showProgressOverlay({ title, message } = {}) {
  const overlay = document.createElement('div')
  overlay.className = 'fixed inset-0 z-[220] flex items-center justify-center bg-black/40 backdrop-blur-sm p-4'
  overlay.innerHTML = `
    <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
      <div class="px-5 py-4 border-b border-slate-200 bg-slate-50">
        <h3 class="font-semibold text-slate-800" data-ui-progress-title>${esc(title || '')}</h3>
      </div>
      <div class="px-6 py-5 space-y-4">
        <p class="text-sm text-slate-700 whitespace-pre-wrap break-words" data-ui-progress-message>${esc(message || '')}</p>
        <div class="h-2 w-full rounded-full bg-slate-200 overflow-hidden" role="progressbar" aria-valuemin="0" aria-valuemax="100">
          <div class="h-full bg-sky-600 transition-[width] duration-150 ease-out w-0" data-ui-progress-bar></div>
        </div>
        <p class="text-xs text-slate-500 tabular-nums" data-ui-progress-detail></p>
      </div>
    </div>
    <style>
      @keyframes uiProgressIndeterminate {
        0% { transform: translateX(-60%); }
        100% { transform: translateX(160%); }
      }
    </style>
  `
  const bar = overlay.querySelector('[data-ui-progress-bar]')
  const msgEl = overlay.querySelector('[data-ui-progress-message]')
  const detailEl = overlay.querySelector('[data-ui-progress-detail]')
  const titleEl = overlay.querySelector('[data-ui-progress-title]')
  document.body.appendChild(overlay)

  const setProgress = (percent) => {
    if (!bar) return
    if (percent == null || Number.isNaN(percent)) {
      // Indeterminate state: show a continuously moving bar instead of a
      // "stuck at 35%" illusion.
      bar.classList.remove('animate-pulse')
      bar.style.width = '35%'
      bar.style.backgroundImage = 'linear-gradient(90deg, rgba(14,165,233,0) 0%, rgba(14,165,233,1) 45%, rgba(14,165,233,0) 100%)'
      bar.style.animation = 'uiProgressIndeterminate 1.1s linear infinite'
      bar.parentElement?.setAttribute('aria-valuenow', '')
      return
    }
    bar.style.animation = ''
    bar.style.backgroundImage = ''
    const p = Math.max(0, Math.min(100, Math.round(percent)))
    bar.style.width = `${p}%`
    bar.parentElement?.setAttribute('aria-valuenow', String(p))
  }

  return {
    setTitle(next) {
      if (titleEl) titleEl.textContent = next || ''
    },
    setMessage(next) {
      if (msgEl) msgEl.textContent = next || ''
    },
    setDetail(next) {
      if (detailEl) detailEl.textContent = next || ''
    },
    setProgress,
    close() {
      removeEl(overlay)
    },
  }
}

export async function uiChoose(message, choices, { title, danger = false, cancelLabel } = {}) {
  const buttons = (choices || []).map((c) => ({
    label: c.label,
    variant: c.variant || (danger ? 'danger' : 'primary'),
    result: c.value,
  }))
  buttons.unshift({ label: cancelLabel || t('common.cancel') || 'Cancel', variant: 'secondary', result: '' })
  return showDialog({
    title: title || (t('common.confirm') || 'Confirm'),
    body: message || '',
    kind: danger ? 'danger' : 'info',
    buttons,
    closeResult: '',
  })
}

