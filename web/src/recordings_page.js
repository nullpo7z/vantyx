import API from './api.js'
import * as AsciinemaPlayer from 'asciinema-player'
import 'asciinema-player/dist/bundle/asciinema-player.css'
import { t } from './i18n.js'
import { formatDateInputValue, formatDateTime } from './datetime.js'
import { queueRecordingExportAndNotify } from './recording_exports_page.js'
import { uiAlert, uiConfirm } from './ui_dialog.js'

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
  // Calendar dates in the viewer's timezone, not UTC: with toISOString()
  // a user in Asia/Tokyo opening the page at 08:00 got "To: yesterday".
  return {
    from: formatDateInputValue(from),
    to: formatDateInputValue(to),
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
  onGoToExports,
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

  const ZOOM_MIN_PX = 8
  const ZOOM_MAX_PX = 40
  const ZOOM_STEP_PX = 2
  const ZOOM_DEFAULT_PX = 16

  function showRecordingPlayerModal(recordingId, label, mediaType, channelType) {
    const modal = document.getElementById('recording-player-modal')
    if (!modal) return
    modal.classList.remove('hidden')
    const isVideo =
      mediaType === 'video' || channelType === 'rdp' || channelType === 'vnc'
    const fileUrl = isVideo
      ? `/api/recordings/${encodeURIComponent(recordingId)}/file?format=mp4`
      : `/api/recordings/${encodeURIComponent(recordingId)}/file`

    const zoomControls = isVideo
      ? ''
      : `
        <div class="flex items-center gap-1">
          <button id="recording-player-zoom-out" type="button" title="${escapeHtml(t('recordings.zoomOut'))}" class="rounded border border-slate-600 bg-slate-700 w-7 h-7 text-slate-200 hover:bg-slate-600 leading-none">−</button>
          <span id="recording-player-zoom-level" class="w-12 text-center text-xs text-slate-400 select-none"></span>
          <button id="recording-player-zoom-in" type="button" title="${escapeHtml(t('recordings.zoomIn'))}" class="rounded border border-slate-600 bg-slate-700 w-7 h-7 text-slate-200 hover:bg-slate-600 leading-none">+</button>
          <button id="recording-player-zoom-reset" type="button" title="${escapeHtml(t('recordings.zoomReset'))}" class="ml-1 rounded border border-slate-600 bg-slate-700 px-2 h-7 text-xs text-slate-200 hover:bg-slate-600">${escapeHtml(t('recordings.zoomReset'))}</button>
        </div>
      `

    modal.innerHTML = `
      <div id="recording-player-backdrop" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
        <div class="bg-slate-900 rounded-lg shadow-xl w-full max-w-4xl mx-4 overflow-hidden border border-slate-700 flex flex-col max-h-[90vh]">
          <div class="px-5 py-3 border-b border-slate-700 flex items-center justify-between gap-3 bg-slate-800 shrink-0">
            <h3 class="font-semibold text-slate-200 truncate min-w-0">${escapeHtml(t('recordings.playerTitle', { label: label || recordingId }))}</h3>
            <div class="flex items-center gap-3 shrink-0">
              ${zoomControls}
              <button id="recording-player-close" class="text-slate-400 hover:text-white text-2xl leading-none transition-colors">&times;</button>
            </div>
          </div>
          <div id="recording-player-wrapper" class="p-4 overflow-auto flex-1 min-h-0 min-h-[60vh]">
            <div id="recording-player-container"></div>
          </div>
        </div>
      </div>
    `
    const container = modal.querySelector('#recording-player-container')
    let player = null
    // null = auto-fit (asciinema-player's default "fit" scaling); once the
    // user zooms in/out we switch to a fixed pixel size they control.
    let zoomPx = null
    // Tracks whether the asciinema player is currently playing, so a
    // zoom change (which has to dispose and re-create the player) can
    // resume playback instead of dropping the user back to a paused
    // poster every time they press +/−.
    let playing = false

    function disposePlayer() {
      if (player && typeof player.dispose === 'function') {
        try {
          player.dispose()
        } catch {
          /* ignore */
        }
      }
      player = null
    }

    function createPlayer(startAt, autoPlay = false) {
      container.innerHTML = ''
      const opts = startAt ? { startAt } : {}
      if (autoPlay) opts.autoPlay = true
      if (zoomPx != null) {
        opts.fit = false
        opts.terminalFontSize = `${zoomPx}px`
      }
      playing = false
      try {
        player = AsciinemaPlayer.create(fileUrl, container, opts)
      } catch (err) {
        container.innerHTML = `<p class="text-sm text-red-400">${escapeHtml(t('recordings.loadingPlayer', { error: err.message || String(err) }))}</p>`
        return
      }
      try {
        // asciinema-player v3 event names: 'play' / 'playing' fire when
        // playback (re)starts, 'pause' and 'ended' when it stops.
        player.addEventListener('play', () => { playing = true })
        player.addEventListener('playing', () => { playing = true })
        player.addEventListener('pause', () => { playing = false })
        player.addEventListener('ended', () => { playing = false })
      } catch {
        /* older player build without addEventListener — zoom just won't auto-resume */
      }
    }

    const zoomLevelEl = modal.querySelector('#recording-player-zoom-level')
    function updateZoomLabel() {
      if (!zoomLevelEl) return
      zoomLevelEl.textContent = zoomPx == null ? t('recordings.zoomFit') : `${zoomPx}px`
    }

    async function applyZoom(nextZoomPx) {
      if (!player) {
        zoomPx = nextZoomPx
        updateZoomLabel()
        return
      }
      const resume = playing
      let startAt = 0
      try {
        startAt = (await player.getCurrentTime()) || 0
      } catch {
        /* keep 0 */
      }
      zoomPx = nextZoomPx
      disposePlayer()
      createPlayer(startAt, resume)
      updateZoomLabel()
    }

    if (isVideo) {
      container.innerHTML = `<video id="recording-video-player" class="w-full max-h-[70vh] bg-black" controls playsinline src="${escapeHtml(fileUrl)}"></video>`
    } else {
      createPlayer()
      updateZoomLabel()
      modal.querySelector('#recording-player-zoom-in')?.addEventListener('click', () => {
        const base = zoomPx == null ? ZOOM_DEFAULT_PX : zoomPx
        void applyZoom(Math.min(ZOOM_MAX_PX, base + ZOOM_STEP_PX))
      })
      modal.querySelector('#recording-player-zoom-out')?.addEventListener('click', () => {
        const base = zoomPx == null ? ZOOM_DEFAULT_PX : zoomPx
        void applyZoom(Math.max(ZOOM_MIN_PX, base - ZOOM_STEP_PX))
      })
      modal.querySelector('#recording-player-zoom-reset')?.addEventListener('click', () => {
        void applyZoom(null)
      })
    }

    const close = () => {
      const video = modal.querySelector('#recording-video-player')
      if (video && video.tagName === 'VIDEO') {
        try {
          video.pause()
          video.removeAttribute('src')
          video.load()
        } catch {
          /* ignore */
        }
      }
      disposePlayer()
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
          const label = [formatDateTime(r.started_at, undefined, ''), r.target_id || ''].filter(Boolean).join(' — ') || r.id
          const isVideo = r.media_type === 'video'
          const castLink = !isVideo
            ? `<a href="/api/recordings/${encodeURIComponent(r.id)}/file?format=cast" download="${escapeHtml(
                r.id,
              )}.cast" class="rounded border border-sky-600 bg-sky-50 px-2 py-1 text-xs font-medium text-sky-800 hover:bg-sky-100" title="${escapeHtml(t('recordings.castDownloadHint'))}">.cast</a>`
            : ''
          const mp4Link = isVideo
            ? `<a href="/api/recordings/${encodeURIComponent(r.id)}/file?format=mp4" download="${escapeHtml(
                r.id,
              )}.mp4" class="rounded border border-emerald-600 bg-emerald-50 px-2 py-1 text-xs font-medium text-emerald-800 hover:bg-emerald-100" title="${escapeHtml(t('recordings.mp4DownloadHint'))}">MP4</a>`
            : ''
          const mp4Btn = !isVideo
            ? `<button type="button" class="recording-queue-export rounded border border-emerald-600 bg-emerald-50 px-2 py-1 text-xs font-medium text-emerald-800 hover:bg-emerald-100" data-id="${escapeHtml(
                r.id,
              )}" data-format="mp4" title="${escapeHtml(t('recordings.mp4ExportHint'))}">MP4</button>`
            : ''
          const gifHint = isVideo ? t('recordings.gifExportHintVideo') : t('recordings.gifExportHintCast')
          const gifBtn = `<button type="button" class="recording-queue-export rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50" data-id="${escapeHtml(
            r.id,
          )}" data-format="gif" title="${escapeHtml(gifHint)}">GIF</button>`
          const deleteBtn = isAdmin
            ? `<button type="button" class="recording-delete-btn rounded border border-red-200 bg-white px-2 py-1 text-xs font-medium text-red-700 hover:bg-red-50" data-id="${escapeHtml(
                r.id,
              )}">${t('recordings.delete')}</button>`
            : ''
          return `
          <tr class="border-b border-slate-200 hover:bg-slate-50">
            <td class="px-4 py-2 text-sm text-slate-700 whitespace-nowrap">${escapeHtml(formatDateTime(r.started_at))}</td>
            <td class="px-4 py-2 text-sm text-slate-700 whitespace-nowrap">${escapeHtml(formatDateTime(r.ended_at))}</td>
            <td class="px-4 py-2 text-sm font-medium text-slate-900 truncate" title="${escapeHtml(r.session_name || '')}">${escapeHtml(r.session_name || '—')}</td>
            <td class="px-4 py-2 text-sm text-slate-600 truncate" title="${escapeHtml(
              r.session_description || '',
            )}">${escapeHtml(r.session_description || '—')}</td>
            <td class="px-4 py-2 text-sm text-slate-600 whitespace-nowrap">${escapeHtml(r.channel_type || '')}</td>
            <td class="px-4 py-2">
              <div class="flex items-center gap-2 flex-wrap">
                <button type="button" class="recording-play-btn rounded border border-slate-300 bg-white px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-50" data-id="${escapeHtml(
                  r.id,
                )}" data-label="${escapeHtml(label)}" data-media-type="${escapeHtml(
                  r.media_type || '',
                )}" data-channel-type="${escapeHtml(r.channel_type || '')}">${t('recordings.play')}</button>
                ${castLink}
                ${mp4Link}
                ${mp4Btn}
                ${gifBtn}
                ${deleteBtn}
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
            ${
              typeof onGoToExports === 'function'
                ? `<button type="button" id="recordings-open-exports" class="ml-auto text-xs text-sky-600 hover:text-sky-800 hover:underline">${t('recordings.openExportsPage')}</button>`
                : ''
            }
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
                <option value="rdp"${recordingsFilterChannel === 'rdp' ? ' selected' : ''}>rdp</option>
                <option value="vnc"${recordingsFilterChannel === 'vnc' ? ' selected' : ''}>vnc</option>
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
            <table class="min-w-full text-left text-sm table-fixed">
              <thead class="bg-slate-50 border-b border-slate-200">
                <tr>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[14%]">${t('recordings.headerStarted')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[14%]">${t('recordings.headerEnded')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[16%]">${t('recordings.headerSessionName')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[20%]">${t('recordings.headerDescription')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[10%]">${t('recordings.headerChannel')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[26%]">${t('recordings.headerActions')}</th>
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
              <button type="button" class="recordings-view-target-btn rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 w-[140px] text-center whitespace-nowrap" data-target-id="${escapeHtml(
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
            <table class="min-w-full text-left text-sm table-fixed">
              <thead class="bg-slate-50 border-b border-slate-200">
                <tr>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[30%]">${t('recordings.headerServer')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[30%]">${t('recordings.headerHost')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[15%]">${t('recordings.headerProtocol')}</th>
                  <th class="px-4 py-2 text-xs font-semibold text-slate-700 w-[25%]">${t('recordings.headerActions')}</th>
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
    mainContent.querySelector('#recordings-open-exports')?.addEventListener('click', () => {
      onGoToExports?.()
    })
    mainContent.querySelectorAll('.recording-queue-export').forEach((btn) => {
      btn.addEventListener('click', async () => {
        if (btn.dataset.queuing === '1') return
        const recordingId = btn.dataset.id || ''
        const format = btn.dataset.format || 'gif'
        btn.dataset.queuing = '1'
        btn.classList.add('opacity-50', 'pointer-events-none')
        const prev = btn.textContent
        btn.textContent = t('recordings.queuingExport')
        try {
          await queueRecordingExportAndNotify(recordingId, format, onGoToExports)
        } finally {
          delete btn.dataset.queuing
          btn.classList.remove('opacity-50', 'pointer-events-none')
          btn.textContent = prev
        }
      })
    })
    mainContent.querySelectorAll('.recording-delete-btn').forEach((btn) => {
      btn.addEventListener('click', async () => {
        const recordingId = btn.dataset.id || ''
        if (!recordingId) return
        const ok = await uiConfirm(t('recordings.confirmDelete'))
        if (!ok) return
        btn.disabled = true
        try {
          await API.deleteRecording(recordingId)
          refresh()
        } catch (err) {
          btn.disabled = false
          await uiAlert(t('recordings.deleteFailed', { error: err.message || String(err) }))
        }
      })
    })
    mainContent.querySelectorAll('.recording-play-btn').forEach((btn) => {
      btn.addEventListener('click', () => {
        showRecordingPlayerModal(
          btn.dataset.id || '',
          btn.dataset.label || '',
          btn.dataset.mediaType || '',
          btn.dataset.channelType || '',
        )
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

