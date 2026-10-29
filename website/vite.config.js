import { defineConfig, searchForWorkspaceRoot } from 'vite'
import { fileURLToPath } from 'node:url'
import react from '@vitejs/plugin-react'
import desktopClientUI from './desktop-client-ui.js'

// The Speek mascot is shared with the desktop client.
const speek = fileURLToPath(new URL('../shared/speek', import.meta.url))

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), desktopClientUI()],
  base: '/',
  resolve: {
    alias: { '@speek': `${speek}/speek.js` }
  },
  server: {
    fs: { allow: [searchForWorkspaceRoot(process.cwd()), speek] }
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  }
})
