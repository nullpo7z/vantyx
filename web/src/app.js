import API from './api.js'
import { renderLogin } from './login.js'
import { applyStoredTheme, toggleStoredTheme } from './theme.js'
import { setLocale, onLocaleChange, t } from './i18n.js'
import { initNav, setActiveNav, showAuthenticatedNav } from './nav.js'
import { renderUsersPage } from './users_page.js'
import { renderRecordingsPage } from './recordings_page.js'
import { renderRecordingExportsPage } from './recording_exports_page.js'
import { renderSessionsPage } from './sessions_page.js'
import {
  buildGroupedSessionListHTML,
  bindSessionListActions,
  countIdleSessions,
} from './session_list_shared.js'
import { renderAccountPage } from './account_page.js'
import { formatDateTime } from './datetime.js'
import { renderSystemSettingsPage } from './system_settings_page.js'
import { renderAccessRequestsAdminPage, renderMyAccessRequests, showAccessRequestModal } from './access_requests.js'
import { renderAuditPage } from './audit_page.js'
import { renderGroupTargetsTable } from './targets_page.js'
import { renderCredentialsPage } from './credentials_page.js'
import {
  TFTP_CAPABILITY_TAG,
  SFTP_DISABLED_TAG,
  TREE_MAIN_CLASS,
} from './constants.js'
import { escapeHtml, renderTagPills, fillExistingTagsPicker, authMethodLabel, isSameOriginBroadcast } from './dom_helpers.js'
import {
  randomToken,
  openPopup,
  getRdpResolutionForTarget,
  setRdpResolutionForTarget,
} from './window_helpers.js'
import { buildAppShellHTML } from './header_template.js'
import { refreshIncomingInvitationsBanner } from './incoming_invitations.js'
import { uiAlert, uiChoose, uiConfirm } from './ui_dialog.js'
import {
  createRealtimeWatcher,
  POLL_MS,
  shouldRefreshIncomingInvitationsBanner,
  shouldRefreshSessionList,
} from './sharing_events.js'

function disconnectAppSessionEvents() {
  if (window.vantyxAppSharingUnsub) {
    try {
      window.vantyxAppSharingUnsub()
    } catch {
      /* ignore */
    }
    window.vantyxAppSharingUnsub = null
  }
  if (window.vantyxSessionEventSource) {
    try {
      window.vantyxSessionEventSource.close()
    } catch {
      /* ignore */
    }
    window.vantyxSessionEventSource = null
  }
}

function teardownGlobalRealtimeWatches() {
  if (typeof window.vantyxStopGlobalIncomingWatch === 'function') {
    try {
      window.vantyxStopGlobalIncomingWatch()
    } catch {
      /* ignore */
    }
    window.vantyxStopGlobalIncomingWatch = null
  }
  if (typeof window.vantyxStopGlobalSessionActivityWatch === 'function') {
    try {
      window.vantyxStopGlobalSessionActivityWatch()
    } catch {
      /* ignore */
    }
    window.vantyxStopGlobalSessionActivityWatch = null
  }
}

function setupGlobalRealtimeWatches() {
  teardownGlobalRealtimeWatches()

  window.vantyxStopGlobalIncomingWatch = createRealtimeWatcher({
    shouldRefresh: shouldRefreshIncomingInvitationsBanner,
    onRefresh: () => {
      const banner = document.getElementById('incoming-invitations-banner')
      if (!banner) return
      void refreshIncomingInvitationsBanner(banner, {
        onJoin: window.vantyxIncomingInviteOnJoin,
      })
    },
    pollMs: POLL_MS.incoming,
  })

  window.vantyxStopGlobalSessionActivityWatch = createRealtimeWatcher({
    shouldRefresh: shouldRefreshSessionList,
    onRefresh: () => {
      if (typeof window.vantyxUpdateActiveSessionCounts === 'function') {
        window.vantyxUpdateActiveSessionCounts()
      }
    },
    pollMs: POLL_MS.incoming,
  })
}

// Sentinel key for the tree's synthetic "root" node in an expanded-groups
// Set (real group IDs never contain this, they're limited to
// [a-zA-Z0-9_-] segments joined by "/").
const ROOT_TOGGLE_ID = '__root__'

