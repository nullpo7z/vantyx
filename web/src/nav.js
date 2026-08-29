/**
 * @file Header navigation links: visibility, active state, and click
 * handlers. The SPA shell calls `initNav` once on render and then uses
 * `setActiveNav` to highlight whatever page is currently in view.
 */

let state = {
  navTargets: null,
  navSessions: null,
  navRecordings: null,
  navRecordingExports: null,
  navGroups: null,
  navUsers: null,
  navCredentials: null,
  navAudit: null,
  navSystem: null,
  navSettings: null,
  navApiRef: null,
  getMe: null,
}

function navLinks() {
  const {
    navTargets,
    navSessions,
    navRecordings,
    navRecordingExports,
    navGroups,
    navUsers,
    navCredentials,
    navAudit,
    navSystem,
    navSettings,
    navApiRef,
  } = state
  return [
    navTargets,
    navSessions,
    navRecordings,
    navRecordingExports,
    navGroups,
    navUsers,
    navCredentials,
    navAudit,
    navSystem,
    navSettings,
    navApiRef,
  ].filter(Boolean)
}

// "Account settings" is deliberately NOT admin-only: it holds per-user
// preferences (language, password, 2FA, SSH keys) every user needs to
// reach (E-8). Server-wide settings live under the admin-only "System
// settings" entry.
function isAdminOnlyNav(el) {
  const { navGroups, navUsers, navCredentials, navAudit, navSystem, navApiRef } = state
  return (
    el === navGroups ||
    el === navUsers ||
    el === navCredentials ||
    el === navAudit ||
    el === navSystem ||
    el === navApiRef
  )
}

function setNavLinkVisible(el, visible) {
  if (!el) return
  if (visible) {
    el.classList.remove('hidden')
  } else {
    el.classList.add('hidden')
  }
}

/**
 * Wire up header navigation links to page renderers and click handlers.
 *
 * @param {object} opts
 * @param {HTMLElement} opts.navTargets
 * @param {HTMLElement} opts.navSessions
 * @param {HTMLElement} opts.navRecordings
 * @param {HTMLElement} [opts.navRecordingExports]
 * @param {HTMLElement} opts.navGroups
 * @param {HTMLElement} opts.navUsers
 * @param {HTMLElement} opts.navCredentials
 * @param {HTMLElement} opts.navAudit
 * @param {HTMLElement} [opts.navSystem]
 * @param {HTMLElement} opts.navSettings
 * @param {() => ({role: string} | null)} opts.getMe
 * @param {() => void} opts.onHome
 * @param {() => void} opts.onSessions
 * @param {() => void} opts.onRecordings
 * @param {() => void} [opts.onRecordingExports]
 * @param {() => void} opts.onGroups
 * @param {() => void} opts.onUsers
 * @param {() => void} opts.onCredentials
 * @param {() => void} opts.onAudit
 * @param {() => void} [opts.onSystem]
 * @param {() => void} opts.onSettings
 */
