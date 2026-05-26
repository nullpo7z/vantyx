/**
 * @file DOM helpers shared between pages and modals.
 *
 * These functions are intentionally tiny and side-effect free so they
 * can be unit-tested without a full SPA shell.
 */

import API from './api.js'
import { t } from './i18n.js'

/**
 * Escape arbitrary text for safe interpolation into innerHTML.
 *
 * @param {unknown} s - Value to escape. Coerced via `Element.textContent`.
 * @returns {string} HTML-safe string.
 */
export function escapeHtml(s) {
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}

/**
 * Sanitise a URL before interpolating it into an `href` / `src`
 * attribute. Rejects javascript:, data:, vbscript:, and file: URIs to
 * prevent XSS via reflected target names / descriptions (CWE-79 /
 * CWE-80). Empty or non-string input collapses to `#`.
 *
 * @param {unknown} url
 * @returns {string}
 */
export function safeUrl(url) {
  if (typeof url !== 'string') return '#'
  const trimmed = url.trim()
  if (trimmed === '') return '#'
  const lower = trimmed.toLowerCase()
  // Reject obvious script protocols regardless of leading whitespace.
  for (const proto of ['javascript:', 'data:', 'vbscript:', 'file:']) {
    if (lower.startsWith(proto)) return '#'
  }
  return escapeHtml(trimmed)
}

/**
 * Render Proxmox-style tag pills (rounded, bordered, with an inline
 * tag glyph) for the supplied tag list.
 *
 * @param {string[] | null | undefined} tags - List of tags to render.
 * @returns {string} HTML for the pills, or an em-dash placeholder.
 */
export function renderTagPills(tags) {
  if (!tags || tags.length === 0) {
    return '<span class="text-xs text-slate-400">—</span>'
  }
  const tagIcon = '<svg class="shrink-0 opacity-70" width="12" height="12" viewBox="0 0 12 12" fill="currentColor" aria-hidden="true"><path d="M2 1.5C2 1.22 2.22 1 2.5 1H7.5l3 3-3 3H2.5C2.22 7 2 6.78 2 6.5v-5z"/></svg>'
  return tags
    .map((t) => `<span class="inline-flex items-center gap-1.5 rounded-md border border-slate-300 bg-white px-2.5 py-1 text-xs font-medium text-slate-700 shadow-sm">${tagIcon}${escapeHtml(t)}</span>`)
    .join('')
}

/**
 * Populate a "pick from registered tags" widget under a tag input
 * field. Clicking a pill appends the tag to the comma-separated input
 * value (deduplicated).
 *
 * @param {HTMLElement} modalEl - Container that owns the input.
 * @param {string} inputId - DOM id of the `<input>` element. The
 *   matching picker container must be `${inputId}-picker`.
 */
export function fillExistingTagsPicker(modalEl, inputId) {
  const input = modalEl.querySelector(`#${inputId}`)
  const container = modalEl.querySelector(`#${inputId}-picker`)
  if (!input || !container) return
  API.tags()
    .then((res) => {
      const allTags = (res && res.tags) || []
      if (allTags.length === 0) {
        container.innerHTML = ''
        return
      }
      container.innerHTML = `<p class="text-xs text-slate-500 mb-1.5">${t('targets.pickExistingTags')}</p><div class="flex flex-wrap gap-2">${allTags.map((tag) => `<button type="button" class="existing-tag-pill rounded border border-slate-300 bg-slate-50 px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-sky-50 hover:border-sky-300 transition-colors" data-tag="${escapeHtml(tag)}">${escapeHtml(tag)}</button>`).join('')}</div>`
      container.querySelectorAll('.existing-tag-pill').forEach((btn) => {
        btn.addEventListener('click', () => {
          const tag = (btn.dataset.tag || '').trim()
          if (!tag) return
          const raw = input.value.trim()
          const current = raw ? raw.split(',').map((s) => s.trim()).filter(Boolean) : []
          if (!current.includes(tag)) {
            input.value = current.length ? `${raw}, ${tag}` : tag
          }
        })
      })
    })
    .catch(() => {
      container.innerHTML = ''
    })
}
