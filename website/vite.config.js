import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import desktopClientUI from './desktop-client-ui.js'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), desktopClientUI()],
  base: '/',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  }
})

