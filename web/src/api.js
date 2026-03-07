const API = {
  async login(username, password) {
    const res = await fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Login failed')
    }
    return res.json()
  },

  async changePassword(currentPassword, newPassword) {
    const res = await fetch('/api/me/password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to change password')
    }
  },

  async logout() {
    const res = await fetch('/api/logout', {
      method: 'POST',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Logout failed')
    }
  },

  async me() {
    const res = await fetch('/api/me', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText || 'Unauthorized' }))
      throw new Error(err.message || 'Unauthorized')
    }
    return res.json()
  },

  async targets() {
    const res = await fetch('/api/targets', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load targets')
    }
    return res.json()
  },

  async groups() {
    const res = await fetch('/api/groups', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load groups')
    }
    return res.json()
  },

  async createGroup({ name, path }) {
    const res = await fetch('/api/groups', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ name, path: path || '' }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to create group')
    }
    return res.json()
  },

  async createTarget({ name, host, port, protocol, group_id, path, ssh_username, ssh_password }) {
    const res = await fetch('/api/targets', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({
        name,
        host,
        port: port || 22,
        protocol: protocol || 'ssh',
        group_id,
        path: path || '',
        ssh_username: ssh_username || '',
        ssh_password: ssh_password || '',
      }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to create target')
    }
    return res.json()
  },

  /** アクティブなターミナルセッション一覧（レジューム用） */
  async terminalSessions() {
    const res = await fetch('/api/terminal/sessions', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load sessions')
    }
    return res.json()
  },

  /** ターミナルセッションを終了する（閉じる用） */
  async terminalSessionDelete(sessionId) {
    const res = await fetch(`/api/terminal/sessions/${encodeURIComponent(sessionId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete session')
    }
  },
}

export default API
