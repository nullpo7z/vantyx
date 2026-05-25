// テーマ管理（ライト / ダーク）。
//
// 仕様:
// - 設定は localStorage['vantyx_theme'] に 'dark' | 'light' で保存。
// - 未設定の場合は OS の prefers-color-scheme: dark に追従。
// - <html class="dark"> の有無で CSS 側のダークモードを切り替える。
//
// FOUC 対策として web/index.html のインラインスクリプトでも同じロジックを
// 適用しているが、SPA 内のページ切替・full page reload・ビルド済み index.html
// のキャッシュ事故などで取りこぼしが起こり得るため、JS モジュール側にも
// 明示的な初期化エントリポイントを用意する。

const STORAGE_KEY = 'vantyx_theme'

function readSavedTheme() {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    return v === 'dark' || v === 'light' ? v : null
  } catch {
    return null
  }
}

function prefersDarkColorScheme() {
  try {
    return Boolean(
      typeof window !== 'undefined' &&
        window.matchMedia &&
        window.matchMedia('(prefers-color-scheme: dark)').matches,
    )
  } catch {
    return false
  }
}

/** 現在保存されているテーマ。未保存なら OS の設定にフォールバック。 */
export function getStoredTheme() {
  const saved = readSavedTheme()
  if (saved) return saved
  return prefersDarkColorScheme() ? 'dark' : 'light'
}

/**
 * localStorage / prefers-color-scheme から導出したテーマを <html> に反映する。
 * ページ遷移後でも繰り返し呼び出して安全（冪等）。
 */
export function applyStoredTheme() {
  if (typeof document === 'undefined') return
  const theme = getStoredTheme()
  const root = document.documentElement
  if (!root) return
  if (theme === 'dark') {
    root.classList.add('dark')
  } else {
    root.classList.remove('dark')
  }
}

/**
 * テーマをトグルし、localStorage に保存する。
 * 戻り値は反映後のテーマ ('dark' | 'light')。
 */
export function toggleStoredTheme() {
  if (typeof document === 'undefined') return 'light'
  const root = document.documentElement
  const isDark = root.classList.toggle('dark')
  const next = isDark ? 'dark' : 'light'
  try {
    localStorage.setItem(STORAGE_KEY, next)
  } catch {
    /* localStorage 不可（プライベートブラウジング等）でも UI 動作は継続 */
  }
  return next
}

// モジュール読み込み時にも一度だけ適用しておく。
// （index.html のインラインスクリプトが何らかの理由で動かなかった場合の保険）
applyStoredTheme()
