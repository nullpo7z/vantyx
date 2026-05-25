import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [tailwindcss()],
  build: {
    chunkSizeWarningLimit: 1000,
    // @novnc/novnc >= 1.7 uses top-level await for WebCodecs feature
    // detection; bump the bundle target to a year that supports it.
    // All modern evergreen browsers we target have shipped TLA since
    // early 2021 (Chrome 89+, Firefox 89+, Safari 15+, Edge 89+).
    target: 'es2022',
  },
  server: {
    port: 5173,
    proxy: {
      // バックエンドは :8080 でリダイレクトのみのため、API を提供する :8443 にプロキシする
      '/api': { target: 'https://localhost:8443', changeOrigin: true, secure: false },
      '/ws': { target: 'https://localhost:8443', ws: true, secure: false },
      '/healthz': { target: 'https://localhost:8443', secure: false },
    },
  },
})
