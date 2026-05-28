function parse(v) {
  const m = String(v || '').trim().match(/^v?(\d+)\.(\d+)\.(\d+)/)
  if (!m) return null
  return { major: Number(m[1]), minor: Number(m[2]), patch: Number(m[3]) }
}

function lt(a, b) {
  if (a.major !== b.major) return a.major < b.major
  if (a.minor !== b.minor) return a.minor < b.minor
  return a.patch < b.patch
}

const current = parse(process.versions.node)
const required = { major: 22, minor: 13, patch: 0 }

if (!current || lt(current, required)) {
  // Keep this plain so it shows up clearly in CI logs.
  console.error(
    [
      `Node.js ${process.versions.node} はサポート対象外です。`,
      `このプロジェクトは Node.js >= ${required.major}.${required.minor}.${required.patch} が必要です。`,
      ``,
      `例:`,
      `  nvm use   # .nvmrc に従う`,
      `  node -v`,
    ].join('\n'),
  )
  process.exit(1)
}

