const NAV_BASE = 'text-sm opacity-80 hover:opacity-100 transition-opacity'
const NAV_ACTIVE = 'text-sm font-semibold border-b-2 border-white pb-1 transition-opacity'

let state = {
  navTargets: null,
  navRecordings: null,
  navGroups: null,
  navUsers: null,
  getMe: null,
}

export function initNav({
  navTargets,
  navRecordings,
  navGroups,
  navUsers,
  getMe,
  onHome,
  onRecordings,
  onGroups,
  onUsers,
}) {
  state = { navTargets, navRecordings, navGroups, navUsers, getMe }

  navTargets.addEventListener('click', (e) => {
    e.preventDefault()
    const me = state.getMe && state.getMe()
    if (!me) return
    if (typeof onHome === 'function') onHome()
  })

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
}

export function setActiveNav(tab) {
  const { navTargets, navRecordings, navGroups, navUsers, getMe } = state
  if (!navTargets || !navRecordings || !navGroups) return
  const me = getMe && getMe()
  const isAdmin = me?.role === 'admin'
  const navHidden = isAdmin ? '' : ' hidden'

  navTargets.className = NAV_BASE
  navGroups.className = NAV_BASE + navHidden
  if (navUsers) {
    navUsers.className = NAV_BASE + navHidden
  }
  navRecordings.className = NAV_BASE

  switch (tab) {
    case 'targets':
      navTargets.className = NAV_ACTIVE
      break
    case 'groups':
      navGroups.className = NAV_ACTIVE + navHidden
      break
    case 'users':
      if (navUsers) {
        navUsers.className = NAV_ACTIVE + navHidden
      }
      break
    case 'recordings':
      navRecordings.className = NAV_ACTIVE
      break
    default:
      break
  }
}