export function initNav({
  navTargets,
  navSessions,
  navRecordings,
  navRecordingExports,
  navGroups,
  navUsers,
  navCredentials,
  navAudit,
  navSystem,
  navSettings,
  getMe,
  onHome,
  onSessions,
  onRecordings,
  onRecordingExports,
  onGroups,
  onUsers,
  onCredentials,
  onAudit,
  onSystem,
  onSettings,
}) {
  const navApiRef = document.getElementById('nav-api-ref')
  state = {
    navTargets,
    navSessions,
    navRecordings,
    navRecordingExports,
    navGroups,
    navUsers,
    navCredentials,
    navAudit,
    navSystem,
    navSettings,
    navApiRef,
    getMe,
  }

  navTargets.addEventListener('click', (e) => {
    e.preventDefault()
    const me = state.getMe && state.getMe()
    if (!me) return
    if (typeof onHome === 'function') onHome()
  })

  if (navSessions) {
    navSessions.addEventListener('click', (e) => {
      e.preventDefault()
      const me = state.getMe && state.getMe()
      if (!me) return
      if (typeof onSessions === 'function') onSessions()
    })
  }

  navRecordings.addEventListener('click', (e) => {
    e.preventDefault()
    const me = state.getMe && state.getMe()
    if (!me) return
    if (typeof onRecordings === 'function') onRecordings()
  })

  if (navRecordingExports) {
    navRecordingExports.addEventListener('click', (e) => {
      e.preventDefault()
      const me = state.getMe && state.getMe()
      if (!me) return
      if (typeof onRecordingExports === 'function') onRecordingExports()
    })
  }

  navGroups.addEventListener('click', (e) => {
    e.preventDefault()
    const me = state.getMe && state.getMe()
    if (!me || me.role !== 'admin') return
    if (typeof onGroups === 'function') onGroups()
  })

  if (navUsers) {
    navUsers.addEventListener('click', (e) => {
      e.preventDefault()
      const me = state.getMe && state.getMe()
      if (!me || me.role !== 'admin') return
      if (typeof onUsers === 'function') onUsers()
    })
  }

  if (navCredentials) {
    navCredentials.addEventListener('click', (e) => {
      e.preventDefault()
      const me = state.getMe && state.getMe()
      if (!me || me.role !== 'admin') return
      if (typeof onCredentials === 'function') onCredentials()
    })
  }

  if (navAudit) {
    navAudit.addEventListener('click', (e) => {
      e.preventDefault()
      const me = state.getMe && state.getMe()
      if (!me || me.role !== 'admin') return
      if (typeof onAudit === 'function') onAudit()
    })
  }

  if (navSystem) {
    navSystem.addEventListener('click', (e) => {
      e.preventDefault()
      const me = state.getMe && state.getMe()
      if (!me || me.role !== 'admin') return
      if (typeof onSystem === 'function') onSystem()
    })
  }

  if (navSettings) {
    navSettings.addEventListener('click', (e) => {
      e.preventDefault()
      // Account settings is for every signed-in user (E-8 / R-4).
      const me = state.getMe && state.getMe()
      if (!me) return
      if (typeof onSettings === 'function') onSettings()
    })
  }
}

/**
 * Mark the supplied tab as active and refresh per-link visibility based
 * on the current user's role.
 *
 * @param {'targets'|'sessions'|'recordings'|'recordingExports'|'groups'|'users'|'credentials'|'audit'|'system'|'settings'} tab
 */
export function setActiveNav(tab) {
  const { navTargets, navRecordings, navGroups, getMe } = state
  if (!navTargets || !navRecordings || !navGroups) return
  const me = getMe && getMe()
  const isAdmin = me?.role === 'admin'

  for (const el of navLinks()) {
    el.removeAttribute('aria-current')
    const visible = isAdminOnlyNav(el) ? isAdmin : true
    el.className = 'vantyx-nav-link' + (visible ? '' : ' hidden')
  }

  const activeEl = {
    targets: state.navTargets,
    sessions: state.navSessions,
    recordings: state.navRecordings,
    recordingExports: state.navRecordingExports,
    groups: state.navGroups,
    users: state.navUsers,
    credentials: state.navCredentials,
    audit: state.navAudit,
    system: state.navSystem,
    settings: state.navSettings,
  }[tab]

  if (activeEl) {
    activeEl.setAttribute('aria-current', 'page')
  }
}

/**
 * Reveal authenticated navigation entries after a successful login.
 *
 * @param {boolean} isAdmin - When true, admin-only entries are also
 *   shown.
 */
export function showAuthenticatedNav(isAdmin) {
  const { navSessions, navRecordings, navRecordingExports, navSettings } = state
  setNavLinkVisible(navSessions, true)
  setNavLinkVisible(navRecordings, true)
  setNavLinkVisible(navRecordingExports, true)
  // Account settings holds per-user preferences (E-8).
  setNavLinkVisible(navSettings, true)
  if (isAdmin) {
    for (const el of navLinks()) {
      if (isAdminOnlyNav(el)) {
        setNavLinkVisible(el, true)
      }
    }
    if (state.navApiRef) {
      setNavLinkVisible(state.navApiRef, true)
    }
  }
}
