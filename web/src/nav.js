let state = {
  navTargets: null,
  navSessions: null,
  navRecordings: null,
  navGroups: null,
  navUsers: null,
  navAudit: null,
  navSettings: null,
  navApiRef: null,
  getMe: null,
}

function navLinks() {
  const { navTargets, navSessions, navRecordings, navGroups, navUsers, navAudit, navSettings, navApiRef } = state
  return [navTargets, navSessions, navRecordings, navGroups, navUsers, navAudit, navSettings, navApiRef].filter(Boolean)
}

function isAdminOnlyNav(el) {
  const { navGroups, navUsers, navAudit, navSettings } = state
  return el === navGroups || el === navUsers || el === navAudit || el === navSettings
}

function setNavLinkVisible(el, visible) {
  if (visible) {
    el.classList.remove('hidden')
  } else {
    el.classList.add('hidden')
  }
}

export function initNav({
  navTargets,
  navSessions,
  navRecordings,
  navGroups,
  navUsers,
  navAudit,
  navSettings,
  getMe,
  onHome,
  onSessions,
  onRecordings,
  onGroups,
  onUsers,
  onAudit,
  onSettings,
}) {
  const navApiRef = document.getElementById('nav-api-ref')
  state = { navTargets, navSessions, navRecordings, navGroups, navUsers, navAudit, navSettings, navApiRef, getMe }

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

  navGroups.addEventListener('click', (e) => {
    e.preventDefault()
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

  if (navAudit) {
    navAudit.addEventListener('click', (e) => {
      e.preventDefault()
      const me = state.getMe && state.getMe()
      if (!me || me.role !== 'admin') return
      if (typeof onAudit === 'function') onAudit()
    })
  }

  if (navSettings) {
    navSettings.addEventListener('click', (e) => {
      e.preventDefault()
      const me = state.getMe && state.getMe()
      if (!me || me.role !== 'admin') return
      if (typeof onSettings === 'function') onSettings()
    })
  }
}

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
    groups: state.navGroups,
    users: state.navUsers,
    audit: state.navAudit,
    settings: state.navSettings,
  }[tab]

  if (activeEl) {
    activeEl.setAttribute('aria-current', 'page')
  }
}

/** ログイン後にユーザー向けナビ項目を表示する */
export function showAuthenticatedNav(isAdmin) {
  const { navSessions, navRecordings } = state
  setNavLinkVisible(navSessions, true)
  setNavLinkVisible(navRecordings, true)
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
