import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        ws: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    chunkSizeWarningLimit: 800,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('/node_modules/')) {
            return
          }
          if (id.includes('monaco-editor') || id.includes('monaco-yaml') || id.includes('@monaco-editor')) {
            return 'monaco'
          }
          if (id.includes('@xterm')) {
            return 'xterm'
          }
          if (id.includes('@xyflow') || id.includes('dagre')) {
            return 'flow'
          }
          if (id.includes('echarts')) {
            return 'charts'
          }
          if (id.includes('@tanstack/react-query') || id.includes('zustand')) {
            return 'data'
          }
          if (id.includes('react-router-dom')) {
            return 'router'
          }
          if (id.includes('/react/') || id.includes('/react-dom/') || id.includes('/scheduler/')) {
            return 'react-vendor'
          }
        },
      },
    },
  },
})
