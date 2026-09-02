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
   * Complete a login whose password was accepted but which requires a
   * second factor. `code` is a TOTP or an unused recovery code.
   *
   * @param {string} mfaToken - Token from the `mfa_required` login response.
   * @param {string} code
   * @returns {Promise<{user_id: string, username: string, role: string, require_password_change?: boolean}>}
   */
  async loginTotp(mfaToken, code) {
    const res = await fetch('/api/login/totp', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mfa_token: mfaToken, code }),
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Verification failed')
    }
    return res.json()
  },

  /** Which sign-in methods the server offers (public). */
  async authMethods() {
    const res = await fetch('/api/auth/methods', { credentials: 'include' })
    if (!res.ok) return { password: true, oidc: { enabled: false } }
    return res.json()
  },

  /** Current user's two-factor status. */
  async totpStatus() {
    const res = await fetch('/api/me/totp', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load two-factor status')
    }
    return res.json()
  },

  /** Start two-factor enrolment: returns otpauth_url, secret and a QR PNG data URL. */
  async totpSetup() {
    const res = await fetch('/api/me/totp/setup', { method: 'POST', credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to start two-factor setup')
    }
    return res.json()
  },

  /** Confirm enrolment with the first code; returns the one-time recovery codes. */
  async totpConfirm(code) {
    const res = await fetch('/api/me/totp/confirm', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ code }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to confirm two-factor setup')
    }
    return res.json()
  },

  /** Disable two-factor authentication (re-authenticates with the password). */
  async totpDisable(password) {
    const res = await fetch('/api/me/totp', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ password }),
    })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to disable two-factor authentication')
    }
  },

  /** Current user's SSH public keys for the CLI gateway. */
  async mySSHKeys() {
    const res = await fetch('/api/me/ssh-keys', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load SSH keys')
    }
    return res.json()
  },

  async addMySSHKey(authorizedKey) {
    const res = await fetch('/api/me/ssh-keys', {
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

  async deleteMySSHKey(keyId) {
    const res = await fetch(`/api/me/ssh-keys/${encodeURIComponent(keyId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete SSH key')
    }
  },

  /** Passkeys (WebAuthn) registered as a second factor. */
  async passkeys() {
    const res = await fetch('/api/me/webauthn', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load passkeys')
    }
    return res.json()
  },

  async passkeyRegisterBegin() {
    const res = await fetch('/api/me/webauthn/register/begin', { method: 'POST', credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to start passkey registration')
    }
    return res.json()
  },

  async passkeyRegisterFinish(name, credential) {
    const res = await fetch('/api/me/webauthn/register/finish', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ name, credential }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Passkey registration failed')
    }
    return res.json()
  },

  async passkeyDelete(id) {
    const res = await fetch(`/api/me/webauthn/${encodeURIComponent(id)}`, { method: 'DELETE', credentials: 'include' })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete the passkey')
    }
  },

  async loginWebAuthnBegin(mfaToken) {
    const res = await fetch('/api/login/webauthn/begin', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ mfa_token: mfaToken }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to start passkey verification')
    }
    return res.json()
  },

  async loginWebAuthnFinish(mfaToken, credential) {
    const res = await fetch('/api/login/webauthn/finish', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ mfa_token: mfaToken, credential }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Passkey verification failed')
    }
    return res.json()
  },

  /** Current user's API tokens (plain values are never returned). */
  async myTokens() {
    const res = await fetch('/api/me/tokens', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load API tokens')
    }
    return res.json()
  },

  /** Create an API token; the response carries the plain token exactly once. */
  async createToken({ name, scope, expires_in_days }) {
    const res = await fetch('/api/me/tokens', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ name, scope, expires_in_days: Number(expires_in_days) || 0 }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to create the token')
    }
    return res.json()
  },

  async revokeToken(id) {
    const res = await fetch(`/api/me/tokens/${encodeURIComponent(id)}`, { method: 'DELETE', credentials: 'include' })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to revoke the token')
    }
  },

  /** Admin: update a user's role / username / disabled flag (omit fields to leave them). */
  async updateUser(userId, { role, username, disabled }) {
    const body = {}
    if (role !== undefined) body.role = role
    if (username !== undefined) body.username = username
    if (disabled !== undefined) body.disabled = disabled
    const res = await fetch(`/api/users/${encodeURIComponent(userId)}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to update user')
    }
    return res.json()
  },

  /** Admin: clear a user's second factor (lockout recovery). */
  async adminResetTotp(userId) {
    const res = await fetch(`/api/users/${encodeURIComponent(userId)}/totp`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to reset two-factor authentication')
    }
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
   * Persist the current user's preferred IANA timezone. Pass an empty
   * string to clear the preference (the SPA then falls back to the
   * browser's local zone).
   *
   * @param {string} timezone - IANA zone name (e.g. `'Asia/Tokyo'`), or `''`.
   * @returns {Promise<{timezone: string}>}
   */
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

  /** 新しい SSH 鍵ペアを生成して保存する。private_key/public_key はこの応答でのみ返る。 */
  async generateSSHKey({ id, label, key_type, passphrase }) {
    const res = await fetch('/api/ssh-keys/generate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({
        id: id || '',
        label: label || '',
        key_type: key_type || '',
        passphrase: passphrase || '',
      }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to generate SSH key')
    }
    return res.json()
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

  async updateTarget(targetId, { name, host, port, protocol, path, group_id, ssh_username, ssh_password, ssh_private_key, ssh_private_key_passphrase, credential_identity_id, ssh_key_id, sftp_enabled, ftp_enabled, tftp_enabled }) {
    const body = {
      name,
      host,
      port: port || 22,
      protocol: protocol || 'ssh',
      path: path || '',
      ssh_username: ssh_username || '',
    }
    if (group_id) body.group_id = group_id
    // Forward whenever explicitly provided (including '' to detach),
    // not just when truthy -- the backend treats an omitted key as
    // "leave the tracked credential source unchanged" and an explicit
    // '' as "detach it", so dropping '' here would silently prevent
    // ever clearing a previously linked Identity/SSH Key.
    if (credential_identity_id !== undefined && credential_identity_id !== null) {
      body.credential_identity_id = credential_identity_id
    }
    if (ssh_key_id !== undefined && ssh_key_id !== null) {
      body.ssh_key_id = ssh_key_id
    }
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

  /**
   * 組み込み TFTP サーバーの書き込みウィンドウ状態変化を SSE で購読する。
   * 接続直後に現在の状態が1件届き、以降は他タブ/他オペレーターによる
   * open/close も含めて変化のたびに届く。
   * @param {string} targetId
   * @param {function(object): void} onStatus - { open, target_id, client_ip?, expires_at? }
   * @param {function(): void} [onError] - 切断時
   */
  subscribeTFTPWriteWindowEvents(targetId, onStatus, onError) {
    const url = new URL(
      `/api/tftp/targets/${encodeURIComponent(targetId)}/write-window/events`,
      window.location.origin,
    ).toString()
    const es = new window.EventSource(url)
    es.onmessage = (ev) => {
      try {
        const data = JSON.parse(ev.data || '{}')
        if (data && typeof onStatus === 'function') onStatus(data)
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

  /**
   * 組み込み TFTP サーバー — 書き込みウィンドウの現在の状態を取得。
   * 常に { open, target_id, client_ip?, expires_at? } を返す
   * （サーバー未起動でも 200 + open:false）。
   */
  async tftpServerGetWriteWindow(targetId) {
    const res = await fetch(`/api/tftp/targets/${encodeURIComponent(targetId)}/write-window`, {
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to get TFTP write window status')
    }
    return res.json()
  },

  /**
   * 組み込み TFTP サーバー — 実 TFTP プロトコル経由の書き込み（WRQ）を許可する
   * 時限ウィンドウを開く。許可される送信元 IP はターゲットに設定された Host
   * 固定（サーバー側で決定・偽装不可）であり、呼び出し側は指定できない。
   * ttlSeconds 省略時はサーバー側デフォルト（5分、最大30分）。
   */
  async tftpServerOpenWriteWindow(targetId, ttlSeconds) {
    const body = {}
    if (ttlSeconds) body.ttl_seconds = ttlSeconds
    const res = await fetch(`/api/tftp/targets/${encodeURIComponent(targetId)}/write-window`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to open TFTP write window')
    }
    return res.json()
  },

  /** 組み込み TFTP サーバー — 開いている書き込みウィンドウを閉じる */
  async tftpServerCloseWriteWindow(targetId) {
    const res = await fetch(`/api/tftp/targets/${encodeURIComponent(targetId)}/write-window`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to close TFTP write window')
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

  /** 稼働中の直接 VNC セッション一覧（共有 UI の session_id 解決用）。 */
  async vncSessions() {
    const res = await fetch('/api/vnc/sessions', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load VNC sessions')
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
  /**
   * セッションを「意図的に放置中」としてマーク（または解除）する。
   * kind は 'terminal' | 'vnc' | 'rdp'。マークされたセッションはアイドル警告の対象外になる。
   */
  async setSessionKeep(sessionId, keep, { kind = 'terminal' } = {}) {
    const base =
      kind === 'rdp'
        ? '/api/rdp/sessions'
        : kind === 'vnc'
          ? '/api/vnc/sessions'
          : '/api/terminal/sessions'
    const res = await fetch(`${base}/${encodeURIComponent(sessionId)}/keep`, {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ keep: !!keep }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to update session')
    }
    return res.json()
  },

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

  /** ユーザー削除（管理者のみ。自分自身と最後の管理者は削除不可） */
  async deleteUser(userId) {
    const res = await fetch(`/api/users/${encodeURIComponent(userId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete user')
    }
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

  /** Admin: import targets from a CSV/JSON File (dry_run validates only). */
  async targetsImport(file, { dryRun = false } = {}) {
    const fd = new FormData()
    fd.append('file', file)
    fd.append('dry_run', dryRun ? '1' : '0')
    const res = await fetch('/api/targets/import', { method: 'POST', credentials: 'include', body: fd })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Import failed')
    }
    return res.json()
  },

  /** Admin: TCP reachability of targets (empty ids = all). */
  async targetsCheck(ids = []) {
    const res = await fetch('/api/targets/check', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ ids }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Reachability check failed')
    }
    return res.json()
  },

  /** Admin: backup policy + stored backups. */
  async backupsGet() {
    const res = await fetch('/api/settings/backups', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load backups')
    }
    return res.json()
  },

  async backupCreate() {
    const res = await fetch('/api/settings/backups', { method: 'POST', credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Backup failed')
    }
    return res.json()
  },

  async backupDelete(name) {
    const res = await fetch(`/api/settings/backups/${encodeURIComponent(name)}`, { method: 'DELETE', credentials: 'include' })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete the backup')
    }
  },

  /** Admin: stage a restore from a stored backup (name) or an uploaded File. */
  async backupRestore({ name, file }) {
    let res
    if (file) {
      const fd = new FormData()
      fd.append('file', file)
      res = await fetch('/api/settings/backups/restore', { method: 'POST', credentials: 'include', body: fd })
    } else {
      res = await fetch('/api/settings/backups/restore', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ name }),
      })
    }
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to stage the restore')
    }
    return res.json()
  },

  async backupRestoreCancel() {
    const res = await fetch('/api/settings/backups/restore', { method: 'DELETE', credentials: 'include' })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to cancel the restore')
    }
  },

  /** Admin: webhook endpoints with delivery stats. */
  async webhooksGet() {
    const res = await fetch('/api/settings/webhooks', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load webhooks')
    }
    return res.json()
  },

  /** Admin: replace the webhook endpoint list. */
  async webhooksPut(endpoints) {
    const res = await fetch('/api/settings/webhooks', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ endpoints }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to save webhooks')
    }
    return res.json()
  },

  /** Admin: send a test event to one endpoint. */
  async webhookTest(id) {
    const res = await fetch(`/api/settings/webhooks/${encodeURIComponent(id)}/test`, { method: 'POST', credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to test the webhook')
    }
    return res.json()
  },

  /** Admin: retention policy and last purge report. */
  async retentionGet() {
    const res = await fetch('/api/settings/retention', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load retention policy')
    }
    return res.json()
  },

  /** Admin: run the retention purge now. */
  async retentionRun() {
    const res = await fetch('/api/settings/retention/run', { method: 'POST', credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to run retention')
    }
    return res.json()
  },

  /** Admin: every live session (terminal / VNC / RDP). */
  async adminSessions() {
    const res = await fetch('/api/admin/sessions', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load sessions')
    }
    return res.json()
  },

  /** Admin: join a session as a read-only viewer; returns { url }. */
  async adminWatchSession(kind, sessionId) {
    const res = await fetch(`/api/admin/sessions/${encodeURIComponent(kind)}/${encodeURIComponent(sessionId)}/watch`, {
      method: 'POST',
      credentials: 'include',
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to watch the session')
    }
    return res.json()
  },

  /** Admin: terminate a session (owner and viewers are disconnected). */
  async adminTerminateSession(kind, sessionId, reason = '') {
    const res = await fetch(`/api/admin/sessions/${encodeURIComponent(kind)}/${encodeURIComponent(sessionId)}`, {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ reason }),
    })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to terminate the session')
    }
  },

  /** Access requests: groups the caller may ask for. */
  async accessRequestGroups() {
    const res = await fetch('/api/access-requests/groups', { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load groups')
    }
    return res.json()
  },

  /** Access requests: list (mine for users; all or by status for admins). */
  async accessRequests({ status = '', mine = false } = {}) {
    const q = new URLSearchParams()
    if (status) q.set('status', status)
    if (mine) q.set('mine', '1')
    const res = await fetch('/api/access-requests' + (q.toString() ? `?${q}` : ''), { credentials: 'include' })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load access requests')
    }
    return res.json()
  },

  async createAccessRequest({ group_id, reason, duration_seconds }) {
    const res = await fetch('/api/access-requests', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ group_id, reason: reason || '', duration_seconds: Number(duration_seconds) || 0 }),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to create the request')
    }
    return res.json()
  },

  async decideAccessRequest(id, decision, { duration_seconds, note } = {}) {
    const body = { note: note || '' }
    if (duration_seconds !== undefined && duration_seconds !== null && duration_seconds !== '') {
      body.duration_seconds = Number(duration_seconds)
    }
    const res = await fetch(`/api/access-requests/${encodeURIComponent(id)}/${decision}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to decide the request')
    }
    return res.json()
  },

  async cancelAccessRequest(id) {
    const res = await fetch(`/api/access-requests/${encodeURIComponent(id)}`, { method: 'DELETE', credentials: 'include' })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to cancel the request')
    }
  },

  /** グループにメンバーを追加（管理者のみ） */
  async addGroupMember(groupId, userId, expiresAt = '') {
    const res = await fetch(`/api/groups/${encodeURIComponent(groupId)}/members`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify({ user_id: userId, expires_at: expiresAt || '' }),
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

  /** Delete a recording (row + media file + derived exports). Admin only. */
  async deleteRecording(recordingId) {
    const res = await fetch(`/api/recordings/${encodeURIComponent(recordingId)}`, {
      method: 'DELETE',
      credentials: 'include',
    })
    if (!res.ok && res.status !== 204) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to delete recording')
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

  /** Queue GIF/MP4 generation for a recording. */
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
  /** @param {'terminal'|'vnc'|'rdp'} kind */
  sessionApiBase(kind = 'terminal') {
    const k = (kind || 'terminal').toLowerCase()
    if (k === 'vnc' || k === 'rdp') return `/api/${k}/sessions`
    return '/api/terminal/sessions'
  },

  /** 招待先のユーザー・グループ候補（セッションオーナー向け） */
  async sessionInvitationOptions(sessionId, { kind = 'terminal' } = {}) {
    const res = await fetch(
      `${API.sessionApiBase(kind)}/${encodeURIComponent(sessionId)}/invitation-options`,
      { credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load invitation options')
    }
    return res.json()
  },

  async createSessionInvitation(sessionId, options = {}, { kind = 'terminal' } = {}) {
    const res = await fetch(
      `${API.sessionApiBase(kind)}/${encodeURIComponent(sessionId)}/invitations`,
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
  async listSessionInvitations(sessionId, { kind = 'terminal' } = {}) {
    const res = await fetch(
      `${API.sessionApiBase(kind)}/${encodeURIComponent(sessionId)}/invitations`,
      { credentials: 'include', cache: 'no-store' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load invitations')
    }
    return res.json()
  },

  /** 招待を取消する */
  async revokeSessionInvitation(sessionId, invitationId, { kind = 'terminal' } = {}) {
    const res = await fetch(
      `${API.sessionApiBase(kind)}/${encodeURIComponent(sessionId)}/invitations/${encodeURIComponent(invitationId)}`,
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
  async regenerateSessionInvitationJoinUrl(sessionId, invitationId, { kind = 'terminal' } = {}) {
    const res = await fetch(
      `${API.sessionApiBase(kind)}/${encodeURIComponent(sessionId)}/invitations/${encodeURIComponent(invitationId)}/join-url`,
      { method: 'POST', credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to regenerate invitation link')
    }
    return res.json()
  },

  /** 招待トークンまたは招待 ID で参加する（SSH/Telnet） */
  async joinSession(sessionId, { invitationToken, invitationId } = {}) {
    return API._joinSession(`/api/terminal/sessions/${encodeURIComponent(sessionId)}/join`, { invitationToken, invitationId })
  },

  async joinVNCSession(sessionId, { invitationToken, invitationId } = {}) {
    return API._joinSession(`/api/vnc/sessions/${encodeURIComponent(sessionId)}/join`, { invitationToken, invitationId })
  },

  async joinRDPSession(sessionId, { invitationToken, invitationId } = {}) {
    return API._joinSession(`/api/rdp/sessions/${encodeURIComponent(sessionId)}/join`, { invitationToken, invitationId })
  },

  async _joinSession(url, { invitationToken, invitationId } = {}) {
    const body = {}
    if (invitationToken) body.invitation_token = invitationToken
    if (invitationId) body.invitation_id = invitationId
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'include',
      body: JSON.stringify(body),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to join session')
    }
    return res.json()
  },

  /** 参加者一覧と書込権限リクエスト一覧 */
  async listSessionParticipants(sessionId, { kind = 'terminal' } = {}) {
    const res = await fetch(
      `${API.sessionApiBase(kind)}/${encodeURIComponent(sessionId)}/participants`,
      { credentials: 'include' },
    )
    if (!res.ok) {
      const err = await res.json().catch(() => ({ message: res.statusText }))
      throw new Error(err.message || 'Failed to load participants')
    }
    return res.json()
  },

  /** 参加者をキックする（オーナー専用） */
  async kickSessionParticipant(sessionId, userId, { kind = 'terminal' } = {}) {
    const res = await fetch(
      `${API.sessionApiBase(kind)}/${encodeURIComponent(sessionId)}/participants/${encodeURIComponent(userId)}`,
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
