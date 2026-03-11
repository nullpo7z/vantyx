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

  /** 指定ユーザーの Vantyx ログイン用 SSH 公開鍵一覧（管理者のみ） */
  async userSSHKeys(userId) {
    const res = await fetch(`/api/users/${encodeURIComponent(userId)}/ssh-keys`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load SSH keys')
    }
    return res.json()
  },

  /** 指定ユーザーに Vantyx ログイン用 SSH 公開鍵を追加（管理者のみ） */
  async addUserSSHKey(userId, authorizedKey) {
    const res = await fetch(`/api/users/${encodeURIComponent(userId)}/ssh-keys`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ authorized_key: authorizedKey }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to add SSH key')
    }
    return res.json()
  },

  /** 指定ユーザーの Vantyx ログイン用 SSH 公開鍵を削除（管理者のみ） */
  async deleteUserSSHKey(userId, keyId) {
    const res = await fetch(`/api/users/${encodeURIComponent(userId)}/ssh-keys/${encodeURIComponent(keyId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete SSH key')
    }
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

  async createTarget({ name, host, port, protocol, group_id, path, ssh_username, ssh_password, ssh_private_key, ssh_private_key_passphrase }) {
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
        ssh_private_key: (ssh_private_key && ssh_private_key.trim()) || '',
        ssh_private_key_passphrase: ssh_private_key_passphrase || '',
      }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to create target')
    }
    return res.json()
  },

  async updateTarget(targetId, { name, host, port, protocol, path, ssh_username, ssh_password, ssh_private_key, ssh_private_key_passphrase }) {
    const body = {
      name,
      host,
      port: port || 22,
      protocol: protocol || 'ssh',
      path: path || '',
      ssh_username: ssh_username || '',
    }
    if (ssh_password !== undefined && ssh_password !== null) {
      body.ssh_password = ssh_password
    }
    if (ssh_private_key !== undefined && ssh_private_key !== null) {
      body.ssh_private_key = ssh_private_key
    }
    if (ssh_private_key_passphrase !== undefined && ssh_private_key_passphrase !== null) {
      body.ssh_private_key_passphrase = ssh_private_key_passphrase
    }
    const res = await fetch(`/api/targets/${encodeURIComponent(targetId)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to update target')
    }
    return res.json()
  },

  async deleteTarget(targetId) {
    const res = await fetch(`/api/targets/${encodeURIComponent(targetId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete target')
    }
  },

  /** ターゲットのタグ一覧 */
  async targetTags(targetId) {
    const res = await fetch(`/api/targets/${encodeURIComponent(targetId)}/tags`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load target tags')
    }
    return res.json()
  },

  /** ターゲットのタグを設定 */
  async setTargetTags(targetId, tags) {
    const res = await fetch(`/api/targets/${encodeURIComponent(targetId)}/tags`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ tags: tags || [] }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to set target tags')
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

  /** アクティブな RDP（ブラウザ）セッション一覧（再接続用） */
  async rdpSessions() {
    const res = await fetch('/api/rdp/sessions', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load RDP sessions')
    }
    return res.json()
  },

  /** RDP ブラウザセッションを終了する（再接続候補からも消す）。 */
  async rdpSessionDelete(sessionId) {
    const res = await fetch(`/api/rdp/sessions/${encodeURIComponent(sessionId)}`, {
      method: 'DELETE',
      credentials: 'include',
      keepalive: true,
    })
    // セッションが既に終了している場合は 404 になり得るため、成功扱いにする。
    if (res.status === 404) return
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete RDP session')
    }
  },

  /** ターミナルセッションを終了する（閉じる用）。keepalive でタブ閉鎖時も送信完了させる。 */
  async terminalSessionDelete(sessionId) {
    const res = await fetch(`/api/terminal/sessions/${encodeURIComponent(sessionId)}`, {
      method: 'DELETE',
      credentials: 'include',
      keepalive: true,
    })
    // セッションが既にサーバー側で終了している場合（exit 等）は 404 になり得るため、成功扱いにする。
    if (res.status === 404) return
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete session')
    }
  },

  /** ユーザー一覧（管理者のみ） */
  async users(params = {}) {
    const q = new URLSearchParams()
    if (params.limit != null) q.set('limit', params.limit)
    if (params.offset != null) q.set('offset', params.offset)
    const res = await fetch(`/api/users?${q}`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load users')
    }
    return res.json()
  },

  /** ユーザー登録（管理者のみ） */
  async createUser({ id, username, password, role }) {
    const res = await fetch('/api/users', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ id: id || undefined, username, password, role: role || 'user' }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to create user')
    }
    return res.json()
  },

  /** グループメンバー一覧（管理者のみ） */
  async groupMembers(groupId) {
    const res = await fetch(`/api/groups/${encodeURIComponent(groupId)}/members`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load members')
    }
    return res.json()
  },

  /** グループのタグ一覧 */
  async groupTags(groupId) {
    const res = await fetch(`/api/groups/${encodeURIComponent(groupId)}/tags`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load group tags')
    }
    return res.json()
  },

  /** グループのタグを設定 */
  async setGroupTags(groupId, tags) {
    const res = await fetch(`/api/groups/${encodeURIComponent(groupId)}/tags`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ tags: tags || [] }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to set group tags')
    }
    return res.json()
  },

  /** グループにメンバーを追加（管理者のみ） */
  async addGroupMember(groupId, userId) {
    const res = await fetch(`/api/groups/${encodeURIComponent(groupId)}/members`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ user_id: userId }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to add member')
    }
  },

  /** 録画一覧（自分のターミナル録画メタデータ） */
  async recordings(params = {}) {
    const q = new URLSearchParams()
    if (params.target_id) q.set('target_id', params.target_id)
    const res = await fetch(`/api/recordings?${q}`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load recordings')
    }
    return res.json()
  },

  /** 登録済みタグ一覧（ユーザー・ターゲット・グループで使用中のタグの重複なし） */
  async tags() {
    const res = await fetch('/api/tags', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load tags')
    }
    return res.json()
  },

  /** ユーザーのタグ一覧（管理者のみ） */
  async userTags(userId) {
    const res = await fetch(`/api/users/${encodeURIComponent(userId)}/tags`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load user tags')
    }
    return res.json()
  },

  /** ユーザーのタグを設定（管理者のみ） */
  async setUserTags(userId, tags) {
    const res = await fetch(`/api/users/${encodeURIComponent(userId)}/tags`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ tags: tags || [] }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to set user tags')
    }
    return res.json()
  },

  /** グループからメンバーを削除（管理者のみ） */
  async removeGroupMember(groupId, userId) {
    const res = await fetch(`/api/groups/${encodeURIComponent(groupId)}/members/${encodeURIComponent(userId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to remove member')
    }
  },

  /** SFTP: ディレクトリ一覧 */
  async filesList(targetId, path = '/') {
    const q = new URLSearchParams()
    if (path && path !== '') q.set('path', path)
    const res = await fetch(`/api/targets/${encodeURIComponent(targetId)}/files?${q}`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to list files')
    }
    return res.json()
  },

  /** SFTP: ファイルダウンロード（Blob を返す） */
  async filesDownload(targetId, path) {
    const q = new URLSearchParams({ path })
    const res = await fetch(`/api/targets/${encodeURIComponent(targetId)}/files/download?${q}`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Download failed')
    }
    return res.blob()
  },

  /** SFTP: ファイルアップロード */
  async filesUpload(targetId, path, file) {
    const form = new FormData()
    form.append('path', path)
    form.append('file', file)
    const res = await fetch(`/api/targets/${encodeURIComponent(targetId)}/files/upload`, {
      method: 'POST',
      credentials: 'include',
      body: form,
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Upload failed')
    }
    return res.json()
  },

  /** SFTP: ファイル・ディレクトリ削除 */
  async filesDelete(targetId, path) {
    const q = new URLSearchParams({ path })
    const res = await fetch(`/api/targets/${encodeURIComponent(targetId)}/files?${q}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Delete failed')
    }
  },
}

export default API
