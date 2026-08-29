import { t as tr } from './i18n.js'

/** 組み込み TFTP 用の内部ターゲット（SSH/Telnet の TFTP トグルで自動作成）。一覧には出さず SSH 行の「ファイル」から操作する。 */
function isEmbeddedTftpCompanionTarget(t, targets) {
  if (t.protocol !== 'tftp' || !t.host) return false
  // 手動で追加された protocol=tftp ターゲットは補助ではないので隠さない。
  // 補助ターゲットは UI が `${name} (TFTP)` の形式で作る。
  const name = (t.name || '').trim()
  if (!name.endsWith(' (TFTP)')) return false
  return targets.some(
    (x) =>
      x.host === t.host &&
      (x.protocol === 'ssh' || x.protocol === 'telnet') &&
      x.tftp_enabled,
  )
}

/** 旧 UI が自動作成した FTP 補助ターゲット。一覧には出さず SSH 行の ftp_enabled で操作する。 */
function isEmbeddedFtpCompanionTarget(t, targets) {
  if (t.protocol !== 'ftp' || !t.host) return false
  // 手動で追加された protocol=ftp ターゲットは補助ではないので隠さない。
  // 補助ターゲットは UI が `${name} (FTP)` の形式で作っていた。
  const name = (t.name || '').trim()
  if (!name.endsWith(' (FTP)')) return false
  return targets.some(
    (x) =>
      x.host === t.host &&
      (x.protocol === 'ssh' || x.protocol === 'telnet') &&
      x.ftp_enabled,
  )
}

function renderHomeConnectButton(t, escapeHtml) {
  const disabledBtnClass =
    'connect-btn-in-group rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white shadow-sm transition-colors opacity-40 cursor-default w-[96px] text-center whitespace-nowrap'
  if (t.protocol === 'ssh' || t.protocol === 'telnet') {
    return `<button type="button" data-terminal-target-id="${escapeHtml(
      t.id,
    )}" data-terminal-protocol="${escapeHtml(t.protocol)}" data-terminal-target-name="${escapeHtml(t.name || '')}" data-has-stored-credentials="${
      t.has_stored_credentials ? '1' : ''
    }" data-needs-password="${t.needs_password ? '1' : ''}" data-needs-passphrase="${
      t.protocol === 'ssh' && t.needs_passphrase ? '1' : ''
    }" data-has-ssh-key="${t.protocol === 'ssh' && t.has_ssh_key ? '1' : ''}"
              class="connect-btn-in-group terminal-open-btn rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors disabled:opacity-50 w-[96px] text-center whitespace-nowrap">
              ${tr('targets.connectBtn')}
            </button>`
  }
  if (t.protocol === 'vnc') {
    return `<button type="button" data-popup-protocol="vnc" data-popup-target-id="${escapeHtml(
      t.id,
    )}" data-popup-target-name="${escapeHtml(t.name || '')}"
              class="connect-btn-in-group vnc-open-btn rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors inline-block w-[96px] text-center whitespace-nowrap">
              ${tr('targets.connectBtn')}
            </button>`
  }
  if (t.protocol === 'rdp') {
    return `<a href="/rdp?target_id=${encodeURIComponent(t.id)}&target_name=${encodeURIComponent(
      t.name || '',
    )}"
              target="_blank" rel="noopener noreferrer"
              data-rdp-target-id="${escapeHtml(t.id)}" data-rdp-target-name="${escapeHtml(t.name || '')}"
              class="connect-btn-in-group rdp-open-link rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors inline-block w-[96px] text-center whitespace-nowrap">
              ${tr('targets.connectBtn')}
            </a>`
  }
  return `<button type="button" disabled class="${disabledBtnClass}">${tr('targets.connectBtn')}</button>`
}

