import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    // ブラウザ単体で開発するときは cmd/acmserve (:8430) を叩く。
    // Wails上ではバインディング経由になるのでプロキシは使われない。
    proxy: { '/api': 'http://127.0.0.1:8430' },
  },
  test: {
    environment: 'happy-dom',
    globals: true,
    include: ['src/**/*.test.ts'],
  },
})
