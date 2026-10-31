import { defineConfig, configDefaults } from 'vitest/config'
import react from '@vitejs/plugin-react'
import path from 'path'

const apiProxyTarget = process.env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:8080'
const base = process.env.VITE_BASE_PATH || '/'
if (!/^\/(?:[A-Za-z0-9_-]+\/)*$/.test(base)) {
  throw new Error(
    'VITE_BASE_PATH must be a local path with leading/trailing slashes, such as /team/',
  )
}
const prefix = base.slice(0, -1)

export default defineConfig(() => ({
  base,
  plugins: [react()],
  test: {
    exclude: [...configDefaults.exclude, 'e2e/**'],
    fileParallelism: false,
    setupFiles: ['./src/test/setup.ts'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html', 'json-summary'],
      reportsDirectory: 'coverage',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/**/*.d.ts', 'src/**/*.test.{ts,tsx}', 'src/vite-env.d.ts'],
      thresholds: {
        branches: 8,
        functions: 10,
        lines: 10,
        statements: 10,
      },
    },
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  optimizeDeps: {
    include: ['@visactor/react-vchart', '@visactor/vchart', '@xterm/addon-fit', '@xterm/xterm'],
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
      [prefix + '/api']: {
        target: apiProxyTarget,
        changeOrigin: true,
        ws: true,
        rewrite: (path) => path.slice(prefix.length),
        cookiePathRewrite: prefix
          ? { '/api/v1/auth': prefix + '/api/v1/auth', '/': prefix + '/' }
          : undefined,
      },
    },
  },
  build: {
    target: 'esnext',
    outDir: 'dist',
    emptyOutDir: true,
    manifest: true,
    sourcemap: false,
    chunkSizeWarningLimit: 800,
  },
}))
