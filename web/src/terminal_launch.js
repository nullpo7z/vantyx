/**
 * スタンドアロン画面（files 等）からターミナルタブを開く際の認証受け渡し。
 * app.js のホーム「接続」と同様に BroadcastChannel + use_stored_credentials を使う。
 */

export function randomToken() {
  const b = new Uint8Array(16)
  crypto.getRandomValues(b)
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
}

/** @param {string} url 絶対または相対 URL */
export function openTerminalTabWithParent(url) {
  const token = randomToken()
  const u = new URL(url, window.location.origin)
  u.searchParams.set('parent_token', token)

  const bc = new BroadcastChannel(`vantyx-terminal-parent-${token}`)
  const timeoutId = window.setTimeout(() => {
    try {
      bc.close()
    } catch {
      /* ignore */
    }
  }, 10 * 60 * 1000)
  bc.onmessage = (ev) => {
    if (ev?.data?.type !== 'focus') return
    try {
      window.focus()
    } catch {
      /* ignore */
    }
    window.clearTimeout(timeoutId)
    try {
      bc.close()
    } catch {
      /* ignore */
    }
  }

  window.open(u.toString(), '_blank', 'noopener')
}

/**
 * 保存済み認証でターミナルを開く。
 * @param {{
 *   targetId: string,
 *   targetName?: string,
 *   protocol?: string,
 *   needsPassword?: boolean,
 *   needsPassphrase?: boolean,
 *   sessionName?: string,
 *   sessionDesc?: string,
 *   password?: string,
 *   passphrase?: string,
 * }} opts
 */
export function openTerminalWithStoredAuth(opts) {
  const targetId = opts.targetId
  const targetName = opts.targetName || targetId
  const protocol = opts.protocol || 'ssh'
  const needsPassword = !!opts.needsPassword
  const needsPassphrase = !!opts.needsPassphrase
  const sessionName = opts.sessionName || ''
  const sessionDesc = opts.sessionDesc || ''
  const password = opts.password || ''
  const passphrase = opts.passphrase || ''

  const params = new URLSearchParams()
  params.set('target_id', targetId)
  params.set('target_name', targetName)
  params.set('protocol', protocol)
  params.set('use_stored_credentials', '1')
  if (needsPassword) params.set('needs_password', '1')
  if (needsPassphrase) params.set('needs_passphrase', '1')
  if (sessionName) params.set('session_name', sessionName)
  if (sessionDesc) params.set('session_description', sessionDesc)

  const needsExtra = needsPassword || needsPassphrase
  if (needsExtra && !password && !passphrase) {
    openTerminalTabWithParent(`/terminal?${params.toString()}`)
    return
  }

  const token = randomToken()
  params.set('channel', token)
  openTerminalTabWithParent(`/terminal?${params.toString()}`)

  const bc = new BroadcastChannel(`vantyx-terminal-${token}`)
  const timeoutId = window.setTimeout(() => {
    try {
      bc.close()
    } catch {
      /* ignore */
    }
  }, 15_000)
  bc.onmessage = (ev) => {
    if (ev?.data?.type !== 'ready') return
    if (ev?.data?.target_id !== targetId) return
    window.clearTimeout(timeoutId)
    const msg = {
      type: 'stored_credentials',
      password,
      name: sessionName,
      description: sessionDesc,
    }
    if (protocol !== 'telnet' && passphrase) {
      msg.private_key_passphrase = passphrase
    }
    try {
      bc.postMessage(msg)
    } finally {
      try {
        bc.close()
      } catch {
        /* ignore */
      }
    }
  }
}

/** @param {{ targetId: string, targetName?: string, protocol?: string }} opts */
export function openTerminalPlain(opts) {
  const params = new URLSearchParams()
  params.set('target_id', opts.targetId)
  params.set('target_name', opts.targetName || opts.targetId)
  params.set('protocol', opts.protocol || 'ssh')
  openTerminalTabWithParent(`/terminal?${params.toString()}`)
}

/**
 * ターゲット情報に応じてターミナルを開く（files 画面向け）。
 * @param {import('./api.js').default} API
 * @param {{ targetId: string, targetName?: string }} opts
 */
export async function openTerminalForTarget(API, opts) {
  const { targetId, targetName } = opts
  if (!targetId) return

  let target
  try {
    const res = await API.targets()
    const items = Array.isArray(res?.items) ? res.items : []
    target = items.find((t) => t.id === targetId) || null
  } catch (err) {
    alert(err.message || 'ターゲット情報の取得に失敗しました')
    return
  }

  const protocol = target?.protocol === 'telnet' ? 'telnet' : 'ssh'
  const name = target?.name || targetName || targetId

  if (target?.has_stored_credentials) {
    openTerminalWithStoredAuth({
      targetId,
      targetName: name,
      protocol,
      needsPassword: !!target.needs_password,
      needsPassphrase: !!target.needs_passphrase,
    })
    return
  }

  openTerminalPlain({ targetId, targetName: name, protocol })
}
