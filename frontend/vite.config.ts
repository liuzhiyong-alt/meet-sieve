import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { wailsMediaFallbackPlugin } from './vite.wails-media'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue(), wailsMediaFallbackPlugin()],
})
