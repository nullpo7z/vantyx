/**
 * RDP connection page.
 * Provides:
 *  1. .rdp file download for native client (mstsc, Remmina, etc.)
 *  2. WebSocket proxy endpoint (/ws/rdp) for browser-based RDP clients.
 */
function escapeHtml(s) {
  if (s == null) return ''
  const div = document.createElement('div')
  div.textContent = s
  return div.innerHTML
}

export function renderRdpPage(container) {
  const params = new URLSearchParams(window.location.search)
  const targetId = params.get('target_id') || ''
  const targetName = params.get('target_name') || targetId || 'RDP'

  if (!targetId) {
    container.innerHTML = `
      <div class="min-h-screen flex items-center justify-center p-6 bg-slate-950">
        <div class="text-center text-slate-300">
          <p class="mb-4">target_id が指定されていません。</p>
          <a href="/" class="rounded bg-sky-600 px-4 py-2 text-sm font-medium text-white hover:bg-sky-700">ホームに戻る</a>
        </div>
      </div>
    `
    return
  }

  const wsScheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const wsUrl = `${wsScheme}//${window.location.host}/ws/rdp?target_id=${encodeURIComponent(targetId)}`
  const rdpFileUrl = `/api/rdp/file?target_id=${encodeURIComponent(targetId)}`

  container.innerHTML = `
    <div class="min-h-screen w-screen flex flex-col bg-slate-950">
      <header class="shrink-0 px-4 sm:px-6 py-3 bg-slate-900 border-b border-slate-800 flex items-center justify-between">
        <div class="min-w-0">
          <div class="text-xs text-slate-400">Vantyx RDP</div>
          <div class="text-sm sm:text-base font-semibold text-slate-100 truncate">${escapeHtml(targetName)}</div>
        </div>
        <div class="flex items-center gap-2">
          <a href="/" class="rounded border border-slate-700 bg-slate-900 px-3 py-1.5 text-xs font-semibold text-slate-200 hover:bg-slate-800">戻る</a>
        </div>
      </header>
      <div class="flex-1 flex flex-col items-center justify-center p-6 gap-8">
        <div class="w-full max-w-lg bg-slate-900 border border-slate-800 rounded-lg overflow-hidden">
          <div class="px-6 py-4 border-b border-slate-800">
            <h2 class="text-base font-semibold text-slate-100">ネイティブ RDP クライアントで接続</h2>
            <p class="text-xs text-slate-400 mt-1">Windows リモートデスクトップ (mstsc)、Remmina、FreeRDP 等のクライアントで接続できます。</p>
          </div>
          <div class="px-6 py-5 flex flex-col gap-4">
            <a href="${escapeHtml(rdpFileUrl)}" download
              class="inline-flex items-center justify-center gap-2 rounded bg-sky-600 px-4 py-2.5 text-sm font-medium text-white hover:bg-sky-700 shadow-sm transition-colors">
              <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"/></svg>
              .rdp ファイルをダウンロード
            </a>
            <p class="text-xs text-slate-500">ダウンロードした .rdp ファイルをダブルクリックで開くと、ターゲットに直接接続します。</p>
          </div>
        </div>

        <div class="w-full max-w-lg bg-slate-900 border border-slate-800 rounded-lg overflow-hidden">
          <div class="px-6 py-4 border-b border-slate-800">
            <h2 class="text-base font-semibold text-slate-100">WebSocket プロキシ</h2>
            <p class="text-xs text-slate-400 mt-1">WebSocket 対応の RDP クライアントから接続する場合のエンドポイントです。</p>
          </div>
          <div class="px-6 py-5">
            <div class="flex items-center gap-2">
              <code id="rdp-ws-url" class="flex-1 px-3 py-2 bg-slate-800 rounded text-xs text-slate-300 font-mono break-all select-all border border-slate-700">${escapeHtml(wsUrl)}</code>
              <button id="rdp-copy-url" type="button" class="shrink-0 rounded border border-slate-700 bg-slate-800 px-3 py-2 text-xs text-slate-300 hover:bg-slate-700 transition-colors">コピー</button>
            </div>
          </div>
        </div>
      </div>
    </div>
  `

  container.querySelector('#rdp-copy-url').addEventListener('click', () => {
    const url = container.querySelector('#rdp-ws-url').textContent
    navigator.clipboard.writeText(url).then(() => {
      const btn = container.querySelector('#rdp-copy-url')
      btn.textContent = 'コピー済み'
      setTimeout(() => { btn.textContent = 'コピー' }, 2000)
    }).catch(() => {})
  })
}
