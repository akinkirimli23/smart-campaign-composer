import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// In development the API runs on :8080 (docker compose); the dev server
// proxies /api so the browser sees one origin, like behind nginx.
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 3000,
    proxy: { '/api': 'http://localhost:8080' },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
})