export function renderGroupTargetsTable(targets, mode = 'manage', escapeHtml, renderTagPills, currentGroupId = '') {
  const isManageMode = mode === 'manage'
  if (!targets || targets.length === 0) {
    return `<p class="text-sm text-slate-500">${tr('targets.emptyInGroup')}</p>`
  }
  const targetTags = (t) => (Array.isArray(t.tags) ? t.tags : [])
  // ホスト単位で TFTP/FTP ターゲット有無と、SSH/Telnet の機能フラグを管理する。
  const tftpActiveByHost = {}
  const tftpCapableByTargetId = {}
  targets.forEach((t) => {
    if (t.protocol === 'tftp' && t.host) {
      // ホームの TFTP トグルが作る補助ターゲットのみ ON として扱う。
      if (isEmbeddedTftpCompanionTarget(t, targets)) {
        tftpActiveByHost[t.host] = t
      }
    }
  })
  targets.forEach((t) => {
    if ((t.protocol === 'ssh' || t.protocol === 'telnet') && t.tftp_enabled) {
      tftpCapableByTargetId[t.id] = true
    }
  })
  // ホーム画面では補助ターゲットを隠すが、サーバー管理では全ターゲットを表示して管理できるようにする。
  const visibleTargets = isManageMode
    ? targets
    : targets.filter(
      (t) => !isEmbeddedTftpCompanionTarget(t, targets) && !isEmbeddedFtpCompanionTarget(t, targets),
    )
  const rows = visibleTargets
    .slice()
    .sort((a, b) => (a.name || '').localeCompare(b.name || ''))
    .map((t) => {
      const tags = targetTags(t)
      const isTftpCapableHost = !!tftpCapableByTargetId[t.id]
      const activeTftp = t.protocol === 'ssh' || t.protocol === 'telnet' ? tftpActiveByHost[t.host] || null : null
      const hasFtpEnabled =
        (t.protocol === 'ssh' || t.protocol === 'telnet') && !!t.ftp_enabled
      // SFTP 有効判定は DB の sftp_enabled のみを使用する（タグには依存しない）。
      const hasSftpEnabled = t.protocol !== 'ssh' ? true : t.sftp_enabled !== false
      const showFileBtn =
        ((t.protocol === 'ssh' || t.protocol === 'telnet') &&
          (((t.protocol === 'ssh' && hasSftpEnabled && (t.has_stored_credentials || t.has_ssh_key))) || hasFtpEnabled || activeTftp)) ||
        t.protocol === 'ftp' ||
        t.protocol === 'tftp'
      // ホーム画面では、「TFTP を使用するホスト」（サーバー管理で機能フラグ ON のホスト）のみトグルを表示する。
      // トグルがない行でも同じ幅のプレースホルダを表示しておき、横幅のガタつきを防ぐ。
      const tftpToggleHtml =
        !isManageMode && (t.protocol === 'ssh' || t.protocol === 'telnet') && isTftpCapableHost
          ? `<label class="group vantyx-switch inline-flex items-center justify-end gap-2 text-[11px] text-slate-600 mr-2 cursor-pointer w-[96px]">
                <input
                  type="checkbox"
                  class="tftp-toggle sr-only"
                  data-tftp-base-id="${escapeHtml(t.id)}"
                  data-tftp-host="${escapeHtml(t.host)}"
                  data-tftp-existing-id="${activeTftp ? escapeHtml(activeTftp.id) : ''}"
                  ${activeTftp ? 'checked' : ''} />
                <span class="vantyx-switch-track inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-[background-color,border-color] duration-200" aria-hidden="true">
                  <span class="vantyx-switch-thumb pointer-events-none inline-block h-4 w-4 shrink-0 translate-x-0.5 rounded-full transition-transform duration-200 group-has-[:checked]:translate-x-4" aria-hidden="true"></span>
                </span>
                <span class="vantyx-switch-label">TFTP</span>
              </label>`
          : '<span class="inline-block w-[96px] mr-2"></span>'
      return `
        <tr class="border-b border-slate-200 hover:bg-slate-50">
          <td class="px-4 py-2 text-sm text-slate-900 font-medium">${escapeHtml(t.name)}</td>
          <td class="px-4 py-2 text-sm text-slate-500">${escapeHtml(t.host)}:${t.port}</td>
          <td class="px-4 py-2 text-sm text-slate-500">${escapeHtml(t.protocol)}</td>
          ${
            isManageMode
              ? `<td class="px-4 py-2"><div class="flex flex-wrap items-center gap-2">${
                  tags.length ? renderTagPills(tags) : '<span class="text-xs text-slate-400">—</span>'
                }</div></td>`
              : ''
          }
          <td class="px-4 py-2 text-right">
            ${
              isManageMode
                ? `
            <div class="flex items-center justify-end gap-2">
              <button type="button" data-target-id="${escapeHtml(t.id)}" data-target-name="${escapeHtml(
                    t.name,
                  )}" data-target-host="${escapeHtml(t.host)}" data-target-port="${t.port}" data-target-protocol="${escapeHtml(
                    t.protocol || 'ssh',
                  )}" data-target-path="${escapeHtml(t.path || '')}" data-target-ssh-username="${escapeHtml(
                    t.ssh_username || '',
                  )}" data-target-tags="${escapeHtml(
                    (tags || []).join(','),
                  )}" data-target-has-ssh-key="${t.has_ssh_key ? '1' : '0'}" data-target-has-passphrase="${
                    t.has_passphrase ? '1' : '0'
                  }" data-target-needs-passphrase="${
                    t.needs_passphrase ? '1' : '0'
                  }" data-target-credential-identity-id="${escapeHtml(t.credential_identity_id || '')}" data-target-ssh-key-id="${escapeHtml(t.ssh_key_id || '')}" data-target-has-tftp-for-host="${activeTftp ? '1' : '0'}" data-target-tftp-id="${
                    activeTftp ? escapeHtml(activeTftp.id) : ''
                  }" data-target-sftp-enabled="${hasSftpEnabled ? '1' : '0'}" data-target-ftp-enabled="${
                    t.ftp_enabled ? '1' : '0'
                  }" data-target-tftp-enabled="${t.tftp_enabled ? '1' : '0'}" data-target-ssh-host-key-fp="${escapeHtml(t.ssh_host_key_fingerprint || '')}" data-target-group-id="${escapeHtml(currentGroupId)}"
                class="edit-btn-in-group rounded bg-slate-100 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-200 border border-slate-300 shadow-sm transition-colors w-[96px] text-center whitespace-nowrap">
              ${tr('targets.editBtn')}
            </button>
              <button type="button" data-target-id="${escapeHtml(t.id)}" data-target-name="${escapeHtml(t.name)}"
                class="delete-btn-in-group rounded border border-red-200 bg-white px-3 py-1.5 text-xs font-medium text-red-700 hover:bg-red-50 shadow-sm transition-colors w-[96px] text-center whitespace-nowrap">
                ${tr('targets.deleteBtn')}
              </button>
            </div>
            `
                : `
            <div class="flex items-center justify-end gap-2">
              ${tftpToggleHtml}
              <button
                type="button"
                class="files-open-btn rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 shadow-sm transition-colors w-[104px] text-center whitespace-nowrap ${
                  showFileBtn ? 'hover:bg-slate-50' : 'opacity-40 cursor-default'
                }"
                ${showFileBtn ? '' : 'disabled'}
                data-files-target-id="${escapeHtml(t.id)}"
                data-files-target-name="${escapeHtml(t.name || '')}"
                data-files-protocol="${escapeHtml(t.protocol)}"
                data-files-host="${escapeHtml(t.host)}"
                data-files-sftp-enabled="${hasSftpEnabled ? '1' : '0'}"
                data-files-ftp-enabled="${hasFtpEnabled ? '1' : '0'}"
                data-files-disabled="${showFileBtn ? '0' : '1'}"
              >${tr('targets.filesBtnLabel')}</button>
              ${renderHomeConnectButton(t, escapeHtml)}
              <button type="button" class="active-sessions-btn rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors w-[200px] text-center whitespace-nowrap" data-target-id="${escapeHtml(
                t.id,
              )}" data-target-name="${escapeHtml(t.name || '')}">${tr('targets.activeSessionsCount', { count: 0 })}</button>
            </div>
            `
            }
          </td>
        </tr>
      `
    })
    .join('')
  const theadTags = isManageMode ? `<th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[22%]">${tr('common.tags')}</th>` : ''
  return `
      <div class="overflow-x-auto">
        <table class="min-w-full text-left text-sm table-fixed">
          <thead class="bg-slate-50 border-b border-slate-200">
            <tr>
              <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[26%]">${tr('common.name')}</th>
              <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[28%]">${tr('common.host')}</th>
              <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[12%]">${tr('common.protocol')}</th>
              ${theadTags}
              <th class="px-4 py-2 w-[34%]"></th>
            </tr>
          </thead>
          <tbody>
            ${rows}
          </tbody>
        </table>
      </div>
    `
}

