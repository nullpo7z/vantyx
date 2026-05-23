import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [tailwindcss()],
  build: {
    chunkSizeWarningLimit: 1000,
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
