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
  optimizeDeps: {
    esbuildOptions: {
      target: 'esnext',
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/docs': {
        target: 'http://localhost:3000',
        changeOrigin: true,
        ws: true,
      },
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
        ws: true,
      },
    },
  },
  build: {
    target: 'esnext',
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
          if (id.includes('@visactor/react-vchart')) {
            return 'vchart-react'
          }
          if (id.includes('@visactor/vchart') || id.includes('@visactor/vchart-extension')) {
            return 'vchart-core'
          }
          if (id.includes('@visactor/vrender-')) {
            return 'vchart-render'
          }
          if (
            id.includes('@visactor/vdataset')
            || id.includes('@visactor/vutils')
            || id.includes('@visactor/vutils-extension')
            || id.includes('@visactor/vscale')
            || id.includes('@visactor/vlayouts')
          ) {
            return 'vchart-runtime'
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
