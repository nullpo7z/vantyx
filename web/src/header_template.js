/**
 * @file Static HTML template for the SPA shell (header + main pane +
 * modal mount points). Extracted from `app.js` so the entry point can
 * focus on orchestration.
 */

import { t, getLocale, SUPPORTED_LOCALES } from './i18n.js'

/**
 * Build the SPA shell HTML. The structure is wired up by `renderApp`
 * which queries the well-known element IDs declared here.
 *
 * Strings are resolved against the active locale at call time;
 * `renderApp` rebuilds the shell when the locale changes so the new
 * dictionary is picked up.
 *
 * @returns {string} HTML markup for the SPA shell.
 */
export function buildAppShellHTML() {
  const current = getLocale()
  const langOptions = SUPPORTED_LOCALES.map(
    (l) =>
      `<option value="${l.code}"${l.code === current ? ' selected' : ''}>${t(l.labelKey)}</option>`,
  ).join('')

  return `
    <div class="flex-1 flex flex-col">
      <header class="text-white shadow z-10 shrink-0">
        <div class="vantyx-header-inner">
          <div class="vantyx-header-start">
            <h1 class="vantyx-brand">${t('app.appName')}</h1>
            <nav class="vantyx-nav" aria-label="${t('common.mainMenuLabel')}">
              <a href="#" id="nav-targets" class="vantyx-nav-link">${t('nav.home')}</a>
              <a href="#" id="nav-sessions" class="vantyx-nav-link hidden">${t('nav.sessions')}</a>
              <a href="#" id="nav-recordings" class="vantyx-nav-link hidden">${t('nav.recordings')}</a>
              <a href="#" id="nav-groups" class="vantyx-nav-link hidden">${t('nav.targets')}</a>
              <a href="#" id="nav-users" class="vantyx-nav-link hidden">${t('nav.users')}</a>
              <a href="#" id="nav-credentials" class="vantyx-nav-link hidden">${t('nav.credentials')}</a>
              <a href="#" id="nav-audit" class="vantyx-nav-link hidden">${t('nav.audit')}</a>
              <a href="#" id="nav-settings" class="vantyx-nav-link hidden">${t('nav.settings')}</a>
              <a href="/docs" id="nav-api-ref" target="_blank" rel="noopener noreferrer" class="vantyx-nav-link hidden">${t('nav.apiRef')}</a>
            </nav>
          </div>
          <div class="vantyx-header-end">
          <label class="sr-only" for="language-switch">${t('language.switchAria')}</label>
          <select id="language-switch" class="bg-transparent text-white text-sm border border-white/30 rounded px-1.5 py-0.5 focus:outline-none focus:ring-1 focus:ring-white/70" title="${t('language.switchAria')}" aria-label="${t('language.switchAria')}">
            ${langOptions}
          </select>
          <div class="w-px h-4 bg-white/20"></div>
          <button id="theme-toggle" type="button" class="text-sm opacity-80 hover:opacity-100 transition-opacity focus:outline-none focus:ring-1 focus:ring-white/70 rounded px-1" aria-label="${t('common.themeLabel')}" title="${t('common.themeLabel')}">
            <svg class="theme-icon-light" xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/></svg>
            <svg class="theme-icon-dark" xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>
          </button>
          <div class="w-px h-4 bg-white/20"></div>
          <button id="user-name" class="text-sm font-medium opacity-90 hover:opacity-100 hover:underline focus:outline-none focus:ring-1 focus:ring-white/70 rounded px-1 cursor-pointer"></button>
          <div class="w-px h-4 bg-white/20"></div>
          <button id="logout-btn" class="text-sm opacity-80 hover:opacity-100 transition-opacity">${t('common.logoutLabel')}</button>
          </div>
        </div>
      </header>
      <main class="flex-1 overflow-auto p-6 flex flex-col items-center" id="main-content">
        <div class="w-full max-w-5xl flex-1 flex flex-col">
          <p class="text-slate-500">${t('common.loading')}</p>
        </div>
      </main>
      <div id="add-target-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="add-user-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="add-member-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="edit-tags-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="ssh-credential-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="active-sessions-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="file-protocol-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="recording-player-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="change-password-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="add-ssh-key-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="credential-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="session-end-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
    </div>
  `
}
