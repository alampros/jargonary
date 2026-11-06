import { fileURLToPath } from 'node:url'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import { editorValidatorPlugin } from './editorValidatorPlugin'

export default defineConfig(({ mode }) => ({
  ...(mode === 'editor' && {
    root: fileURLToPath(new URL('./editor', import.meta.url)),
    publicDir: false,
  }),
  plugins: [react(), editorValidatorPlugin()],
  base: './',
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    origin: 'http://127.0.0.1:5173',
    cors: { origin: /^http:\/\/(localhost|127\.0\.0\.1)(:\d+)?$/ },
  },
  build: {
    outDir: fileURLToPath(
      new URL(mode === 'editor' ? '../.cache/pages/editor' : '../internal/webui/dist', import.meta.url),
    ),
    emptyOutDir: true,
  },
}))
