import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Guest Web 使用独立输出目录，LAN Server 不会暴露桌面 bundle。
export default defineConfig({
  base: '/guest-assets/',
  plugins: [vue()],
  build: {
    outDir: 'dist/guest',
    emptyOutDir: true,
    rollupOptions: {
      input: fileURLToPath(new URL('./guest.html', import.meta.url)),
    },
  },
})