export function renderApp(container) {
  // 画面遷移（renderApp 再呼び出し）時にも保存済みテーマを必ず適用し直す。
  applyStoredTheme()
  container.innerHTML = buildAppShellHTML()

  const mainContent = document.getElementById('main-content')
  const userNameEl = document.getElementById('user-name')
  const logoutBtn = document.getElementById('logout-btn')
  const navTargets = document.getElementById('nav-targets')
  const navSessions = document.getElementById('nav-sessions')
  const navRecordings = document.getElementById('nav-recordings')
  const navRecordingExports = document.getElementById('nav-recording-exports')
  const navGroups = document.getElementById('nav-groups')
  const navUsers = document.getElementById('nav-users')
  const navCredentials = document.getElementById('nav-credentials')
  const navAudit = document.getElementById('nav-audit')
  const navAccessRequests = document.getElementById('nav-access-requests')
  const navSystem = document.getElementById('nav-system')
  const navSettings = document.getElementById('nav-settings')

  let meData = null
  let groupsCache = null
  let selectedGroupId = ''
  const expandedGroups = new Set([ROOT_TOGGLE_ID])
  const recordingsExpandedGroups = new Set([ROOT_TOGGLE_ID])
  /** 録画ページ用: 選択中のグループID・ターゲットID（サーバー）・表示名 */
  let selectedRecordingsGroupId = ''
  let selectedRecordingsTargetId = ''
  let selectedRecordingsTargetName = ''
  let recordingsFilterFrom = ''
  let recordingsFilterTo = ''
  let recordingsFilterChannel = ''
  let recordingsFilterUserId = ''
  let stopRecordingExportsPoll = null
  /** 新しいタブに渡す SSH 認証情報（BroadcastChannel 用） */
  const pendingTerminalCreds = Object.create(null)
  /** ターミナルタブ用: 親タブへフォーカス要求するための待受（opener が無い環境向け） */
  const pendingTerminalParents = Object.create(null)

  function openTerminalTabWithParent(url) {
    const token = randomToken()
    const u = new URL(url, window.location.origin)
    u.searchParams.set('parent_token', token)

    const bc = new BroadcastChannel(`vantyx-terminal-parent-${token}`)
    pendingTerminalParents[token] = bc
    const timeoutId = window.setTimeout(() => {
      try { bc.close() } catch { /* ignore */ }
      delete pendingTerminalParents[token]
    }, 10 * 60 * 1000)
    bc.onmessage = (ev) => {
      if (ev?.data?.type !== 'focus') return
      try { window.focus() } catch { /* ignore */ }
      // If active sessions modal is open, refresh it on return.
      try {
        const m = document.getElementById('active-sessions-modal')
        if (m && !m.classList.contains('hidden') && typeof m._vantyxRefreshActiveSessions === 'function') {
          m._vantyxRefreshActiveSessions()
        }
        if (typeof window.vantyxUpdateActiveSessionCounts === 'function') {
          window.vantyxUpdateActiveSessionCounts()
        }
      } catch { /* ignore */ }
      window.clearTimeout(timeoutId)
      try { bc.close() } catch { /* ignore */ }
      delete pendingTerminalParents[token]
    }

    // ターミナルタブ側は parent_token (BroadcastChannel) で親タブへ戻れるため、opener は無効化する。
    window.open(u.toString(), '_blank', 'noopener')
  }

  // When this tab regains focus, refresh live session/invitation UI if visible.
  window.addEventListener('focus', () => {
    try {
      const m = document.getElementById('active-sessions-modal')
      if (m && !m.classList.contains('hidden') && typeof m._vantyxRefreshActiveSessions === 'function') {
        m._vantyxRefreshActiveSessions()
      }
      if (typeof window.vantyxUpdateActiveSessionCounts === 'function') {
        window.vantyxUpdateActiveSessionCounts()
      }
      const incomingBanner = document.getElementById('incoming-invitations-banner')
      if (incomingBanner) {
        void refreshIncomingInvitationsBanner(incomingBanner, {
          onJoin: window.vantyxIncomingInviteOnJoin,
        })
      }
    } catch { /* ignore */ }
  })

  function showUserInfo() {
    if (!meData) return
    showSettings()
  }

  async function showUsersPage() {
    disconnectAppSessionEvents()
    setActiveNav('users')
    await renderUsersPage({
      mainContent,
      escapeHtml,
      renderTagPills,
      fillExistingTagsPicker,
    })
  }

  async function showCredentialsPage() {
    disconnectAppSessionEvents()
    delete mainContent.dataset.treeMode
    setActiveNav('credentials')
    await renderCredentialsPage({ mainContent, escapeHtml })
  }

  async function showSessionsPage() {
    disconnectAppSessionEvents()
    delete mainContent.dataset.treeMode
    setActiveNav('sessions')
    const sessionEndModal = document.getElementById('session-end-modal')
    await renderSessionsPage({
      mainContent,
      escapeHtml,
      sessionEndModal,
      openTerminalTab: openTerminalTabWithParent,
      getRdpResolutionForTarget,
      isAdmin: !!(meData && meData.role === 'admin'),
    })
  }

  async function showRecordingExportsPage() {
    disconnectAppSessionEvents()
    delete mainContent.dataset.treeMode
    mainContent.className = TREE_MAIN_CLASS
    if (typeof stopRecordingExportsPoll === 'function') {
      stopRecordingExportsPoll()
      stopRecordingExportsPoll = null
    }
    setActiveNav('recordingExports')
    stopRecordingExportsPoll = await renderRecordingExportsPage({
      mainContent,
      escapeHtml,
      onBackToRecordings: () => showRecordingsPage(),
    })
  }

  async function showRecordingsPage() {
    disconnectAppSessionEvents()
    delete mainContent.dataset.treeMode
    mainContent.className = TREE_MAIN_CLASS
    if (typeof stopRecordingExportsPoll === 'function') {
      stopRecordingExportsPoll()
      stopRecordingExportsPoll = null
    }
    setActiveNav('recordings')
    await renderRecordingsPage({
      mainContent,
      meData,
      escapeHtml,
      buildGroupTree,
      renderGroupTree,
      ensureGroupPathExpanded,
      getGroupsCache: () => groupsCache,
      setGroupsCache: (v) => {
        groupsCache = v
      },
      expandedGroups: recordingsExpandedGroups,
      getState: () => ({
        groupId: selectedRecordingsGroupId,
        targetId: selectedRecordingsTargetId,
        targetName: selectedRecordingsTargetName,
        filterFrom: recordingsFilterFrom,
        filterTo: recordingsFilterTo,
        filterChannel: recordingsFilterChannel,
        filterUserId: recordingsFilterUserId,
      }),
      setState: (partial) => {
        if ('groupId' in partial) selectedRecordingsGroupId = partial.groupId
        if ('targetId' in partial) selectedRecordingsTargetId = partial.targetId
        if ('targetName' in partial) selectedRecordingsTargetName = partial.targetName
        if ('filterFrom' in partial) recordingsFilterFrom = partial.filterFrom
        if ('filterTo' in partial) recordingsFilterTo = partial.filterTo
        if ('filterChannel' in partial) recordingsFilterChannel = partial.filterChannel
        if ('filterUserId' in partial) recordingsFilterUserId = partial.filterUserId
      },
      refresh: () => showRecordingsPage(),
      onGoToExports: () => showRecordingExportsPage(),
    })
  }

  async function showAuditLogs() {
    disconnectAppSessionEvents()
    await renderAuditPage({ mainContent, meData, setActiveNav })
  }

  // Access requests (admin only): approve / deny membership requests.
  async function showAccessRequests() {
    if (!meData || meData.role !== 'admin') return
    disconnectAppSessionEvents()
    await renderAccessRequestsAdminPage(mainContent, { onDecided: () => { groupsCache = null } })
  }

  // System settings (admin only): server-wide knobs such as audit forwarding.
  async function showSystemSettings() {
    if (!meData || meData.role !== 'admin') return
    disconnectAppSessionEvents()
    await renderSystemSettingsPage(mainContent)
  }

  // Account settings: profile, language, password, 2FA and SSH keys.
  // Also opened from the user name in the header.
  async function showSettings() {
    disconnectAppSessionEvents()
    try {
      meData = await API.me()
      userNameEl.textContent = meData.username
    } catch {
      /* keep the cached meData */
    }
    await renderAccountPage(mainContent, { meData })
  }

  function showEditTagsModal({ type, id, label, currentTags, onSaved }) {
    const modal = document.getElementById('edit-tags-modal')
    modal.classList.remove('hidden')
    const tagsStr = (currentTags || []).join(', ')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.editTagsTitle', { label: escapeHtml(label) })}</h3>
            <button id="edit-tags-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="edit-tags-form">
            <div class="px-6 py-5 space-y-5">
              <p class="text-sm text-slate-600">${t('app.editTagsIntro')}</p>
              ${currentTags.length ? `<div class="flex flex-wrap gap-2">${renderTagPills(currentTags)}</div>` : ''}
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldTagsCsv')}</label>
                <input type="text" id="edit-tags-input" value="${escapeHtml(tagsStr)}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('app.placeholderTagsCsv')}" />
                <div id="edit-tags-input-picker" class="mt-2"></div>
              </div>
              <p id="edit-tags-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="edit-tags-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('app.cancel')}</button>
              <button type="submit" id="edit-tags-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('app.save')}</button>
            </div>
          </form>
        </div>
      </div>
    `
    const close = () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    fillExistingTagsPicker(modal, 'edit-tags-input')
    modal.querySelector('#edit-tags-close').addEventListener('click', close)
    modal.querySelector('#edit-tags-cancel').addEventListener('click', close)
    modal.querySelector('#edit-tags-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errorEl = modal.querySelector('#edit-tags-error')
      const submitBtn = modal.querySelector('#edit-tags-submit')
      const raw = modal.querySelector('#edit-tags-input').value.trim()
      const tags = raw ? raw.split(',').map((t) => t.trim()).filter(Boolean) : []
      errorEl.classList.add('hidden')
      // バックエンドの validateTag (1–64 文字、英数字・ハイフン・アンダースコア) と同等のチェック
      const tagPattern = /^[A-Za-z0-9_-]+$/
      for (const tag of tags) {
        if (!tag || tag.length > 64 || !tagPattern.test(tag)) {
          errorEl.textContent = t('app.tagsInvalid')
          errorEl.classList.remove('hidden')
          return
        }
      }
      submitBtn.disabled = true
      try {
        if (type === 'group') await API.setGroupTags(id, tags)
        else if (type === 'target') await API.setTargetTags(id, tags)
        else if (type === 'user') await API.setUserTags(id, tags)
        close()
        if (onSaved) await onSaved()
      } catch (err) {
        errorEl.textContent = err.message || t('app.saveFailed')
        errorEl.classList.remove('hidden')
      } finally {
        submitBtn.disabled = false
      }
    })
  }

  async function showAddMemberModal(groupId, currentMemberIds) {
    const modal = document.getElementById('add-member-modal')
    modal.classList.remove('hidden')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.addMemberTitle')}</h3>
            <button id="add-member-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="add-member-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldUser')}</label>
                <select id="add-member-user" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                  <option value="">${t('app.pickPlaceholder')}</option>
                </select>
              </div>
              <div>
                <label for="add-member-expires" class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldMemberExpires')}</label>
                <input type="datetime-local" id="add-member-expires" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" />
                <p class="mt-1 text-xs text-slate-500">${t('app.memberExpiresHint')}</p>
              </div>
              <p id="add-member-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="add-member-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('app.cancel')}</button>
              <button type="submit" id="add-member-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('app.add')}</button>
            </div>
          </form>
        </div>
      </div>
    `
    const close = () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#add-member-close').addEventListener('click', close)
    modal.querySelector('#add-member-cancel').addEventListener('click', close)
    const selectEl = modal.querySelector('#add-member-user')
    try {
      const users = await API.users()
      const existingSet = new Set(currentMemberIds || [])
      const toAdd = (users || []).filter((u) => !existingSet.has(u.id))
      toAdd.forEach((u) => {
        const opt = document.createElement('option')
        opt.value = u.id
        opt.textContent = `${u.username} (${u.id})`
        selectEl.appendChild(opt)
      })
      if (toAdd.length === 0) {
        selectEl.innerHTML = `<option value="">${t('app.noMoreUsers')}</option>`
        selectEl.disabled = true
      }
    } catch {
      selectEl.innerHTML = `<option value="">${t('app.fetchUsersFailed')}</option>`
      selectEl.disabled = true
    }
    modal.querySelector('#add-member-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errorEl = modal.querySelector('#add-member-error')
      const submitBtn = modal.querySelector('#add-member-submit')
      const userId = selectEl.value?.trim()
      if (!userId) return
      errorEl.classList.add('hidden')
      const expiresRaw = modal.querySelector('#add-member-expires').value
      let expiresAt = ''
      if (expiresRaw) {
        const d = new Date(expiresRaw)
        if (Number.isNaN(d.getTime()) || d.getTime() <= Date.now()) {
          errorEl.textContent = t('app.memberExpiresInvalid')
          errorEl.classList.remove('hidden')
          return
        }
        expiresAt = d.toISOString()
      }
      submitBtn.disabled = true
      try {
        await API.addGroupMember(groupId, userId, expiresAt)
        close()
        groupsCache = null
        await showTreeView('manage', true)
      } catch (err) {
        errorEl.textContent = err.message || t('app.addUserFailed')
        errorEl.classList.remove('hidden')
      } finally {
        submitBtn.disabled = false
      }
    })
  }

  function showAddGroupModal() {
    const modal = document.getElementById('add-target-modal')
    modal.classList.remove('hidden')
    const parentLabel = selectedGroupId || 'root'
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-xl mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.addGroupTitle')}</h3>
            <button id="add-group-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="add-group-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label for="add-group-name" class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldName')}</label>
                <input type="text" id="add-group-name" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('app.placeholderGroupName')}" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldParentGroup')}</label>
                <div class="text-sm text-slate-800 px-3 py-2 rounded border border-slate-200 bg-slate-50">
                  ${escapeHtml(parentLabel)}
                </div>
                <p class="text-xs text-slate-500 mt-1.5">${t('app.addGroupHint')}</p>
              </div>
              <p id="add-group-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="add-group-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('app.cancel')}</button>
              <button type="submit" id="add-group-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('app.add')}</button>
            </div>
          </form>
        </div>
      </div>
    `
    const close = () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#add-group-close').addEventListener('click', close)
    modal.querySelector('#add-group-cancel').addEventListener('click', close)
    modal.querySelector('#add-group-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errorEl = modal.querySelector('#add-group-error')
      const submitBtn = modal.querySelector('#add-group-submit')
      errorEl.classList.add('hidden')
      const name = modal.querySelector('#add-group-name').value.trim()
      const path = selectedGroupId || ''
      if (!name) return
      submitBtn.disabled = true
      try {
        await API.createGroup({ name, path })
        groupsCache = null
        close()
        await showTreeView('manage')
      } catch (err) {
        errorEl.textContent = err.message || t('app.addUserFailed')
        errorEl.classList.remove('hidden')
      } finally {
        submitBtn.disabled = false
      }
    })
  }

  function showEditGroupModal(group) {
    const modal = document.getElementById('add-target-modal')
    modal.classList.remove('hidden')
    const gid = (group?.id || '').trim()
    const currentName = (group?.name || '').trim()
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-xl mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.editGroupTitle')}</h3>
            <button id="edit-group-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="edit-group-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldParentGroup')}</label>
                <div class="text-sm text-slate-800 px-3 py-2 rounded border border-slate-200 bg-slate-50 font-mono">${escapeHtml(gid || '')}</div>
                <p class="text-xs text-slate-500 mt-1.5">${t('app.editGroupHint')}</p>
              </div>
              <div>
                <label for="edit-group-name" class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldName')}</label>
                <input type="text" id="edit-group-name" required value="${escapeHtml(currentName)}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" />
              </div>
              <p id="edit-group-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="edit-group-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('app.cancel')}</button>
              <button type="submit" id="edit-group-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('common.save')}</button>
            </div>
          </form>
        </div>
      </div>
    `
    const close = () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#edit-group-close')?.addEventListener('click', close)
    modal.querySelector('#edit-group-cancel')?.addEventListener('click', close)
    modal.querySelector('#edit-group-form')?.addEventListener('submit', async (e) => {
      e.preventDefault()
      const errorEl = modal.querySelector('#edit-group-error')
      const submitBtn = modal.querySelector('#edit-group-submit')
      errorEl.classList.add('hidden')
      const name = modal.querySelector('#edit-group-name').value.trim()
      if (!gid || !name) return
      submitBtn.disabled = true
      try {
        await API.updateGroup(gid, { name })
        groupsCache = null
        close()
        await showTreeView('manage', true)
      } catch (err) {
        errorEl.textContent = err.message || t('app.updateFailed')
        errorEl.classList.remove('hidden')
      } finally {
        submitBtn.disabled = false
      }
    })
  }

  async function showActiveSessionsModal(selectedGroupIdForModal, targetsForModal) {
    const modal = document.getElementById('active-sessions-modal')
    modal.classList.remove('hidden')
    const targetIdsInGroup = (targetsForModal || []).map((t) => t.id)
    const modalTitle = targetIdsInGroup.length === 1 && targetsForModal[0].name
      ? t('app.activeSessionsTitleOne', { name: escapeHtml(targetsForModal[0].name) })
      : t('app.activeSessionsTitleMany')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-2xl mx-4 overflow-hidden border border-slate-200/50 max-h-[90vh] flex flex-col">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50 shrink-0">
            <h3 class="font-semibold text-slate-800">${modalTitle}</h3>
            <button id="active-sessions-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <div id="active-sessions-body" class="px-5 py-4 overflow-y-auto flex-1 min-h-0">
            <p class="text-sm text-slate-500">${t('app.loading')}</p>
          </div>
        </div>
      </div>
    `
    const close = () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#active-sessions-close').addEventListener('click', close)
    const bodyEl = modal.querySelector('#active-sessions-body')
    const refresh = async () => {
      try {
        const [sessionsRes, rdpRes] = await Promise.all([API.terminalSessions(), API.rdpSessions()])
        const allSessions = sessionsRes.items || []
        const sessions = targetIdsInGroup.length > 0
          ? allSessions.filter((s) => targetIdsInGroup.includes(s.target_id))
          : allSessions
        const allRdp = rdpRes.items || []
        const rdpSessions = targetIdsInGroup.length > 0
          ? allRdp.filter((r) => targetIdsInGroup.includes(r.target_id))
          : allRdp
        const hasAny = sessions.length > 0 || rdpSessions.length > 0
        const idleN = countIdleSessions(sessions, rdpSessions)
        const viewAllLink = `<p class="mt-4 pt-3 border-t border-slate-200"><a href="#" id="active-sessions-view-all" class="text-sm font-medium text-sky-700 hover:text-sky-900">${t('app.viewAllSessions')}</a></p>`
        if (!hasAny) {
          const oneServer = targetIdsInGroup.length === 1
          bodyEl.innerHTML = (targetIdsInGroup.length > 0
            ? (oneServer
              ? `<p class="text-sm text-slate-500">${t('app.activeSessionsEmptyTarget')}</p>`
              : `<p class="text-sm text-slate-500">${t('app.activeSessionsEmptyGroup')}</p>`)
            : `<p class="text-sm text-slate-500">${t('app.activeSessionsEmptyAll')}</p>`) + viewAllLink
          bodyEl.querySelector('#active-sessions-view-all')?.addEventListener('click', (e) => {
            e.preventDefault()
            close()
            showSessionsPage()
          })
          return
        }
        const idleBanner = idleN > 0
          ? `<div class="mb-4 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900">${t('app.activeSessionsIdleWarn', { n: idleN })}</div>`
          : ''
        bodyEl.innerHTML = idleBanner + buildGroupedSessionListHTML(sessions, rdpSessions, escapeHtml) + viewAllLink
        bindSessionListActions(bodyEl, {
          openTerminalTab: openTerminalTabWithParent,
          getRdpResolutionForTarget,
          onEnded: refresh,
          escapeHtml,
          sessionEndModal: document.getElementById('session-end-modal'),
        })
        bodyEl.querySelector('#active-sessions-view-all')?.addEventListener('click', (e) => {
          e.preventDefault()
          close()
          showSessionsPage()
        })
      } catch {
        bodyEl.innerHTML = `<p class="text-sm text-red-600">${t('app.activeSessionsFetchFailed')}</p>`
      }
    }
    // Expose refresh hook for parent focus event
    modal._vantyxRefreshActiveSessions = refresh
    await refresh()
  }

  function showFileProtocolModal(opts) {
    const modal = document.getElementById('file-protocol-modal')
    if (!modal) return
    const title = opts?.title || t('app.fileProtocolTitleDefault')
    const targetName = opts?.targetName || ''
    const actions = Array.isArray(opts?.actions) ? opts.actions : []
    modal.classList.remove('hidden')
    modal.innerHTML = `
      <div id="file-protocol-backdrop" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <div class="min-w-0">
              <h3 class="font-semibold text-slate-800">${escapeHtml(title)}</h3>
              ${targetName ? `<p class="text-xs text-slate-500 mt-0.5 truncate">${escapeHtml(targetName)}</p>` : ''}
            </div>
            <button id="file-protocol-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <div class="px-5 py-4 space-y-2">
            ${actions.length
    ? actions.map((a, i) => `
              <button type="button" class="file-protocol-action w-full text-left rounded border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-800 hover:bg-slate-50 shadow-sm transition-colors" data-action-idx="${i}">
                ${escapeHtml(a.label || '')}
              </button>
            `).join('')
    : `<p class="text-sm text-slate-600">${t('app.fileProtocolNone')}</p>`}
          </div>
          <div class="px-5 py-3 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
            <button type="button" id="file-protocol-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('app.cancel')}</button>
          </div>
        </div>
      </div>
    `
    function close() {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#file-protocol-close')?.addEventListener('click', close)
    modal.querySelector('#file-protocol-cancel')?.addEventListener('click', close)
    modal.querySelector('#file-protocol-backdrop')?.addEventListener('click', (e) => {
      if (e.target && e.target.id === 'file-protocol-backdrop') close()
    })
    modal.querySelectorAll('.file-protocol-action').forEach((btn) => {
      btn.addEventListener('click', () => {
        const idx = parseInt(btn.dataset.actionIdx, 10)
        const action = Number.isFinite(idx) ? actions[idx] : null
        if (action && typeof action.onSelect === 'function') {
          action.onSelect()
        }
        close()
      })
    })
  }

  /** 保存済み認証のターゲット用: 接続前にモーダルで不足情報を入力させる。
   * パスワード認証・パスフレーズ無しの公開鍵・パスフレーズありで登録済みの場合はセッション名と説明のみ。
   * パスワード未登録のときはパスワード欄、パスフレーズ未登録のときはパスフレーズ欄を表示する。 */
  function showStoredCredentialModal(targetId, targetName, needsPassword, needsPassphrase, protocol = 'ssh', urlOpts = null) {
    const isTelnet = protocol === 'telnet'
    if (isTelnet) needsPassphrase = false
    const modal = document.getElementById('ssh-credential-modal')
    modal.classList.remove('hidden')
    const passwordBlock = needsPassword
      ? `
      <div>
        <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.sshCredsPasswordMissing', { auth: isTelnet ? 'Telnet' : 'SSH' })}</label>
        <input type="password" id="ssh-cred-password" autocomplete="current-password" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" />
      </div>`
      : ''
    const passphraseBlock = needsPassphrase
      ? `
      <div>
        <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.sshCredsPassphraseMissing')}</label>
        <input type="password" id="ssh-cred-passphrase" autocomplete="off" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('app.sshCredsPassphrasePlaceholder')}" />
      </div>`
      : ''
    const hasExtraFields = needsPassword || needsPassphrase
    const introText = hasExtraFields
      ? t('app.sshCredsHintExtra')
      : t('app.sshCredsHintStored')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.sshCredsConnectTitle', { name: escapeHtml(targetName || targetId) })}</h3>
            <button id="ssh-cred-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="ssh-cred-form">
            <div class="px-6 py-5 space-y-5">
              <p class="text-sm text-slate-600">${introText}</p>
              ${passwordBlock}
              ${passphraseBlock}
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldSessionName')}</label>
                <input type="text" id="ssh-cred-session-name" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('app.placeholderSessionName')}" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldSessionDesc')}</label>
                <input type="text" id="ssh-cred-session-desc" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('app.placeholderSessionDesc')}" />
              </div>
              <p id="ssh-cred-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="ssh-cred-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('app.cancel')}</button>
              <button type="submit" id="ssh-cred-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('app.connect')}</button>
            </div>
          </form>
        </div>
      </div>
    `
    const close = () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#ssh-cred-close').addEventListener('click', close)
    modal.querySelector('#ssh-cred-cancel').addEventListener('click', close)
    modal.querySelector('#ssh-cred-form').addEventListener('submit', (e) => {
      e.preventDefault()
      const errEl = modal.querySelector('#ssh-cred-error')
      const sessionName = (modal.querySelector('#ssh-cred-session-name').value || '').trim()
      const sessionDesc = (modal.querySelector('#ssh-cred-session-desc').value || '').trim()
      const password = modal.querySelector('#ssh-cred-password')?.value ?? ''
      const passphrase = modal.querySelector('#ssh-cred-passphrase')?.value ?? ''
      if (needsPassword && !password) {
        errEl.textContent = t('app.enterPassword')
        errEl.classList.remove('hidden')
        return
      }
      if (needsPassphrase && !passphrase) {
        errEl.textContent = t('app.enterPassphrase')
        errEl.classList.remove('hidden')
        return
      }
      errEl.classList.add('hidden')
      const params = new URLSearchParams()
      params.set('target_id', targetId)
      params.set('target_name', targetName || '')
      params.set('protocol', protocol)
      params.set('use_stored_credentials', '1')
      params.set('session_name', sessionName)
      params.set('session_description', sessionDesc)
      const token = randomToken()
      pendingTerminalCreds[token] = { targetId, targetName, protocol, password: password || '', passphrase: isTelnet ? '' : (passphrase || ''), sessionName, sessionDesc, useStoredCredentials: true }
      params.set('channel', token)
      // needs_password / needs_passphrase drive which fields the terminal
      // page's own retry form shows if the broadcast-supplied credentials
      // turn out to be wrong -- must be set for every launch path, not
      // just the one with a custom toUrl(), or a retry after a failed
      // login shows session name/description only with no way to fix
      // the password.
      params.set('needs_password', needsPassword ? '1' : '0')
      params.set('needs_passphrase', needsPassphrase ? '1' : '0')
      if (urlOpts?.toUrl) {
        openTerminalTabWithParent(urlOpts.toUrl(params))
      } else {
        openTerminalTabWithParent(`/terminal?${params.toString()}`)
      }

      // 新しいタブとは BroadcastChannel で不足分（パスワード/パスフレーズ）を受け渡しする（localStorageに保存しない）
      const bc = new BroadcastChannel(`vantyx-terminal-${token}`)
      const channelTargetId = urlOpts?.channelTargetId || targetId
      const timeoutId = window.setTimeout(() => {
        try { bc.close() } catch { /* ignore */ }
        delete pendingTerminalCreds[token]
      }, 15_000)
      bc.onmessage = (ev) => {
        if (!isSameOriginBroadcast(ev)) return
        if (ev?.data?.type !== 'ready') return
        if (ev?.data?.target_id !== channelTargetId) return
        const creds = pendingTerminalCreds[token]
        if (!creds) return
        try {
          const storedMsg = {
            type: 'stored_credentials',
            password: creds.password || '',
            name: creds.sessionName || '',
            description: creds.sessionDesc || '',
          }
          if (creds.protocol !== 'telnet' && creds.passphrase) {
            storedMsg.private_key_passphrase = creds.passphrase
          }
          bc.postMessage(storedMsg)
        } finally {
          window.clearTimeout(timeoutId)
          try { bc.close() } catch { /* ignore */ }
          delete pendingTerminalCreds[token]
        }
      }
      close()
    })
  }

  function showSSHCredentialModal(targetId, targetName, protocol = 'ssh', urlOpts = null, hasSshKey = false) {
    const isTelnet = protocol === 'telnet'
    const authLabel = isTelnet ? 'Telnet' : 'SSH'
    // The passphrase field only makes sense when the target already has
    // a private key stored server-side (no username, otherwise this
    // wouldn't be the no-stored-credentials modal) that this connection
    // could unlock. For a plain password-only target it's just
    // confusing noise, so hide it unless has_ssh_key says a key exists.
    const showPassphraseField = !isTelnet && hasSshKey
    const modal = document.getElementById('ssh-credential-modal')
    modal.classList.remove('hidden')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.sshCredsConnectTitle', { name: escapeHtml(targetName || targetId) })}</h3>
            <button id="ssh-cred-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="ssh-cred-form">
            <div class="px-6 py-5 space-y-5">
              <p class="text-sm text-slate-600">${t('app.newConnIntro')}</p>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldAuthUsername', { auth: authLabel })}</label>
                <input type="text" id="ssh-cred-username" autocomplete="username" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('app.placeholderRoot')}" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldAuthPassword', { auth: authLabel })}</label>
                <input type="password" id="ssh-cred-password" autocomplete="current-password" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" />
              </div>
              ${showPassphraseField ? `<div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldPassphraseOpt')}</label>
                <input type="password" id="ssh-cred-passphrase" autocomplete="off" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('app.placeholderPassphraseOpt')}" />
              </div>` : ''}
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldSessionName')}</label>
                <input type="text" id="ssh-cred-session-name" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('app.placeholderSessionName')}" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldSessionDesc')}</label>
                <input type="text" id="ssh-cred-session-desc" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('app.placeholderSessionDescNew')}" />
              </div>
              <p id="ssh-cred-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="ssh-cred-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('app.cancel')}</button>
              <button type="submit" id="ssh-cred-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('app.connect')}</button>
            </div>
          </form>
        </div>
      </div>
    `
    const close = () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#ssh-cred-close').addEventListener('click', close)
    modal.querySelector('#ssh-cred-cancel').addEventListener('click', close)
    modal.querySelector('#ssh-cred-form').addEventListener('submit', (e) => {
      e.preventDefault()
      const errorEl = modal.querySelector('#ssh-cred-error')
      const username = modal.querySelector('#ssh-cred-username').value.trim()
      const password = modal.querySelector('#ssh-cred-password').value
      const passphrase = (modal.querySelector('#ssh-cred-passphrase')?.value ?? '').trim()
      const sessionName = modal.querySelector('#ssh-cred-session-name').value.trim()
      const sessionDesc = modal.querySelector('#ssh-cred-session-desc').value.trim()
      if (!username) {
        errorEl.textContent = t('app.usernameRequired')
        errorEl.classList.remove('hidden')
        return
      }
      const token = randomToken()
      pendingTerminalCreds[token] = { targetId, targetName, protocol, username, password, passphrase: isTelnet ? '' : passphrase, sessionName, sessionDesc }
      let url
      if (urlOpts?.toUrl) {
        const p = new URLSearchParams()
        p.set('channel', token)
        p.set('session_name', sessionName)
        p.set('session_description', sessionDesc)
        url = urlOpts.toUrl(p)
      } else {
        url = `/terminal?target_id=${encodeURIComponent(targetId)}&target_name=${encodeURIComponent(targetName || '')}&protocol=${encodeURIComponent(protocol)}&channel=${encodeURIComponent(token)}&has_ssh_key=${hasSshKey ? '1' : '0'}`
      }
      // NOTE: ターミナルの「戻る/セッション終了」で元タブに戻れるよう、opener を残す（noreferrer/noopener は付けない）
      openTerminalTabWithParent(url)

      // 新しいタブとは BroadcastChannel で認証情報を受け渡しする
      const bc = new BroadcastChannel(`vantyx-terminal-${token}`)
      const channelTargetId = urlOpts?.channelTargetId || targetId
      const timeoutId = window.setTimeout(() => {
        try { bc.close() } catch { /* ignore */ }
        delete pendingTerminalCreds[token]
      }, 15_000)
      bc.onmessage = (ev) => {
        if (!isSameOriginBroadcast(ev)) return
        if (ev?.data?.type !== 'ready') return
        if (ev?.data?.target_id !== channelTargetId) return
        const creds = pendingTerminalCreds[token]
        if (!creds) return
        try {
          const credMsg = {
            type: 'credentials',
            username: creds.username,
            password: creds.password,
            name: creds.sessionName || '',
            description: creds.sessionDesc || '',
          }
          if (creds.protocol !== 'telnet' && creds.passphrase) {
            credMsg.private_key_passphrase = creds.passphrase
          }
          bc.postMessage(credMsg)
        } finally {
          window.clearTimeout(timeoutId)
          try { bc.close() } catch { /* ignore */ }
          delete pendingTerminalCreds[token]
        }
      }
      close()
    })
  }

  async function showTreeView(mode = 'manage', useCache = false) {
    const isAdminRole = meData?.role === 'admin'
    // 非管理者は manage モードに入れない（サーバー管理はサーバー管理者専用）。
    // URL や履歴から到達した場合もホームに降格する。
    if (mode === 'manage' && !isAdminRole) {
      mode = 'home'
    }
    const isManageMode = mode === 'manage'
    const pageTitle = isManageMode ? t('app.pageManage') : t('app.pageHome')
    setActiveNav(isManageMode ? 'groups' : 'targets')
    mainContent.className = TREE_MAIN_CLASS

    const currentModeIndicator = mainContent.dataset.treeMode
    const isSameMode = currentModeIndicator === mode
    const treeContainer = mainContent.querySelector('aside .overflow-y-auto')
    const scrollPos = treeContainer ? treeContainer.scrollTop : 0

    if (!isSameMode && !useCache) {
      mainContent.innerHTML = `<div class="w-full flex-1 flex items-center justify-center"><p class="text-slate-500">${t('app.loading')}</p></div>`
    }

    try {
      if (mode === 'manage') {
        disconnectAppSessionEvents()
      }
      if (!useCache || !groupsCache) {
        groupsCache = await API.groups()
      }
      const groups = groupsCache
      // Ensure renderGroupTree sees the current mode (used for showing group actions).
      mainContent.dataset.treeMode = mode
      const treeRoot = buildGroupTree(groups || [])
      const treeHtml = renderGroupTree(treeRoot, selectedGroupId, 0, expandedGroups)
      const selectedGroup = (groups || []).find((g) => g.id === selectedGroupId)
      const targets = selectedGroup ? (selectedGroup.targets || []) : []
      const label = selectedGroupId || 'root'

      const addGroupBtnHtml = isManageMode
        ? `<button type="button" id="btn-add-group" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('app.addBtn')}</button>`
        : ''

      const addTargetBtnHtml = isManageMode
        ? `<button type="button" id="btn-add-target-in-group" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors disabled:opacity-50" ${selectedGroupId ? '' : 'disabled'}>${t('app.addTargetBtn')}</button>`
        : `<button type="button" id="btn-request-access" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('access.requestBtn')}</button>`
      const showMembersSection = isManageMode && isAdminRole && selectedGroupId

      mainContent.innerHTML = `
        <div class="w-full h-full flex flex-col gap-4">
          ${!isManageMode ? '<section id="incoming-invitations-banner" class="hidden w-full bg-white rounded-lg shadow-sm border border-slate-200 px-6 py-4 space-y-3"></section>' : ''}
          ${!isManageMode ? '<section id="my-access-requests" class="hidden w-full bg-white rounded-lg shadow-sm border border-slate-200 px-6 py-3 space-y-2"></section>' : ''}
          <div class="flex gap-6 w-full flex-1 min-h-0">
            <aside class="w-80 flex-col border-r border-slate-200 bg-white shadow-sm shrink-0 rounded-lg overflow-hidden flex">
              <div class="px-4 py-3 border-b border-slate-200 text-sm font-semibold text-slate-700 flex items-center justify-between">
                <span>${t('app.accessGroupsTitle')}</span>
                ${addGroupBtnHtml}
              </div>
              <div class="px-3 py-3 text-xs text-slate-800 overflow-y-auto flex-1 min-h-0">
                ${treeHtml || `<p class="text-slate-500 p-2">${t('app.noGroups')}</p>`}
              </div>
            </aside>
            <div class="flex-1 flex flex-col gap-4 min-h-0">
              ${!isManageMode ? '<div id="idle-sessions-banner" class="hidden rounded-lg border border-amber-200 bg-amber-50 px-5 py-4 text-sm text-amber-900"></div>' : ''}
              <section class="flex-1 bg-white rounded-lg shadow-sm border border-slate-200 overflow-hidden flex flex-col min-h-0">
                <div class="px-5 py-3 border-b border-slate-200 flex items-center justify-between bg-slate-50">
                  <div>
                    <h2 class="text-sm font-semibold text-slate-800">${escapeHtml(label)}</h2>
                  </div>
                  <div class="flex items-center gap-2">
                    <span class="text-xs text-slate-500">${t('app.targetsSuffix', { n: targets.length })}</span>
                    ${addTargetBtnHtml}
                  </div>
                </div>
                <div class="px-5 py-4">
                  ${renderGroupTargetsTable(targets, mode, escapeHtml, renderTagPills, selectedGroupId)}
                  ${showMembersSection ? `<div id="group-members-container" class="mt-6 border-t border-slate-200 pt-4"><p class="text-slate-500">${t('app.loading')}</p></div>` : ''}
                </div>
              </section>
            </div>
          </div>
        </div>
      `
      const newTreeContainer = mainContent.querySelector('aside .overflow-y-auto')
      if (newTreeContainer && scrollPos > 0) {
        newTreeContainer.scrollTop = scrollPos
      }

      if (showMembersSection) {
        const membersContainer = mainContent.querySelector('#group-members-container')
        if (membersContainer) {
          const thisGroupId = selectedGroupId
          Promise.all([API.groupMembers(thisGroupId), API.groupTags(thisGroupId).catch(() => ({ tags: [] }))])
            .then(([members, tagsRes]) => {
              // 別のグループに切り替わっていれば、この結果は破棄する
              if (thisGroupId !== selectedGroupId) {
                return
              }
              const memberIds = (members || []).map((m) => m.id)
              const groupTags = (tagsRes && tagsRes.tags) ? tagsRes.tags : []
              const tagsHtml = `
                <div class="mb-4 flex items-center justify-between flex-wrap gap-2">
                  <div>
                    <h3 class="text-sm font-semibold text-slate-700 mb-1">${t('app.tagsHeading')}</h3>
                    <p class="text-xs text-slate-500">${t('app.tagsHint')}</p>
                    <div class="flex flex-wrap gap-2 mt-2">
                      ${groupTags.length ? renderTagPills(groupTags) : `<span class="text-xs text-slate-400">${t('app.noTags')}</span>`}
                    </div>
                  </div>
                  <button type="button" id="btn-edit-group-tags" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('app.editTagsBtn')}</button>
                </div>
              `
              const rows = (members || []).map((m) => `
                <tr class="border-b border-slate-200 hover:bg-slate-50${m.expired ? ' opacity-60' : ''}">
                  <td class="px-4 py-2 text-sm font-medium text-slate-900">${escapeHtml(m.id)}</td>
                  <td class="px-4 py-2 text-sm text-slate-700">${escapeHtml(m.username)}</td>
                  <td class="px-4 py-2 text-sm text-slate-600 whitespace-nowrap">${
                    m.expires_at
                      ? `${escapeHtml(formatDateTime(m.expires_at))}${m.expired ? ` <span class="ml-1 inline-flex items-center rounded-full bg-red-100 px-2 py-0.5 text-xs font-medium text-red-700">${t('app.memberExpired')}</span>` : ''}`
                      : `<span class="text-slate-400">${t('app.memberNoExpiry')}</span>`
                  }</td>
                  <td class="px-4 py-2 text-right">
                    <button type="button" class="remove-member-btn rounded border border-red-200 bg-white px-3 py-1.5 text-xs font-medium text-red-700 hover:bg-red-50" data-user-id="${escapeHtml(m.id)}">${t('app.delete')}</button>
                  </td>
                </tr>
              `).join('')
              membersContainer.innerHTML = `
                ${tagsHtml}
                <h3 class="text-sm font-semibold text-slate-700 mb-2">${t('app.membersHeading')}</h3>
                <div class="flex items-center justify-between mb-2">
                  <span class="text-xs text-slate-500">${t('app.membersCount', { n: memberIds.length })}</span>
                  <button type="button" id="btn-add-member" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('app.addMember')}</button>
                </div>
                <div class="overflow-x-auto border border-slate-200 rounded-lg">
                  <table class="min-w-full text-left text-sm">
                    <thead class="bg-slate-50 border-b border-slate-200">
                      <tr>
                        <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('app.memberHeaderId')}</th>
                        <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('app.memberHeaderName')}</th>
                        <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('app.memberHeaderExpires')}</th>
                        <th class="px-4 py-2"></th>
                      </tr>
                    </thead>
                    <tbody>${rows || `<tr><td colspan="4" class="px-4 py-4 text-center text-slate-500">${t('app.noMembers')}</td></tr>`}</tbody>
                  </table>
                </div>
              `
              membersContainer.querySelector('#btn-edit-group-tags')?.addEventListener('click', async () => {
                const tagsRes = await API.groupTags(selectedGroupId).catch(() => ({ tags: [] }))
                const currentTags = (tagsRes && tagsRes.tags) ? tagsRes.tags : []
                showEditTagsModal({
                  type: 'group',
                  id: selectedGroupId,
                  label: selectedGroupId,
                  currentTags,
                  onSaved: () => showTreeView('manage', true),
                })
              })
              membersContainer.querySelector('#btn-add-member')?.addEventListener('click', () => showAddMemberModal(selectedGroupId, memberIds))
              membersContainer.querySelectorAll('.remove-member-btn').forEach((btn) => {
                btn.addEventListener('click', async () => {
                  const uid = btn.dataset.userId || ''
                  if (!uid) return
                  if (!(await uiConfirm(t('app.confirmRemoveMember', { user: escapeHtml(uid) }), { danger: true }))) return
                  try {
                    await API.removeGroupMember(selectedGroupId, uid)
                    groupsCache = null
                    await showTreeView('manage', true)
                  } catch (err) {
                    await uiAlert(err.message || t('app.deleteFailed'))
                  }
                })
              })
            })
            .catch((err) => {
              // 既に別のグループを表示している場合、このエラーは無視する
              if (thisGroupId !== selectedGroupId) {
                return
              }
              // 認証エラーや一時的な通信エラーなどはコンソールにのみ出し、画面には控えめに表示
              console.error('Failed to load group members', err)
              membersContainer.innerHTML = `<p class="text-sm text-red-600">${t('app.membersFetchFailed')}</p>`
            })
        }
      }

      if (isManageMode) {
        mainContent.querySelector('#btn-add-group')?.addEventListener('click', showAddGroupModal)
        mainContent.querySelector('#btn-request-access')?.addEventListener('click', () => {
          showAccessRequestModal({
            onCreated: () => renderMyAccessRequests(mainContent.querySelector('#my-access-requests')),
          })
        })
        if (!isManageMode) {
          renderMyAccessRequests(mainContent.querySelector('#my-access-requests'), {
            onChanged: () => { groupsCache = null },
          })
        }
        mainContent.querySelector('#btn-add-target-in-group')?.addEventListener('click', () => {
          if (!selectedGroupId) return
          showAddTargetModal()
        })
        mainContent.querySelectorAll('.edit-btn-in-group').forEach((btn) => {
          btn.addEventListener('click', () => {
            const target = {
              id: btn.dataset.targetId || '',
              name: btn.dataset.targetName || '',
              host: btn.dataset.targetHost || '',
              port: parseInt(btn.dataset.targetPort, 10) || 22,
              protocol: btn.dataset.targetProtocol || 'ssh',
              path: btn.dataset.targetPath || '',
              ssh_username: btn.dataset.targetSshUsername || '',
              tags: (btn.dataset.targetTags || '').split(',').map((s) => s.trim()).filter(Boolean),
              has_ssh_key: btn.dataset.targetHasSshKey === '1',
              has_passphrase: btn.dataset.targetHasPassphrase === '1',
              needs_passphrase: btn.dataset.targetNeedsPassphrase === '1',
              credential_identity_id: btn.dataset.targetCredentialIdentityId || '',
              ssh_key_id: btn.dataset.targetSshKeyId || '',
              group_id: btn.dataset.targetGroupId || '',
              has_tftp_for_host: btn.dataset.targetHasTftpForHost === '1',
              tftp_target_id: btn.dataset.targetTftpId || '',
              sftp_enabled: btn.dataset.targetSftpEnabled === '1',
              ftp_enabled: btn.dataset.targetFtpEnabled === '1',
              tftp_enabled: btn.dataset.targetTftpEnabled === '1',
              ssh_host_key_fingerprint: btn.dataset.targetSshHostKeyFp || '',
            }
            if (target.id) showEditTargetModal(target)
          })
        })
        mainContent.querySelectorAll('.delete-btn-in-group').forEach((btn) => {
          btn.addEventListener('click', async () => {
            const targetId = btn.dataset.targetId || ''
            const targetName = btn.dataset.targetName || ''
            if (!targetId) return
            if (!(await uiConfirm(t('app.confirmDeleteTarget', { name: escapeHtml(targetName) || targetId }), { danger: true }))) return
            try {
              await API.deleteTarget(targetId)
              groupsCache = null
              await showTreeView('manage')
            } catch (err) {
              await uiAlert(err.message || t('app.deleteFailed'))
            }
          })
        })
      } else {
        // アクティブなセッション数は SSE でサーバーから通知を受けて更新（ポーリングなし）。
        const updateActiveSessionCounts = async () => {
          try {
            const [sessionsRes, rdpRes] = await Promise.all([API.terminalSessions(), API.rdpSessions()])
            const termItems = Array.isArray(sessionsRes.items) ? sessionsRes.items : []
            const rdpItems = Array.isArray(rdpRes.items) ? rdpRes.items : []
            const counts = {}
            const idleByTarget = {}
            termItems.forEach((s) => {
              const tid = s.target_id
              if (!tid) return
              counts[tid] = (counts[tid] || 0) + 1
              if (s.idle) idleByTarget[tid] = true
            })
            rdpItems.forEach((s) => {
              const tid = s.target_id
              if (!tid) return
              counts[tid] = (counts[tid] || 0) + 1
              if (s.idle) idleByTarget[tid] = true
            })
            const totalIdle = countIdleSessions(termItems, rdpItems)
            const idleBanner = mainContent.querySelector('#idle-sessions-banner')
            if (idleBanner) {
              if (totalIdle > 0) {
                idleBanner.classList.remove('hidden')
                idleBanner.innerHTML = t('app.idleBanner', { n: totalIdle })
                idleBanner.querySelector('#idle-banner-sessions-link')?.addEventListener('click', (e) => {
                  e.preventDefault()
                  showSessionsPage()
                })
              } else {
                idleBanner.classList.add('hidden')
              }
            }
            mainContent.querySelectorAll('.active-sessions-btn').forEach((btn) => {
              const targetId = btn.dataset.targetId || ''
              const baseLabel = t('app.activeSessionsLabel')
              const n = targetId ? (counts[targetId] || 0) : 0
              const idleMark = idleByTarget[targetId] ? ' ⚠' : ''
              btn.textContent = `${baseLabel} (${n})${idleMark}`
              if (idleByTarget[targetId]) {
                btn.classList.add('border-amber-300', 'bg-amber-50')
              } else {
                btn.classList.remove('border-amber-300', 'bg-amber-50')
              }
            })
            // アクティブセッション modal が開いていれば中身も更新
            const m = document.getElementById('active-sessions-modal')
            if (m && !m.classList.contains('hidden') && typeof m._vantyxRefreshActiveSessions === 'function') {
              m._vantyxRefreshActiveSessions()
            }
          } catch (e) {
            if (e && e.message !== 'Unauthorized' && e.message !== 'unauthorized') {
              console.error('Failed to load active session counts', e)
            }
          }
        }
        window.vantyxUpdateActiveSessionCounts = updateActiveSessionCounts
        const incomingBanner = mainContent.querySelector('#incoming-invitations-banner')
        window.vantyxIncomingInviteOnJoin = ({ session_id: sid, target_id: tid }) => {
          const params = new URLSearchParams()
          params.set('session_id', sid)
          params.set('mode', 'viewer')
          if (tid) params.set('target_id', tid)
          openTerminalTabWithParent(`/terminal?${params.toString()}`)
        }
        const refreshIncomingBanner = () => {
          if (!incomingBanner) return
          return refreshIncomingInvitationsBanner(incomingBanner, {
            onJoin: window.vantyxIncomingInviteOnJoin,
          })
        }
        updateActiveSessionCounts()
        refreshIncomingBanner()
        mainContent.querySelectorAll('.active-sessions-btn').forEach((btn) => {
          btn.addEventListener('click', (e) => {
            e.preventDefault()
            const targetId = btn.dataset.targetId || ''
            const targetName = btn.dataset.targetName || ''
            showActiveSessionsModal(selectedGroupId, targetId ? [{ id: targetId, name: targetName }] : targets)
          })
        })
        // TFTP 有効化トグル（ホーム画面）: TFTP サーバーの起動/停止用に TFTP ターゲットを作成/削除する。
        mainContent.querySelectorAll('.tftp-toggle').forEach((chk) => {
          chk.addEventListener('change', async () => {
            const host = chk.dataset.tftpHost || ''
            const baseId = chk.dataset.tftpBaseId || ''
            const existingId = chk.dataset.tftpExistingId || ''
            if (!host || !baseId) return
            chk.disabled = true
            try {
              if (chk.checked) {
                // 既に TFTP ターゲット（サーバー起動中）がある場合は何もしない。
                if (existingId) return
                const base = targets.find((t) => t.id === baseId)
                if (!base) return
                const name = `${base.name || base.id} (TFTP)`
                const createdTftp = await API.createTarget({
                  name,
                  host: base.host,
                  port: 69,
                  protocol: 'tftp',
                  group_id: selectedGroupId || '',
                  ssh_username: '',
                })
                if (createdTftp && createdTftp.id) {
                  chk.dataset.tftpExistingId = createdTftp.id
                }
              } else {
                // OFF にする前に、この TFTP ターゲットに紐づくアクティブな TFTP セッションが無いか確認する。
                if (existingId) {
                  try {
                    const sessionsRes = await API.terminalSessions()
                    const items = Array.isArray(sessionsRes.items) ? sessionsRes.items : []
                    const hasTftpSession = items.some((s) => {
                      const desc = s.description || ''
                      return typeof desc === 'string'
                        && desc.startsWith('TFTP_CONSOLE:')
                        && desc.includes(`tftp_target_id=${existingId}`)
                    })
                    if (hasTftpSession) {
                      // アクティブな TFTP セッションがある場合は OFF にできない。
                      await uiAlert(t('app.tftpHasActiveAlert'))
                      chk.checked = true
                      return
                    }
                  } catch (e) {
                    console.error('Failed to check TFTP sessions', e)
                    // セッション状態が確認できない場合は、安全のため OFF を拒否する。
                    await uiAlert(t('app.tftpCheckFailed'))
                    chk.checked = true
                    return
                  }
                  // OFF にした場合は紐づく TFTP ターゲットを削除（サーバー停止）。
                  const tftpId = existingId
                  if (tftpId) {
                    await API.deleteTarget(tftpId)
                    chk.dataset.tftpExistingId = ''
                  }
                }
              }
              // ツリーを再読み込みして表示と状態を同期。
              groupsCache = null
              await showTreeView(mode)
            } catch (err) {
              console.error('Failed to toggle TFTP target', err)
            } finally {
              chk.disabled = false
            }
          })
        })
        // SSH: 認証情報が保存済みならタブを直接開き、未保存なら認証モーダル表示。
        mainContent.querySelectorAll('.terminal-open-btn').forEach((btn) => {
          btn.addEventListener('click', (e) => {
            e.preventDefault()
            const id = btn.dataset.terminalTargetId || ''
            const name = btn.dataset.terminalTargetName || ''
            const protocol = btn.dataset.terminalProtocol || 'ssh'
            if (!id) return
            if (btn.dataset.hasStoredCredentials) {
              showStoredCredentialModal(id, name, btn.dataset.needsPassword === '1', btn.dataset.needsPassphrase === '1', protocol)
            } else {
              showSSHCredentialModal(id, name, protocol, null, btn.dataset.hasSshKey === '1')
            }
          })
        })
        // VNC: ポップアップで開く（専用ウィンドウ）
        mainContent.querySelectorAll('button[data-popup-protocol]').forEach((btn) => {
          btn.addEventListener('click', (e) => {
            e.preventDefault()
            const protocol = btn.dataset.popupProtocol || ''
            const id = btn.dataset.popupTargetId || ''
            const name = btn.dataset.popupTargetName || ''
            if (!protocol || !id) return
            const u = `/${protocol}?target_id=${encodeURIComponent(id)}&target_name=${encodeURIComponent(name || '')}`
            const title = `Vantyx VNC - ${name || id}`
            openPopup(u, title, 1400, 900)
          })
        })

        // RDP: 解像度設定（ローカル保存）に基づき、新しいタブで開く（親タブへ戻れるよう parent_token 付きで開く）
        mainContent.querySelectorAll('.rdp-open-link').forEach((link) => {
          link.addEventListener('click', (e) => {
            e.preventDefault()
            const id = link.dataset.rdpTargetId || ''
            const { w, h } = getRdpResolutionForTarget(id)
            const u = new URL(link.href, window.location.origin)
            if (w) u.searchParams.set('rw', String(w))
            if (h) u.searchParams.set('rh', String(h))
            openTerminalTabWithParent(u.toString())
          })
        })

        // 未対応プロトコル (tftp/ftp 等) はアラート表示。
        mainContent.querySelectorAll('.connect-btn-in-group:not(.terminal-open-btn):not(.vnc-open-btn):not([data-popup-protocol]):not(a)').forEach((btn) => {
          btn.addEventListener('click', () => {
            void uiAlert(t('app.unsupportedProtoAlert'))
          })
        })
      }

      // ファイルボタン（ホーム・サーバー管理の両方）: SFTP/FTP/TFTP（+コンソール）を開く。
      mainContent.querySelectorAll('.files-open-btn').forEach((btn) => {
        btn.addEventListener('click', (e) => {
          e.preventDefault()
          if (btn.disabled || btn.dataset.filesDisabled === '1') {
            return
          }
          const targetId = btn.dataset.filesTargetId || ''
          const targetName = btn.dataset.filesTargetName || ''
          const proto = btn.dataset.filesProtocol || 'ssh'
          const hostForTftp = btn.dataset.filesHost || ''
          if (!targetId) return
          const actions = []
          const sftpEnabled = btn.dataset.filesSftpEnabled === '1'
          if (proto === 'ssh' || proto === 'telnet') {
            if (proto === 'ssh' && sftpEnabled) {
              actions.push({
                label: 'SFTP (SSH)',
                onSelect: () => {
                  const url = `/files?target_id=${encodeURIComponent(targetId)}&target_name=${encodeURIComponent(targetName || '')}&protocol=sftp`
                  window.location.href = url
                },
              })
            }
            const ftpEnabled = btn.dataset.filesFtpEnabled === '1'
            if (ftpEnabled) {
              actions.push({
                label: 'FTP',
                onSelect: () => {
                  const url = `/files?target_id=${encodeURIComponent(targetId)}&target_name=${encodeURIComponent(targetName || '')}&protocol=ftp`
                  window.location.href = url
                },
              })
            }
            if (hostForTftp) {
              const tftpTarget = targets.find((t) => t.protocol === 'tftp' && t.host === hostForTftp)
              if (tftpTarget) {
                if (proto === 'ssh') {
                  const sshTarget = targets.find((t) => t.id === targetId)
                  if (sshTarget) {
                    actions.push({
                      label: t('app.tftpProtoLabel'),
                      onSelect: () => {
                        const name = tftpTarget.name || hostForTftp
                        const urlOpts = {
                          channelTargetId: sshTarget.id,
                          toUrl: (p) => {
                            p.delete('target_id')
                            p.delete('protocol')
                            p.set('ssh_target_id', sshTarget.id)
                            p.set('tftp_target_id', tftpTarget.id)
                            p.set('target_name', name)
                            return `/tftp-console?${p.toString()}`
                          },
                        }
                        if (sshTarget.has_stored_credentials) {
                          showStoredCredentialModal(
                            sshTarget.id,
                            sshTarget.name || targetName,
                            sshTarget.needs_password,
                            sshTarget.needs_passphrase,
                            'ssh',
                            urlOpts,
                          )
                        } else {
                          showSSHCredentialModal(sshTarget.id, sshTarget.name || targetName, 'ssh', urlOpts, !!sshTarget.has_ssh_key)
                        }
                      },
                    })
                  }
                } else {
                  // Telnet ホストはコンソール連携しない（TFTP ファイル操作のみ）。
                  actions.push({
                    label: 'TFTP',
                    onSelect: () => {
                      const url = `/files?target_id=${encodeURIComponent(tftpTarget.id)}&target_name=${encodeURIComponent(tftpTarget.name || hostForTftp || '')}&protocol=tftp`
                      window.location.href = url
                    },
                  })
                }
              }
            }
          }
          if (proto === 'ftp') {
            actions.push({
              label: 'FTP',
              onSelect: () => {
                const url = `/files?target_id=${encodeURIComponent(targetId)}&target_name=${encodeURIComponent(targetName || '')}&protocol=ftp`
                window.location.href = url
              },
            })
          }
          if (proto === 'tftp') {
            const url = `/files?target_id=${encodeURIComponent(targetId)}&target_name=${encodeURIComponent(targetName || '')}&protocol=tftp`
            window.location.href = url
            return
          }
          if (actions.length === 1) {
            actions[0].onSelect()
            return
          }
          showFileProtocolModal({
            title: t('app.fileProtoTitle'),
            targetName,
            actions,
          })
        })
      })

      mainContent.querySelectorAll('[data-group-toggle="1"]').forEach((el) => {
        el.addEventListener('click', (e) => {
          e.preventDefault()
          e.stopPropagation()
          const gid = el.getAttribute('data-group-id') || ''
          if (!gid) return
          expandedGroups.has(gid) ? expandedGroups.delete(gid) : expandedGroups.add(gid)
          showTreeView(mode, true)
        })
      })
      mainContent.querySelectorAll('[data-group-select="1"]').forEach((el) => {
        el.addEventListener('click', () => {
          const gid = el.getAttribute('data-group-id') || ''
          selectedGroupId = gid
          showTreeView(mode, true)
        })
      })
      // Group edit/delete actions (manage mode only).
      if (isManageMode) {
        mainContent.querySelectorAll('[data-group-edit="1"]').forEach((btn) => {
          btn.addEventListener('click', (e) => {
            e.preventDefault()
            e.stopPropagation()
            const gid = btn.getAttribute('data-group-id') || ''
            if (!gid) return
            const groups = Array.isArray(groupsCache) ? groupsCache : (groupsCache?.items || [])
            const g = (groups || []).find((x) => x.id === gid) || null
            if (!g) return
            showEditGroupModal(g)
          })
        })
        mainContent.querySelectorAll('[data-group-delete="1"]').forEach((btn) => {
          btn.addEventListener('click', async (e) => {
            e.preventDefault()
            e.stopPropagation()
            const gid = btn.getAttribute('data-group-id') || ''
            const gname = btn.getAttribute('data-group-name') || gid
            if (!gid) return
            if (!(await uiConfirm(t('app.confirmDeleteGroup', { name: escapeHtml(gname) || gid }), { danger: true }))) return
            try {
              await API.deleteGroup(gid)
              groupsCache = null
              if (selectedGroupId === gid) selectedGroupId = ''
              await showTreeView('manage', true)
            } catch (err) {
              await uiAlert(err.message || t('app.deleteFailed'))
            }
          })
        })
      }

    } catch (e) {
      mainContent.dataset.treeMode = mode
      mainContent.innerHTML = `
        <div class="w-full flex-1 bg-white rounded-lg shadow-sm border border-slate-200 overflow-hidden flex flex-col min-h-0">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h2 class="text-sm font-semibold text-slate-800">${escapeHtml(pageTitle)}</h2>
            ${isManageMode ? `<button type="button" id="btn-add-group" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('app.add')}</button>` : ''}
          </div>
          <div class="px-5 py-6 text-center">
            <p class="text-sm text-red-600">${escapeHtml(e.message || t('app.fetchFailed'))}</p>
          </div>
        </div>
      `
      if (isManageMode) {
        mainContent.querySelector('#btn-add-group')?.addEventListener('click', showAddGroupModal)
      }
    }
  }

  function showAddTargetModal() {
    const modal = document.getElementById('add-target-modal')
    modal.classList.remove('hidden')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-4xl mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.addTargetTitle')}</h3>
            <button id="add-target-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="add-target-form">
            <div class="px-6 py-5 max-h-[85vh] overflow-y-auto">
              <div class="grid grid-cols-1 lg:grid-cols-2 gap-x-8 gap-y-5">
                <div class="space-y-5">
                  <div>
                    <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldName')}</label>
                    <input type="text" id="add-target-name" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('app.placeholderTargetName')}" />
                  </div>
                  <div>
                    <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.addTargetGroupLabel')}</label>
                    <div class="w-full rounded border border-slate-200 px-3 py-2 text-sm text-slate-800 bg-slate-50 font-mono">${escapeHtml(selectedGroupId || 'root')}</div>
                  </div>
                  <div>
                    <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.addTargetHost')}</label>
                    <input type="text" id="add-target-host" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400 font-mono" placeholder="${t('app.placeholderHost')}" />
                  </div>
                  <div class="grid grid-cols-2 gap-4">
                    <div>
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.addTargetPort')}</label>
                      <input type="number" id="add-target-port" min="1" max="65535" value="22" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                    </div>
                    <div>
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.addTargetProto')}</label>
                      <select id="add-target-protocol" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                        <option value="ssh">SSH</option>
                        <option value="telnet">Telnet</option>
                        <option value="vnc">VNC</option>
                        <option value="rdp">RDP</option>
                        <option value="tftp">TFTP</option>
                        <option value="ftp">FTP</option>
                      </select>
                    </div>
                  </div>
                  <div id="add-target-host-key-wrap" class="rounded border border-slate-200 bg-slate-50 px-4 py-3 hidden">
                    <div class="flex items-center justify-between mb-2">
                      <p class="text-xs font-medium text-slate-700">${t('hostKey.sectionTitle')}</p>
                      <button type="button" id="add-target-host-key-refetch" class="rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100 shadow-sm">${t('hostKey.refetch')}</button>
                    </div>
                    <p class="text-[11px] text-slate-500 mb-1">${t('hostKey.fingerprintLabel')}</p>
                    <div id="add-target-host-key-fp" class="font-mono text-xs break-all bg-white border border-slate-200 rounded px-2 py-1.5 text-slate-700 select-all min-h-[2rem]">${t('hostKey.notRegistered')}</div>
                    <p id="add-target-host-key-status" class="text-[11px] text-slate-500 mt-1"></p>
                  </div>
                  <div id="add-target-rdp-res-wrap" class="grid grid-cols-2 gap-4 hidden">
                    <div>
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.addTargetRdpWidth')}</label>
                      <input type="number" id="add-target-rdp-width" min="640" max="3840" value="1920" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                    </div>
                    <div>
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.addTargetRdpHeight')}</label>
                      <input type="number" id="add-target-rdp-height" min="480" max="2160" value="1080" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                    </div>
                  </div>
                  <div id="add-target-file-protocols-wrap" class="space-y-3 hidden">
                    <p class="text-xs font-medium text-slate-700">${t('app.fileProtocolHeading')}</p>
                    <div class="space-y-2 pl-0">
                      <label id="add-target-sftp-label" class="flex items-start gap-2 cursor-pointer hidden">
                        <input type="checkbox" id="add-target-enable-sftp" class="mt-0.5 rounded border-slate-300 text-sky-600 focus:ring-sky-500" />
                        <span class="text-sm text-slate-800">${t('app.sftpLabel')}</span>
                      </label>
                      <label class="flex items-start gap-2 cursor-pointer">
                        <input type="checkbox" id="add-target-enable-ftp" class="mt-0.5 rounded border-slate-300 text-sky-600 focus:ring-sky-500" />
                        <span class="text-sm text-slate-800">FTP</span>
                      </label>
                      <label class="flex items-start gap-2 cursor-pointer">
                        <input type="checkbox" id="add-target-enable-tftp" class="mt-0.5 rounded border-slate-300 text-sky-600 focus:ring-sky-500" />
                        <span class="text-sm text-slate-800">${t('app.tftpLabel')}</span>
                      </label>
                    </div>
                  </div>
                </div>
                <div class="space-y-5">
                  <div id="add-target-cred-fields">
                    <div class="flex items-start justify-between gap-3 mb-3">
                      <div class="flex-1 space-y-2">
                        <label class="block text-xs font-medium text-slate-600">${t('app.targetCredentialMode')}</label>
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" name="add-target-cred-mode" value="identity" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" checked />
                          <span class="text-sm text-slate-800">${t('app.targetCredentialModeIdentity')}</span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" name="add-target-cred-mode" value="key" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" />
                          <span class="text-sm text-slate-800">${t('app.targetCredentialModeKey')}</span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" name="add-target-cred-mode" value="manual" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" />
                          <span class="text-sm text-slate-800">${t('app.targetCredentialModeManual')}</span>
                        </label>
                      </div>
                      <div class="shrink-0 pt-5">
                        <button type="button" id="add-target-manage-credentials" class="rounded border border-slate-300 bg-white px-3 py-2 text-xs font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">
                          ${t('app.manageCredentials')}
                        </button>
                      </div>
                    </div>
                    <div id="add-target-identity-wrap" class="mb-4">
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.targetCredentialIdentity')}</label>
                      <select id="add-target-credential-identity" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                        <option value="">${t('app.credentialsNone')}</option>
                      </select>
                    </div>
                    <div id="add-target-ssh-key-wrap" class="mb-4 hidden">
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.targetCredentialKey')}</label>
                      <select id="add-target-ssh-key" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                        <option value="">${t('app.credentialsPickKey')}</option>
                      </select>
                    </div>
                    <div id="add-target-auth-type-wrap" class="space-y-3 hidden">
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.authMethodLabel')}</label>
                      <div class="space-y-2">
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" name="add-target-auth-type" value="password" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" checked />
                          <span class="text-sm text-slate-800">${t('app.authPassword')}</span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" name="add-target-auth-type" value="key" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" />
                          <span class="text-sm text-slate-800">${t('app.authPublicKey')}</span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" name="add-target-auth-type" value="key_passphrase" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" />
                          <span class="text-sm text-slate-800">${t('app.authPublicKeyWithPp')}</span>
                        </label>
                      </div>
                    </div>
                    <div class="space-y-5 mt-4">
                      <div id="add-target-username-wrap">
                        <label id="add-target-username-label" class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.targetUsernameOpt')}</label>
                        <input type="text" id="add-target-ssh-username" autocomplete="username" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('app.placeholderRoot')}" />
                      </div>
                      <div id="add-target-password-wrap">
                        <label id="add-target-password-label" class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.targetPasswordOpt')}</label>
                        <input type="password" id="add-target-ssh-password" autocomplete="current-password" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('app.targetPasswordHint')}" />
                      </div>
                      <div id="add-target-key-wrap" class="hidden">
                        <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.targetPrivateKeyLabel')}</label>
                        <textarea id="add-target-ssh-private-key" rows="4" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono placeholder-slate-400" placeholder="${t('app.targetPrivateKeyPlaceholder')}"></textarea>
                      </div>
                      <div id="add-target-passphrase-wrap" class="hidden">
                        <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.targetKeyPassphraseLabel')}</label>
                        <input type="password" id="add-target-ssh-key-passphrase" autocomplete="off" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('app.targetKeyPassphrasePlaceholder')}" />
                      </div>
                    </div>
                  </div>
                </div>
              </div>
              <p id="add-target-error" class="text-sm text-red-600 hidden mt-5"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="add-target-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('app.addTargetCancel')}</button>
              <button type="submit" id="add-target-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('app.addTargetSubmit')}</button>
            </div>
          </form>
        </div>
      </div>
    `
    const defaultPorts = { ssh: 22, telnet: 23, vnc: 5900, rdp: 3389, tftp: 69, ftp: 21 }
    const addProtoSelect = modal.querySelector('#add-target-protocol')
    const addCredFields = modal.querySelector('#add-target-cred-fields')
    const addIdentityWrap = modal.querySelector('#add-target-identity-wrap')
    const addIdentitySelect = modal.querySelector('#add-target-credential-identity')
    const addSshKeyWrap = modal.querySelector('#add-target-ssh-key-wrap')
    const addSshKeySelect = modal.querySelector('#add-target-ssh-key')
    const addCredManageBtn = modal.querySelector('#add-target-manage-credentials')
    const addUsernameWrap = modal.querySelector('#add-target-username-wrap')
    const addAuthTypeWrap = modal.querySelector('#add-target-auth-type-wrap')
    const addPasswordWrap = modal.querySelector('#add-target-password-wrap')
    const addPortInput = modal.querySelector('#add-target-port')
    const addKeyWrap = modal.querySelector('#add-target-key-wrap')
    const addPassphraseWrap = modal.querySelector('#add-target-passphrase-wrap')
    const addUsernameLabel = modal.querySelector('#add-target-username-label')
    const addPasswordLabel = modal.querySelector('#add-target-password-label')
    const addRdpResWrap = modal.querySelector('#add-target-rdp-res-wrap')
    const addRdpWidthInput = modal.querySelector('#add-target-rdp-width')
    const addRdpHeightInput = modal.querySelector('#add-target-rdp-height')
    const addFileProtocolsWrap = modal.querySelector('#add-target-file-protocols-wrap')
    const addSftpLabel = modal.querySelector('#add-target-sftp-label')
    const addSftpCheckbox = modal.querySelector('#add-target-enable-sftp')
    const addFtpCheckbox = modal.querySelector('#add-target-enable-ftp')
    const addTftpCheckbox = modal.querySelector('#add-target-enable-tftp')
    const addHostKeyWrap = modal.querySelector('#add-target-host-key-wrap')
    const addHostKeyFpEl = modal.querySelector('#add-target-host-key-fp')
    const addHostKeyStatusEl = modal.querySelector('#add-target-host-key-status')
    const addHostKeyRefetchBtn = modal.querySelector('#add-target-host-key-refetch')
    let addCapturedFingerprint = ''
    let addCapturedKey = ''

    let addCredentialIdentities = []
    let addSSHKeys = []
    function getAddCredMode() {
      return modal.querySelector('input[name="add-target-cred-mode"]:checked')?.value || 'identity'
    }
    async function refreshAddCredentialLists({ keepSelection = true } = {}) {
      const prevIdent = keepSelection && addIdentitySelect ? addIdentitySelect.value : ''
      const prevKey = keepSelection && addSshKeySelect ? addSshKeySelect.value : ''
      if (addIdentitySelect) {
        addIdentitySelect.innerHTML = `<option value="">${t('app.credentialsNone')}</option>`
      }
      if (addSshKeySelect) {
        addSshKeySelect.innerHTML = `<option value="">${t('app.credentialsPickKey')}</option>`
      }
      try {
        const [identRes, keysRes] = await Promise.all([
          API.credentialIdentities(),
          API.sshKeys(),
        ])
        addCredentialIdentities = (identRes && identRes.items) || []
        addSSHKeys = (keysRes && keysRes.items) || []
        addCredentialIdentities.forEach((ident) => {
          const opt = document.createElement('option')
          opt.value = ident.id
          const auth = authMethodLabel(ident.auth_method)
          opt.textContent = `${ident.label || ident.id} (${auth})`
          addIdentitySelect?.appendChild(opt)
        })
        addSSHKeys.forEach((k) => {
          const opt = document.createElement('option')
          opt.value = k.id
          opt.textContent = k.label || k.id
          addSshKeySelect?.appendChild(opt)
        })
      } catch {
        addCredentialIdentities = []
        addSSHKeys = []
      }
      if (prevIdent && addIdentitySelect) addIdentitySelect.value = prevIdent
      if (prevKey && addSshKeySelect) addSshKeySelect.value = prevKey
    }
    function applyAddCredentialIdentity(id) {
      const ident = (addCredentialIdentities || []).find((x) => x.id === id)
      if (!ident) return
      const uEl = modal.querySelector('#add-target-ssh-username')
      const pwEl = modal.querySelector('#add-target-ssh-password')
      const kEl = modal.querySelector('#add-target-ssh-private-key')
      const ppEl = modal.querySelector('#add-target-ssh-key-passphrase')
      if (uEl) uEl.value = ident.ssh_username || ''
      if (pwEl) {
        pwEl.value = ''
        pwEl.placeholder = ident.has_password ? t('app.credentialsSavedHint') : t('app.targetPasswordHint')
      }
      if (kEl) {
        kEl.value = ''
        kEl.placeholder = ident.has_ssh_key ? t('app.credentialsSavedHint') : t('app.targetPrivateKeyPlaceholder')
      }
      if (ppEl) {
        ppEl.value = ''
        if (ident.has_passphrase) ppEl.placeholder = t('app.credentialsSavedHint')
      }
    }
    function syncAddCredentialMode() {
      const mode = getAddCredMode()
      const proto = addProtoSelect.value
      const isSsh = proto === 'ssh'
      addIdentityWrap?.classList.toggle('hidden', mode !== 'identity')
      addSshKeyWrap?.classList.toggle('hidden', mode !== 'key')
      addAuthTypeWrap?.classList.toggle('hidden', !isSsh || mode !== 'manual')
      const hideManualSecrets = mode === 'identity' || mode === 'key'
      addPasswordWrap?.classList.toggle('hidden', hideManualSecrets || (isSsh && mode === 'manual' && (modal.querySelector('input[name="add-target-auth-type"]:checked')?.value || 'password') !== 'password'))
      addKeyWrap?.classList.toggle('hidden', hideManualSecrets || !isSsh || mode !== 'manual' || (modal.querySelector('input[name="add-target-auth-type"]:checked')?.value || 'password') === 'password')
      addPassphraseWrap?.classList.toggle('hidden', hideManualSecrets || !isSsh || mode !== 'manual' || (modal.querySelector('input[name="add-target-auth-type"]:checked')?.value || 'password') !== 'key_passphrase')
      addUsernameWrap?.classList.toggle('hidden', mode === 'identity')
      if (mode === 'identity') applyAddCredentialIdentity(addIdentitySelect?.value?.trim() || '')
      if (mode === 'manual' && isSsh) syncAddAuthType()
      else if (!isSsh && (proto === 'rdp' || proto === 'telnet')) {
        addPasswordWrap?.classList.remove('hidden')
        addKeyWrap?.classList.add('hidden')
        addPassphraseWrap?.classList.add('hidden')
      }
    }

    function setAddHostKeyFp(value) {
      addCapturedFingerprint = (value || '').trim()
      if (addCapturedFingerprint) {
        addHostKeyFpEl.textContent = addCapturedFingerprint
        addHostKeyFpEl.classList.remove('text-slate-500')
        addHostKeyFpEl.classList.add('text-slate-800')
      } else {
        addHostKeyFpEl.textContent = t('hostKey.notRegistered')
        addHostKeyFpEl.classList.add('text-slate-500')
      }
    }
    function resetAddHostKey() {
      addCapturedFingerprint = ''
      addCapturedKey = ''
      addHostKeyStatusEl.textContent = ''
      setAddHostKeyFp('')
    }
    function syncAddAuthType() {
      if (getAddCredMode() !== 'manual') {
        syncAddCredentialMode()
        return
      }
      const proto = addProtoSelect.value
      if (proto === 'rdp' || proto === 'telnet') {
        addPasswordWrap.classList.remove('hidden')
        addKeyWrap.classList.add('hidden')
        addPassphraseWrap.classList.add('hidden')
        return
      }
      if (proto !== 'ssh') return
      const authType = modal.querySelector('input[name="add-target-auth-type"]:checked')?.value || 'password'
      addPasswordWrap.classList.toggle('hidden', authType !== 'password')
      addKeyWrap.classList.toggle('hidden', authType === 'password')
      addPassphraseWrap.classList.toggle('hidden', authType !== 'key_passphrase')
    }
    function syncAddProtocol() {
      const proto = addProtoSelect.value
      const hasCreds = proto === 'ssh' || proto === 'telnet' || proto === 'rdp' || proto === 'ftp'
      addCredFields.style.display = hasCreds ? '' : 'none'
      addAuthTypeWrap.classList.toggle('hidden', proto !== 'ssh')
      if (addHostKeyWrap) {
        addHostKeyWrap.classList.toggle('hidden', proto !== 'ssh')
        if (proto !== 'ssh') {
          resetAddHostKey()
        }
      }
      addUsernameLabel.textContent =
        proto === 'telnet' ? t('app.telnetUsernameOpt')
          : proto === 'rdp' ? t('app.rdpUsernameOpt')
            : proto === 'ftp' ? t('app.ftpUsernameOpt')
              : t('app.sshUsernameOpt')
      addPasswordLabel.textContent =
        proto === 'telnet' ? t('app.telnetPasswordOpt')
          : proto === 'rdp' ? t('app.rdpPasswordOpt')
            : proto === 'ftp' ? t('app.ftpPasswordOpt')
              : t('app.sshPasswordOpt')
      addRdpResWrap.classList.toggle('hidden', proto !== 'rdp')
      const canEditFileProtocols = proto === 'ssh' || proto === 'telnet'
      addFileProtocolsWrap.classList.toggle('hidden', !canEditFileProtocols)
      if (addSftpLabel && addSftpCheckbox) {
        addSftpLabel.classList.toggle('hidden', proto !== 'ssh')
        addSftpCheckbox.checked = proto === 'ssh' // 新規追加時はデフォルトで SFTP 有効
      }
      if (!canEditFileProtocols) {
        if (addFtpCheckbox) addFtpCheckbox.checked = false
        if (addTftpCheckbox) addTftpCheckbox.checked = false
      }
      if (defaultPorts[proto] !== undefined) {
        addPortInput.value = defaultPorts[proto]
      }
      syncAddCredentialMode()
    }
    addProtoSelect.addEventListener('change', syncAddProtocol)
    modal.querySelectorAll('input[name="add-target-auth-type"]').forEach((radio) => {
      radio.addEventListener('change', syncAddAuthType)
    })
    modal.querySelectorAll('input[name="add-target-cred-mode"]').forEach((radio) => {
      radio.addEventListener('change', syncAddCredentialMode)
    })
    // Initial render: apply protocol-dependent visibility immediately.
    // Without this, the dialog can show only a subset of fields until the
    // user toggles the protocol dropdown once.
    syncAddProtocol()
    // Re-probe whenever host or port changes so we never silently
    // commit a fingerprint captured from a different host:port pair.
    const addHostInputForReset = modal.querySelector('#add-target-host')
    if (addHostInputForReset) addHostInputForReset.addEventListener('input', resetAddHostKey)
    addPortInput.addEventListener('input', resetAddHostKey)
    async function probeAddHostKey({ silent = false } = {}) {
      if (addProtoSelect.value !== 'ssh') return null
      const host = (addHostInputForReset && addHostInputForReset.value || '').trim()
      const port = parseInt(addPortInput.value, 10) || 22
      if (!host) {
        if (!silent) addHostKeyStatusEl.textContent = t('app.placeholderHost')
        return null
      }
      addHostKeyStatusEl.textContent = t('hostKey.fetching')
      addHostKeyRefetchBtn.disabled = true
      try {
        const result = await API.probeHostKey({ host, port })
        addCapturedKey = `${host}:${port}`
        setAddHostKeyFp(result.fingerprint)
        addHostKeyStatusEl.textContent = t('hostKey.probedAt', { time: new Date().toLocaleTimeString() })
        return result.fingerprint
      } catch (err) {
        addCapturedFingerprint = ''
        addCapturedKey = ''
        addHostKeyStatusEl.textContent = t('hostKey.probeFailed', { error: err.message || String(err) })
        setAddHostKeyFp('')
        return null
      } finally {
        addHostKeyRefetchBtn.disabled = false
      }
    }
    addHostKeyRefetchBtn.addEventListener('click', () => probeAddHostKey())

    void refreshAddCredentialLists().then(() => syncAddCredentialMode())
    addIdentitySelect?.addEventListener('change', () => applyAddCredentialIdentity(addIdentitySelect.value.trim()))
    addCredManageBtn?.addEventListener('click', async () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
      await showCredentialsPage()
    })

    modal.querySelector('#add-target-close').addEventListener('click', () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    })
    modal.querySelector('#add-target-cancel').addEventListener('click', () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    })
    modal.querySelector('#add-target-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errorEl = modal.querySelector('#add-target-error')
      const submitBtn = modal.querySelector('#add-target-submit')
      errorEl.classList.add('hidden')
      const name = modal.querySelector('#add-target-name').value.trim()
      const group_id = selectedGroupId
      const host = modal.querySelector('#add-target-host').value.trim()
      const port = parseInt(modal.querySelector('#add-target-port').value, 10) || 22
      const protocol = modal.querySelector('#add-target-protocol').value
      const credMode = getAddCredMode()
      const ssh_username = modal.querySelector('#add-target-ssh-username').value.trim()
      const credential_identity_id = addIdentitySelect ? addIdentitySelect.value.trim() : ''
      const ssh_key_id = addSshKeySelect ? addSshKeySelect.value.trim() : ''
      const authType = protocol === 'ssh' ? (modal.querySelector('input[name="add-target-auth-type"]:checked')?.value || 'password') : 'password'
      const hasCreds = protocol === 'ssh' || protocol === 'telnet' || protocol === 'rdp' || protocol === 'ftp'
      if (hasCreds && credMode === 'identity' && !credential_identity_id) {
        errorEl.textContent = t('app.targetCredentialIdentityRequired')
        errorEl.classList.remove('hidden')
        return
      }
      if (hasCreds && credMode === 'key') {
        if (!ssh_key_id) {
          errorEl.textContent = t('app.targetCredentialKeyRequired')
          errorEl.classList.remove('hidden')
          return
        }
        if (!ssh_username) {
          errorEl.textContent = t('app.targetCredentialUsernameRequired')
          errorEl.classList.remove('hidden')
          return
        }
      }
      const payload = { name, host, port, protocol, group_id, ssh_username }
      if (credMode === 'identity' && credential_identity_id) {
        payload.credential_identity_id = credential_identity_id
      } else if (credMode === 'key' && ssh_key_id) {
        payload.ssh_key_id = ssh_key_id
      } else if (credMode === 'manual') {
        if (protocol === 'rdp' || protocol === 'ftp' || protocol === 'telnet' || authType === 'password') {
          const v = modal.querySelector('#add-target-ssh-password').value
          if (v !== '') payload.ssh_password = v
        }
        if (protocol === 'ssh' && (authType === 'key' || authType === 'key_passphrase')) {
          const keyVal = modal.querySelector('#add-target-ssh-private-key').value.trim()
          if (keyVal) payload.ssh_private_key = keyVal
          if (authType === 'key_passphrase') {
            const pp = modal.querySelector('#add-target-ssh-key-passphrase').value
            if (pp !== '') payload.ssh_private_key_passphrase = pp
          }
        }
      }
      if (!name || !host) {
        errorEl.textContent = t('app.nameHostRequired')
        errorEl.classList.remove('hidden')
        return
      }
      if (!group_id) {
        errorEl.textContent = t('app.chooseGroupFirst')
        errorEl.classList.remove('hidden')
        return
      }
      let rdpW = 1920
      let rdpH = 1080
      if (protocol === 'rdp') {
        rdpW = parseInt(addRdpWidthInput.value, 10) || 1920
        rdpH = parseInt(addRdpHeightInput.value, 10) || 1080
      }
      const enableSftp = !(addSftpCheckbox && addFileProtocolsWrap && !addFileProtocolsWrap.classList.contains('hidden') && addSftpCheckbox.checked === false)
      const enableFtp = !!(addFtpCheckbox && !addFileProtocolsWrap.classList.contains('hidden') && addFtpCheckbox.checked)
      const enableTftp = !!(addTftpCheckbox && !addFileProtocolsWrap.classList.contains('hidden') && addTftpCheckbox.checked)
      if (protocol === 'ssh' || protocol === 'telnet') {
        payload.sftp_enabled = protocol === 'ssh' ? enableSftp : false
        payload.ftp_enabled = enableFtp
        payload.tftp_enabled = enableTftp
      }
      // SSH targets: TOFU host-key flow.
      // - If we have not probed yet (or host:port changed), probe now.
      // - Show a confirm dialog so the operator chooses to trust the
      //   fingerprint, register without a fingerprint, or cancel.
      // - On dial failure, fall back to "register without fingerprint
      //   or cancel" so the user is never silently shipped a target
      //   with an unknown key.
      if (protocol === 'ssh') {
        const probeKey = `${host}:${port}`
        let fp = addCapturedKey === probeKey ? addCapturedFingerprint : ''
        if (!fp) {
          fp = await probeAddHostKey({ silent: true })
        }
        if (fp) {
          const body = t('hostKey.confirmBody', { host, port, fp })
          const accept = t('hostKey.confirmAccept')
          const skip = t('hostKey.confirmSkip')
          const cancel = t('hostKey.confirmCancel')
          const choice = await uiChoose(
            `${t('hostKey.confirmTitle')}\n\n${body}`,
            [
              { label: accept, value: 'accept', variant: 'primary' },
              { label: skip, value: 'skip', variant: 'secondary' },
            ],
            { cancelLabel: cancel, title: t('hostKey.confirmTitle') },
          )
          if (choice === 'accept') {
            payload.ssh_host_key_fingerprint = fp
          } else if (choice === 'skip') {
            const skipOk = await uiConfirm(`${t('hostKey.registerWithoutFingerprint')}?`, { title: t('hostKey.confirmTitle') })
            if (!skipOk) {
              return
            }
          } else {
            return
          }
        } else {
          // Probe failed: the operator can still register without a
          // fingerprint if they understand the risk. Otherwise abort.
          const skipOk = await uiConfirm(
            `${addHostKeyStatusEl.textContent || ''}\n\n${t('hostKey.registerWithoutFingerprint')}?`,
            { title: t('hostKey.confirmTitle') },
          )
          if (!skipOk) {
            return
          }
        }
      }
      submitBtn.disabled = true
      try {
        const created = await API.createTarget(payload)
        if (protocol === 'rdp' && created && created.id) {
          setRdpResolutionForTarget(created.id, rdpW, rdpH)
        }
        // TFTP / FTP はフラグのみ（ftp_enabled / tftp_enabled）。TFTP サーバー用ターゲットはホームの TFTP トグルのみ。
        modal.classList.add('hidden')
        modal.innerHTML = ''
        groupsCache = null
        await showTreeView('manage')
      } catch (err) {
        errorEl.textContent = err.message || t('app.addUserFailed')
        errorEl.classList.remove('hidden')
      } finally {
        submitBtn.disabled = false
      }
    })
  }

  function showEditTargetModal(target) {
    const modal = document.getElementById('add-target-modal')
    const allGroups = Array.isArray(groupsCache) ? groupsCache : (groupsCache?.items || [])
    const currentGroupId = target.group_id || ''
    const editGroupOptions = allGroups
      .slice()
      .sort((a, b) => (a.id || '').localeCompare(b.id || ''))
      .map((g) => `<option value="${escapeHtml(g.id)}" ${g.id === currentGroupId ? 'selected' : ''}>${escapeHtml(g.id)}</option>`)
      .join('')
    // 資格情報の使い方の初期選択: ターゲットが最後に Identity / SSH Key
    // ライブラリのどのエントリから設定されたかを反映する（無ければ手入力）。
    const initialCredMode = target.credential_identity_id
      ? 'identity'
      : (target.ssh_key_id ? 'key' : 'manual')
    modal.classList.remove('hidden')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-4xl mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">${t('app.editTargetTitle')}</h3>
            <button id="edit-target-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="edit-target-form" data-edit-target-id="${escapeHtml(target.id)}">
            <div class="px-6 py-5 max-h-[85vh] overflow-y-auto">
              <div class="grid grid-cols-1 lg:grid-cols-2 gap-x-8 gap-y-5">
                <div class="space-y-5">
                  <div>
                    <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldName')}</label>
                    <input type="text" id="edit-target-name" required value="${escapeHtml(target.name)}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" />
                  </div>
                  <div>
                    <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.addTargetHost')}</label>
                    <input type="text" id="edit-target-host" required value="${escapeHtml(target.host)}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                  </div>
                  <div>
                    <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.addTargetGroupLabel')}</label>
                    <select id="edit-target-group" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono">
                      ${editGroupOptions}
                    </select>
                  </div>
                  <div class="grid grid-cols-2 gap-4">
                    <div>
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.addTargetPort')}</label>
                      <input type="number" id="edit-target-port" min="1" max="65535" value="${target.port || 22}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                    </div>
                    <div>
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.addTargetProto')}</label>
                      <select id="edit-target-protocol" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                        <option value="ssh" ${(target.protocol || 'ssh') === 'ssh' ? 'selected' : ''}>SSH</option>
                        <option value="telnet" ${target.protocol === 'telnet' ? 'selected' : ''}>Telnet</option>
                        <option value="vnc" ${target.protocol === 'vnc' ? 'selected' : ''}>VNC</option>
                        <option value="rdp" ${target.protocol === 'rdp' ? 'selected' : ''}>RDP</option>
                        <option value="tftp" ${target.protocol === 'tftp' ? 'selected' : ''}>TFTP</option>
                        <option value="ftp" ${target.protocol === 'ftp' ? 'selected' : ''}>FTP</option>
                      </select>
                    </div>
                  </div>
                  <div>
                    <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.fieldTagsCsv')}</label>
                    <input type="text" id="edit-target-tags" value="${escapeHtml((target.tags || []).join(', '))}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="${t('app.placeholderTagsCsv')}" />
                    <div id="edit-target-tags-picker" class="mt-2"></div>
                  </div>
                  <div id="edit-target-file-protocols-wrap" class="space-y-3 hidden">
                    <p class="text-xs font-medium text-slate-700">${t('app.fileProtocolHeading')}</p>
                    <div class="space-y-2 pl-0">
                      <label id="edit-target-sftp-label" class="flex items-start gap-2 cursor-pointer hidden">
                        <input type="checkbox" id="edit-target-enable-sftp" class="mt-0.5 rounded border-slate-300 text-sky-600 focus:ring-sky-500" />
                        <span class="text-sm text-slate-800">${t('app.sftpLabel')}</span>
                      </label>
                      <label class="flex items-start gap-2 cursor-pointer">
                        <input type="checkbox" id="edit-target-enable-ftp" class="mt-0.5 rounded border-slate-300 text-sky-600 focus:ring-sky-500" />
                        <span class="text-sm text-slate-800">FTP</span>
                      </label>
                      <label class="flex items-start gap-2 cursor-pointer">
                        <input type="checkbox" id="edit-target-enable-tftp" class="mt-0.5 rounded border-slate-300 text-sky-600 focus:ring-sky-500" />
                        <span class="text-sm text-slate-800">${t('app.tftpLabel')}</span>
                      </label>
                    </div>
                  </div>
                  <div id="edit-target-host-key-wrap" class="rounded border border-slate-200 bg-slate-50 px-4 py-3 hidden">
                    <div class="flex items-center justify-between mb-2">
                      <p class="text-xs font-medium text-slate-700">${t('hostKey.sectionTitle')}</p>
                      <div class="flex gap-2">
                        <button type="button" id="edit-target-host-key-refetch" class="rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-700 hover:bg-slate-100 shadow-sm">${t('hostKey.refetch')}</button>
                        <button type="button" id="edit-target-host-key-clear" class="rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-rose-700 hover:bg-rose-50 shadow-sm">${t('hostKey.clear')}</button>
                      </div>
                    </div>
                    <p class="text-[11px] text-slate-500 mb-1">${t('hostKey.fingerprintLabel')}</p>
                    <div id="edit-target-host-key-fp" class="font-mono text-xs break-all bg-white border border-slate-200 rounded px-2 py-1.5 text-slate-700 select-all min-h-[2rem]">${escapeHtml(target.ssh_host_key_fingerprint || '') || t('hostKey.notRegistered')}</div>
                    <p id="edit-target-host-key-status" class="text-[11px] text-slate-500 mt-1"></p>
                  </div>
                  <div id="edit-target-rdp-res-wrap" class="grid grid-cols-2 gap-4 hidden">
                    <div>
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.addTargetRdpWidth')}</label>
                      <input type="number" id="edit-target-rdp-width" min="640" max="3840" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                    </div>
                    <div>
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.addTargetRdpHeight')}</label>
                      <input type="number" id="edit-target-rdp-height" min="480" max="2160" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                    </div>
                  </div>
                </div>
                <div class="space-y-5">
                  <div id="edit-target-cred-fields">
                    <div class="flex items-start justify-between gap-3 mb-3">
                      <div class="flex-1 space-y-2">
                        <label class="block text-xs font-medium text-slate-600">${t('app.targetCredentialMode')}</label>
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" name="edit-target-cred-mode" value="identity" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" ${initialCredMode === 'identity' ? 'checked' : ''} />
                          <span class="text-sm text-slate-800">${t('app.targetCredentialModeIdentity')}</span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" name="edit-target-cred-mode" value="key" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" ${initialCredMode === 'key' ? 'checked' : ''} />
                          <span class="text-sm text-slate-800">${t('app.targetCredentialModeKey')}</span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" name="edit-target-cred-mode" value="manual" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" ${initialCredMode === 'manual' ? 'checked' : ''} />
                          <span class="text-sm text-slate-800">${t('app.targetCredentialModeManual')}</span>
                        </label>
                      </div>
                      <div class="shrink-0 pt-5">
                        <button type="button" id="edit-target-manage-credentials" class="rounded border border-slate-300 bg-white px-3 py-2 text-xs font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">
                          ${t('app.manageCredentials')}
                        </button>
                      </div>
                    </div>
                    <div id="edit-target-identity-wrap" class="mb-4 hidden">
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.targetCredentialIdentity')}</label>
                      <select id="edit-target-credential-identity" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                        <option value="">${t('app.credentialsNone')}</option>
                      </select>
                    </div>
                    <div id="edit-target-ssh-key-wrap" class="mb-4 hidden">
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.targetCredentialKey')}</label>
                      <select id="edit-target-ssh-key" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                        <option value="">${t('app.credentialsPickKey')}</option>
                      </select>
                    </div>
                    <div id="edit-target-auth-type-wrap" class="space-y-3 hidden">
                      <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.authMethodLabel')}</label>
                      <div class="space-y-2">
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" name="edit-target-auth-type" value="password" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" />
                          <span class="text-sm text-slate-800">${t('app.authPassword')}</span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" name="edit-target-auth-type" value="key" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" />
                          <span class="text-sm text-slate-800">${t('app.authPublicKey')}</span>
                        </label>
                        <label class="flex items-center gap-2 cursor-pointer">
                          <input type="radio" name="edit-target-auth-type" value="key_passphrase" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" />
                          <span class="text-sm text-slate-800">${t('app.authPublicKeyWithPp')}</span>
                        </label>
                      </div>
                    </div>
                    <div class="space-y-5 mt-4">
                      <div id="edit-target-username-wrap">
                        <label id="edit-target-username-label" class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.targetUsernameOpt')}</label>
                        <input type="text" id="edit-target-ssh-username" value="${escapeHtml(target.ssh_username || '')}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('app.placeholderRoot')}" />
                      </div>
                      <div id="edit-target-password-wrap">
                        <label id="edit-target-password-label" class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.targetPasswordOpt')}</label>
                        <input type="password" id="edit-target-ssh-password" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('app.editTargetPasswordHint')}" />
                      </div>
                      <div id="edit-target-key-wrap" class="hidden">
                        <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.targetPrivateKeyLabel')}</label>
                        <textarea id="edit-target-ssh-private-key" rows="4" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono placeholder-slate-400" placeholder="${target.has_ssh_key ? t('app.editTargetKeyPlaceholderConfigured') : t('app.targetPrivateKeyPlaceholder')}" autocomplete="off"></textarea>
                        ${target.has_ssh_key ? `<label class="mt-1.5 flex items-center gap-2 text-xs text-slate-600"><input type="checkbox" id="edit-target-clear-ssh-key" class="rounded border-slate-300" /> ${t('app.editTargetClearKey')}</label>` : ''}
                      </div>
                      <div id="edit-target-passphrase-wrap" class="hidden">
                        <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('app.targetKeyPassphraseLabel')}</label>
                        <input type="password" id="edit-target-ssh-key-passphrase" autocomplete="off" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="${t('app.editTargetKeyPassphraseHint')}" />
                      </div>
                    </div>
                  </div>
                </div>
              </div>
              <p id="edit-target-error" class="text-sm text-red-600 hidden mt-5"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="edit-target-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('app.cancel')}</button>
              <button type="submit" id="edit-target-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('app.editTargetUpdate')}</button>
            </div>
          </form>
        </div>
      </div>
    `
    // 画面上のタグ入力からは内部タグ（TFTP_CAPABILITY_TAG, SFTP_DISABLED_TAG）を除外して表示する。
    const displayTags = (target.tags || []).filter((t) => t !== TFTP_CAPABILITY_TAG && t !== SFTP_DISABLED_TAG)
    const tagsInput = modal.querySelector('#edit-target-tags')
    if (tagsInput) {
      tagsInput.value = escapeHtml((displayTags || []).join(', '))
    }
    fillExistingTagsPicker(modal, 'edit-target-tags')
    const editDefaultPorts = { ssh: 22, telnet: 23, vnc: 5900, rdp: 3389, tftp: 69, ftp: 21 }
    const editProtoSelect = modal.querySelector('#edit-target-protocol')
    const editCredFields = modal.querySelector('#edit-target-cred-fields')
    const editIdentityWrap = modal.querySelector('#edit-target-identity-wrap')
    const editIdentitySelect = modal.querySelector('#edit-target-credential-identity')
    const editSshKeyWrap = modal.querySelector('#edit-target-ssh-key-wrap')
    const editSshKeySelect = modal.querySelector('#edit-target-ssh-key')
    const editCredManageBtn = modal.querySelector('#edit-target-manage-credentials')
    const editUsernameWrap = modal.querySelector('#edit-target-username-wrap')
    const editAuthTypeWrap = modal.querySelector('#edit-target-auth-type-wrap')
    const editPasswordWrap = modal.querySelector('#edit-target-password-wrap')
    const editPortInput = modal.querySelector('#edit-target-port')
    const editKeyWrap = modal.querySelector('#edit-target-key-wrap')
    const editPassphraseWrap = modal.querySelector('#edit-target-passphrase-wrap')
    const editUsernameLabel = modal.querySelector('#edit-target-username-label')
    const editPasswordLabel = modal.querySelector('#edit-target-password-label')
    const editRdpResWrap = modal.querySelector('#edit-target-rdp-res-wrap')
    const editRdpWidthInput = modal.querySelector('#edit-target-rdp-width')
    const editRdpHeightInput = modal.querySelector('#edit-target-rdp-height')
    const editFileProtocolsWrap = modal.querySelector('#edit-target-file-protocols-wrap')
    const editSftpLabel = modal.querySelector('#edit-target-sftp-label')
    const editSftpCheckbox = modal.querySelector('#edit-target-enable-sftp')
    const editFtpCheckbox = modal.querySelector('#edit-target-enable-ftp')
    const editTftpCheckbox = modal.querySelector('#edit-target-enable-tftp')
    const editHostKeyWrap = modal.querySelector('#edit-target-host-key-wrap')
    const editHostKeyFpEl = modal.querySelector('#edit-target-host-key-fp')
    const editHostKeyStatusEl = modal.querySelector('#edit-target-host-key-status')
    const editHostKeyRefetchBtn = modal.querySelector('#edit-target-host-key-refetch')
    const editHostKeyClearBtn = modal.querySelector('#edit-target-host-key-clear')
    // Track the fingerprint the user has staged (probed but not yet
    // saved). Empty string = same as the persisted value; explicit
    // "__cleared" sentinel = user pressed Clear and we should send
    // "" to the API on save.
    let editStagedFingerprint = ''

    let editCredentialIdentities = []
    let editSSHKeys = []
    function getEditCredMode() {
      return modal.querySelector('input[name="edit-target-cred-mode"]:checked')?.value || 'manual'
    }
    async function refreshEditCredentialLists({ keepSelection = true } = {}) {
      const prevIdent = keepSelection && editIdentitySelect ? editIdentitySelect.value : ''
      const prevKey = keepSelection && editSshKeySelect ? editSshKeySelect.value : ''
      if (editIdentitySelect) {
        editIdentitySelect.innerHTML = `<option value="">${t('app.credentialsNone')}</option>`
      }
      if (editSshKeySelect) {
        editSshKeySelect.innerHTML = `<option value="">${t('app.credentialsPickKey')}</option>`
      }
      try {
        const [identRes, keysRes] = await Promise.all([
          API.credentialIdentities(),
          API.sshKeys(),
        ])
        editCredentialIdentities = (identRes && identRes.items) || []
        editSSHKeys = (keysRes && keysRes.items) || []
        editCredentialIdentities.forEach((ident) => {
          const opt = document.createElement('option')
          opt.value = ident.id
          const auth = authMethodLabel(ident.auth_method)
          opt.textContent = `${ident.label || ident.id} (${auth})`
          editIdentitySelect?.appendChild(opt)
        })
        editSSHKeys.forEach((k) => {
          const opt = document.createElement('option')
          opt.value = k.id
          opt.textContent = k.label || k.id
          editSshKeySelect?.appendChild(opt)
        })
      } catch {
        editCredentialIdentities = []
        editSSHKeys = []
      }
      if (prevIdent && editIdentitySelect) editIdentitySelect.value = prevIdent
      if (prevKey && editSshKeySelect) editSshKeySelect.value = prevKey
    }
    function applyEditCredentialIdentity(id) {
      const ident = (editCredentialIdentities || []).find((x) => x.id === id)
      if (!ident) return
      const uEl = modal.querySelector('#edit-target-ssh-username')
      const pwEl = modal.querySelector('#edit-target-ssh-password')
      const kEl = modal.querySelector('#edit-target-ssh-private-key')
      const ppEl = modal.querySelector('#edit-target-ssh-key-passphrase')
      if (uEl) uEl.value = ident.ssh_username || uEl.value
      if (pwEl && ident.has_password) pwEl.placeholder = t('app.credentialsSavedHint')
      if (kEl && ident.has_ssh_key) kEl.placeholder = t('app.credentialsSavedHint')
      if (ppEl && ident.has_passphrase) ppEl.placeholder = t('app.credentialsSavedHint')
    }
    function syncEditCredentialMode() {
      const mode = getEditCredMode()
      const proto = editProtoSelect.value
      const isSsh = proto === 'ssh'
      editIdentityWrap?.classList.toggle('hidden', mode !== 'identity')
      editSshKeyWrap?.classList.toggle('hidden', mode !== 'key')
      editAuthTypeWrap?.classList.toggle('hidden', !isSsh || mode !== 'manual')
      const hideManualSecrets = mode === 'identity' || mode === 'key'
      editPasswordWrap?.classList.toggle('hidden', hideManualSecrets || (isSsh && mode === 'manual' && (modal.querySelector('input[name="edit-target-auth-type"]:checked')?.value || 'password') !== 'password'))
      editKeyWrap?.classList.toggle('hidden', hideManualSecrets || !isSsh || mode !== 'manual' || (modal.querySelector('input[name="edit-target-auth-type"]:checked')?.value || 'password') === 'password')
      editPassphraseWrap?.classList.toggle('hidden', hideManualSecrets || !isSsh || mode !== 'manual' || (modal.querySelector('input[name="edit-target-auth-type"]:checked')?.value || 'password') !== 'key_passphrase')
      editUsernameWrap?.classList.toggle('hidden', mode === 'identity')
      if (mode === 'identity') applyEditCredentialIdentity(editIdentitySelect?.value?.trim() || '')
      if (mode === 'manual' && isSsh) syncEditAuthType()
      else if (!isSsh && (proto === 'rdp' || proto === 'telnet')) {
        editPasswordWrap?.classList.remove('hidden')
        editKeyWrap?.classList.add('hidden')
        editPassphraseWrap?.classList.add('hidden')
      }
    }
    function syncEditHostKeyClearBtn() {
      if (!editHostKeyClearBtn) return
      const persisted = (target.ssh_host_key_fingerprint || '').trim()
      const staged = editStagedFingerprint
      const hasFingerprint = staged === '__cleared'
        ? false
        : (staged ? true : persisted !== '')
      editHostKeyClearBtn.disabled = !hasFingerprint
      editHostKeyClearBtn.classList.toggle('opacity-50', !hasFingerprint)
      editHostKeyClearBtn.classList.toggle('cursor-not-allowed', !hasFingerprint)
    }
    function setEditHostKeyFp(value) {
      const v = (value || '').trim()
      if (v) {
        editHostKeyFpEl.textContent = v
        editHostKeyFpEl.classList.remove('text-slate-500')
        editHostKeyFpEl.classList.add('text-slate-800')
      } else {
        editHostKeyFpEl.textContent = t('hostKey.notRegistered')
        editHostKeyFpEl.classList.add('text-slate-500')
      }
      syncEditHostKeyClearBtn()
    }
    // Render whatever the dataset already gave us first so the
    // modal does not flash "未登録" when the cached value is fine,
    // then asynchronously refresh from the API in case the value
    // was adopted via the terminal-page TOFU flow in a different
    // tab. If the fresh value differs and the user has not staged
    // anything yet, swap the displayed fingerprint and update the
    // baseline `target.ssh_host_key_fingerprint` so save-time diffs
    // remain accurate.
    setEditHostKeyFp(target.ssh_host_key_fingerprint || '')
    ;(async () => {
      try {
        const all = await API.targets()
        const fresh = Array.isArray(all)
          ? all.find((x) => x && x.id === target.id)
          : null
        if (!fresh) return
        const next = fresh.ssh_host_key_fingerprint || ''
        if (next === (target.ssh_host_key_fingerprint || '')) return
        target.ssh_host_key_fingerprint = next
        if (!editStagedFingerprint) {
          setEditHostKeyFp(next)
        } else {
          syncEditHostKeyClearBtn()
        }
      } catch { /* keep cached value */ }
    })()

    function syncEditAuthType() {
      if (getEditCredMode() !== 'manual') {
        syncEditCredentialMode()
        return
      }
      const proto = editProtoSelect.value
      if (proto === 'rdp' || proto === 'telnet') {
        editPasswordWrap.classList.remove('hidden')
        editKeyWrap.classList.add('hidden')
        editPassphraseWrap.classList.add('hidden')
        return
      }
      if (proto !== 'ssh') return
      const authType = modal.querySelector('input[name="edit-target-auth-type"]:checked')?.value || 'password'
      editPasswordWrap.classList.toggle('hidden', authType !== 'password')
      editKeyWrap.classList.toggle('hidden', authType === 'password')
      editPassphraseWrap.classList.toggle('hidden', authType !== 'key_passphrase')
    }
    function syncEditProtocol() {
      const proto = editProtoSelect.value
      const hasCreds = proto === 'ssh' || proto === 'telnet' || proto === 'rdp' || proto === 'ftp'
      editCredFields.style.display = hasCreds ? '' : 'none'
      if (editHostKeyWrap) {
        editHostKeyWrap.classList.toggle('hidden', proto !== 'ssh')
      }
      editUsernameLabel.textContent =
        proto === 'telnet' ? t('app.telnetUsernameOpt')
          : proto === 'rdp' ? t('app.rdpUsernameOpt')
            : proto === 'ftp' ? t('app.ftpUsernameOpt')
              : t('app.sshUsernameOpt')
      editPasswordLabel.textContent =
        proto === 'telnet' ? t('app.telnetPasswordOpt')
          : proto === 'rdp' ? t('app.rdpPasswordOpt')
            : proto === 'ftp' ? t('app.ftpPasswordOpt')
              : t('app.sshPasswordOpt')
      editRdpResWrap.classList.toggle('hidden', proto !== 'rdp')
      const canEditFileProtocols = proto === 'ssh' || proto === 'telnet'
      if (editFileProtocolsWrap) {
        editFileProtocolsWrap.classList.toggle('hidden', !canEditFileProtocols)
      }
      if (editSftpLabel && editSftpCheckbox) {
        editSftpLabel.classList.toggle('hidden', proto !== 'ssh')
        // DB の sftp_enabled のみで判定（タグには依存しない）
        const sftpEnabled = proto === 'ssh' && (target.sftp_enabled !== false)
        editSftpCheckbox.checked = sftpEnabled
      }
      if (!canEditFileProtocols) {
        if (editFtpCheckbox) editFtpCheckbox.checked = false
        if (editTftpCheckbox) editTftpCheckbox.checked = false
      }
      syncEditCredentialMode()
    }
    editProtoSelect.addEventListener('change', () => {
      syncEditProtocol()
      const proto = editProtoSelect.value
      if (editDefaultPorts[proto] !== undefined) {
        editPortInput.value = editDefaultPorts[proto]
      }
    })
    modal.querySelectorAll('input[name="edit-target-auth-type"]').forEach((radio) => {
      radio.addEventListener('change', syncEditAuthType)
    })
    modal.querySelectorAll('input[name="edit-target-cred-mode"]').forEach((radio) => {
      radio.addEventListener('change', syncEditCredentialMode)
    })
    const initialAuthType = target.protocol === 'ssh'
      ? (target.has_ssh_key ? ((target.needs_passphrase || target.has_passphrase) ? 'key_passphrase' : 'key') : 'password')
      : 'password'
    const initialAuthRadio = modal.querySelector(`input[name="edit-target-auth-type"][value="${initialAuthType}"]`)
    if (initialAuthRadio) initialAuthRadio.checked = true
    syncEditProtocol()

    void refreshEditCredentialLists().then(() => {
      // 資格情報リスト読み込み後、登録済みの Identity / SSH Key を選択状態にする。
      if (initialCredMode === 'identity' && target.credential_identity_id && editIdentitySelect) {
        editIdentitySelect.value = target.credential_identity_id
      }
      if (initialCredMode === 'key' && target.ssh_key_id && editSshKeySelect) {
        editSshKeySelect.value = target.ssh_key_id
      }
      syncEditCredentialMode()
    })
    editIdentitySelect?.addEventListener('change', () => applyEditCredentialIdentity(editIdentitySelect.value.trim()))
    editCredManageBtn?.addEventListener('click', async () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
      await showCredentialsPage()
    })

    // ファイル転送プロトコルの初期状態（SFTP は SSH のときのみ選択可能）
    if ((target.protocol === 'ssh' || target.protocol === 'telnet') && editFileProtocolsWrap) {
      editFileProtocolsWrap.classList.remove('hidden')
      if (editSftpLabel) editSftpLabel.classList.toggle('hidden', target.protocol !== 'ssh')
      if (editSftpCheckbox) {
        const sftpEnabled = target.protocol === 'ssh' && (target.sftp_enabled !== false)
        editSftpCheckbox.checked = sftpEnabled
      }
      if (editFtpCheckbox) editFtpCheckbox.checked = !!target.ftp_enabled
      if (editTftpCheckbox) editTftpCheckbox.checked = !!target.tftp_enabled
    }

    const { w: initialRdpW, h: initialRdpH } = getRdpResolutionForTarget(target.id)
    if (editRdpWidthInput) editRdpWidthInput.value = initialRdpW || 1920
    if (editRdpHeightInput) editRdpHeightInput.value = initialRdpH || 1080

    modal.querySelector('#edit-target-close').addEventListener('click', () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    })
    modal.querySelector('#edit-target-cancel').addEventListener('click', () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    })
    if (editHostKeyRefetchBtn) {
      editHostKeyRefetchBtn.addEventListener('click', async () => {
        const host = modal.querySelector('#edit-target-host').value.trim()
        const port = parseInt(modal.querySelector('#edit-target-port').value, 10) || 22
        if (!host || editProtoSelect.value !== 'ssh') return
        editHostKeyStatusEl.textContent = t('hostKey.fetching')
        editHostKeyRefetchBtn.disabled = true
        try {
          const result = await API.probeHostKey({ host, port })
          if (result.fingerprint && result.fingerprint !== (target.ssh_host_key_fingerprint || '')) {
            editStagedFingerprint = result.fingerprint
            setEditHostKeyFp(result.fingerprint)
            editHostKeyStatusEl.textContent = t('hostKey.probedAt', { time: new Date().toLocaleTimeString() })
          } else {
            editStagedFingerprint = ''
            setEditHostKeyFp(result.fingerprint)
            editHostKeyStatusEl.textContent = t('hostKey.probedAt', { time: new Date().toLocaleTimeString() })
          }
        } catch (err) {
          editHostKeyStatusEl.textContent = t('hostKey.probeFailed', { error: err.message || String(err) })
        } finally {
          editHostKeyRefetchBtn.disabled = false
        }
      })
    }
    if (editHostKeyClearBtn) {
      editHostKeyClearBtn.addEventListener('click', () => {
        editStagedFingerprint = '__cleared'
        setEditHostKeyFp('')
        editHostKeyStatusEl.textContent = t('hostKey.cleared')
      })
    }
    modal.querySelector('#edit-target-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const form = modal.querySelector('#edit-target-form')
      const targetId = form.dataset.editTargetId || ''
      if (!targetId) return
      const errorEl = modal.querySelector('#edit-target-error')
      const submitBtn = modal.querySelector('#edit-target-submit')
      errorEl.classList.add('hidden')
      const name = modal.querySelector('#edit-target-name').value.trim()
      const host = modal.querySelector('#edit-target-host').value.trim()
      const port = parseInt(modal.querySelector('#edit-target-port').value, 10) || 22
      const protocol = modal.querySelector('#edit-target-protocol').value
      const group_id = (modal.querySelector('#edit-target-group')?.value || '').trim()
      const credMode = getEditCredMode()
      const ssh_username = modal.querySelector('#edit-target-ssh-username').value.trim()
      const credential_identity_id = editIdentitySelect ? editIdentitySelect.value.trim() : ''
      const ssh_key_id = editSshKeySelect ? editSshKeySelect.value.trim() : ''
      const authType = protocol === 'ssh' ? (modal.querySelector('input[name="edit-target-auth-type"]:checked')?.value || 'password') : 'password'
      const hasCreds = protocol === 'ssh' || protocol === 'telnet' || protocol === 'rdp' || protocol === 'ftp'
      const clearKeyChecked = modal.querySelector('#edit-target-clear-ssh-key') && modal.querySelector('#edit-target-clear-ssh-key').checked
      let ssh_password, ssh_private_key, ssh_private_key_passphrase
      if (hasCreds && credMode === 'identity' && !credential_identity_id) {
        errorEl.textContent = t('app.targetCredentialIdentityRequired')
        errorEl.classList.remove('hidden')
        return
      }
      if (hasCreds && credMode === 'key') {
        if (!ssh_key_id) {
          errorEl.textContent = t('app.targetCredentialKeyRequired')
          errorEl.classList.remove('hidden')
          return
        }
        if (!ssh_username) {
          errorEl.textContent = t('app.targetCredentialUsernameRequired')
          errorEl.classList.remove('hidden')
          return
        }
      }
      if (credMode === 'manual') {
        if (protocol === 'rdp' || protocol === 'telnet' || authType === 'password') {
          const pwVal = modal.querySelector('#edit-target-ssh-password').value
          ssh_password = pwVal === '' ? undefined : pwVal
        }
        if (protocol === 'ssh') {
          if (authType === 'key' || authType === 'key_passphrase') {
            const keyVal = modal.querySelector('#edit-target-ssh-private-key').value
            ssh_private_key = clearKeyChecked ? '' : (keyVal === '' ? undefined : keyVal)
            if (authType === 'key_passphrase') {
              const keyPassVal = modal.querySelector('#edit-target-ssh-key-passphrase').value
              ssh_private_key_passphrase = clearKeyChecked ? '' : (keyPassVal === '' ? undefined : keyPassVal)
            } else {
              // "公開鍵" (パスフレーズなし) 選択時は、以前 key_passphrase で
              // 保存されていたパスフレーズが残ってしまわないよう明示的に消す。
              ssh_private_key_passphrase = ''
            }
          } else {
            // パスワード認証を選択した場合、以前登録された秘密鍵/パスフレーズが
            // 残って接続時にパスフレーズを要求され続けないよう明示的に消す。
            ssh_private_key = ''
            ssh_private_key_passphrase = ''
          }
        }
      }
      const tagsRaw = modal.querySelector('#edit-target-tags').value.trim()
      const userTags = tagsRaw ? tagsRaw.split(',').map((s) => s.trim()).filter(Boolean) : []
      const tags = userTags.slice()
      const enableSftpEdit = !!(editSftpCheckbox && editFileProtocolsWrap && !editFileProtocolsWrap.classList.contains('hidden') && editSftpCheckbox.checked)
      const enableFtpEdit = !!(editFtpCheckbox && editFileProtocolsWrap && !editFileProtocolsWrap.classList.contains('hidden') && editFtpCheckbox.checked)
      const enableTftpEdit = !!(editTftpCheckbox && editFileProtocolsWrap && !editFileProtocolsWrap.classList.contains('hidden') && editTftpCheckbox.checked)
      const prevTftpEnabled = !!target.has_tftp_for_host
      if (!name || !host) {
        errorEl.textContent = t('app.nameHostRequired')
        errorEl.classList.remove('hidden')
        return
      }
      const updatePayload = { name, host, port, protocol, path: target.path || '', ssh_username }
      if (group_id) updatePayload.group_id = group_id
      // Always send both explicitly (even as '') so the backend's "omit
      // = leave unchanged" pointer semantics see this as a deliberate
      // choice -- omitting them here (the old behavior) would leave a
      // switch to "manual" silently unable to detach a previously
      // linked Identity/SSH Key, since nothing would tell the backend
      // credential source is even being touched.
      updatePayload.credential_identity_id = credMode === 'identity' ? credential_identity_id : ''
      updatePayload.ssh_key_id = credMode === 'key' ? ssh_key_id : ''
      if (protocol === 'ssh' || protocol === 'telnet') {
        updatePayload.sftp_enabled = protocol === 'ssh' ? enableSftpEdit : false
        updatePayload.ftp_enabled = enableFtpEdit
        updatePayload.tftp_enabled = enableTftpEdit
      }
      if (ssh_password !== undefined) updatePayload.ssh_password = ssh_password
      if (ssh_private_key !== undefined) updatePayload.ssh_private_key = ssh_private_key
      if (ssh_private_key_passphrase !== undefined) updatePayload.ssh_private_key_passphrase = ssh_private_key_passphrase
      submitBtn.disabled = true
      try {
        await API.updateTarget(targetId, updatePayload)
        if (protocol === 'rdp') {
          const rdpW = parseInt(editRdpWidthInput.value, 10) || 1920
          const rdpH = parseInt(editRdpHeightInput.value, 10) || 1080
          setRdpResolutionForTarget(targetId, rdpW, rdpH)
        }
        // Host key: only call the dedicated endpoint when the user
        // staged a change. Empty string clears, "SHA256:..." adopts.
        if (protocol === 'ssh' && editStagedFingerprint) {
          const fp = editStagedFingerprint === '__cleared' ? '' : editStagedFingerprint
          try {
            await API.updateTargetHostKey(targetId, fp)
          } catch (hkErr) {
            errorEl.textContent = t('hostKey.updateFailed', { error: hkErr.message || String(hkErr) })
            errorEl.classList.remove('hidden')
            submitBtn.disabled = false
            return
          }
        }
        await API.setTargetTags(targetId, tags)
        // TFTP はここではターゲットを自動作成しない（ホーム画面の TFTP トグルのみで起動する）。
        if (protocol === 'ssh' || protocol === 'telnet') {
          if (!enableTftpEdit && prevTftpEnabled && target.tftp_target_id) {
            try {
              await API.deleteTarget(target.tftp_target_id)
            } catch (e) { console.error('Failed to delete TFTP target', e) }
          }
        }
        modal.classList.add('hidden')
        modal.innerHTML = ''
        groupsCache = null
        await showTreeView('manage')
      } catch (err) {
        errorEl.textContent = err.message || t('app.updateFailed')
        errorEl.classList.remove('hidden')
      } finally {
        submitBtn.disabled = false
      }
    })
  }


  function buildGroupTree(groups) {
    const root = { id: '', name: 'root', children: {}, group: null }
      ; (groups || []).forEach((g) => {
        const id = (g.id || '').trim()
        if (!id) return
        const parts = id.split('/').filter(Boolean)
        let node = root
        let acc = ''
        parts.forEach((part, idx) => {
          acc = acc ? `${acc}/${part}` : part
          if (!node.children[part]) {
            node.children[part] = { id: acc, name: part, children: {}, group: null }
          }
          node = node.children[part]
          if (idx === parts.length - 1) {
            node.group = g
          }
        })
      })
    return root
  }

  function ensureGroupPathExpanded(groupId, expandedSet) {
    expandedSet.add(ROOT_TOGGLE_ID)
    if (!groupId) return
    const parts = groupId.split('/').filter(Boolean)
    let acc = ''
    for (let i = 0; i < parts.length - 1; i++) {
      acc = acc ? `${acc}/${parts[i]}` : parts[i]
      expandedSet.add(acc)
    }
  }

  /** selectedId: highlight this group row. expandedSet: which nodes are expanded in the tree. */
  function renderGroupTree(node, selectedId, depth = 0, expandedSet = expandedGroups) {
    const children = node.children || {}
    const keys = Object.keys(children)
    if (keys.length === 0) {
      return ''
    }
    if (depth === 0) {
      const isSelectedRoot = selectedId === ''
      const rowClassRoot = isSelectedRoot ? 'bg-sky-100 text-sky-800 font-medium' : ''
      const isRootExpanded = expandedSet.has(ROOT_TOGGLE_ID)
      const rootCaret = isRootExpanded ? '▼' : '▶'
      return `
        <ul class="space-y-1">
          <li>
            <div class="flex items-start py-1.5 pr-2 rounded hover:bg-slate-50 cursor-pointer ${rowClassRoot}" data-group-select="1" data-group-id="">
              <div class="w-5 flex items-center justify-center text-[10px] text-slate-700 hover:text-slate-900 leading-none cursor-pointer shrink-0 self-stretch" data-group-toggle="1" data-group-id="${ROOT_TOGGLE_ID}">${rootCaret}</div>
              <div class="w-3 h-3 rounded-sm bg-slate-200 border border-slate-300 shrink-0 mt-0.5"></div>
              <div class="flex-1 min-w-0 pl-2">
                <div class="text-xs font-medium text-slate-800">root</div>
                <div class="text-[10px] text-slate-500 leading-snug">${t('app.treeGroupCount', { n: keys.length })}</div>
              </div>
            </div>
            ${isRootExpanded ? renderGroupTree(node, selectedId, 1, expandedSet) : ''}
          </li>
        </ul>
      `
    }
    // Indent per level is halved from the old pl-4+ml-2 (24px) to pl-3
    // (12px), and stops growing past depth 6 (deeper rows still get the
    // vertical guide line, just no further leftward push) so a handful of
    // nesting levels doesn't eat the whole sidebar width before any text
    // is shown.
    const padClass = depth <= 6 ? 'pl-3 border-l border-slate-200' : 'border-l border-slate-200'
    let html = `<ul class="space-y-1 ${padClass}">`
    keys
      .slice()
      .sort((a, b) => a.localeCompare(b))
      .forEach((key) => {
        const child = children[key]
        const gid = child.id || ''
        const groupName = child.group?.name || child.name || ''
        const count = (child.group && child.group.targets ? child.group.targets.length : 0) || 0
        const isSelected = child.id === selectedId
        const rowClass = isSelected ? 'bg-sky-100 text-sky-800 font-medium' : ''
        const hasChildren = child.children && Object.keys(child.children).length > 0
        const isExpanded = expandedSet.has(child.id)
        const caret = hasChildren ? (isExpanded ? '▼' : '▶') : ''
        const caretHtml = hasChildren
          ? `<div class="w-5 flex items-center justify-center text-[10px] text-slate-700 hover:text-slate-900 leading-none cursor-pointer shrink-0 self-stretch" data-group-toggle="1" data-group-id="${escapeHtml(child.id)}">${caret}</div>`
          : `<div class="w-5 shrink-0 self-stretch" data-group-toggle="0"></div>`
        const actionHtml = (mainContent?.dataset?.treeMode === 'manage' && gid)
          ? `
              <div class="flex items-center gap-1 shrink-0">
                <button type="button" class="rounded border border-slate-300 bg-white px-2 py-0.5 text-[10px] font-semibold text-slate-700 hover:bg-slate-50" data-group-edit="1" data-group-id="${escapeHtml(gid)}">${t('common.edit')}</button>
                <button type="button" class="rounded border border-red-200 bg-white px-2 py-0.5 text-[10px] font-semibold text-red-700 hover:bg-red-50" data-group-delete="1" data-group-id="${escapeHtml(gid)}" data-group-name="${escapeHtml(groupName)}">${t('common.delete')}</button>
              </div>
            `
          : ''

        html += `
          <li>
            <div class="flex items-start py-1.5 pr-2 rounded hover:bg-slate-50 cursor-pointer ${rowClass}" data-group-select="1" data-group-id="${escapeHtml(child.id)}">
              ${caretHtml}
              <div class="w-3 h-3 rounded-sm bg-slate-200 border border-slate-300 shrink-0 mt-0.5"></div>
              <div class="flex-1 min-w-0 pl-2">
                <div class="text-xs font-medium text-slate-800 truncate" title="${escapeHtml(child.name)}">${escapeHtml(child.name)}</div>
                <div class="text-[10px] text-slate-500 leading-snug truncate" title="${escapeHtml(child.id)}">${escapeHtml(child.id)}${count ? ` · ${t('app.treeTargetCount', { n: count })}` : ''}</div>
              </div>
              ${actionHtml}
            </div>
            ${hasChildren && isExpanded ? renderGroupTree(child, selectedId, depth + 1, expandedSet) : ''}
          </li>
        `
      })
    html += '</ul>'
    return html
  }

  ; (async () => {
    try {
      meData = await API.me()
      userNameEl.textContent = meData.username
      showAuthenticatedNav(meData.role === 'admin')
      setupGlobalRealtimeWatches()
    } catch {
      teardownGlobalRealtimeWatches()
      renderLogin(container)
      return
    }

    try {
      groupsCache = await API.groups()
    } catch {
      groupsCache = null
    }

    const initialView = new URLSearchParams(window.location.search).get('view')
    if (initialView === 'sessions') {
      try {
        const url = new URL(window.location.href)
        url.searchParams.delete('view')
        window.history.replaceState({}, '', url.toString())
      } catch { /* ignore */ }
      showSessionsPage()
    } else if (initialView === 'credentials') {
      try {
        const url = new URL(window.location.href)
        url.searchParams.delete('view')
        window.history.replaceState({}, '', url.toString())
      } catch { /* ignore */ }
      showCredentialsPage()
    } else {
      showTreeView('home')
    }
  })()

  initNav({
    navTargets,
    navSessions,
    navRecordings,
    navRecordingExports,
    navGroups,
    navUsers,
    navCredentials,
    navAudit,
    navAccessRequests,
    navSystem,
    navSettings,
    getMe: () => meData,
    onHome: () => showTreeView('home'),
    onSessions: () => showSessionsPage(),
    onRecordings: () => showRecordingsPage(),
    onRecordingExports: () => showRecordingExportsPage(),
    onGroups: () => showTreeView('manage'),
    onUsers: () => showUsersPage(),
    onCredentials: () => showCredentialsPage(),
    onAudit: () => showAuditLogs(),
    onAccessRequests: () => showAccessRequests(),
    onSystem: () => showSystemSettings(),
    onSettings: () => showSettings(),
  })

  userNameEl.addEventListener('click', (e) => {
    e.preventDefault()
    if (!meData) return
    showUserInfo()
  })

  logoutBtn.addEventListener('click', async () => {
    teardownGlobalRealtimeWatches()
    try {
      await API.logout()
    } catch {
      /* サーバー不通時も renderLogin で UI はログイン画面へ戻す（HttpOnly Cookie は JS から削除不可） */
    }
    renderLogin(container)
  })

  // テーマ切替（ライト/ダーク）。実体は theme.js に集約しており、ここでは
  // クリック時にトグルを呼ぶだけ。renderApp 再呼出時にも先頭で applyStoredTheme
  // を実行しているため、画面遷移後でも保存済みテーマが必ず反映される。
  const themeToggleBtn = document.getElementById('theme-toggle')
  if (themeToggleBtn) {
    themeToggleBtn.addEventListener('click', () => {
      toggleStoredTheme()
    })
  }

  // Language switcher: persist via i18n, then re-render the whole shell
  // so every `t(...)` lookup in the chrome and the active page picks up
  // the new dictionary. We rely on the i18nchange event so the same
  // re-render happens for other surfaces (e.g. settings page select).
  const languageSwitch = document.getElementById('language-switch')
  if (languageSwitch) {
    languageSwitch.addEventListener('change', () => {
      setLocale(languageSwitch.value)
    })
  }
  const unsubscribeLocale = onLocaleChange(() => {
    if (typeof unsubscribeLocale === 'function') unsubscribeLocale()
    renderApp(container)
  })
}

