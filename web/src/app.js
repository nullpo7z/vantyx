import API from './api.js'
import { renderLogin } from './login.js'
import * as AsciinemaPlayer from 'asciinema-player'
import 'asciinema-player/dist/bundle/asciinema-player.css'

export function renderApp(container) {
  container.innerHTML = `
    <div class="flex-1 flex flex-col">
      <header class="bg-sky-800 text-white px-6 py-3 flex items-center justify-between shadow z-10 shrink-0">
        <div class="flex items-center gap-8">
          <h1 class="text-xl font-semibold tracking-wide">Vantyx</h1>
          <nav class="flex items-center gap-6">
            <a href="#" id="nav-targets" class="text-sm font-semibold border-b-2 border-white pb-1 transition-opacity">ホーム</a>
            <a href="#" id="nav-recordings" class="text-sm opacity-80 hover:opacity-100 transition-opacity hidden">録画</a>
            <a href="#" id="nav-groups" class="text-sm opacity-80 hover:opacity-100 transition-opacity hidden">サーバー管理</a>
            <a href="#" id="nav-users" class="text-sm opacity-80 hover:opacity-100 transition-opacity hidden">ユーザー管理</a>
            <a href="/docs" id="nav-api-ref" target="_blank" rel="noopener noreferrer" class="text-sm opacity-80 hover:opacity-100 transition-opacity hidden">API リファレンス</a>
          </nav>
        </div>
        <div class="flex items-center gap-4">
          <button id="user-name" class="text-sm font-medium opacity-90 hover:opacity-100 hover:underline focus:outline-none focus:ring-1 focus:ring-white/70 rounded px-1 cursor-pointer"></button>
          <div class="w-px h-4 bg-white/20"></div>
          <button id="logout-btn" class="text-sm opacity-80 hover:opacity-100 transition-opacity">ログアウト</button>
        </div>
      </header>
      <main class="flex-1 overflow-auto p-6 flex flex-col items-center" id="main-content">
        <div class="w-full max-w-5xl flex-1 flex flex-col">
          <p class="text-slate-500">読み込み中…</p>
        </div>
      </main>
      <div id="add-target-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="add-user-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="add-member-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="edit-tags-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="ssh-credential-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="active-sessions-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="recording-player-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="change-password-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
      <div id="add-ssh-key-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
    </div>
  `

  const mainContent = document.getElementById('main-content')
  const userNameEl = document.getElementById('user-name')
  const logoutBtn = document.getElementById('logout-btn')
  const navTargets = document.getElementById('nav-targets')
  const navRecordings = document.getElementById('nav-recordings')
  const navGroups = document.getElementById('nav-groups')
  const navUsers = document.getElementById('nav-users')

  let meData = null
  let groupsCache = null
  let selectedGroupId = ''
  let expandedGroups = new Set()
  /** 録画ページ用: 選択中のグループID・ターゲットID（サーバー）・表示名 */
  let selectedRecordingsGroupId = ''
  let selectedRecordingsTargetId = ''
  let selectedRecordingsTargetName = ''
  /** 新しいタブに渡す SSH 認証情報（BroadcastChannel 用） */
  const pendingTerminalCreds = Object.create(null)
  /** ターミナルタブ用: 親タブへフォーカス要求するための待受（opener が無い環境向け） */
  const pendingTerminalParents = Object.create(null)

  function randomToken() {
    const b = new Uint8Array(16)
    crypto.getRandomValues(b)
    return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
  }

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
      } catch { /* ignore */ }
      window.clearTimeout(timeoutId)
      try { bc.close() } catch { /* ignore */ }
      delete pendingTerminalParents[token]
    }

    // ターミナルタブ側は parent_token (BroadcastChannel) で親タブへ戻れるため、opener は無効化する。
    window.open(u.toString(), '_blank', 'noopener')
  }

  function getRdpResolutionForTarget(id) {
    try {
      if (!id) return { w: 1920, h: 1080 }
      const raw = localStorage.getItem(`vantyx_rdp_res_${id}`)
      if (!raw) return { w: 1920, h: 1080 }
      const parsed = JSON.parse(raw)
      const w = Number(parsed.w) || 1920
      const h = Number(parsed.h) || 1080
      return { w, h }
    } catch {
      return { w: 1920, h: 1080 }
    }
  }

  function setRdpResolutionForTarget(id, w, h) {
    try {
      if (!id) return
      const ww = Number(w) || 0
      const hh = Number(h) || 0
      if (!ww || !hh) {
        localStorage.removeItem(`vantyx_rdp_res_${id}`)
        return
      }
      localStorage.setItem(`vantyx_rdp_res_${id}`, JSON.stringify({ w: ww, h: hh }))
    } catch {
      // ignore storage errors
    }
  }
  function openPopup(url, title, w = 1280, h = 800) {
    const left = Math.max(0, Math.round((window.screen.width - w) / 2))
    const top = Math.max(0, Math.round((window.screen.height - h) / 2))
    const feats = [
      'popup=yes',
      'resizable=yes',
      'scrollbars=no',
      'noopener=yes',
      `width=${w}`,
      `height=${h}`,
      `left=${left}`,
      `top=${top}`,
    ].join(',')
    window.open(url, title || '_blank', feats)
  }

  // When this tab regains focus, refresh active sessions modal if open.
  window.addEventListener('focus', () => {
    try {
      const m = document.getElementById('active-sessions-modal')
      if (m && !m.classList.contains('hidden') && typeof m._vantyxRefreshActiveSessions === 'function') {
        m._vantyxRefreshActiveSessions()
      }
    } catch { /* ignore */ }
  })

  function showUserInfo() {
    if (!meData) return
    const navHidden = meData?.role !== 'admin' ? ' hidden' : ''
    navTargets.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity'
    navGroups.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity' + navHidden
    navUsers.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity' + navHidden
    navRecordings.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity'
    mainContent.innerHTML = `
      <h2 class="text-lg font-medium text-slate-800 mb-4">ユーザー情報</h2>
      <div class="bg-white rounded-lg border border-slate-200 shadow-sm overflow-hidden">
        <dl class="divide-y divide-slate-200">
          <div class="px-4 py-3 sm:grid sm:grid-cols-3 sm:gap-4">
            <dt class="text-sm font-medium text-slate-500">ユーザーID</dt>
            <dd class="mt-1 text-sm text-slate-800 sm:mt-0 sm:col-span-2">${escapeHtml(meData.user_id)}</dd>
          </div>
          <div class="px-4 py-3 sm:grid sm:grid-cols-3 sm:gap-4">
            <dt class="text-sm font-medium text-slate-500">ユーザー名</dt>
            <dd class="mt-1 text-sm text-slate-800 sm:mt-0 sm:col-span-2">${escapeHtml(meData.username)}</dd>
          </div>
        </dl>
        <div class="px-4 py-3 border-t border-slate-200">
          <button type="button" id="btn-change-password" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">パスワードを変更</button>
        </div>
      </div>
    `
    mainContent.querySelector('#btn-change-password').addEventListener('click', showChangePasswordModal)
  }

  async function showUsersPage() {
    const navHidden = meData?.role !== 'admin' ? ' hidden' : ''
    navTargets.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity'
    navGroups.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity' + navHidden
    navUsers.className = 'text-sm font-semibold border-b-2 border-white pb-1 transition-opacity' + navHidden
    navRecordings.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity'
    mainContent.innerHTML = '<p class="text-slate-500">読み込み中…</p>'
    try {
      const users = await API.users()
      const rows = (users || []).map((u) => {
        const userTags = Array.isArray(u.tags) ? u.tags : []
        return `
        <tr class="border-b border-slate-200 hover:bg-slate-50">
          <td class="px-4 py-2 text-sm font-medium text-slate-900">${escapeHtml(u.id)}</td>
          <td class="px-4 py-2 text-sm text-slate-700">${escapeHtml(u.username)}</td>
          <td class="px-4 py-2 text-sm text-slate-600">${escapeHtml(u.role || 'user')}</td>
          <td class="px-4 py-2"><div class="flex flex-wrap items-center gap-2">${userTags.length ? renderTagPills(userTags) : '<span class="text-xs text-slate-400">—</span>'} <button type="button" class="edit-user-btn rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50" data-user-id="${escapeHtml(u.id)}" data-username="${escapeHtml(u.username)}" data-user-role="${escapeHtml(u.role || 'user')}" data-user-tags="${escapeHtml((userTags || []).join(','))}">編集</button> <button type="button" class="user-ssh-keys-btn rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50" data-user-id="${escapeHtml(u.id)}" data-username="${escapeHtml(u.username)}">公開鍵</button></div></td>
        </tr>
      `
      }).join('')
      mainContent.innerHTML = `
        <div class="w-full flex flex-col">
          <div class="flex items-center justify-between mb-4">
            <h2 class="text-lg font-medium text-slate-800">ユーザー管理</h2>
            <button type="button" id="btn-add-user" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">ユーザーを追加</button>
          </div>
          <div class="bg-white rounded-lg border border-slate-200 shadow-sm overflow-hidden">
            <div class="overflow-x-auto">
              <table class="min-w-full text-left text-sm">
                <thead class="bg-slate-50 border-b border-slate-200">
                  <tr>
                    <th class="px-4 py-2 text-xs font-semibold text-slate-700">ユーザーID</th>
                    <th class="px-4 py-2 text-xs font-semibold text-slate-700">ユーザー名</th>
                    <th class="px-4 py-2 text-xs font-semibold text-slate-700">ロール</th>
                    <th class="px-4 py-2 text-xs font-semibold text-slate-700">タグ</th>
                  </tr>
                </thead>
                <tbody>${rows || '<tr><td colspan="4" class="px-4 py-6 text-center text-slate-500">ユーザーがありません</td></tr>'}</tbody>
              </table>
            </div>
          </div>
        </div>
      `
      mainContent.querySelector('#btn-add-user').addEventListener('click', showAddUserModal)
      mainContent.querySelectorAll('.edit-user-btn').forEach((btn) => {
        btn.addEventListener('click', () => {
          const user = {
            id: btn.dataset.userId || '',
            username: btn.dataset.username || '',
            role: btn.dataset.userRole || 'user',
            tags: (btn.dataset.userTags || '').split(',').map((s) => s.trim()).filter(Boolean),
          }
          if (user.id) showEditUserModal(user)
        })
      })
      mainContent.querySelectorAll('.user-ssh-keys-btn').forEach((btn) => {
        btn.addEventListener('click', () => {
          showUserSSHKeysModal(btn.dataset.userId || '', btn.dataset.username || '')
        })
      })
    } catch (e) {
      mainContent.innerHTML = `<p class="text-sm text-red-600">${escapeHtml(e.message || '取得に失敗しました')}</p>`
    }
  }

  function showRecordingPlayerModal(recordingId, label, userId, sessionId) {
    const modal = document.getElementById('recording-player-modal')
    modal.classList.remove('hidden')
    const fileUrl = `/api/recordings/${encodeURIComponent(recordingId)}/file`
    const watermarkText = [userId, sessionId].filter(Boolean).length
      ? [userId && `User: ${userId}`, sessionId && `Session: ${sessionId}`].filter(Boolean).join(' · ')
      : ''
    modal.innerHTML = `
      <div id="recording-player-backdrop" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-slate-900 rounded-lg shadow-xl w-full max-w-4xl mx-4 overflow-hidden border border-slate-700 flex flex-col max-h-[90vh]">
          <div class="px-5 py-3 border-b border-slate-700 flex items-center justify-between bg-slate-800 shrink-0">
            <h3 class="font-semibold text-slate-200">録画再生 — ${escapeHtml(label || recordingId)}</h3>
            <button id="recording-player-close" class="text-slate-400 hover:text-white text-2xl leading-none transition-colors">&times;</button>
          </div>
          <div id="recording-player-wrapper" class="p-4 overflow-auto flex-1 min-h-0 relative">
            <div id="recording-player-container"></div>
            ${watermarkText ? `<div id="recording-watermark" class="absolute inset-0 pointer-events-none flex items-end justify-center pb-2 text-slate-500/70 text-xs font-mono select-none" aria-hidden="true">${escapeHtml(watermarkText)}</div>` : ''}
          </div>
        </div>
      </div>
    `
    const container = modal.querySelector('#recording-player-container')
    let player = null
    try {
      // 録画ファイルの width/height のまま表示（fit 指定なし＝崩れ防止）
      player = AsciinemaPlayer.create(fileUrl, container, {})
    } catch (err) {
      container.innerHTML = `<p class="text-sm text-red-400">再生の読み込みに失敗しました: ${escapeHtml(err.message || String(err))}</p>`
    }
    const close = () => {
      if (player && typeof player.dispose === 'function') {
        try { player.dispose() } catch { /* ignore */ }
      }
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#recording-player-close').addEventListener('click', close)
    modal.querySelector('#recording-player-backdrop').addEventListener('click', (e) => { if (e.target.id === 'recording-player-backdrop') close() })
  }

  async function showRecordingsPage() {
    const navHidden = meData?.role !== 'admin' ? ' hidden' : ''
    navTargets.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity'
    navGroups.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity' + navHidden
    navUsers.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity' + navHidden
    navRecordings.className = 'text-sm font-semibold border-b-2 border-white pb-1 transition-opacity'
    mainContent.innerHTML = '<div class="flex gap-6 w-full h-full"><p class="text-slate-500">読み込み中…</p></div>'
    try {
      if (!groupsCache) {
        groupsCache = await API.groups()
      }
      const groups = groupsCache
      const treeRoot = buildGroupTree(groups || [])
      const treeHtml = renderGroupTree(treeRoot, 0, selectedRecordingsGroupId)
      const selectedGroup = (groups || []).find((g) => g.id === selectedRecordingsGroupId)
      const targets = selectedGroup ? (selectedGroup.targets || []) : []

      let sectionContent = ''
      let sectionHeader = ''

      if (selectedRecordingsTargetId) {
        const res = await API.recordings({ target_id: selectedRecordingsTargetId })
        const items = (res && res.items) || []
        const rows = items.map((r) => {
          const label = [r.started_at || '', r.target_id || ''].filter(Boolean).join(' — ') || r.id
          return `
          <tr class="border-b border-slate-200 hover:bg-slate-50">
            <td class="px-4 py-2 text-sm text-slate-700">${escapeHtml(r.started_at || '')}</td>
            <td class="px-4 py-2 text-sm text-slate-700">${escapeHtml(r.ended_at || '—')}</td>
            <td class="px-4 py-2 text-sm font-medium text-slate-900">${escapeHtml(r.session_name || '—')}</td>
            <td class="px-4 py-2 text-sm text-slate-600 max-w-[12rem] truncate" title="${escapeHtml(r.session_description || '')}">${escapeHtml(r.session_description || '—')}</td>
            <td class="px-4 py-2 text-sm text-slate-600">${escapeHtml(r.channel_type || '')}</td>
            <td class="px-4 py-2">
              <div class="flex items-center gap-2 flex-wrap">
                <button type="button" class="recording-play-btn rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50" data-id="${escapeHtml(r.id)}" data-label="${escapeHtml(label)}" data-user-id="${escapeHtml(r.user_id || '')}" data-session-id="${escapeHtml(r.session_id || '')}">再生</button>
                <a href="/api/recordings/${encodeURIComponent(r.id)}/file?format=cast" download="${escapeHtml(r.id)}.cast" class="rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50">.cast</a>
                <a href="/api/recordings/${encodeURIComponent(r.id)}/file?format=gif" download="${escapeHtml(r.id)}.gif" class="recording-download-video rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50" data-format="gif">GIF</a>
                <a href="/api/recordings/${encodeURIComponent(r.id)}/file?format=webm" download="${escapeHtml(r.id)}.webm" class="recording-download-video rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50" data-format="webm">WebM</a>
              </div>
            </td>
          </tr>
        `
        }).join('')
        sectionHeader = `
          <div class="flex items-center gap-3 flex-wrap">
            <button type="button" id="recordings-back-to-servers" class="text-xs text-sky-600 hover:text-sky-800 hover:underline">← サーバー一覧</button>
            <h2 class="text-sm font-semibold text-slate-800">${escapeHtml(selectedRecordingsTargetName || selectedRecordingsTargetId)} — 録画一覧</h2>
            <span class="text-xs text-slate-500">${items.length} 件</span>
          </div>
        `
        sectionContent = `
          <div class="overflow-x-auto flex-1 min-h-0">
            <table class="min-w-full text-left text-sm">
              <thead class="bg-slate-50 border-b border-slate-200">
                <tr>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">開始</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">終了</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">セッション名</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">説明</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">チャネル</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">操作</th>
                </tr>
              </thead>
              <tbody>${rows || '<tr><td colspan="6" class="px-4 py-6 text-center text-slate-500">このサーバーの録画はありません</td></tr>'}</tbody>
            </table>
          </div>
        `
      } else if (selectedRecordingsGroupId && targets.length > 0) {
        const targetRows = targets.map((t) => `
          <tr class="border-b border-slate-200 hover:bg-slate-50">
            <td class="px-4 py-2 text-sm font-medium text-slate-900">${escapeHtml(t.name || t.id || '')}</td>
            <td class="px-4 py-2 text-sm text-slate-600 font-mono">${escapeHtml(t.host || '')}</td>
            <td class="px-4 py-2">
              <button type="button" class="recordings-view-target-btn rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700" data-target-id="${escapeHtml(t.id)}" data-target-name="${escapeHtml(t.name || t.id || '')}">録画を見る</button>
            </td>
          </tr>
        `).join('')
        sectionHeader = `
          <h2 class="text-sm font-semibold text-slate-800">${escapeHtml(selectedRecordingsGroupId)} — サーバー一覧</h2>
          <span class="text-xs text-slate-500">${targets.length} サーバー</span>
        `
        sectionContent = `
          <div class="overflow-x-auto flex-1 min-h-0">
            <table class="min-w-full text-left text-sm">
              <thead class="bg-slate-50 border-b border-slate-200">
                <tr>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">サーバー名</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">ホスト</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">操作</th>
                </tr>
              </thead>
              <tbody>${targetRows}</tbody>
            </table>
          </div>
        `
      } else if (selectedRecordingsGroupId && targets.length === 0) {
        sectionHeader = `<h2 class="text-sm font-semibold text-slate-800">${escapeHtml(selectedRecordingsGroupId)}</h2>`
        sectionContent = '<div class="px-5 py-8 text-center text-sm text-slate-500">このグループにサーバーがありません。</div>'
      } else {
        sectionHeader = '<h2 class="text-sm font-semibold text-slate-800">録画</h2>'
        sectionContent = '<div class="px-5 py-8 text-center text-sm text-slate-500">左のグループを選択し、サーバー一覧から「録画を見る」でそのサーバーの録画を表示します。</div>'
      }

      mainContent.innerHTML = `
        <div class="flex gap-6 w-full h-full">
          <aside class="w-64 flex-col border-r border-slate-200 bg-white shadow-sm shrink-0 rounded-lg overflow-hidden flex">
            <div class="px-4 py-3 border-b border-slate-200 text-sm font-semibold text-slate-700">アクセスグループ</div>
            <div class="px-3 py-3 text-xs text-slate-800 overflow-y-auto flex-1 min-h-0" id="recordings-tree-container">
              ${treeHtml || '<p class="text-slate-500 p-2">グループがありません。</p>'}
            </div>
          </aside>
          <section class="flex-1 bg-white rounded-lg shadow-sm border border-slate-200 overflow-hidden flex flex-col min-h-0">
            <div class="px-5 py-3 border-b border-slate-200 flex items-center justify-between bg-slate-50 flex-wrap gap-2">
              ${sectionHeader}
            </div>
            ${sectionContent}
          </section>
        </div>
      `

      mainContent.querySelector('#recordings-back-to-servers')?.addEventListener('click', () => {
        selectedRecordingsTargetId = ''
        selectedRecordingsTargetName = ''
        showRecordingsPage()
      })
      mainContent.querySelectorAll('.recordings-view-target-btn').forEach((btn) => {
        btn.addEventListener('click', () => {
          selectedRecordingsTargetId = btn.dataset.targetId || ''
          selectedRecordingsTargetName = btn.dataset.targetName || ''
          showRecordingsPage()
        })
      })
      mainContent.querySelectorAll('.recording-play-btn').forEach((btn) => {
        btn.addEventListener('click', () => {
          showRecordingPlayerModal(btn.dataset.id || '', btn.dataset.label || '', btn.dataset.userId || '', btn.dataset.sessionId || '')
        })
      })
      mainContent.querySelectorAll('.recording-download-video').forEach((a) => {
        a.addEventListener('click', async (e) => {
          e.preventDefault()
          const url = a.getAttribute('href')
          const format = a.dataset.format || 'gif'
          const filename = a.getAttribute('download') || `recording.${format}`
          try {
            const res = await fetch(url, { credentials: 'include' })
            if (!res.ok) {
              const err = await res.json().catch(() => ({ message: res.statusText }))
              alert(err.message || '動画のダウンロードに失敗しました。サーバーに agg（および WebM の場合は ffmpeg）がインストールされている必要があります。')
              return
            }
            const blob = await res.blob()
            const x = document.createElement('a')
            x.href = URL.createObjectURL(blob)
            x.download = filename
            x.click()
            URL.revokeObjectURL(x.href)
          } catch (err) {
            alert(err.message || 'ダウンロードに失敗しました')
          }
        })
      })

      mainContent.querySelectorAll('[data-group-toggle="1"]').forEach((el) => {
        el.addEventListener('click', (e) => {
          e.preventDefault()
          e.stopPropagation()
          const gid = el.getAttribute('data-group-id') || ''
          if (!gid) return
          expandedGroups.has(gid) ? expandedGroups.delete(gid) : expandedGroups.add(gid)
          showRecordingsPage()
        })
      })
      mainContent.querySelectorAll('[data-group-select="1"]').forEach((el) => {
        el.addEventListener('click', () => {
          const gid = el.getAttribute('data-group-id') || ''
          selectedRecordingsGroupId = gid
          selectedRecordingsTargetId = ''
          selectedRecordingsTargetName = ''
          showRecordingsPage()
        })
      })
    } catch (e) {
      mainContent.innerHTML = `<p class="text-sm text-red-600">${escapeHtml(e.message || '取得に失敗しました')}</p>`
    }
  }

  function showAddUserModal() {
    const modal = document.getElementById('add-user-modal')
    modal.classList.remove('hidden')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">ユーザーを追加</h3>
            <button id="add-user-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="add-user-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">ユーザー名</label>
                <input type="text" id="add-user-username" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: alice" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">パスワード</label>
                <input type="password" id="add-user-password" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="8文字以上・大文字・小文字・数字・記号" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">ロール</label>
                <select id="add-user-role" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                  <option value="user">user</option>
                  <option value="admin">admin</option>
                </select>
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">ユーザーID（任意）</label>
                <input type="text" id="add-user-id" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="省略時はユーザー名から自動生成" />
              </div>
              <p id="add-user-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="add-user-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">キャンセル</button>
              <button type="submit" id="add-user-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">追加</button>
            </div>
          </form>
        </div>
      </div>
    `
    const close = () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#add-user-close').addEventListener('click', close)
    modal.querySelector('#add-user-cancel').addEventListener('click', close)
    modal.querySelector('#add-user-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errorEl = modal.querySelector('#add-user-error')
      const submitBtn = modal.querySelector('#add-user-submit')
      errorEl.classList.add('hidden')
      const username = modal.querySelector('#add-user-username').value.trim()
      const password = modal.querySelector('#add-user-password').value
      const role = modal.querySelector('#add-user-role').value || 'user'
      const id = modal.querySelector('#add-user-id').value.trim() || undefined
      if (!username || !password) {
        errorEl.textContent = 'ユーザー名とパスワードを入力してください'
        errorEl.classList.remove('hidden')
        return
      }
      submitBtn.disabled = true
      try {
        await API.createUser({ id, username, password, role })
        close()
        await showUsersPage()
      } catch (err) {
        errorEl.textContent = err.message || '追加に失敗しました'
        errorEl.classList.remove('hidden')
      } finally {
        submitBtn.disabled = false
      }
    })
  }

  function showEditUserModal(user) {
    const modal = document.getElementById('edit-tags-modal')
    modal.classList.remove('hidden')
    const tagsStr = (user.tags || []).join(', ')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">ユーザーを編集</h3>
            <button id="edit-user-modal-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="edit-user-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">ユーザーID</label>
                <p class="text-sm text-slate-800">${escapeHtml(user.id)}</p>
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">ユーザー名</label>
                <p class="text-sm text-slate-800">${escapeHtml(user.username)}</p>
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">ロール</label>
                <p class="text-sm text-slate-800">${escapeHtml(user.role || 'user')}</p>
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">タグ（カンマ区切り）</label>
                <input type="text" id="edit-user-tags-input" value="${escapeHtml(tagsStr)}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: prod, network, ops" />
                <div id="edit-user-tags-input-picker" class="mt-2"></div>
              </div>
              <p id="edit-user-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="edit-user-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">キャンセル</button>
              <button type="submit" id="edit-user-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">保存</button>
            </div>
          </form>
        </div>
      </div>
    `
    const close = () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#edit-user-modal-close').addEventListener('click', close)
    modal.querySelector('#edit-user-cancel').addEventListener('click', close)
    fillExistingTagsPicker(modal, 'edit-user-tags-input')
    modal.querySelector('#edit-user-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errorEl = modal.querySelector('#edit-user-error')
      const submitBtn = modal.querySelector('#edit-user-submit')
      const raw = modal.querySelector('#edit-user-tags-input').value.trim()
      const tags = raw ? raw.split(',').map((t) => t.trim()).filter(Boolean) : []
      errorEl.classList.add('hidden')
      submitBtn.disabled = true
      try {
        await API.setUserTags(user.id, tags)
        close()
        await showUsersPage()
      } catch (err) {
        errorEl.textContent = err.message || '保存に失敗しました'
        errorEl.classList.remove('hidden')
      } finally {
        submitBtn.disabled = false
      }
    })
  }

  async function showUserSSHKeysModal(userId, username) {
    if (!userId) return
    const modal = document.getElementById('add-ssh-key-modal')
    modal.classList.remove('hidden')
    const safeName = escapeHtml(username || userId)
    modal.innerHTML = `
      <div class="fixed inset-0 bg-black/40 flex items-center justify-center p-4" id="user-ssh-keys-backdrop">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-2xl border border-slate-200 max-h-[90vh] flex flex-col">
          <div class="px-5 py-3 border-b border-slate-200 flex items-center justify-between shrink-0">
            <h3 class="font-semibold text-slate-800">SSH 公開鍵 — ${safeName}</h3>
            <button id="user-ssh-keys-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <div class="px-5 py-4 overflow-auto flex-1 min-h-0">
            <p class="text-xs text-slate-600 mb-3">Vantyx に SSH でログインする際に使う公開鍵を、このユーザーに紐づけて登録します。</p>
            <div id="user-ssh-keys-list" class="mb-4">読み込み中…</div>
            <div class="border-t border-slate-200 pt-4">
              <label class="block text-xs font-medium text-slate-600 mb-1.5">公開鍵を追加（authorized_keys 形式の1行）</label>
              <textarea id="user-ssh-key-input" rows="2" class="w-full rounded border border-slate-300 px-3 py-2 text-sm font-mono text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="ssh-ed25519 AAAAC3... user@host"></textarea>
              <p id="user-ssh-key-error" class="mt-1 text-sm text-red-600 hidden"></p>
              <button type="button" id="user-ssh-key-add-btn" class="mt-2 rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700">追加</button>
            </div>
          </div>
        </div>
      </div>
    `
    const close = () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    const renderList = (keys) => {
      const listEl = modal.querySelector('#user-ssh-keys-list')
      if (!listEl) return
      if (!Array.isArray(keys)) keys = []
      const keyRows = keys.map((k) => {
        const keyDisplay = (k.key_line || '').length > 56 ? (k.key_line || '').slice(0, 53) + '...' : (k.key_line || '')
        return `
          <div class="flex items-center justify-between gap-2 py-2 border-b border-slate-100 text-sm">
            <span class="font-mono text-slate-700 truncate flex-1" title="${escapeHtml(k.key_line || '')}">${escapeHtml(keyDisplay)}</span>
            <span class="text-xs text-slate-400 shrink-0">${escapeHtml(k.created_at || '')}</span>
            <button type="button" class="user-ssh-key-del-btn rounded border border-red-200 px-2 py-0.5 text-xs text-red-700 hover:bg-red-50 shrink-0" data-key-id="${escapeHtml(String(k.id))}">削除</button>
          </div>
        `
      }).join('')
      listEl.innerHTML = keyRows
        ? `<div class="space-y-0">${keyRows}</div>`
        : '<p class="text-slate-500 text-sm">登録された公開鍵はありません。</p>'
      modal.querySelectorAll('.user-ssh-key-del-btn').forEach((btn) => {
        btn.addEventListener('click', async () => {
          if (!confirm('この公開鍵を削除しますか？')) return
          try {
            await API.deleteUserSSHKey(userId, btn.dataset.keyId)
            const keys = await API.userSSHKeys(userId)
            renderList(keys)
          } catch (e) {
            alert(e.message || '削除に失敗しました')
          }
        })
      })
    }
    modal.querySelector('#user-ssh-keys-close').addEventListener('click', close)
    modal.querySelector('#user-ssh-keys-backdrop').addEventListener('click', (e) => { if (e.target.id === 'user-ssh-keys-backdrop') close() })
    modal.querySelector('#user-ssh-key-add-btn').addEventListener('click', async () => {
      const errorEl = modal.querySelector('#user-ssh-key-error')
      const raw = (modal.querySelector('#user-ssh-key-input').value || '').trim()
      const line = raw.split(/\r?\n/)[0]?.trim() || raw
      errorEl.classList.add('hidden')
      if (!line) {
        errorEl.textContent = '公開鍵を1行で入力してください。'
        errorEl.classList.remove('hidden')
        return
      }
      try {
        await API.addUserSSHKey(userId, line)
        modal.querySelector('#user-ssh-key-input').value = ''
        const keys = await API.userSSHKeys(userId)
        renderList(keys)
      } catch (err) {
        errorEl.textContent = err.message || '登録に失敗しました。'
        errorEl.classList.remove('hidden')
      }
    })
    try {
      const keys = await API.userSSHKeys(userId)
      renderList(keys)
    } catch (e) {
      modal.querySelector('#user-ssh-keys-list').innerHTML = `<p class="text-sm text-red-600">${escapeHtml(e.message || '取得に失敗しました')}</p>`
    }
  }

  function showChangePasswordModal() {
    const modal = document.getElementById('change-password-modal')
    modal.classList.remove('hidden')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">パスワードを変更</h3>
            <button id="change-password-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="change-password-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label for="change-password-current" class="block text-xs font-medium text-slate-600 mb-1.5">現在のパスワード</label>
                <input type="password" id="change-password-current" autocomplete="current-password" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="現在のパスワード" />
              </div>
              <div>
                <label for="change-password-new" class="block text-xs font-medium text-slate-600 mb-1.5">新しいパスワード</label>
                <input type="password" id="change-password-new" autocomplete="new-password" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="8文字以上・大文字・小文字・数字・記号" />
              </div>
              <div>
                <label for="change-password-confirm" class="block text-xs font-medium text-slate-600 mb-1.5">新しいパスワード（確認）</label>
                <input type="password" id="change-password-confirm" autocomplete="new-password" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="もう一度入力" />
              </div>
              <p id="change-password-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="change-password-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">キャンセル</button>
              <button type="submit" id="change-password-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">変更</button>
            </div>
          </form>
        </div>
      </div>
    `
    const close = () => {
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#change-password-close').addEventListener('click', close)
    modal.querySelector('#change-password-cancel').addEventListener('click', close)
    modal.querySelector('#change-password-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errorEl = modal.querySelector('#change-password-error')
      const submitBtn = modal.querySelector('#change-password-submit')
      const current = modal.querySelector('#change-password-current').value
      const newPass = modal.querySelector('#change-password-new').value
      const confirmPass = modal.querySelector('#change-password-confirm').value
      errorEl.classList.add('hidden')
      if (newPass !== confirmPass) {
        errorEl.textContent = '新しいパスワードが一致しません'
        errorEl.classList.remove('hidden')
        return
      }
      submitBtn.disabled = true
      try {
        await API.changePassword(current, newPass)
        close()
        showUserInfo()
      } catch (err) {
        errorEl.textContent = err.message || 'パスワードの変更に失敗しました'
        errorEl.classList.remove('hidden')
      } finally {
        submitBtn.disabled = false
      }
    })
  }

  function showEditTagsModal({ type, id, label, currentTags, onSaved }) {
    const modal = document.getElementById('edit-tags-modal')
    modal.classList.remove('hidden')
    const tagsStr = (currentTags || []).join(', ')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">タグを編集 — ${escapeHtml(label)}</h3>
            <button id="edit-tags-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="edit-tags-form">
            <div class="px-6 py-5 space-y-5">
              <p class="text-sm text-slate-600">タグはアクセス権の付与に使われます。ユーザーとターゲット（またはグループ）で同じタグを持つとアクセス可能になります。英数字・ハイフン・アンダースコア、1〜64文字。</p>
              ${currentTags.length ? `<div class="flex flex-wrap gap-2">${renderTagPills(currentTags)}</div>` : ''}
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">タグ（カンマ区切り）</label>
                <input type="text" id="edit-tags-input" value="${escapeHtml(tagsStr)}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: prod, network, ops" />
                <div id="edit-tags-input-picker" class="mt-2"></div>
              </div>
              <p id="edit-tags-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="edit-tags-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">キャンセル</button>
              <button type="submit" id="edit-tags-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">保存</button>
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
      submitBtn.disabled = true
      try {
        if (type === 'group') await API.setGroupTags(id, tags)
        else if (type === 'target') await API.setTargetTags(id, tags)
        else if (type === 'user') await API.setUserTags(id, tags)
        close()
        if (onSaved) await onSaved()
      } catch (err) {
        errorEl.textContent = err.message || '保存に失敗しました'
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
            <h3 class="font-semibold text-slate-800">メンバーを追加</h3>
            <button id="add-member-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="add-member-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">ユーザー</label>
                <select id="add-member-user" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                  <option value="">選択してください</option>
                </select>
              </div>
              <p id="add-member-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="add-member-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">キャンセル</button>
              <button type="submit" id="add-member-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">追加</button>
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
        selectEl.innerHTML = '<option value="">追加できるユーザーがいません</option>'
        selectEl.disabled = true
      }
    } catch {
      selectEl.innerHTML = '<option value="">ユーザー一覧の取得に失敗しました</option>'
      selectEl.disabled = true
    }
    modal.querySelector('#add-member-form').addEventListener('submit', async (e) => {
      e.preventDefault()
      const errorEl = modal.querySelector('#add-member-error')
      const submitBtn = modal.querySelector('#add-member-submit')
      const userId = selectEl.value?.trim()
      if (!userId) return
      errorEl.classList.add('hidden')
      submitBtn.disabled = true
      try {
        await API.addGroupMember(groupId, userId)
        close()
        groupsCache = null
        await showTreeView('manage', true)
      } catch (err) {
        errorEl.textContent = err.message || '追加に失敗しました'
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
            <h3 class="font-semibold text-slate-800">サーバー管理グループを追加</h3>
            <button id="add-group-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="add-group-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">名前</label>
                <input type="text" id="add-group-name" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="例: Network" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">親グループ</label>
                <div class="text-sm text-slate-800 px-3 py-2 rounded border border-slate-200 bg-slate-50">
                  ${escapeHtml(parentLabel)}
                </div>
                <p class="text-xs text-slate-500 mt-1.5">左側ツリーで選択中のグループの直下に作成します。（ルート直下に作成する場合は何も選択せずに追加してください）</p>
              </div>
              <p id="add-group-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="add-group-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">キャンセル</button>
              <button type="submit" id="add-group-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">追加</button>
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
        errorEl.textContent = err.message || '追加に失敗しました'
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
      ? `アクティブなセッション — ${escapeHtml(targetsForModal[0].name)}`
      : 'アクティブなセッション（再接続）'
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-2xl mx-4 overflow-hidden border border-slate-200/50 max-h-[90vh] flex flex-col">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50 shrink-0">
            <h3 class="font-semibold text-slate-800">${modalTitle}</h3>
            <button id="active-sessions-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <div id="active-sessions-body" class="px-5 py-4 overflow-y-auto flex-1 min-h-0">
            <p class="text-sm text-slate-500">読み込み中…</p>
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
        if (!hasAny) {
          const oneServer = targetIdsInGroup.length === 1
          bodyEl.innerHTML = targetIdsInGroup.length > 0
            ? (oneServer
              ? '<p class="text-sm text-slate-500">このサーバーに対する再接続可能なセッションはありません。接続したあと、一度切断するとここに表示され、再接続できます。</p>'
              : '<p class="text-sm text-slate-500">このグループ内のサーバーに対する再接続可能なセッションはありません。ターミナルで接続したあと、一度切断するとここに表示され、再接続できます。</p>')
            : '<p class="text-sm text-slate-500">アクティブなセッションはありません。左のツリーでサーバー（グループ）を選択すると、そのグループに属するサーバー単位で表示されます。</p>'
          return
        }
        const parts = []
        if (sessions.length > 0) {
          const byTarget = {}
          sessions.forEach((s) => {
            const id = s.target_id
            if (!byTarget[id]) byTarget[id] = []
            byTarget[id].push(s)
          })
          const targetIds = Object.keys(byTarget).sort((a, b) => {
            const na = byTarget[a][0].target_name || a
            const nb = byTarget[b][0].target_name || b
            return na.localeCompare(nb)
          })
          parts.push(targetIds.map((targetId) => {
            const list = byTarget[targetId]
            const serverName = list[0].target_name || targetId
            const rows = list.map((s) => {
              const titleText = s.name ? escapeHtml(s.name) : '(無題)'
              const descHtml = s.description ? `<p class="text-xs text-slate-500 mt-0.5 break-words">${escapeHtml(s.description)}</p>` : ''
              return `<li class="flex items-start justify-between gap-3 py-2 px-3 rounded border border-slate-100 hover:bg-slate-50">
                <div class="min-w-0 flex-1">
                  <p class="text-sm font-medium text-slate-800">${titleText}</p>
                  ${descHtml}
                </div>
                <button type="button" data-terminal-reconnect-session-id="${escapeHtml(s.session_id)}" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shrink-0 self-center">再接続</button>
              </li>`
            }).join('')
            return `<div class="mb-4"><h4 class="text-xs font-semibold text-slate-600 uppercase tracking-wide mb-2">${escapeHtml(serverName)}（SSH ターミナル）</h4><ul class="space-y-2">${rows}</ul></div>`
          }).join(''))
        }
        if (rdpSessions.length > 0) {
          const rdpHtml = rdpSessions.map((r) => {
            const name = escapeHtml(r.target_name || r.target_id)
            const url = `/rdp?target_id=${encodeURIComponent(r.target_id)}&target_name=${encodeURIComponent(r.target_name || r.target_id)}`
            return `<li class="flex items-start justify-between gap-3 py-2 px-3 rounded border border-slate-100 hover:bg-slate-50">
              <div class="min-w-0 flex-1">
                <p class="text-sm font-medium text-slate-800">${name}</p>
                <p class="text-xs text-slate-500 mt-0.5">RDP（ブラウザ）</p>
              </div>
              <a href="${url}" target="_blank" rel="noopener noreferrer" data-rdp-target-id="${escapeHtml(r.target_id)}" class="rdp-reconnect-link rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shrink-0 self-center">再接続</a>
            </li>`
          }).join('')
          parts.push(`<div class="mb-4"><h4 class="text-xs font-semibold text-slate-600 uppercase tracking-wide mb-2">RDP（ブラウザ）</h4><ul class="space-y-2">${rdpHtml}</ul></div>`)
        }
        bodyEl.innerHTML = parts.join('')
        bodyEl.querySelectorAll('[data-terminal-reconnect-session-id]').forEach((btn) => {
          btn.addEventListener('click', () => {
            const sid = btn.getAttribute('data-terminal-reconnect-session-id') || ''
            if (!sid) return
            openTerminalTabWithParent(`/terminal?session_id=${encodeURIComponent(sid)}`)
          })
        })
        bodyEl.querySelectorAll('.rdp-reconnect-link').forEach((link) => {
          link.addEventListener('click', (e) => {
            e.preventDefault()
            const id = link.dataset.rdpTargetId || ''
            const { w, h } = getRdpResolutionForTarget(id)
            const u = new URL(link.href, window.location.origin)
            if (w) u.searchParams.set('rw', String(w))
            if (h) u.searchParams.set('rh', String(h))
            window.open(u.toString(), '_blank', 'noopener')
          })
        })
      } catch {
        bodyEl.innerHTML = '<p class="text-sm text-red-600">セッション一覧の取得に失敗しました。</p>'
      }
    }
    // Expose refresh hook for parent focus event
    modal._vantyxRefreshActiveSessions = refresh
    await refresh()
  }

  /** 保存済み認証のターゲット用: 接続前にモーダルで不足情報を入力させる。
   * パスワード認証・パスフレーズ無しの公開鍵・パスフレーズありで登録済みの場合はセッション名と説明のみ。
   * パスワード未登録のときはパスワード欄、パスフレーズ未登録のときはパスフレーズ欄を表示する。 */
  function showStoredCredentialModal(targetId, targetName, needsPassword, needsPassphrase) {
    const modal = document.getElementById('ssh-credential-modal')
    modal.classList.remove('hidden')
    const passwordBlock = needsPassword
      ? `
      <div>
        <label class="block text-xs font-medium text-slate-600 mb-1.5">SSH パスワード <span class="text-amber-600">（未登録のため入力してください）</span></label>
        <input type="password" id="ssh-cred-password" autocomplete="current-password" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" />
      </div>`
      : ''
    const passphraseBlock = needsPassphrase
      ? `
      <div>
        <label class="block text-xs font-medium text-slate-600 mb-1.5">秘密鍵のパスフレーズ <span class="text-amber-600">（未登録のため入力してください）</span></label>
        <input type="password" id="ssh-cred-passphrase" autocomplete="off" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="暗号化された秘密鍵のパスフレーズ" />
      </div>`
      : ''
    const hasExtraFields = needsPassword || needsPassphrase
    const introText = hasExtraFields
      ? '不足している情報を入力してください。接続でコンソールを開きます。'
      : 'セッション名と説明を入力してください（任意）。接続で保存済み認証を使ってコンソールを開きます。'
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">接続: ${escapeHtml(targetName || targetId)}</h3>
            <button id="ssh-cred-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="ssh-cred-form">
            <div class="px-6 py-5 space-y-5">
              <p class="text-sm text-slate-600">${introText}</p>
              ${passwordBlock}
              ${passphraseBlock}
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">セッション名（任意）</label>
                <input type="text" id="ssh-cred-session-name" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: 本番デプロイ用" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">説明（任意）</label>
                <input type="text" id="ssh-cred-session-desc" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: リリース作業用" />
              </div>
              <p id="ssh-cred-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="ssh-cred-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">キャンセル</button>
              <button type="submit" id="ssh-cred-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">接続</button>
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
        errEl.textContent = 'パスワードを入力してください。'
        errEl.classList.remove('hidden')
        return
      }
      if (needsPassphrase && !passphrase) {
        errEl.textContent = '秘密鍵のパスフレーズを入力してください。'
        errEl.classList.remove('hidden')
        return
      }
      errEl.classList.add('hidden')
      const params = new URLSearchParams()
      params.set('target_id', targetId)
      params.set('target_name', targetName || '')
      params.set('use_stored_credentials', '1')
      params.set('session_name', sessionName)
      params.set('session_description', sessionDesc)
      const token = randomToken()
      pendingTerminalCreds[token] = { targetId, targetName, password: password || '', passphrase: passphrase || '', sessionName, sessionDesc, useStoredCredentials: true }
      params.set('channel', token)
      openTerminalTabWithParent(`/terminal?${params.toString()}`)

      // 新しいタブとは BroadcastChannel で不足分（パスワード/パスフレーズ）を受け渡しする（localStorageに保存しない）
      const bc = new BroadcastChannel(`vantyx-terminal-${token}`)
      const timeoutId = window.setTimeout(() => {
        try { bc.close() } catch { /* ignore */ }
        delete pendingTerminalCreds[token]
      }, 15_000)
      bc.onmessage = (ev) => {
        if (ev?.data?.type !== 'ready') return
        if (ev?.data?.target_id !== targetId) return
        const creds = pendingTerminalCreds[token]
        if (!creds) return
        try {
          bc.postMessage({
            type: 'stored_credentials',
            password: creds.password || '',
            private_key_passphrase: creds.passphrase || '',
            name: creds.sessionName || '',
            description: creds.sessionDesc || '',
          })
        } finally {
          window.clearTimeout(timeoutId)
          try { bc.close() } catch { /* ignore */ }
          delete pendingTerminalCreds[token]
        }
      }
      close()
    })
  }

  function showSSHCredentialModal(targetId, targetName) {
    const modal = document.getElementById('ssh-credential-modal')
    modal.classList.remove('hidden')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">接続: ${escapeHtml(targetName || targetId)}</h3>
            <button id="ssh-cred-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="ssh-cred-form">
            <div class="px-6 py-5 space-y-5">
              <p class="text-sm text-slate-600">接続に必要な情報を入力してください。入力後にコンソールを開きます。</p>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">SSH ユーザー名</label>
                <input type="text" id="ssh-cred-username" autocomplete="username" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: root" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">SSH パスワード</label>
                <input type="password" id="ssh-cred-password" autocomplete="current-password" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">秘密鍵のパスフレーズ（任意）</label>
                <input type="password" id="ssh-cred-passphrase" autocomplete="off" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="保存済み鍵が暗号化されている場合のみ" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">セッション名（任意）</label>
                <input type="text" id="ssh-cred-session-name" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: 本番デプロイ用" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">説明（任意）</label>
                <input type="text" id="ssh-cred-session-desc" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: リリース作業用セッション" />
              </div>
              <p id="ssh-cred-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="ssh-cred-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">キャンセル</button>
              <button type="submit" id="ssh-cred-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">接続</button>
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
        errorEl.textContent = 'ユーザー名を入力してください'
        errorEl.classList.remove('hidden')
        return
      }
      const token = randomToken()
      pendingTerminalCreds[token] = { targetId, targetName, username, password, passphrase, sessionName, sessionDesc }
      const url = `/terminal?target_id=${encodeURIComponent(targetId)}&target_name=${encodeURIComponent(targetName || '')}&channel=${encodeURIComponent(token)}`
      // NOTE: ターミナルの「戻る/セッション終了」で元タブに戻れるよう、opener を残す（noreferrer/noopener は付けない）
      openTerminalTabWithParent(url)

      // 新しいタブとは BroadcastChannel で認証情報を受け渡しする
      const bc = new BroadcastChannel(`vantyx-terminal-${token}`)
      const timeoutId = window.setTimeout(() => {
        try { bc.close() } catch { /* ignore */ }
        delete pendingTerminalCreds[token]
      }, 15_000)
      bc.onmessage = (ev) => {
        if (ev?.data?.type !== 'ready') return
        if (ev?.data?.target_id !== targetId) return
        const creds = pendingTerminalCreds[token]
        if (!creds) return
        try {
          bc.postMessage({
            type: 'credentials',
            username: creds.username,
            password: creds.password,
            private_key_passphrase: creds.passphrase || '',
            name: creds.sessionName || '',
            description: creds.sessionDesc || '',
          })
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
    const isManageMode = mode === 'manage'
    const pageTitle = isManageMode ? 'サーバー管理' : 'ホーム'

    const currentModeIndicator = mainContent.dataset.treeMode
    const isSameMode = currentModeIndicator === mode
    const treeContainer = mainContent.querySelector('aside .overflow-y-auto')
    const scrollPos = treeContainer ? treeContainer.scrollTop : 0

    if (!isSameMode && !useCache) {
      mainContent.innerHTML = '<div class="w-full flex-1 flex items-center justify-center"><p class="text-slate-500">読み込み中…</p></div>'
    }

    try {
      if (!useCache || !groupsCache) {
        groupsCache = await API.groups()
      }
      const groups = groupsCache
      const treeRoot = buildGroupTree(groups || [])
      const treeHtml = renderGroupTree(treeRoot, 0)
      const selectedGroup = (groups || []).find((g) => g.id === selectedGroupId)
      const targets = selectedGroup ? (selectedGroup.targets || []) : []
      const label = selectedGroupId || 'root'

      const addGroupBtnHtml = isManageMode
        ? `<button type="button" id="btn-add-group" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">追加</button>`
        : ''

      const addTargetBtnHtml = isManageMode
        ? `<button type="button" id="btn-add-target-in-group" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors disabled:opacity-50" ${selectedGroupId ? '' : 'disabled'}>サーバーを追加</button>`
        : ''
      const isAdmin = meData?.role === 'admin'
      const showMembersSection = isManageMode && isAdmin && selectedGroupId

      mainContent.dataset.treeMode = mode
      mainContent.innerHTML = `
        <div class="flex gap-6 w-full h-full">
          <aside class="w-64 flex-col border-r border-slate-200 bg-white shadow-sm shrink-0 rounded-lg overflow-hidden flex">
            <div class="px-4 py-3 border-b border-slate-200 text-sm font-semibold text-slate-700 flex items-center justify-between">
              <span>アクセスグループ</span>
              ${addGroupBtnHtml}
            </div>
            <div class="px-3 py-3 text-xs text-slate-800 overflow-y-auto flex-1 min-h-0">
              ${treeHtml || '<p class="text-slate-500 p-2">グループがありません。</p>'}
            </div>
          </aside>
          <section class="flex-1 bg-white rounded-lg shadow-sm border border-slate-200 overflow-hidden flex flex-col min-h-0">
            <div class="px-5 py-3 border-b border-slate-200 flex items-center justify-between bg-slate-50">
              <div>
                <h2 class="text-sm font-semibold text-slate-800">${escapeHtml(label)}</h2>
              </div>
              <div class="flex items-center gap-2">
                <span class="text-xs text-slate-500">${targets.length} サーバー</span>
                ${addTargetBtnHtml}
              </div>
            </div>
            <div class="px-5 py-4">
              ${renderGroupTargetsTable(targets, mode)}
              ${showMembersSection ? '<div id="group-members-container" class="mt-6 border-t border-slate-200 pt-4"><p class="text-slate-500">読み込み中…</p></div>' : ''}
            </div>
          </section>
        </div>
      `
      const newTreeContainer = mainContent.querySelector('aside .overflow-y-auto')
      if (newTreeContainer && scrollPos > 0) {
        newTreeContainer.scrollTop = scrollPos
      }

      if (showMembersSection) {
        const membersContainer = mainContent.querySelector('#group-members-container')
        if (membersContainer) {
          Promise.all([API.groupMembers(selectedGroupId), API.groupTags(selectedGroupId).catch(() => ({ tags: [] }))])
            .then(([members, tagsRes]) => {
              const memberIds = (members || []).map((m) => m.id)
              const groupTags = (tagsRes && tagsRes.tags) ? tagsRes.tags : []
              const tagsHtml = `
                <div class="mb-4 flex items-center justify-between flex-wrap gap-2">
                  <div>
                    <h3 class="text-sm font-semibold text-slate-700 mb-1">タグ</h3>
                    <p class="text-xs text-slate-500">同じタグを持つユーザーはこのグループのターゲットにアクセスできます</p>
                    <div class="flex flex-wrap gap-2 mt-2">
                      ${groupTags.length ? renderTagPills(groupTags) : '<span class="text-xs text-slate-400">タグなし</span>'}
                    </div>
                  </div>
                  <button type="button" id="btn-edit-group-tags" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">タグを編集</button>
                </div>
              `
              const rows = (members || []).map((m) => `
                <tr class="border-b border-slate-200 hover:bg-slate-50">
                  <td class="px-4 py-2 text-sm font-medium text-slate-900">${escapeHtml(m.id)}</td>
                  <td class="px-4 py-2 text-sm text-slate-700">${escapeHtml(m.username)}</td>
                  <td class="px-4 py-2 text-right">
                    <button type="button" class="remove-member-btn rounded border border-red-200 bg-white px-3 py-1.5 text-xs font-medium text-red-700 hover:bg-red-50" data-user-id="${escapeHtml(m.id)}">削除</button>
                  </td>
                </tr>
              `).join('')
              membersContainer.innerHTML = `
                ${tagsHtml}
                <h3 class="text-sm font-semibold text-slate-700 mb-2">メンバー（アクセス権）</h3>
                <div class="flex items-center justify-between mb-2">
                  <span class="text-xs text-slate-500">${memberIds.length} 人</span>
                  <button type="button" id="btn-add-member" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">メンバーを追加</button>
                </div>
                <div class="overflow-x-auto border border-slate-200 rounded-lg">
                  <table class="min-w-full text-left text-sm">
                    <thead class="bg-slate-50 border-b border-slate-200">
                      <tr>
                        <th class="px-4 py-2 text-xs font-semibold text-slate-700">ユーザーID</th>
                        <th class="px-4 py-2 text-xs font-semibold text-slate-700">ユーザー名</th>
                        <th class="px-4 py-2"></th>
                      </tr>
                    </thead>
                    <tbody>${rows || '<tr><td colspan="3" class="px-4 py-4 text-center text-slate-500">メンバーがいません</td></tr>'}</tbody>
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
                  if (!uid || !confirm(`「${escapeHtml(uid)}」をこのグループから削除してもよろしいですか？`)) return
                  try {
                    await API.removeGroupMember(selectedGroupId, uid)
                    groupsCache = null
                    await showTreeView('manage', true)
                  } catch (err) {
                    alert(err.message || '削除に失敗しました')
                  }
                })
              })
            })
            .catch(() => {
              membersContainer.innerHTML = '<p class="text-sm text-red-600">メンバー一覧の取得に失敗しました</p>'
            })
        }
      }

      if (isManageMode) {
        mainContent.querySelector('#btn-add-group')?.addEventListener('click', showAddGroupModal)
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
              needs_passphrase: btn.dataset.targetNeedsPassphrase === '1',
            }
            if (target.id) showEditTargetModal(target)
          })
        })
        mainContent.querySelectorAll('.delete-btn-in-group').forEach((btn) => {
          btn.addEventListener('click', async () => {
            const targetId = btn.dataset.targetId || ''
            const targetName = btn.dataset.targetName || ''
            if (!targetId) return
            if (!confirm(`「${escapeHtml(targetName) || targetId}」を削除してもよろしいですか？`)) return
            try {
              await API.deleteTarget(targetId)
              groupsCache = null
              await showTreeView('manage')
            } catch (err) {
              alert(err.message || '削除に失敗しました')
            }
          })
        })
      } else {
        mainContent.querySelectorAll('.active-sessions-btn').forEach((btn) => {
          btn.addEventListener('click', (e) => {
            e.preventDefault()
            const targetId = btn.dataset.targetId || ''
            const targetName = btn.dataset.targetName || ''
            showActiveSessionsModal(selectedGroupId, targetId ? [{ id: targetId, name: targetName }] : targets)
          })
        })
        // SSH: 認証情報が保存済みならタブを直接開き、未保存なら認証モーダル表示。
        mainContent.querySelectorAll('.terminal-open-btn').forEach((btn) => {
          btn.addEventListener('click', (e) => {
            e.preventDefault()
            const id = btn.dataset.terminalTargetId || ''
            const name = btn.dataset.terminalTargetName || ''
            if (!id) return
            if (btn.dataset.hasStoredCredentials) {
              showStoredCredentialModal(id, name, btn.dataset.needsPassword === '1', btn.dataset.needsPassphrase === '1')
            } else {
              showSSHCredentialModal(id, name)
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

        // RDP: 解像度設定（ローカル保存）に基づき、新しいタブで開く
        mainContent.querySelectorAll('.rdp-open-link').forEach((link) => {
          link.addEventListener('click', (e) => {
            e.preventDefault()
            const id = link.dataset.rdpTargetId || ''
            const name = link.dataset.rdpTargetName || ''
            const { w, h } = getRdpResolutionForTarget(id)
            const u = new URL(link.href, window.location.origin)
            if (w) u.searchParams.set('rw', String(w))
            if (h) u.searchParams.set('rh', String(h))
            window.open(u.toString(), '_blank', 'noopener')
          })
        })

        // 未対応プロトコル (telnet等) はアラート表示。
        mainContent.querySelectorAll('.connect-btn-in-group:not(.terminal-open-btn):not(.vnc-open-btn):not([data-popup-protocol]):not(a)').forEach((btn) => {
          btn.addEventListener('click', () => {
            alert('このターゲットは SSH / VNC / RDP のみ対応しています。')
          })
        })
      }

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

      const navHidden = meData?.role !== 'admin' ? ' hidden' : ''
      navRecordings.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity'
      if (isManageMode) {
        navTargets.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity'
        navGroups.className = 'text-sm font-semibold border-b-2 border-white pb-1 transition-opacity' + navHidden
        navUsers.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity' + navHidden
      } else {
        navTargets.className = 'text-sm font-semibold border-b-2 border-white pb-1 transition-opacity'
        navGroups.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity' + navHidden
        navUsers.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity' + navHidden
      }

    } catch (e) {
      mainContent.dataset.treeMode = mode
      mainContent.innerHTML = `
        <div class="w-full flex-1 bg-white rounded-lg shadow-sm border border-slate-200 overflow-hidden flex flex-col min-h-0">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h2 class="text-sm font-semibold text-slate-800">${escapeHtml(pageTitle)}</h2>
            ${isManageMode ? `<button type="button" id="btn-add-group" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">追加</button>` : ''}
          </div>
          <div class="px-5 py-6 text-center">
            <p class="text-sm text-red-600">${escapeHtml(e.message || '取得に失敗しました')}</p>
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
        <div class="bg-white rounded-lg shadow-xl w-full max-w-xl mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">サーバーを追加</h3>
            <button id="add-target-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="add-target-form">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">名前</label>
                <input type="text" id="add-target-name" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="例: My Server" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">登録先サーバー管理グループ</label>
                <div class="w-full rounded border border-slate-200 px-3 py-2 text-sm text-slate-800 bg-slate-50 font-mono">${escapeHtml(selectedGroupId || 'root')}</div>
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">ホスト</label>
                <input type="text" id="add-target-host" required class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400 font-mono" placeholder="例: 192.168.1.1" />
              </div>
              <div class="grid grid-cols-2 gap-4">
                <div>
                  <label class="block text-xs font-medium text-slate-600 mb-1.5">ポート</label>
                  <input type="number" id="add-target-port" min="1" max="65535" value="22" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                </div>
                <div>
                  <label class="block text-xs font-medium text-slate-600 mb-1.5">プロトコル</label>
                  <select id="add-target-protocol" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                    <option value="ssh">SSH</option>
                    <option value="telnet">Telnet</option>
                    <option value="vnc">VNC</option>
                    <option value="rdp">RDP</option>
                    <option value="tftp">TFTP</option>
                  </select>
                </div>
              </div>
              <div id="add-target-rdp-res-wrap" class="grid grid-cols-2 gap-4 hidden">
                <div>
                  <label class="block text-xs font-medium text-slate-600 mb-1.5">RDP 解像度（幅）</label>
                  <input type="number" id="add-target-rdp-width" min="640" max="3840" value="1920" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                </div>
                <div>
                  <label class="block text-xs font-medium text-slate-600 mb-1.5">RDP 解像度（高さ）</label>
                  <input type="number" id="add-target-rdp-height" min="480" max="2160" value="1080" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                </div>
              </div>
              <div id="add-target-cred-fields">
                <div id="add-target-auth-type-wrap" class="space-y-3 hidden">
                  <label class="block text-xs font-medium text-slate-600 mb-1.5">認証方法</label>
                  <div class="space-y-2">
                    <label class="flex items-center gap-2 cursor-pointer">
                      <input type="radio" name="add-target-auth-type" value="password" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" checked />
                      <span class="text-sm text-slate-800">パスワード認証</span>
                    </label>
                    <label class="flex items-center gap-2 cursor-pointer">
                      <input type="radio" name="add-target-auth-type" value="key" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" />
                      <span class="text-sm text-slate-800">公開鍵認証（パスフレーズなし）</span>
                    </label>
                    <label class="flex items-center gap-2 cursor-pointer">
                      <input type="radio" name="add-target-auth-type" value="key_passphrase" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" />
                      <span class="text-sm text-slate-800">公開鍵認証（パスフレーズあり）</span>
                    </label>
                  </div>
                </div>
                <div class="space-y-5 mt-4">
                  <div id="add-target-username-wrap">
                    <label id="add-target-username-label" class="block text-xs font-medium text-slate-600 mb-1.5">ユーザー名（任意）</label>
                    <input type="text" id="add-target-ssh-username" autocomplete="username" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="例: root" />
                  </div>
                  <div id="add-target-password-wrap">
                    <label id="add-target-password-label" class="block text-xs font-medium text-slate-600 mb-1.5">パスワード（任意）</label>
                    <input type="password" id="add-target-ssh-password" autocomplete="current-password" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="保存すると接続時に利用できます" />
                  </div>
                  <div id="add-target-key-wrap" class="hidden">
                    <label class="block text-xs font-medium text-slate-600 mb-1.5">SSH 秘密鍵（PEM）</label>
                    <textarea id="add-target-ssh-private-key" rows="4" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono placeholder-slate-400" placeholder="-----BEGIN ... 形式の秘密鍵を貼り付け"></textarea>
                  </div>
                  <div id="add-target-passphrase-wrap" class="hidden">
                    <label class="block text-xs font-medium text-slate-600 mb-1.5">秘密鍵のパスフレーズ</label>
                    <input type="password" id="add-target-ssh-key-passphrase" autocomplete="off" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="暗号化された秘密鍵のパスフレーズ" />
                  </div>
                </div>
              </div>
              <p id="add-target-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="add-target-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">キャンセル</button>
              <button type="submit" id="add-target-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">追加</button>
            </div>
          </form>
        </div>
      </div>
    `
    const defaultPorts = { ssh: 22, telnet: 23, vnc: 5900, rdp: 3389, tftp: 69 }
    const addProtoSelect = modal.querySelector('#add-target-protocol')
    const addCredFields = modal.querySelector('#add-target-cred-fields')
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
    function syncAddAuthType() {
      const proto = addProtoSelect.value
      if (proto === 'rdp') {
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
      const hasCreds = proto === 'ssh' || proto === 'rdp'
      addCredFields.style.display = hasCreds ? '' : 'none'
      addAuthTypeWrap.classList.toggle('hidden', proto !== 'ssh')
      addUsernameLabel.textContent = proto === 'rdp' ? 'RDP ユーザー名（任意）' : 'SSH ユーザー名（任意）'
      addPasswordLabel.textContent = proto === 'rdp' ? 'RDP パスワード（任意）' : 'SSH パスワード（任意）'
      addRdpResWrap.classList.toggle('hidden', proto !== 'rdp')
      if (defaultPorts[proto] !== undefined) {
        addPortInput.value = defaultPorts[proto]
      }
      syncAddAuthType()
    }
    addProtoSelect.addEventListener('change', syncAddProtocol)
    modal.querySelectorAll('input[name="add-target-auth-type"]').forEach((radio) => {
      radio.addEventListener('change', syncAddAuthType)
    })
    syncAddProtocol()

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
      const ssh_username = modal.querySelector('#add-target-ssh-username').value.trim()
      const authType = protocol === 'ssh' ? (modal.querySelector('input[name="add-target-auth-type"]:checked')?.value || 'password') : 'password'
      const payload = { name, host, port, protocol, group_id, ssh_username }
      if (protocol === 'rdp' || authType === 'password') {
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
      if (!name || !host) {
        errorEl.textContent = '名前とホストを入力してください'
        errorEl.classList.remove('hidden')
        return
      }
      if (!group_id) {
        errorEl.textContent = '登録先のサーバー管理グループを左ツリーで選択してください'
        errorEl.classList.remove('hidden')
        return
      }
      let rdpW = 1920
      let rdpH = 1080
      if (protocol === 'rdp') {
        rdpW = parseInt(addRdpWidthInput.value, 10) || 1920
        rdpH = parseInt(addRdpHeightInput.value, 10) || 1080
      }
      submitBtn.disabled = true
      try {
        const created = await API.createTarget(payload)
        if (protocol === 'rdp' && created && created.id) {
          setRdpResolutionForTarget(created.id, rdpW, rdpH)
        }
        modal.classList.add('hidden')
        modal.innerHTML = ''
        groupsCache = null
        await showTreeView('manage')
      } catch (err) {
        errorEl.textContent = err.message || '追加に失敗しました'
        errorEl.classList.remove('hidden')
      } finally {
        submitBtn.disabled = false
      }
    })
  }

  function showEditTargetModal(target) {
    const modal = document.getElementById('add-target-modal')
    modal.classList.remove('hidden')
    modal.innerHTML = `
      <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-white rounded-lg shadow-xl w-full max-w-xl mx-4 overflow-hidden border border-slate-200/50">
          <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
            <h3 class="font-semibold text-slate-800">サーバーを編集</h3>
            <button id="edit-target-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
          </div>
          <form id="edit-target-form" data-edit-target-id="${escapeHtml(target.id)}">
            <div class="px-6 py-5 space-y-5">
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">名前</label>
                <input type="text" id="edit-target-name" required value="${escapeHtml(target.name)}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" />
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">ホスト</label>
                <input type="text" id="edit-target-host" required value="${escapeHtml(target.host)}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
              </div>
              <div class="grid grid-cols-2 gap-4">
                <div>
                  <label class="block text-xs font-medium text-slate-600 mb-1.5">ポート</label>
                  <input type="number" id="edit-target-port" min="1" max="65535" value="${target.port || 22}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                </div>
                <div>
                  <label class="block text-xs font-medium text-slate-600 mb-1.5">プロトコル</label>
                  <select id="edit-target-protocol" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white">
                    <option value="ssh" ${(target.protocol || 'ssh') === 'ssh' ? 'selected' : ''}>SSH</option>
                    <option value="telnet" ${target.protocol === 'telnet' ? 'selected' : ''}>Telnet</option>
                    <option value="vnc" ${target.protocol === 'vnc' ? 'selected' : ''}>VNC</option>
                    <option value="rdp" ${target.protocol === 'rdp' ? 'selected' : ''}>RDP</option>
                    <option value="tftp" ${target.protocol === 'tftp' ? 'selected' : ''}>TFTP</option>
                  </select>
                </div>
              </div>
              <div id="edit-target-cred-fields">
                <div id="edit-target-auth-type-wrap" class="space-y-3 hidden">
                  <label class="block text-xs font-medium text-slate-600 mb-1.5">認証方法</label>
                  <div class="space-y-2">
                    <label class="flex items-center gap-2 cursor-pointer">
                      <input type="radio" name="edit-target-auth-type" value="password" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" />
                      <span class="text-sm text-slate-800">パスワード認証</span>
                    </label>
                    <label class="flex items-center gap-2 cursor-pointer">
                      <input type="radio" name="edit-target-auth-type" value="key" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" />
                      <span class="text-sm text-slate-800">公開鍵認証（パスフレーズなし）</span>
                    </label>
                    <label class="flex items-center gap-2 cursor-pointer">
                      <input type="radio" name="edit-target-auth-type" value="key_passphrase" class="rounded-full border-slate-300 text-sky-600 focus:ring-sky-500" />
                      <span class="text-sm text-slate-800">公開鍵認証（パスフレーズあり）</span>
                    </label>
                  </div>
                </div>
                <div class="space-y-5 mt-4">
                  <div id="edit-target-username-wrap">
                    <label id="edit-target-username-label" class="block text-xs font-medium text-slate-600 mb-1.5">ユーザー名（任意）</label>
                    <input type="text" id="edit-target-ssh-username" value="${escapeHtml(target.ssh_username || '')}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="例: root" />
                  </div>
                  <div id="edit-target-password-wrap">
                    <label id="edit-target-password-label" class="block text-xs font-medium text-slate-600 mb-1.5">パスワード（任意）</label>
                    <input type="password" id="edit-target-ssh-password" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="変更する場合のみ入力（空のままなら変更しません）" />
                  </div>
                  <div id="edit-target-key-wrap" class="hidden">
                    <label class="block text-xs font-medium text-slate-600 mb-1.5">SSH 秘密鍵（PEM）</label>
                    <textarea id="edit-target-ssh-private-key" rows="4" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono placeholder-slate-400" placeholder="${target.has_ssh_key ? '設定済み。上書きする場合は新しい鍵を貼り付け' : '-----BEGIN ... 形式の秘密鍵を貼り付け'}" autocomplete="off"></textarea>
                    ${target.has_ssh_key ? '<label class="mt-1.5 flex items-center gap-2 text-xs text-slate-600"><input type="checkbox" id="edit-target-clear-ssh-key" class="rounded border-slate-300" /> 保存済み秘密鍵をクリア</label>' : ''}
                  </div>
                  <div id="edit-target-passphrase-wrap" class="hidden">
                    <label class="block text-xs font-medium text-slate-600 mb-1.5">秘密鍵のパスフレーズ</label>
                    <input type="password" id="edit-target-ssh-key-passphrase" autocomplete="off" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white placeholder-slate-400" placeholder="変更する場合のみ入力" />
                  </div>
                </div>
              </div>
              <div>
                <label class="block text-xs font-medium text-slate-600 mb-1.5">タグ（カンマ区切り）</label>
                <input type="text" id="edit-target-tags" value="${escapeHtml((target.tags || []).join(', '))}" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="例: prod, network, ops" />
                <div id="edit-target-tags-picker" class="mt-2"></div>
              </div>
              <div id="edit-target-rdp-res-wrap" class="grid grid-cols-2 gap-4 hidden">
                <div>
                  <label class="block text-xs font-medium text-slate-600 mb-1.5">RDP 解像度（幅）</label>
                  <input type="number" id="edit-target-rdp-width" min="640" max="3840" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                </div>
                <div>
                  <label class="block text-xs font-medium text-slate-600 mb-1.5">RDP 解像度（高さ）</label>
                  <input type="number" id="edit-target-rdp-height" min="480" max="2160" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white font-mono" />
                </div>
              </div>
              <p id="edit-target-error" class="text-sm text-red-600 hidden"></p>
            </div>
            <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
              <button type="button" id="edit-target-cancel" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">キャンセル</button>
              <button type="submit" id="edit-target-submit" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">更新</button>
            </div>
          </form>
        </div>
      </div>
    `
    fillExistingTagsPicker(modal, 'edit-target-tags')
    const editDefaultPorts = { ssh: 22, telnet: 23, vnc: 5900, rdp: 3389, tftp: 69 }
    const editProtoSelect = modal.querySelector('#edit-target-protocol')
    const editCredFields = modal.querySelector('#edit-target-cred-fields')
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
    function syncEditAuthType() {
      const proto = editProtoSelect.value
      if (proto === 'rdp') {
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
      const hasCreds = proto === 'ssh' || proto === 'rdp'
      editCredFields.style.display = hasCreds ? '' : 'none'
      editAuthTypeWrap.classList.toggle('hidden', proto !== 'ssh')
      editUsernameLabel.textContent = proto === 'rdp' ? 'RDP ユーザー名（任意）' : 'SSH ユーザー名（任意）'
      editPasswordLabel.textContent = proto === 'rdp' ? 'RDP パスワード（任意）' : 'SSH パスワード（任意）'
      editRdpResWrap.classList.toggle('hidden', proto !== 'rdp')
      syncEditAuthType()
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
    const initialAuthType = target.protocol === 'ssh'
      ? (target.has_ssh_key ? (target.needs_passphrase ? 'key_passphrase' : 'key') : 'password')
      : 'password'
    const initialAuthRadio = modal.querySelector(`input[name="edit-target-auth-type"][value="${initialAuthType}"]`)
    if (initialAuthRadio) initialAuthRadio.checked = true
    syncEditProtocol()

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
      const ssh_username = modal.querySelector('#edit-target-ssh-username').value.trim()
      const authType = protocol === 'ssh' ? (modal.querySelector('input[name="edit-target-auth-type"]:checked')?.value || 'password') : 'password'
      const clearKeyChecked = modal.querySelector('#edit-target-clear-ssh-key') && modal.querySelector('#edit-target-clear-ssh-key').checked
      let ssh_password, ssh_private_key, ssh_private_key_passphrase
      if (protocol === 'rdp' || authType === 'password') {
        const pwVal = modal.querySelector('#edit-target-ssh-password').value
        ssh_password = pwVal === '' ? undefined : pwVal
      }
      if (protocol === 'ssh' && (authType === 'key' || authType === 'key_passphrase')) {
        const keyVal = modal.querySelector('#edit-target-ssh-private-key').value
        ssh_private_key = clearKeyChecked ? '' : (keyVal === '' ? undefined : keyVal)
        if (authType === 'key_passphrase') {
          const keyPassVal = modal.querySelector('#edit-target-ssh-key-passphrase').value
          ssh_private_key_passphrase = clearKeyChecked ? '' : (keyPassVal === '' ? undefined : keyPassVal)
        }
      }
      const tagsRaw = modal.querySelector('#edit-target-tags').value.trim()
      const tags = tagsRaw ? tagsRaw.split(',').map((s) => s.trim()).filter(Boolean) : []
      if (!name || !host) {
        errorEl.textContent = '名前とホストを入力してください'
        errorEl.classList.remove('hidden')
        return
      }
      const updatePayload = { name, host, port, protocol, path: target.path || '', ssh_username }
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
        await API.setTargetTags(targetId, tags)
        modal.classList.add('hidden')
        modal.innerHTML = ''
        groupsCache = null
        await showTreeView('manage')
      } catch (err) {
        errorEl.textContent = err.message || '更新に失敗しました'
        errorEl.classList.remove('hidden')
      } finally {
        submitBtn.disabled = false
      }
    })
  }

  function renderGroupTargetsTable(targets, mode = 'manage') {
    const isManageMode = mode === 'manage'
    if (!targets || targets.length === 0) {
      return '<p class="text-sm text-slate-500">このグループに登録されているサーバーはありません。</p>'
    }
    const targetTags = (t) => Array.isArray(t.tags) ? t.tags : []
    const rows = targets
      .slice()
      .sort((a, b) => (a.name || '').localeCompare(b.name || ''))
      .map(
        (t) => {
          const tags = targetTags(t)
          return `
        <tr class="border-b border-slate-200 hover:bg-slate-50">
          <td class="px-4 py-2 text-sm text-slate-900 font-medium">${escapeHtml(t.name)}</td>
          <td class="px-4 py-2 text-sm text-slate-500">${escapeHtml(t.host)}:${t.port}</td>
          <td class="px-4 py-2 text-sm text-slate-500">${escapeHtml(t.protocol)}</td>
          ${isManageMode ? `<td class="px-4 py-2"><div class="flex flex-wrap items-center gap-2">${tags.length ? renderTagPills(tags) : '<span class="text-xs text-slate-400">—</span>'}</div></td>` : ''}
          <td class="px-4 py-2 text-right">
            ${isManageMode ? `
            <div class="flex items-center justify-end gap-2">
              <button type="button" data-target-id="${escapeHtml(t.id)}" data-target-name="${escapeHtml(t.name)}" data-target-host="${escapeHtml(t.host)}" data-target-port="${t.port}" data-target-protocol="${escapeHtml(t.protocol || 'ssh')}" data-target-path="${escapeHtml(t.path || '')}" data-target-ssh-username="${escapeHtml(t.ssh_username || '')}" data-target-tags="${escapeHtml((tags || []).join(','))}" data-target-has-ssh-key="${t.has_ssh_key ? '1' : '0'}" data-target-needs-passphrase="${t.needs_passphrase ? '1' : '0'}"
                class="edit-btn-in-group rounded bg-slate-100 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-200 border border-slate-300 shadow-sm transition-colors">
                編集
              </button>
              <button type="button" data-target-id="${escapeHtml(t.id)}" data-target-name="${escapeHtml(t.name)}"
                class="delete-btn-in-group rounded border border-red-200 bg-white px-3 py-1.5 text-xs font-medium text-red-700 hover:bg-red-50 shadow-sm transition-colors">
                削除
              </button>
            </div>
            ` : `
            <div class="flex items-center justify-end gap-2">
              ${t.protocol === 'ssh'
            ? `<button type="button" data-terminal-target-id="${escapeHtml(t.id)}" data-terminal-target-name="${escapeHtml(t.name || '')}" data-has-stored-credentials="${t.has_stored_credentials ? '1' : ''}" data-needs-password="${t.needs_password ? '1' : ''}" data-needs-passphrase="${t.needs_passphrase ? '1' : ''}"
              class="connect-btn-in-group terminal-open-btn rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors disabled:opacity-50">
              接続
            </button>`
            : t.protocol === 'vnc'
            ? `<button type="button" data-popup-protocol="vnc" data-popup-target-id="${escapeHtml(t.id)}" data-popup-target-name="${escapeHtml(t.name || '')}"
              class="connect-btn-in-group vnc-open-btn rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors inline-block">
              VNC
            </button>`
            : t.protocol === 'rdp'
            ? `<a href="/rdp?target_id=${encodeURIComponent(t.id)}&target_name=${encodeURIComponent(t.name || '')}"
              target="_blank" rel="noopener noreferrer"
              data-rdp-target-id="${escapeHtml(t.id)}" data-rdp-target-name="${escapeHtml(t.name || '')}"
              class="connect-btn-in-group rdp-open-link rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors inline-block">
              RDP
            </a>`
            : t.protocol === 'tftp'
            ? `<a href="/files?target_id=${encodeURIComponent(t.id)}&target_name=${encodeURIComponent(t.name || '')}&protocol=tftp" class="connect-btn-in-group rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors inline-block">ファイル</a>`
            : `<button data-target-id="${escapeHtml(t.id)}" data-target-name="${escapeHtml(t.name)}" data-protocol="${escapeHtml(t.protocol)}"
              class="connect-btn-in-group rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors disabled:opacity-50">
              接続
            </button>`}
              ${(t.protocol === 'ssh' && (t.has_stored_credentials || t.has_ssh_key)) || t.protocol === 'tftp' ? (t.protocol === 'ssh' ? `<a href="/files?target_id=${encodeURIComponent(t.id)}&target_name=${encodeURIComponent(t.name || '')}" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">ファイル</a>` : '') : ''}
              <button type="button" class="active-sessions-btn rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors" data-target-id="${escapeHtml(t.id)}" data-target-name="${escapeHtml(t.name || '')}">アクティブなセッション</button>
            </div>
            `}
          </td>
        </tr>
      `
        }
      )
      .join('')
    const theadTags = isManageMode ? '<th class="px-4 py-2 text-xs font-semibold text-slate-700">タグ</th>' : ''
    return `
      <div class="overflow-x-auto">
        <table class="min-w-full text-left text-sm">
          <thead class="bg-slate-50 border-b border-slate-200">
            <tr>
              <th class="px-4 py-2 text-xs font-semibold text-slate-700">名前</th>
              <th class="px-4 py-2 text-xs font-semibold text-slate-700">ホスト</th>
              <th class="px-4 py-2 text-xs font-semibold text-slate-700">プロトコル</th>
              ${theadTags}
              <th class="px-4 py-2"></th>
            </tr>
          </thead>
          <tbody>
            ${rows}
          </tbody>
        </table>
      </div>
    `
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

  /** selectedIdForHighlight: 省略時は selectedGroupId を使用（ホーム/サーバー管理）。録画ページでは selectedRecordingsGroupId を渡す */
  function renderGroupTree(node, depth, selectedIdForHighlight) {
    const selectedId = selectedIdForHighlight !== undefined ? selectedIdForHighlight : selectedGroupId
    const children = node.children || {}
    const keys = Object.keys(children)
    if (keys.length === 0) {
      return depth === 0 ? '' : ''
    }
    const padClass = depth > 0 ? 'pl-4 border-l border-black ml-2' : ''
    let html = `<ul class="space-y-1 ${padClass}">`
    if (depth === 0) {
      const isSelectedRoot = selectedId === ''
      const rowClassRoot = isSelectedRoot ? 'bg-sky-100 text-sky-800 font-medium' : ''
      html += `
        <li>
          <div class="flex items-center py-1 pr-2 rounded hover:bg-slate-50 cursor-pointer ${rowClassRoot}" data-group-select="1" data-group-id="">
            <div class="w-[28px] shrink-0 self-stretch"></div>
            <div class="w-3 h-3 rounded-sm bg-slate-200 border border-slate-300 shrink-0"></div>
            <div class="flex-1 min-w-0 pl-2">
              <div class="text-xs font-medium text-slate-800 truncate">root</div>
              <div class="text-[10px] text-slate-500 truncate">${keys.length} グループ</div>
            </div>
          </div>
        </li>
      `
    }
    keys
      .slice()
      .sort((a, b) => a.localeCompare(b))
      .forEach((key) => {
        const child = children[key]
        const count = (child.group && child.group.targets ? child.group.targets.length : 0) || 0
        const isSelected = child.id === selectedId
        const rowClass = isSelected ? 'bg-sky-100 text-sky-800 font-medium' : ''
        const hasChildren = child.children && Object.keys(child.children).length > 0
        const isExpanded = expandedGroups.has(child.id)
        const caret = hasChildren ? (isExpanded ? '▼' : '▶') : ''
        const caretHtml = hasChildren
          ? `<div class="w-[40px] flex items-center justify-center text-[10px] text-slate-700 hover:text-slate-900 leading-none cursor-pointer shrink-0 self-stretch" data-group-toggle="1" data-group-id="${escapeHtml(child.id)}">${caret}</div>`
          : `<div class="w-[40px] shrink-0 self-stretch" data-group-toggle="0"></div>`
        html += `
          <li>
            <div class="flex items-center py-1 pr-2 rounded hover:bg-slate-50 cursor-pointer ${rowClass}" data-group-select="1" data-group-id="${escapeHtml(child.id)}">
              ${caretHtml}
              <div class="w-3 h-3 rounded-sm bg-slate-200 border border-slate-300 shrink-0"></div>
              <div class="flex-1 min-w-0 pl-2">
                <div class="text-xs font-medium text-slate-800 truncate">${escapeHtml(child.name)}</div>
                <div class="text-[10px] text-slate-500 truncate">${escapeHtml(child.id)}${count ? ` ・ ${count} 台` : ''}</div>
              </div>
            </div>
            ${hasChildren && isExpanded ? renderGroupTree(child, depth + 1) : ''}
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
      const isAdmin = meData.role === 'admin'
      const navApiRef = document.getElementById('nav-api-ref')
      navRecordings?.classList.remove('hidden')
      if (isAdmin) {
        navApiRef?.classList.remove('hidden')
        navUsers?.classList.remove('hidden')
        navGroups?.classList.remove('hidden')
      } else {
        navApiRef?.classList.add('hidden')
        navUsers?.classList.add('hidden')
        navGroups?.classList.add('hidden')
      }
    } catch {
      renderLogin(container)
      return
    }

    try {
      groupsCache = await API.groups()
    } catch {
      groupsCache = null
    }

    showTreeView('home')
  })()

  navTargets.addEventListener('click', (e) => {
    e.preventDefault()
    if (!meData) return
    showTreeView('home')
  })

  navRecordings.addEventListener('click', (e) => {
    e.preventDefault()
    if (!meData) return
    showRecordingsPage()
  })

  userNameEl.addEventListener('click', (e) => {
    e.preventDefault()
    if (!meData) return
    showUserInfo()
  })

  navGroups.addEventListener('click', (e) => {
    e.preventDefault()
    showTreeView('manage')
  })

  navUsers?.addEventListener('click', (e) => {
    e.preventDefault()
    if (!meData || meData.role !== 'admin') return
    showUsersPage()
  })

  logoutBtn.addEventListener('click', async () => {
    try {
      await API.logout()
    } catch {
      /* サーバーが応答しなくてもクライアント側のCookieは消す */
    }
    document.cookie = 'vantyx_session=; path=/; max-age=0'
    renderLogin(container)
  })
}

function escapeHtml(s) {
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}

/** Proxmox風のタグピル（角丸・枠線・タグアイコン）を返す。tags は文字列配列。 */
function renderTagPills(tags) {
  if (!tags || tags.length === 0) {
    return '<span class="text-xs text-slate-400">—</span>'
  }
  const tagIcon = '<svg class="shrink-0 opacity-70" width="12" height="12" viewBox="0 0 12 12" fill="currentColor" aria-hidden="true"><path d="M2 1.5C2 1.22 2.22 1 2.5 1H7.5l3 3-3 3H2.5C2.22 7 2 6.78 2 6.5v-5z"/></svg>'
  return tags
    .map((t) => `<span class="inline-flex items-center gap-1.5 rounded-md border border-slate-300 bg-white px-2.5 py-1 text-xs font-medium text-slate-700 shadow-sm">${tagIcon}${escapeHtml(t)}</span>`)
    .join('')
}

/** モーダル内のタグ入力欄の下に「登録済みのタグから選択」を表示し、クリックで入力欄に追加する */
function fillExistingTagsPicker(modalEl, inputId) {
  const input = modalEl.querySelector(`#${inputId}`)
  const container = modalEl.querySelector(`#${inputId}-picker`)
  if (!input || !container) return
  API.tags()
    .then((res) => {
      const allTags = (res && res.tags) || []
      if (allTags.length === 0) {
        container.innerHTML = ''
        return
      }
      container.innerHTML = `<p class="text-xs text-slate-500 mb-1.5">登録済みのタグから選択:</p><div class="flex flex-wrap gap-2">${allTags.map((t) => `<button type="button" class="existing-tag-pill rounded border border-slate-300 bg-slate-50 px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-sky-50 hover:border-sky-300 transition-colors" data-tag="${escapeHtml(t)}">${escapeHtml(t)}</button>`).join('')}</div>`
      container.querySelectorAll('.existing-tag-pill').forEach((btn) => {
        btn.addEventListener('click', () => {
          const tag = (btn.dataset.tag || '').trim()
          if (!tag) return
          const raw = input.value.trim()
          const current = raw ? raw.split(',').map((s) => s.trim()).filter(Boolean) : []
          if (!current.includes(tag)) {
            input.value = current.length ? `${raw}, ${tag}` : tag
          }
        })
      })
    })
    .catch(() => {
      container.innerHTML = ''
    })
}
