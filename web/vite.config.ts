import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // 開発時は Go サーバー（cmd/server）に /api をそのまま転送する。
      // 本番ビルドは cmd/server が web/dist を静的配信するので不要。
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
