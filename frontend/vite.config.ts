import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Proxy /api ke backend agar dev server bisa memakai origin yang sama.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.INGATIN_API_TARGET || 'http://127.0.0.1:8081',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
  },
})
