/**
 * @file Entry point for the Vantyx SPA.
 *
 * Inspects `?view=` / pathname to decide whether to render the main
 * application shell, the login screen, or a dedicated full-window
 * page (terminal, files, VNC, RDP, TFTP console). All HTML mounting
 * happens inside `#app`.
 */

import { applyStoredTheme } from './theme.js'
import { applyHtmlLangAttribute, t } from './i18n.js'
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
      await API.me()
      initFileTransferManager()
    } catch {
      /* not logged in */
    }
  }
  void bootTransfers()

  // Standalone full-screen terminal page (opened in a new tab).
  if (window.location.pathname === '/terminal') {
    try {
      await API.me()
      renderTerminalPage(appEl)
    } catch {
      renderLoginRequired(appEl)
    }
    return
  }

  // VNC viewer page (noVNC; requires target_id in query).
  if (window.location.pathname === '/vnc') {
    try {
      await API.me()
      renderVncPage(appEl)
    } catch {
      renderLoginRequired(appEl)
    }
    return
  }

  // RDP connection page (requires target_id in query).
  if (window.location.pathname === '/rdp') {
    try {
      await API.me()
      renderRdpPage(appEl)
    } catch {
      renderLoginRequired(appEl)
    }
    return
  }

  // File manager page (SFTP/FTP; requires target_id in query).
  if (window.location.pathname === '/files') {
    try {
      await API.me()
      renderFilesPage(appEl)
    } catch {
      renderLoginRequired(appEl)
    }
    return
  }

  // TFTP console page: top = Vantyx TFTP directory, bottom = SSH terminal.
  if (window.location.pathname === '/tftp-console') {
    try {
      await API.me()
      renderTFTPConsolePage(appEl)
    } catch {
      renderLoginRequired(appEl)
    }
    return
  }

  try {
    await API.me()
    renderApp(appEl)
  } catch {
    renderLogin(appEl)
  }
}

init()
