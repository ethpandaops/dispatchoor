import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:9090',
        changeOrigin: true,
        ws: true,
      },
      '/health': {
        target: 'http://localhost:9090',
        changeOrigin: true,
      },
      '/metrics': {
        target: 'http://localhost:9090',
        changeOrigin: true,
      },
    },
  },
})
