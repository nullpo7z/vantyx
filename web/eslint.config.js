import js from '@eslint/js'

export default [
  js.configs.recommended,
  {
    files: ['src/**/*.js'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { window: 'readonly', document: 'readonly', fetch: 'readonly', WebSocket: 'readonly', ResizeObserver: 'readonly', TextEncoder: 'readonly', Blob: 'readonly', ArrayBuffer: 'readonly', alert: 'readonly', crypto: 'readonly', BroadcastChannel: 'readonly', URLSearchParams: 'readonly' },
    },
  },
  { ignores: ['dist', 'node_modules'] },
]
