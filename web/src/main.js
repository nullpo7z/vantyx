/**
 * @file Entry point for the Vantyx SPA.
 *
 * Inspects `?view=` / pathname to decide whether to render the main
 * application shell, the login screen, or a dedicated full-window
 * page (terminal, files, VNC, RDP, TFTP console). All HTML mounting
 * happens inside `#app`.
 */

import { applyStoredTheme } from './theme.js'
import { applyHtmlLangAttribute, applyServerLocale, registerServerSync, t } from './i18n.js'
import API from './api.js'
import { initFileTransferManager } from './file_transfer_manager.js'
import { renderLogin } from './login.js'
import { renderApp } from './app.js'
import { renderTerminalPage } from './terminal_page.js'
import { renderFilesPage } from './files_page.js'
import { renderTFTPConsolePage } from './tftp_console_page.js'
import { renderVncPage } from './vnc_page.js'
import { renderRdpPage } from './rdp_page.js'

applyStoredTheme()
applyHtmlLangAttribute()

// User-driven locale changes (settings page / header switcher) are
// persisted to the server so the preference follows the user across
// devices. The handler is best-effort: server failures fall back to
// the local-only behaviour.
registerServerSync((locale) => API.updateLocale(locale).catch(() => undefined))

/**
 * Apply the locale value returned by `/api/login` or `/api/me` if one
 * is present. Server-supplied locale overrides what was cached in
 * localStorage from a previous browser, so the user sees the same UI
 * language they last picked even on a fresh device.
 *
 * @param {{locale?: string} | null | undefined} me
 */
function syncLocaleFromServer(me) {
  if (me && typeof me.locale === 'string' && me.locale) {
    applyServerLocale(me.locale)
  }
}

/** Render the shared "you must log in first" screen used by every
 *  full-window page when {@link API.me} returns an unauthorised error.
 *  Kept inline so we don't need to import the SPA shell here. */
function renderLoginRequired(container) {
  container.innerHTML = `
    <div class="min-h-screen flex items-center justify-center p-6">
      <div class="w-full max-w-md bg-white rounded-lg shadow-sm border border-slate-200 p-6">
        <h1 class="text-lg font-semibold text-slate-800 mb-2">${t('common.loginRequired')}</h1>
        <p class="text-sm text-slate-600 mb-4">${t('common.loginRequiredDesc')}</p>
        <a href="/" class="inline-flex items-center justify-center rounded bg-sky-600 px-4 py-2 text-sm font-medium text-white hover:bg-sky-700">${t('common.goLogin')}</a>
      </div>
    </div>
  `
}

const appEl = document.getElementById('app')

async function init() {
  const bootTransfers = async () => {
    try {
      const me = await API.me()
      syncLocaleFromServer(me)
      initFileTransferManager()
    } catch {
      /* not logged in */
    }
  }
  void bootTransfers()

  // Standalone full-screen terminal page (opened in a new tab).
  if (window.location.pathname === '/terminal') {
    try {
      const me = await API.me()
      syncLocaleFromServer(me)
      renderTerminalPage(appEl)
    } catch {
      renderLoginRequired(appEl)
    }
    return
  }

  // VNC viewer page (noVNC; requires target_id in query).
  if (window.location.pathname === '/vnc') {
    try {
      const me = await API.me()
      syncLocaleFromServer(me)
      renderVncPage(appEl)
    } catch {
      renderLoginRequired(appEl)
    }
    return
  }

  // RDP connection page (requires target_id in query).
  if (window.location.pathname === '/rdp') {
    try {
      const me = await API.me()
      syncLocaleFromServer(me)
      renderRdpPage(appEl)
    } catch {
      renderLoginRequired(appEl)
    }
    return
  }

  // File manager page (SFTP/FTP; requires target_id in query).
  if (window.location.pathname === '/files') {
    try {
      const me = await API.me()
      syncLocaleFromServer(me)
      renderFilesPage(appEl)
    } catch {
      renderLoginRequired(appEl)
    }
    return
  }

  // TFTP console page: top = Vantyx TFTP directory, bottom = SSH terminal.
  if (window.location.pathname === '/tftp-console') {
    try {
      const me = await API.me()
      syncLocaleFromServer(me)
      renderTFTPConsolePage(appEl)
    } catch {
      renderLoginRequired(appEl)
    }
    return
  }

  try {
    const me = await API.me()
    syncLocaleFromServer(me)
    renderApp(appEl)
  } catch {
    renderLogin(appEl)
  }
}

init()
