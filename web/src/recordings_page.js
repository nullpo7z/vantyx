import API from './api.js'
import * as AsciinemaPlayer from 'asciinema-player'
import 'asciinema-player/dist/bundle/asciinema-player.css'

export async function renderRecordingsPage({
  mainContent,
  escapeHtml,
  buildGroupTree,
  renderGroupTree,
  getGroupsCache,
  setGroupsCache,
  expandedGroups,
  getState,
  setState,
  refresh,
}) {
  const { groupId: selectedRecordingsGroupId, targetId: selectedRecordingsTargetId, targetName: selectedRecordingsTargetName } = getState()

  function showRecordingPlayerModal(recordingId, label, userId, sessionId) {
    const modal = document.getElementById('recording-player-modal')
    if (!modal) return
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
            ${
              watermarkText
                ? `<div id="recording-watermark" class="absolute inset-0 pointer-events-none flex items-end justify-center pb-2 text-slate-500/70 text-xs font-mono select-none" aria-hidden="true">${escapeHtml(
                    watermarkText,
                  )}</div>`
                : ''
            }
          </div>
        </div>
      </div>
    `
    const container = modal.querySelector('#recording-player-container')
    let player = null
    try {
      player = AsciinemaPlayer.create(fileUrl, container, {})
    } catch (err) {
      container.innerHTML = `<p class="text-sm text-red-400">再生の読み込みに失敗しました: ${escapeHtml(err.message || String(err))}</p>`
    }
    const close = () => {
      if (player && typeof player.dispose === 'function') {
        try {
          player.dispose()
        } catch {
          /* ignore */
        }
      }
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#recording-player-close').addEventListener('click', close)
    modal.querySelector('#recording-player-backdrop').addEventListener('click', (e) => {
      if (e.target.id === 'recording-player-backdrop') close()
    })
  }

  mainContent.innerHTML = '<div class="flex gap-6 w-full h-full"><p class="text-slate-500">読み込み中…</p></div>'

  try {
    let groupsCache = getGroupsCache()
    if (!groupsCache) {
      groupsCache = await API.groups()
      setGroupsCache(groupsCache)
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
      const rows = items
        .map((r) => {
          const label = [r.started_at || '', r.target_id || ''].filter(Boolean).join(' — ') || r.id
          return `
          <tr class="border-b border-slate-200 hover:bg-slate-50">
            <td class="px-4 py-2 text-sm text-slate-700">${escapeHtml(r.started_at || '')}</td>
            <td class="px-4 py-2 text-sm text-slate-700">${escapeHtml(r.ended_at || '—')}</td>
            <td class="px-4 py-2 text-sm font-medium text-slate-900">${escapeHtml(r.session_name || '—')}</td>
            <td class="px-4 py-2 text-sm text-slate-600 max-w-[12rem] truncate" title="${escapeHtml(
              r.session_description || '',
            )}">${escapeHtml(r.session_description || '—')}</td>
            <td class="px-4 py-2 text-sm text-slate-600">${escapeHtml(r.channel_type || '')}</td>
            <td class="px-4 py-2">
              <div class="flex items-center gap-2 flex-wrap">
                <button type="button" class="recording-play-btn rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50" data-id="${escapeHtml(
                  r.id,
                )}" data-label="${escapeHtml(label)}" data-user-id="${escapeHtml(r.user_id || '')}" data-session-id="${escapeHtml(
                  r.session_id || '',
                )}">再生</button>
                <a href="/api/recordings/${encodeURIComponent(r.id)}/file?format=cast" download="${escapeHtml(
                  r.id,
                )}.cast" class="rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50">.cast</a>
                <a href="/api/recordings/${encodeURIComponent(r.id)}/file?format=gif" download="${escapeHtml(
                  r.id,
                )}.gif" class="recording-download-video rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50" data-format="gif">GIF</a>
                <a href="/api/recordings/${encodeURIComponent(r.id)}/file?format=webm" download="${escapeHtml(
                  r.id,
                )}.webm" class="recording-download-video rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50" data-format="webm">WebM</a>
              </div>
            </td>
          </tr>
        `
        })
        .join('')
      sectionHeader = `
          <div class="flex items-center gap-3 flex-wrap">
            <button type="button" id="recordings-back-to-servers" class="text-xs text-sky-600 hover:text-sky-800 hover:underline">← サーバー一覧</button>
            <h2 class="text-sm font-semibold text-slate-800">${escapeHtml(
              selectedRecordingsTargetName || selectedRecordingsTargetId,
            )} — 録画一覧</h2>
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
              <tbody>${
                rows ||
                '<tr><td colspan="6" class="px-4 py-6 text-center text-slate-500">このサーバーの録画はありません</td></tr>'
              }</tbody>
            </table>
          </div>
        `
    } else if (selectedRecordingsGroupId && targets.length > 0) {
      const targetRows = targets
        .map(
          (t) => `
          <tr class="border-b border-slate-200 hover:bg-slate-50">
            <td class="px-4 py-2 text-sm font-medium text-slate-900">${escapeHtml(t.name || t.id || '')}</td>
            <td class="px-4 py-2 text-sm text-slate-600 font-mono">${escapeHtml(t.host || '')}</td>
            <td class="px-4 py-2">
              <button type="button" class="recordings-view-target-btn rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700" data-target-id="${escapeHtml(
                t.id,
              )}" data-target-name="${escapeHtml(t.name || t.id || '')}">録画を見る</button>
            </td>
          </tr>
        `,
        )
        .join('')
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
      sectionContent =
        '<div class="px-5 py-8 text-center text-sm text-slate-500">左のグループを選択し、サーバー一覧から「録画を見る」でそのサーバーの録画を表示します。</div>'
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
      setState({ targetId: '', targetName: '' })
      refresh()
    })
    mainContent.querySelectorAll('.recordings-view-target-btn').forEach((btn) => {
      btn.addEventListener('click', () => {
        setState({
          targetId: btn.dataset.targetId || '',
          targetName: btn.dataset.targetName || '',
        })
        refresh()
      })
    })
    mainContent.querySelectorAll('.recording-play-btn').forEach((btn) => {
      btn.addEventListener('click', () => {
        showRecordingPlayerModal(
          btn.dataset.id || '',
          btn.dataset.label || '',
          btn.dataset.userId || '',
          btn.dataset.sessionId || '',
        )
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
            // eslint-disable-next-line no-alert
            alert(
              err.message ||
                '動画のダウンロードに失敗しました。サーバーに agg（および WebM の場合は ffmpeg）がインストールされている必要があります。',
            )
            return
          }
          const blob = await res.blob()
          const x = document.createElement('a')
          x.href = URL.createObjectURL(blob)
          x.download = filename
          x.click()
          URL.revokeObjectURL(x.href)
        } catch (err) {
          // eslint-disable-next-line no-alert
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
        if (expandedGroups.has(gid)) expandedGroups.delete(gid)
        else expandedGroups.add(gid)
        refresh()
      })
    })
    mainContent.querySelectorAll('[data-group-select="1"]').forEach((el) => {
      el.addEventListener('click', () => {
        const gid = el.getAttribute('data-group-id') || ''
        setState({
          groupId: gid,
          targetId: '',
          targetName: '',
        })
        refresh()
      })
    })
  } catch (e) {
    mainContent.innerHTML = `<p class="text-sm text-red-600">${escapeHtml(e.message || '取得に失敗しました')}</p>`
  }
}

