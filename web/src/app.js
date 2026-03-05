import API from './api.js'
import { renderLogin } from './login.js'
import { openTerminal } from './terminal.js'

export function renderApp(container) {
  container.innerHTML = `
    <div class="flex-1 flex flex-col">
      <header class="bg-sky-800 text-white px-6 py-3 flex items-center justify-between shadow z-10 shrink-0">
        <div class="flex items-center gap-8">
          <h1 class="text-xl font-semibold tracking-wide">Vantyx</h1>
          <nav class="flex items-center gap-6">
            <a href="#" id="nav-targets" class="text-sm font-semibold border-b-2 border-white pb-1 transition-opacity">ホーム</a>
            <a href="#" id="nav-groups" class="text-sm opacity-80 hover:opacity-100 transition-opacity">サーバー管理</a>
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
      <div id="terminal-modal" class="hidden fixed inset-0 z-50 overflow-hidden"></div>
    </div>
  `

  const mainContent = document.getElementById('main-content')
  const userNameEl = document.getElementById('user-name')
  const logoutBtn = document.getElementById('logout-btn')
  const navTargets = document.getElementById('nav-targets')
  const navGroups = document.getElementById('nav-groups')

  let meData = null
  let groupsCache = null
  let selectedGroupId = ''
  let expandedGroups = new Set()

  function showUserInfo() {
    if (!meData) return
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
      </div>
    `
    // ユーザー情報表示中はどのタブもアクティブ表示にしない
    navTargets.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity'
    navGroups.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity'
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
            </div>
          </section>
        </div>
      `
      const newTreeContainer = mainContent.querySelector('aside .overflow-y-auto')
      if (newTreeContainer && scrollPos > 0) {
        newTreeContainer.scrollTop = scrollPos
      }

      if (isManageMode) {
        mainContent.querySelector('#btn-add-group')?.addEventListener('click', showAddGroupModal)
        mainContent.querySelector('#btn-add-target-in-group')?.addEventListener('click', () => {
          if (!selectedGroupId) return
          showAddTargetModal()
        })
        mainContent.querySelectorAll('.edit-btn-in-group').forEach((btn) => {
          btn.addEventListener('click', () => {
            alert('サーバー情報の編集機能は未実装です。')
          })
        })
      } else {
        mainContent.querySelectorAll('.connect-btn-in-group').forEach((btn) => {
          btn.addEventListener('click', () => {
            const targetId = btn.dataset.targetId
            const targetName = btn.dataset.targetName
            const protocol = btn.dataset.protocol
            if (protocol !== 'ssh') {
              alert('このターゲットは SSH のみ対応しています。Telnet は未対応です。')
              return
            }
            openTerminal(container, targetId, targetName)
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

      if (isManageMode) {
        navTargets.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity'
        navGroups.className = 'text-sm font-semibold border-b-2 border-white pb-1 transition-opacity'
      } else {
        navTargets.className = 'text-sm font-semibold border-b-2 border-white pb-1 transition-opacity'
        navGroups.className = 'text-sm opacity-80 hover:opacity-100 transition-opacity'
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
                  </select>
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
      submitBtn.disabled = true
      try {
        await API.createTarget({ name, host, port, protocol, group_id })
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

  function renderGroupTargetsTable(targets, mode = 'manage') {
    const isManageMode = mode === 'manage'
    if (!targets || targets.length === 0) {
      return '<p class="text-sm text-slate-500">このグループに登録されているサーバーはありません。</p>'
    }
    const rows = targets
      .slice()
      .sort((a, b) => (a.name || '').localeCompare(b.name || ''))
      .map(
        (t) => `
        <tr class="border-b border-slate-200 hover:bg-slate-50">
          <td class="px-4 py-2 text-sm text-slate-900 font-medium">${escapeHtml(t.name)}</td>
          <td class="px-4 py-2 text-sm text-slate-500">${escapeHtml(t.host)}:${t.port}</td>
          <td class="px-4 py-2 text-sm text-slate-500">${escapeHtml(t.protocol)}</td>
          <td class="px-4 py-2 text-right">
            ${isManageMode ? `
            <button data-target-id="${escapeHtml(t.id)}" data-target-name="${escapeHtml(t.name)}"
              class="edit-btn-in-group rounded bg-slate-100 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-200 border border-slate-300 shadow-sm transition-colors disabled:opacity-50">
              編集
            </button>
            ` : `
            <button data-target-id="${escapeHtml(t.id)}" data-target-name="${escapeHtml(t.name)}" data-protocol="${escapeHtml(t.protocol)}"
              class="connect-btn-in-group rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors disabled:opacity-50">
              接続
            </button>
            `}
          </td>
        </tr>
      `,
      )
      .join('')
    return `
      <div class="overflow-x-auto">
        <table class="min-w-full text-left text-sm">
          <thead class="bg-slate-50 border-b border-slate-200">
            <tr>
              <th class="px-4 py-2 text-xs font-semibold text-slate-700">名前</th>
              <th class="px-4 py-2 text-xs font-semibold text-slate-700">ホスト</th>
              <th class="px-4 py-2 text-xs font-semibold text-slate-700">プロトコル</th>
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

  function renderGroupTree(node, depth) {
    const children = node.children || {}
    const keys = Object.keys(children)
    if (keys.length === 0) {
      return depth === 0 ? '' : ''
    }
    const padClass = depth > 0 ? 'pl-4 border-l border-black ml-2' : ''
    let html = `<ul class="space-y-1 ${padClass}">`
    if (depth === 0) {
      const isSelectedRoot = selectedGroupId === ''
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
        const isSelected = child.id === selectedGroupId
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

  userNameEl.addEventListener('click', (e) => {
    e.preventDefault()
    if (!meData) return
    showUserInfo()
  })

  navGroups.addEventListener('click', (e) => {
    e.preventDefault()
    showTreeView('manage')
  })

  logoutBtn.addEventListener('click', () => {
    document.cookie = 'vantyx_session=; path=/; max-age=0'
    renderLogin(container)
  })
}

function escapeHtml(s) {
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}
