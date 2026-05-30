import API from './api.js'
import * as AsciinemaPlayer from 'asciinema-player'
import 'asciinema-player/dist/bundle/asciinema-player.css'
import { t } from './i18n.js'
import { showProgressOverlay, uiAlert } from './ui_dialog.js'

function formatBytes(n) {
  const v = Number(n)
  if (!Number.isFinite(v) || v < 0) return '0 B'
  if (v < 1024) return `${v} B`
  if (v < 1024 * 1024) return `${(v / 1024).toFixed(1)} KB`
  if (v < 1024 * 1024 * 1024) return `${(v / (1024 * 1024)).toFixed(1)} MB`
  return `${(v / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

async function downloadRecordingWithProgress(url, filename, formatLabel) {
  const progress = showProgressOverlay({
    title: t('recordings.downloadProgressTitle'),
    message: t('recordings.downloadConverting', { format: formatLabel }),
  })
  progress.setProgress(null)

  try {
    const res = await fetch(url, { credentials: 'include' })
    if (!res.ok) {
      if (res.status === 503) {
        throw new Error(t('recordings.downloadFailedNoTools'))
      }
      throw new Error(t('recordings.downloadFailed'))
    }

    const total = Number.parseInt(res.headers.get('Content-Length') || '', 10) || 0
    const body = res.body
    if (!body) {
      progress.setMessage(t('recordings.downloadReceiving'))
      if (total > 0) progress.setProgress(0)
      const blob = await res.blob()
      progress.setProgress(100)
      return blob
    }

    progress.setMessage(t('recordings.downloadReceiving'))
    const reader = body.getReader()
    const chunks = []
    let loaded = 0

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      chunks.push(value)
      loaded += value.length
      if (total > 0) {
        if (loaded === value.length) progress.setProgress(0)
        const pct = Math.min(100, Math.round((loaded / total) * 100))
        progress.setProgress(pct)
        progress.setDetail(
          t('recordings.downloadProgressDetail', {
            percent: pct,
            loaded: formatBytes(loaded),
            total: formatBytes(total),
          }),
        )
      } else {
        progress.setProgress(null)
        progress.setDetail(t('recordings.downloadProgressBytes', { loaded: formatBytes(loaded) }))
      }
    }

    progress.setProgress(100)
    progress.setMessage(t('recordings.downloadSaving'))
    return new Blob(chunks, { type: res.headers.get('Content-Type') || undefined })
  } finally {
    progress.close()
  }
}

/** Overlay opacity for recording playback watermark (0–1). */
const RECORDING_WATERMARK_OPACITY = 0.32

/** Target protocol label for tables (SSH, Telnet, …). */
function formatTargetProtocol(protocol) {
  const p = String(protocol || 'ssh').toLowerCase()
  if (p === 'ssh') return 'SSH'
  if (p === 'telnet') return 'Telnet'
  if (p === 'vnc') return 'VNC'
  if (p === 'rdp') return 'RDP'
  if (p === 'tftp') return 'TFTP'
  if (p === 'ftp') return 'FTP'
  if (protocol) return String(protocol).toUpperCase()
  return '—'
}

function defaultDateRange() {
  const to = new Date()
  const from = new Date(to)
  from.setDate(from.getDate() - 30)
  return {
    from: from.toISOString().slice(0, 10),
    to: to.toISOString().slice(0, 10),
  }
}

export async function renderRecordingsPage({
  mainContent,
  meData,
  escapeHtml,
  buildGroupTree,
  renderGroupTree,
  ensureGroupPathExpanded,
  getGroupsCache,
  setGroupsCache,
  expandedGroups,
  getState,
  setState,
  refresh,
}) {
  const {
    groupId: selectedRecordingsGroupId,
    targetId: selectedRecordingsTargetId,
    targetName: selectedRecordingsTargetName,
    filterFrom: recordingsFilterFrom,
    filterTo: recordingsFilterTo,
    filterChannel: recordingsFilterChannel,
    filterUserId: recordingsFilterUserId,
  } = getState()
  const isAdmin = meData && meData.role === 'admin'
  const dates = defaultDateRange()
  const fromVal = recordingsFilterFrom || dates.from
  const toVal = recordingsFilterTo || dates.to

  function showRecordingPlayerModal(recordingId, label, userId, sessionId, startedAt) {
    const modal = document.getElementById('recording-player-modal')
    if (!modal) return
    modal.classList.remove('hidden')
    const fileUrl = `/api/recordings/${encodeURIComponent(recordingId)}/file`
    const watermarkText = [userId, sessionId].filter(Boolean).length
      ? [userId && `User: ${userId}`, sessionId && `Session: ${sessionId}`].filter(Boolean).join(' · ')
      : ''
    // Always render a watermark layer so it can't silently disappear when
    // metadata is missing. Fall back to recordingId. Include timestamps.
    const ts = String(startedAt || '').trim()
    const stamp = ts ? `Recorded: ${ts}` : `Generated: ${new Date().toISOString()}`
    const watermarkLabel =
      (watermarkText ? `${watermarkText} · ${stamp}` : '') ||
      (recordingId ? `Recording: ${recordingId} · ${stamp}` : `Vantyx · ${stamp}`)
    const wmHtml = (() => {
      const esc = (s) => escapeHtml(String(s || ''))
      const line = esc(watermarkLabel)
      const block = `
        <div class="w-[420px] h-[300px] p-4 flex items-center justify-center">
          <div class="transform -rotate-12 font-mono text-sm leading-tight text-center text-white/70"
            style="text-shadow: 0 0 1px rgba(0,0,0,0.55), 0 0 3px rgba(0,0,0,0.35);">
            <div>${line}</div>
            <div class="mt-1">${line}</div>
          </div>
        </div>
      `
      // 60 blocks is enough to cover typical modal sizes with wrapping.
      return Array.from({ length: 60 }).map(() => block).join('')
    })()

    modal.innerHTML = `
      <div id="recording-player-backdrop" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-slate-900 rounded-lg shadow-xl w-full max-w-4xl mx-4 overflow-hidden border border-slate-700 flex flex-col max-h-[90vh]">
          <div class="px-5 py-3 border-b border-slate-700 flex items-center justify-between bg-slate-800 shrink-0">
            <h3 class="font-semibold text-slate-200">${escapeHtml(t('recordings.playerTitle', { label: label || recordingId }))}</h3>
            <button id="recording-player-close" class="text-slate-400 hover:text-white text-2xl leading-none transition-colors">&times;</button>
          </div>
          <div id="recording-player-wrapper" class="p-4 overflow-auto flex-1 min-h-0 min-h-[60vh] relative">
            <div id="recording-player-container"></div>
            <div id="recording-watermark" class="absolute inset-0 pointer-events-none select-none flex flex-wrap content-start z-[999]" style="opacity: ${RECORDING_WATERMARK_OPACITY}" aria-hidden="true">
              ${wmHtml}
            </div>
          </div>
        </div>
      </div>
    `
    const container = modal.querySelector('#recording-player-container')
    let player = null
    try {
      player = AsciinemaPlayer.create(fileUrl, container, {})
    } catch (err) {
      container.innerHTML = `<p class="text-sm text-red-400">${escapeHtml(t('recordings.loadingPlayer', { error: err.message || String(err) }))}</p>`
    }

    const ensureWatermarkPlacement = () => {
      const wm = modal.querySelector('#recording-watermark')
      const wrap = modal.querySelector('#recording-player-wrapper')
      if (!wm || !wrap) return
      const fsEl = document.fullscreenElement
      const target = fsEl || wrap
      if (wm.parentElement !== target) {
        if (target instanceof HTMLElement) {
          const pos = window.getComputedStyle(target).position
          if (pos === 'static' || !pos) target.style.position = 'relative'
        }
        target.appendChild(wm)
      }
    }

    const onFsChange = () => ensureWatermarkPlacement()
    document.addEventListener('fullscreenchange', onFsChange)
    ensureWatermarkPlacement()
    const close = () => {
      if (player && typeof player.dispose === 'function') {
        try {
          player.dispose()
        } catch {
          /* ignore */
        }
      }
      try {
        document.removeEventListener('fullscreenchange', onFsChange)
      } catch {
        /* ignore */
      }
      modal.classList.add('hidden')
      modal.innerHTML = ''
    }
    modal.querySelector('#recording-player-close').addEventListener('click', close)
    modal.querySelector('#recording-player-backdrop').addEventListener('click', (e) => {
      if (e.target.id === 'recording-player-backdrop') close()
    })
  }

  mainContent.innerHTML = `<div class="flex gap-6 w-full h-full"><p class="text-slate-500">${t('recordings.loading')}</p></div>`

  try {
    let groupsCache = getGroupsCache()
    if (!groupsCache) {
      groupsCache = await API.groups()
      setGroupsCache(groupsCache)
    }
    const groups = groupsCache
    const treeRoot = buildGroupTree(groups || [])
    ensureGroupPathExpanded(selectedRecordingsGroupId, expandedGroups)
    const treeHtml = renderGroupTree(treeRoot, selectedRecordingsGroupId, 0, expandedGroups)
    const selectedGroup = (groups || []).find((g) => g.id === selectedRecordingsGroupId)
    const targets = selectedGroup ? (selectedGroup.targets || []) : []

    let sectionContent = ''
    let sectionHeader = ''

    if (selectedRecordingsTargetId) {
      const selectedTarget = targets.find((tg) => tg.id === selectedRecordingsTargetId)
      const selectedTargetProtocol = formatTargetProtocol(selectedTarget?.protocol)
      let adminUsers = []
      if (isAdmin) {
        try {
          const usersRes = await API.users()
          adminUsers = (usersRes && usersRes.items) || []
        } catch {
          /* ignore */
        }
      }
      const userOptions =
        `<option value=""${recordingsFilterUserId === '' ? ' selected' : ''}>${t('recordings.filterUserSelf')}</option>` +
        adminUsers
          .map(
            (u) =>
              `<option value="${escapeHtml(u.id)}"${recordingsFilterUserId === u.id ? ' selected' : ''}>${escapeHtml(u.id)}</option>`,
          )
          .join('')
      const recParams = {
        target_id: selectedRecordingsTargetId,
        from: fromVal,
        to: toVal,
      }
      if (recordingsFilterChannel) recParams.channel_type = recordingsFilterChannel
      if (isAdmin && recordingsFilterUserId) recParams.user_id = recordingsFilterUserId
      const res = await API.recordings(recParams)
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
                )}" data-started-at="${escapeHtml(r.started_at || '')}">${t('recordings.play')}</button>
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
            <button type="button" id="recordings-back-to-servers" class="text-xs text-sky-600 hover:text-sky-800 hover:underline">${t('recordings.backToServers')}</button>
            <h2 class="text-sm font-semibold text-slate-800">${escapeHtml(t('recordings.targetRecordingsTitle', { name: selectedRecordingsTargetName || selectedRecordingsTargetId }))}</h2>
            <span class="text-xs font-medium text-slate-600">${escapeHtml(selectedTargetProtocol)}</span>
            <span class="text-xs text-slate-500">${t('recordings.targetCount', { n: items.length })}</span>
          </div>
        `
      sectionContent = `
          <div class="px-4 py-3 border-b border-slate-200 flex gap-3 flex-wrap items-end bg-white">
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1">${t('recordings.filterFrom')}</label>
              <input id="rec-filter-from" type="date" value="${escapeHtml(fromVal)}" class="rounded border border-slate-300 px-3 py-2 text-sm" />
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1">${t('recordings.filterTo')}</label>
              <input id="rec-filter-to" type="date" value="${escapeHtml(toVal)}" class="rounded border border-slate-300 px-3 py-2 text-sm" />
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1">${t('recordings.filterChannel')}</label>
              <select id="rec-filter-channel" class="rounded border border-slate-300 px-3 py-2 text-sm bg-white">
                <option value=""${recordingsFilterChannel === '' ? ' selected' : ''}>${t('recordings.filterChannelAll')}</option>
                <option value="browser"${recordingsFilterChannel === 'browser' ? ' selected' : ''}>browser</option>
                <option value="cli"${recordingsFilterChannel === 'cli' ? ' selected' : ''}>cli</option>
              </select>
            </div>
            ${
              isAdmin
                ? `<div>
              <label class="block text-xs font-medium text-slate-600 mb-1">${t('recordings.filterUserAdmin')}</label>
              <select id="rec-filter-user" class="rounded border border-slate-300 px-3 py-2 text-sm bg-white min-w-[8rem]">${userOptions}</select>
            </div>`
                : ''
            }
            <button type="button" id="rec-filter-apply" class="rounded bg-sky-600 px-3 py-2 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('recordings.filterApply')}</button>
          </div>
          <div class="overflow-x-auto flex-1 min-h-0">
            <table class="min-w-full text-left text-sm">
              <thead class="bg-slate-50 border-b border-slate-200">
                <tr>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('recordings.headerStarted')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('recordings.headerEnded')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('recordings.headerSessionName')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('recordings.headerDescription')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('recordings.headerChannel')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('recordings.headerActions')}</th>
                </tr>
              </thead>
              <tbody>${
                rows ||
                `<tr><td colspan="6" class="px-4 py-6 text-center text-slate-500">${t('recordings.empty')}</td></tr>`
              }</tbody>
            </table>
          </div>
        `
    } else if (selectedRecordingsGroupId && targets.length > 0) {
      const targetRows = targets
        .map(
          (tg) => `
          <tr class="border-b border-slate-200 hover:bg-slate-50">
            <td class="px-4 py-2 text-sm font-medium text-slate-900">${escapeHtml(tg.name || tg.id || '')}</td>
            <td class="px-4 py-2 text-sm text-slate-600 font-mono">${escapeHtml(tg.host || '')}</td>
            <td class="px-4 py-2 text-sm text-slate-500">${escapeHtml(formatTargetProtocol(tg.protocol))}</td>
            <td class="px-4 py-2">
              <button type="button" class="recordings-view-target-btn rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700" data-target-id="${escapeHtml(
                tg.id,
              )}" data-target-name="${escapeHtml(tg.name || tg.id || '')}">${t('recordings.viewRecordings')}</button>
            </td>
          </tr>
        `,
        )
        .join('')
      sectionHeader = `
          <h2 class="text-sm font-semibold text-slate-800">${escapeHtml(t('recordings.serverListTitle', { group: selectedRecordingsGroupId }))}</h2>
          <span class="text-xs text-slate-500">${t('recordings.targetCountSuffix', { n: targets.length })}</span>
        `
      sectionContent = `
          <div class="overflow-x-auto flex-1 min-h-0">
            <table class="min-w-full text-left text-sm">
              <thead class="bg-slate-50 border-b border-slate-200">
                <tr>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('recordings.headerServer')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('recordings.headerHost')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('recordings.headerProtocol')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700">${t('recordings.headerActions')}</th>
                </tr>
              </thead>
              <tbody>${targetRows}</tbody>
            </table>
          </div>
        `
    } else if (selectedRecordingsGroupId && targets.length === 0) {
      sectionHeader = `<h2 class="text-sm font-semibold text-slate-800">${escapeHtml(selectedRecordingsGroupId)}</h2>`
      sectionContent = `<div class="px-5 py-8 text-center text-sm text-slate-500">${t('recordings.serverListEmpty')}</div>`
    } else {
      sectionHeader = `<h2 class="text-sm font-semibold text-slate-800">${t('recordings.rootTitle')}</h2>`
      sectionContent =
        `<div class="px-5 py-8 text-center text-sm text-slate-500">${t('recordings.rootHint')}</div>`
    }

    mainContent.innerHTML = `
        <div class="flex gap-6 w-full h-full">
          <aside class="w-64 flex-col border-r border-slate-200 bg-white shadow-sm shrink-0 rounded-lg overflow-hidden flex">
            <div class="px-4 py-3 border-b border-slate-200 text-sm font-semibold text-slate-700">${t('recordings.accessGroup')}</div>
            <div class="px-3 py-3 text-xs text-slate-800 overflow-y-auto flex-1 min-h-0" id="recordings-tree-container">
              ${treeHtml || `<p class="text-slate-500 p-2">${t('recordings.noGroups')}</p>`}
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

    mainContent.querySelector('#rec-filter-apply')?.addEventListener('click', () => {
      setState({
        filterFrom: mainContent.querySelector('#rec-filter-from')?.value || '',
        filterTo: mainContent.querySelector('#rec-filter-to')?.value || '',
        filterChannel: mainContent.querySelector('#rec-filter-channel')?.value || '',
        filterUserId: mainContent.querySelector('#rec-filter-user')?.value || '',
      })
      refresh()
    })

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
          btn.dataset.startedAt || '',
        )
      })
    })
    mainContent.querySelectorAll('.recording-download-video').forEach((a) => {
      a.addEventListener('click', async (e) => {
        e.preventDefault()
        if (a.dataset.downloading === '1') return
        const url = a.getAttribute('href')
        const format = a.dataset.format || 'gif'
        const formatLabel = format.toUpperCase()
        const filename = a.getAttribute('download') || `recording.${format}`
        const prevText = a.textContent
        a.dataset.downloading = '1'
        a.classList.add('opacity-50', 'pointer-events-none')
        a.textContent = t('recordings.downloading')
        try {
          const blob = await downloadRecordingWithProgress(url, filename, formatLabel)
          const x = document.createElement('a')
          x.href = URL.createObjectURL(blob)
          x.download = filename
          x.click()
          URL.revokeObjectURL(x.href)
        } catch (err) {
          await uiAlert(err instanceof Error ? err.message : t('recordings.downloadFailed'))
        } finally {
          delete a.dataset.downloading
          a.classList.remove('opacity-50', 'pointer-events-none')
          a.textContent = prevText
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
        ensureGroupPathExpanded(gid, expandedGroups)
        setState({
          groupId: gid,
          targetId: '',
          targetName: '',
        })
        refresh()
      })
    })
  } catch (e) {
    mainContent.innerHTML = `<p class="text-sm text-red-600">${escapeHtml(e.message || t('recordings.fetchFailed'))}</p>`
  }
}

