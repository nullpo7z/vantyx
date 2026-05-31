/**
 * @file Thin wrapper around the Vantyx REST API.
 *
 * Every method returns a parsed JSON body on success and throws an
 * Error whose `message` field is the server-supplied message (or the
 * HTTP status text when the response is not JSON). Callers can rely on
 * `try { await API.foo() } catch (err) { showError(err.message) }`.
 *
 * Cookies are required: the session is stored in `vantyx_session` and
 * every request opts in with `credentials: 'include'`.
 */

const API = {
  /**
   * Authenticate with the local credential store.
   *
   * @param {string} username
   * @param {string} password
   * @returns {Promise<{user_id: string, username: string, role: string, require_password_change?: boolean}>}
   * @throws {Error} When the server rejects the credentials.
   */
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

  /**
   * Rotate the current user's password.
   *
   * @param {string} currentPassword
   * @param {string} newPassword
   * @returns {Promise<void>}
   */
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

  /**
   * Persist the current user's UI locale preference. Pass an empty
   * string to clear the preference (the SPA then uses its own default).
   *
   * @param {string} locale - One of `''`, `'en'`, `'ja'`.
   * @returns {Promise<{locale: string}>}
   */
  async updateLocale(locale) {
    const res = await fetch('/api/me/locale', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ locale: locale || '' }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to update locale')
    }
    return res.json()
  },

  /**
   * Invalidate the current session server-side and clear the cookie.
   *
   * @returns {Promise<void>}
   */
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

  /**
   * Return information about the currently authenticated user.
   *
   * @returns {Promise<{user_id: string, username: string, role: string}>}
   * @throws {Error} On 401 / 403.
   */
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

  async updateGroup(groupId, { name }) {
    const res = await fetch(`/api/groups/${encodeURIComponent(groupId)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ name }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to update group')
    }
    return res.json()
  },

  async deleteGroup(groupId) {
    const res = await fetch(`/api/groups/${encodeURIComponent(groupId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete group')
    }
  },

  // --- SSH keys (admin only) ---

  async sshKeys() {
    const res = await fetch('/api/ssh-keys', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load SSH keys')
    }
    return res.json()
  },

  async createSSHKey({ id, label, ssh_private_key, ssh_private_key_passphrase }) {
    const res = await fetch('/api/ssh-keys', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({
        id: id || '',
        label: label || '',
        ssh_private_key: (ssh_private_key && ssh_private_key.trim()) || '',
        ssh_private_key_passphrase: ssh_private_key_passphrase || '',
      }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to create SSH key')
    }
    return res.json()
  },

  async updateSSHKey(keyId, { label, ssh_private_key, ssh_private_key_passphrase }) {
    const body = { label: label || '' }
    if (ssh_private_key !== undefined && ssh_private_key !== null) body.ssh_private_key = ssh_private_key
    if (ssh_private_key_passphrase !== undefined && ssh_private_key_passphrase !== null) {
      body.ssh_private_key_passphrase = ssh_private_key_passphrase
    }
    const res = await fetch(`/api/ssh-keys/${encodeURIComponent(keyId)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to update SSH key')
    }
    return res.json()
  },

  async deleteSSHKey(keyId) {
    const res = await fetch(`/api/ssh-keys/${encodeURIComponent(keyId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete SSH key')
    }
  },

  // --- Credential identities (admin only) ---

  async credentialIdentities() {
    const res = await fetch('/api/credential-identities', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load identities')
    }
    return res.json()
  },

  async createCredentialIdentity({ id, label, ssh_username, ssh_password, ssh_key_id }) {
    const res = await fetch('/api/credential-identities', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({
        id: id || '',
        label: label || '',
        ssh_username: ssh_username || '',
        ssh_password: ssh_password || '',
        ssh_key_id: ssh_key_id || '',
      }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to create identity')
    }
    return res.json()
  },

  async updateCredentialIdentity(identityId, { label, ssh_username, ssh_password, ssh_key_id }) {
    const body = { label: label || '', ssh_username: ssh_username || '' }
    if (ssh_password !== undefined && ssh_password !== null) body.ssh_password = ssh_password
    if (ssh_key_id !== undefined && ssh_key_id !== null) body.ssh_key_id = ssh_key_id
    const res = await fetch(`/api/credential-identities/${encodeURIComponent(identityId)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to update identity')
    }
    return res.json()
  },

  async deleteCredentialIdentity(identityId) {
    const res = await fetch(`/api/credential-identities/${encodeURIComponent(identityId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete identity')
    }
  },

  async createTarget({ name, host, port, protocol, group_id, path, ssh_username, ssh_password, ssh_private_key, ssh_private_key_passphrase, credential_identity_id, ssh_key_id, sftp_enabled, ftp_enabled, tftp_enabled, ssh_host_key_fingerprint }) {
    const payload = {
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
    }
    if (credential_identity_id) payload.credential_identity_id = credential_identity_id
    if (ssh_key_id) payload.ssh_key_id = ssh_key_id
    if (typeof sftp_enabled === 'boolean') payload.sftp_enabled = sftp_enabled
    if (typeof ftp_enabled === 'boolean') payload.ftp_enabled = ftp_enabled
    if (typeof tftp_enabled === 'boolean') payload.tftp_enabled = tftp_enabled
    if (typeof ssh_host_key_fingerprint === 'string' && ssh_host_key_fingerprint.trim() !== '') {
      payload.ssh_host_key_fingerprint = ssh_host_key_fingerprint.trim()
    }
    const res = await fetch('/api/targets', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify(payload),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to create target')
    }
    return res.json()
  },

  async updateTarget(targetId, { name, host, port, protocol, path, ssh_username, ssh_password, ssh_private_key, ssh_private_key_passphrase, credential_identity_id, ssh_key_id, sftp_enabled, ftp_enabled, tftp_enabled }) {
    const body = {
      name,
      host,
      port: port || 22,
      protocol: protocol || 'ssh',
      path: path || '',
      ssh_username: ssh_username || '',
    }
    if (credential_identity_id) body.credential_identity_id = credential_identity_id
    if (ssh_key_id) body.ssh_key_id = ssh_key_id
    if (typeof sftp_enabled === 'boolean') {
      body.sftp_enabled = sftp_enabled
    }
    if (typeof ftp_enabled === 'boolean') {
      body.ftp_enabled = ftp_enabled
    }
    if (typeof tftp_enabled === 'boolean') {
      body.tftp_enabled = tftp_enabled
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

  /**
   * Probe an SSH host and return the SHA-256 fingerprint of its host
   * key without persisting anything. Admin-only on the server.
   *
   * Used by the Add / Edit Target modals for the trust-on-first-use
   * confirmation step, and by the inline "再取得" button.
   *
   * @param {{host: string, port?: number}} args
   * @returns {Promise<{host: string, port: number, fingerprint: string}>}
   */
  async probeHostKey({ host, port }) {
    const res = await fetch('/api/targets/probe-host-key', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ host, port: port || 22 }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to probe SSH host key')
    }
    return res.json()
  },

  /**
   * Adopt or clear the expected SSH host key fingerprint for an
   * existing target. Pass an empty string to clear; otherwise the
   * value must be in "SHA256:<base64>" form.
   *
   * Admin-only on the server. Returns the refreshed target row so
   * callers can update local state.
   *
   * @param {string} targetId
   * @param {string} fingerprint Pass "" to clear; otherwise "SHA256:..." form.
   * @returns {Promise<object>} Updated target row.
   */
  async updateTargetHostKey(targetId, fingerprint) {
    const res = await fetch(`/api/targets/${encodeURIComponent(targetId)}/ssh-host-key`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ fingerprint: fingerprint || '' }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to update SSH host key fingerprint')
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

  /**
   * セッション作成・削除のリアルタイム通知を購読する（SSE）。
   * 返すオブジェクトの close() を呼ぶと購読を解除する。
   * @param {function(object): void} onMessage - イベント受信時（data: { type: 'session_change' }）
   */
  subscribeSessionEvents(onMessage) {
    const url = new URL('/api/events/sessions', window.location.origin).toString()
    const es = new window.EventSource(url)
    es.onmessage = (ev) => {
      try {
        const data = JSON.parse(ev.data || '{}')
        if (data && typeof onMessage === 'function') onMessage(data)
      } catch {
        // ignore invalid JSON
      }
    }
    es.onerror = () => {
      es.close()
    }
    return {
      close() {
        es.close()
      },
    }
  },

  /**
   * バックグラウンドファイル転送の状態変化を SSE で購読する。
   * イベントは個別のジョブ snapshot を返す。
   * @param {function(object): void} onSnapshot - スナップショット受信時
   * @param {function(): void} [onError] - 切断時
   */
  subscribeFileTransferEvents(onSnapshot, onError) {
    const url = new URL('/api/events/file-transfers', window.location.origin).toString()
    const es = new window.EventSource(url)
    es.onmessage = (ev) => {
      try {
        const data = JSON.parse(ev.data || '{}')
        if (data && typeof onSnapshot === 'function') onSnapshot(data)
      } catch {
        /* ignore */
      }
    }
    es.onerror = () => {
      es.close()
      if (typeof onError === 'function') onError()
    }
    return {
      close() {
        es.close()
      },
    }
  },

  /** 監査ログ（管理者のみ） */
  async auditLogs({ limit = 200, event = '', user_id = '', from = '', to = '', exclude_event = '', after_id = '' } = {}) {
    const q = new URLSearchParams()
    if (limit) q.set('limit', String(limit))
    if (event) q.set('event', event)
    if (user_id) q.set('user_id', user_id)
    if (from) q.set('from', from)
    if (to) q.set('to', to)
    if (exclude_event) q.set('exclude_event', exclude_event)
    if (after_id) q.set('after_id', after_id)
    const url = '/api/audit' + (q.toString() ? `?${q.toString()}` : '')
    const res = await fetch(url, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load audit logs')
    }
    return res.json()
  },

  /** 監査ログ転送（syslog/SIEM）設定（管理者のみ） */
  async auditForwarderSettingsGet() {
    const res = await fetch('/api/settings/audit-forwarder', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load settings')
    }
    return res.json()
  },

  /** 監査ログ転送（syslog/SIEM）設定を保存（管理者のみ） */
  async auditForwarderSettingsPut({ config }) {
    const res = await fetch('/api/settings/audit-forwarder', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ config }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to save settings')
    }
    return res.json()
  },

  /** コマンドログ検索（管理者のみ） */
  async commandLogs({ query = '', user_id = '', target_id = '', from = '', to = '', limit = 200, after_id = '' } = {}) {
    const q = new URLSearchParams()
    if (query) q.set('query', query)
    if (user_id) q.set('user_id', user_id)
    if (target_id) q.set('target_id', target_id)
    if (from) q.set('from', from)
    if (to) q.set('to', to)
    if (limit) q.set('limit', String(limit))
    if (after_id) q.set('after_id', after_id)
    const res = await fetch('/api/commands' + (q.toString() ? `?${q.toString()}` : ''), { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load command logs')
    }
    return res.json()
  },

  /** 組み込み TFTP サーバー — ディレクトリ一覧 */
  async tftpServerFilesList(targetId, path = '/') {
    const q = new URLSearchParams()
    if (path) q.set('path', path)
    const res = await fetch(
      `/api/tftp/targets/${encodeURIComponent(targetId)}/files` + (q.toString() ? `?${q.toString()}` : ''),
      { credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to list TFTP files')
    }
    return res.json()
  },

  /** 組み込み TFTP サーバー — ファイルアップロード */
  async tftpServerUpload(targetId, path, file) {
    const form = new FormData()
    form.append('path', path)
    form.append('file', file)
    const res = await fetch(`/api/tftp/targets/${encodeURIComponent(targetId)}/files/upload`, {
      method: 'POST',
      credentials: 'include',
      body: form,
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to upload TFTP file')
    }
    return res.json().catch(() => ({}))
  },

  /** 組み込み TFTP サーバー — ファイルダウンロード（Blob） */
  async tftpServerDownload(targetId, path) {
    const q = new URLSearchParams({ path })
    const res = await fetch(
      `/api/tftp/targets/${encodeURIComponent(targetId)}/files/download?${q.toString()}`,
      { credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to download TFTP file')
    }
    return res.blob()
  },

  /** 組み込み TFTP サーバー — ファイル削除 */
  async tftpServerDelete(targetId, path) {
    const q = new URLSearchParams({ path })
    const res = await fetch(`/api/tftp/targets/${encodeURIComponent(targetId)}/files?${q.toString()}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete TFTP file')
    }
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
    if (params.from) q.set('from', params.from)
    if (params.to) q.set('to', params.to)
    if (params.channel_type) q.set('channel_type', params.channel_type)
    if (params.session_id) q.set('session_id', params.session_id)
    if (params.user_id) q.set('user_id', params.user_id)
    const res = await fetch(`/api/recordings?${q}`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load recordings')
    }
    return res.json()
  },

  /** Background recording export jobs for the current user. */
  async recordingExports() {
    const res = await fetch('/api/recordings/exports', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load recording exports')
    }
    return res.json()
  },

  /** Cancel a queued or running recording export job. */
  async cancelRecordingExport(exportId) {
    const res = await fetch(`/api/recordings/exports/${encodeURIComponent(exportId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to cancel recording export')
    }
  },

  /** Delete a background recording export job. */
  async deleteRecordingExport(exportId) {
    const res = await fetch(`/api/recordings/exports/${encodeURIComponent(exportId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete recording export')
    }
  },

  /** Queue GIF/WebM generation for a terminal recording. */
  async startRecordingExport(recordingId, format) {
    const q = new URLSearchParams()
    q.set('format', format)
    const res = await fetch(
      `/api/recordings/${encodeURIComponent(recordingId)}/export?${q}`,
      { method: 'POST', credentials: 'include' },
    )
    if (!res.ok && res.status !== 202) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to queue recording export')
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

  /** リモートファイル: ディレクトリ一覧（transfer: 'ftp' | 'sftp' で SSH ターゲットの転送方式を指定） */
  async filesList(targetId, path = '/', { transfer } = {}) {
    const q = new URLSearchParams()
    if (path && path !== '') q.set('path', path)
    if (transfer) q.set('transfer', transfer)
    const res = await fetch(`/api/targets/${encodeURIComponent(targetId)}/files?${q}`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to list files')
    }
    return res.json()
  },

  /** リモートファイル: ダウンロード（Blob を返す） */
  async filesDownload(targetId, path, { transfer } = {}) {
    const q = new URLSearchParams({ path })
    if (transfer) q.set('transfer', transfer)
    const res = await fetch(`/api/targets/${encodeURIComponent(targetId)}/files/download?${q}`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Download failed')
    }
    return res.blob()
  },

  /** リモートファイル: アップロード */
  async filesUpload(targetId, path, file, { transfer } = {}) {
    const form = new FormData()
    form.append('path', path)
    form.append('file', file)
    const uploadUrl = transfer
      ? `/api/targets/${encodeURIComponent(targetId)}/files/upload?transfer=${encodeURIComponent(transfer)}`
      : `/api/targets/${encodeURIComponent(targetId)}/files/upload`
    const res = await fetch(uploadUrl, {
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

  /** バックグラウンド転送一覧 */
  async fileTransfers(options = {}) {
    const params = new URLSearchParams()
    const opts = options || {}
    if (opts.limit != null) params.set('limit', String(opts.limit))
    if (opts.query) params.set('query', opts.query)
    if (opts.targetId) params.set('target_id', opts.targetId)
    if (opts.direction) params.set('direction', opts.direction)
    if (opts.backend) params.set('backend', opts.backend)
    if (opts.userId) params.set('user_id', opts.userId)
    if (opts.from) params.set('from', opts.from)
    if (opts.to) params.set('to', opts.to)
    if (opts.afterCursor) params.set('after_cursor', opts.afterCursor)
    if (Array.isArray(opts.states)) {
      for (const s of opts.states) params.append('state', s)
    } else if (typeof opts.state === 'string' && opts.state) {
      params.set('state', opts.state)
    }
    const qs = params.toString()
    const url = qs ? `/api/file-transfers?${qs}` : '/api/file-transfers'
    const res = await fetch(url, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load file transfers')
    }
    return res.json()
  },

  async fileTransferGet(transferId) {
    const res = await fetch(`/api/file-transfers/${encodeURIComponent(transferId)}`, { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load transfer')
    }
    return res.json()
  },

  async fileTransferCancel(transferId) {
    const res = await fetch(`/api/file-transfers/${encodeURIComponent(transferId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to cancel transfer')
    }
  },

  async fileTransferContent(transferId) {
    const res = await fetch(`/api/file-transfers/${encodeURIComponent(transferId)}/content`, {
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Download failed')
    }
    return res.blob()
  },

  async fileTransferStartDownload({ backend, target_id, path, transfer }) {
    const body = { backend, target_id, path }
    if (transfer) body.transfer = transfer
    const res = await fetch('/api/file-transfers/download', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to start download')
    }
    return res.json()
  },

  /** リモートファイル: ファイル・ディレクトリ削除 */
  async filesDelete(targetId, path, { transfer } = {}) {
    const q = new URLSearchParams({ path })
    if (transfer) q.set('transfer', transfer)
    const res = await fetch(`/api/targets/${encodeURIComponent(targetId)}/files?${q}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Delete failed')
    }
  },

  /**
   * 共有セッションの招待を発行する。
   * @param {string} sessionId
   * @param {{mode?: string, invitee_user_id?: string, ttl_seconds?: number}} options
   */
  /** 招待先のユーザー・グループ候補（セッションオーナー向け） */
  async sessionInvitationOptions(sessionId) {
    const res = await fetch(
      `/api/terminal/sessions/${encodeURIComponent(sessionId)}/invitation-options`,
      { credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load invitation options')
    }
    return res.json()
  },

  async createSessionInvitation(sessionId, options = {}) {
    const res = await fetch(
      `/api/terminal/sessions/${encodeURIComponent(sessionId)}/invitations`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(options || {}),
      },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to create invitation')
    }
    return res.json()
  },

  /** 自分宛ての未使用指名招待一覧（ホーム画面用） */
  async listIncomingInvitations() {
    const res = await fetch('/api/invitations/incoming', { credentials: 'include', cache: 'no-store' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load incoming invitations')
    }
    return res.json()
  },

  /** 共有セッションの招待一覧 */
  async listSessionInvitations(sessionId) {
    const res = await fetch(
      `/api/terminal/sessions/${encodeURIComponent(sessionId)}/invitations`,
      { credentials: 'include', cache: 'no-store' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load invitations')
    }
    return res.json()
  },

  /** 招待を取消する */
  async revokeSessionInvitation(sessionId, invitationId) {
    const res = await fetch(
      `/api/terminal/sessions/${encodeURIComponent(sessionId)}/invitations/${encodeURIComponent(invitationId)}`,
      { method: 'DELETE', credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to revoke invitation')
    }
  },

  /**
   * 有効な招待の参加 URL を再発行する（以前のリンクは無効）。
   * @returns {Promise<{ join_url: string, token?: string }>}
   */
  async regenerateSessionInvitationJoinUrl(sessionId, invitationId) {
    const res = await fetch(
      `/api/terminal/sessions/${encodeURIComponent(sessionId)}/invitations/${encodeURIComponent(invitationId)}/join-url`,
      { method: 'POST', credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to regenerate invitation link')
    }
    return res.json()
  },

  /** 招待トークンまたは招待 ID で参加する */
  async joinSession(sessionId, { invitationToken, invitationId } = {}) {
    const body = {}
    if (invitationToken) body.invitation_token = invitationToken
    if (invitationId) body.invitation_id = invitationId
    const res = await fetch(
      `/api/terminal/sessions/${encodeURIComponent(sessionId)}/join`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify(body),
      },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to join session')
    }
    return res.json()
  },

  /** 参加者一覧と書込権限リクエスト一覧 */
  async listSessionParticipants(sessionId) {
    const res = await fetch(
      `/api/terminal/sessions/${encodeURIComponent(sessionId)}/participants`,
      { credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load participants')
    }
    return res.json()
  },

  /** 参加者をキックする（オーナー専用） */
  async kickSessionParticipant(sessionId, userId) {
    const res = await fetch(
      `/api/terminal/sessions/${encodeURIComponent(sessionId)}/participants/${encodeURIComponent(userId)}`,
      { method: 'DELETE', credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to remove participant')
    }
  },

  /** 書込権限のリクエストを作成する */
  async createSessionWriteRequest(sessionId) {
    const res = await fetch(
      `/api/terminal/sessions/${encodeURIComponent(sessionId)}/write-requests`,
      { method: 'POST', credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to request write token')
    }
    return res.json()
  },

  /** 書込権限リクエストを承認する */
  async grantSessionWriteRequest(sessionId, requestId) {
    const res = await fetch(
      `/api/terminal/sessions/${encodeURIComponent(sessionId)}/write-requests/${encodeURIComponent(requestId)}/grant`,
      { method: 'POST', credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to grant write request')
    }
    return res.json()
  },

  /** 書込権限リクエストを拒否する */
  async denySessionWriteRequest(sessionId, requestId) {
    const res = await fetch(
      `/api/terminal/sessions/${encodeURIComponent(sessionId)}/write-requests/${encodeURIComponent(requestId)}/deny`,
      { method: 'POST', credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to deny write request')
    }
    return res.json()
  },

  /** 書込権限を自発的に手放す */
  async releaseSessionWriteToken(sessionId) {
    const res = await fetch(
      `/api/terminal/sessions/${encodeURIComponent(sessionId)}/write-token/release`,
      { method: 'POST', credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to release write token')
    }
  },
}

export default API
