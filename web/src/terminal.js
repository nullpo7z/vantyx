import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import { t } from './i18n.js'

/**
 * Opens a terminal modal: first shows a form for target SSH credentials,
 * then connects WebSocket to /ws/ssh?target_id=..., sends credentials as first text message,
 * then bridges xterm.js with the SSH session.
 */
export function openTerminal(container, targetId, targetName) {
  const modal = container.querySelector('#terminal-modal')
  if (!modal) return

  modal.classList.remove('hidden')
  modal.innerHTML = `
    <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4">
      <div id="terminal-dialog" class="bg-white rounded-lg shadow-xl w-full max-w-3xl max-h-[90vh] flex flex-col overflow-hidden border border-slate-200/50">
        <div class="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
          <h3 class="font-semibold text-slate-800 flex items-center gap-2">
            <span class="w-2 h-2 rounded-full bg-green-500"></span>
            ${escapeHtml(targetName)}
          </h3>
          <button id="terminal-close" class="text-slate-500 hover:text-slate-700 text-2xl leading-none transition-colors">&times;</button>
        </div>
        
        <form id="terminal-credentials" class="flex flex-col">
          <div class="px-6 py-5 space-y-5">
            <p class="text-sm text-slate-600">${t('terminal.legacyIntro')}</p>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('terminal.sshUsername')}</label>
              <input type="text" id="term-username" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" placeholder="root" />
            </div>
            <div>
              <label class="block text-xs font-medium text-slate-600 mb-1.5">${t('terminal.sshPassword')}</label>
              <input type="password" id="term-password" class="w-full rounded border border-slate-300 px-3 py-2 text-sm text-slate-800 focus:outline-none focus:ring-1 focus:ring-sky-500 focus:border-sky-500 bg-white" />
            </div>
            <p id="terminal-connect-error" class="text-sm text-red-600 hidden"></p>
          </div>
          <div class="px-6 py-4 bg-slate-50 flex justify-end gap-3 border-t border-slate-200">
            <button type="button" id="terminal-cancel-btn" class="rounded border border-slate-300 bg-white px-3 py-1.5 text-xs font-semibold text-slate-700 hover:bg-slate-50 shadow-sm transition-colors">${t('terminal.cancel')}</button>
            <button type="submit" id="terminal-connect-btn" class="rounded bg-sky-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">${t('terminal.connect')}</button>
          </div>
        </form>
        
        <div id="terminal-body" class="hidden flex-1 flex flex-col min-h-0 bg-slate-900 p-2">
          <div id="terminal-container" class="flex-1 min-h-[400px]"></div>
        </div>
      </div>
    </div>
  `

  const credentialsPanel = modal.querySelector('#terminal-credentials')
  const bodyPanel = modal.querySelector('#terminal-body')
  const closeBtn = modal.querySelector('#terminal-close')
  const connectBtn = modal.querySelector('#terminal-connect-btn')
  const cancelBtn = modal.querySelector('#terminal-cancel-btn')
  const errorEl = modal.querySelector('#terminal-connect-error')

  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const wsUrl = `${protocol}//${window.location.host}/ws/ssh?target_id=${encodeURIComponent(targetId)}`

  function close() {
    modal.classList.add('hidden')
    modal.innerHTML = ''
  }

  closeBtn.addEventListener('click', close)
  cancelBtn.addEventListener('click', close)

  credentialsPanel.addEventListener('submit', (e) => {
    e.preventDefault()
    const username = modal.querySelector('#term-username').value.trim()
    const password = modal.querySelector('#term-password').value
    if (!username) {
      errorEl.textContent = t('terminal.usernameRequired')
      errorEl.classList.remove('hidden')
      return
    }

    errorEl.classList.add('hidden')
    connectBtn.disabled = true

    const ws = new WebSocket(wsUrl)
    ws.binaryType = 'arraybuffer'

    ws.onopen = () => {
      ws.send(JSON.stringify({ username, password }))
    }

    ws.onmessage = (ev) => {
      if (typeof ev.data === 'string') {
        if (ev.data.startsWith('error:')) {
          errorEl.textContent = ev.data.slice(6).trim()
          errorEl.classList.remove('hidden')
          connectBtn.disabled = false
          ws.close()
          return
        }
      }
      if (!term) {
        credentialsPanel.classList.add('hidden')
        bodyPanel.classList.remove('hidden')
        startXterm(ws)
      }
      if (term) {
        if (typeof ev.data === 'string') {
          term.write(ev.data)
        } else {
          term.write(new Uint8Array(ev.data))
        }
      }
    }

    ws.onerror = () => {
      errorEl.textContent = t('terminal.wsConnectFailed')
      errorEl.classList.remove('hidden')
      connectBtn.disabled = false
    }

    ws.onclose = () => {
      if (term) term.write(`\r\n\n${t('terminal.closed')}\r\n`)
      connectBtn.disabled = false
    }
  })

  let term = null

  function startXterm(ws) {
    const termEl = modal.querySelector('#terminal-container')
    term = new Terminal({
      cursorBlink: true,
      theme: { background: '#0f172a', foreground: '#e2e8f0' },
      fontFamily: 'ui-monospace, monospace',
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.loadAddon(new WebLinksAddon())
    term.open(termEl)
    fitAddon.fit()

    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data))
      }
    })

    const resizeObserver = new ResizeObserver(() => fitAddon.fit())
    resizeObserver.observe(termEl)

    const onClose = () => {
      resizeObserver.disconnect()
      try { ws.close() } catch { /* ignore */ }
      close()
    }
    modal.querySelector('#terminal-close').replaceWith(modal.querySelector('#terminal-close').cloneNode(true))
    modal.querySelector('#terminal-close').addEventListener('click', onClose)
  }
}

function escapeHtml(s) {
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}
