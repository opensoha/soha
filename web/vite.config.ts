import { defineConfig, configDefaults } from 'vitest/config'
import react from '@vitejs/plugin-react'
import path from 'path'
import { readFile, writeFile } from 'node:fs/promises'
import { gzipSync } from 'node:zlib'
import { normalizePath } from 'vite'
import type { GetManualChunk } from 'rollup'

const apiProxyTarget = process.env.VITE_API_PROXY_TARGET || 'http://127.0.0.1:8080'
const base = process.env.VITE_BASE_PATH || '/'
if (!/^\/(?:[A-Za-z0-9_-]+\/)*$/.test(base)) {
  throw new Error(
    'VITE_BASE_PATH must be a local path with leading/trailing slashes, such as /team/',
  )
}
const prefix = base.slice(0, -1)
const workbenchSessionEntry = normalizePath(
  path.resolve(__dirname, 'src/features/auth/workbench-session-start.ts'),
)
const applicationReadsEntry = normalizePath(
  path.resolve(__dirname, 'src/features/delivery/applications/entry-reads.tsx'),
)
const appShellEntry = normalizePath(path.resolve(__dirname, 'src/layouts/app-layout.tsx'))
const applicationWorkbenchEntry = normalizePath(
  path.resolve(__dirname, 'src/features/delivery/applications/workspace-page.tsx'),
)
const applicationDetailEntry = normalizePath(
  path.resolve(__dirname, 'src/features/delivery/applications/detail-page.tsx'),
)

function createWorkbenchDocument(index: string, initialAssets: string[]) {
  const main = /<script type="module" crossorigin src="([^"]+)"><\/script>/
  const preload = /<link rel="modulepreload" crossorigin href="([^"]+)">/g
  if (!main.test(index)) throw new Error('Could not find the built Web entry script')
  const dependencies = Array.from(index.matchAll(preload), ([, href]) => href).join(' ')
  return index
    .replace('<html ', '<html data-soha-workbench-entry ')
    .replace(main, '<meta name="soha-workbench-entry" content="$1">')
    .replace(preload, '')
    .replace(
      '</head>',
      initialAssets
        .map(
          (href) => `  <link rel="modulepreload" crossorigin fetchpriority="low" href="${href}">`,
        )
        .join('\n') + '\n  </head>',
    )
    .replace(
      '</head>',
      `  <meta name="soha-workbench-entry-dependencies" content="${dependencies}">\n  </head>`,
    )
}

function workbenchChunks(): GetManualChunk {
  const entries = [
    ['workbench-session', workbenchSessionEntry],
    ['soha-bootstrap', normalizePath(path.resolve(__dirname, 'src/main.tsx'))],
    ['application-entry', applicationReadsEntry],
    ['app-shell', appShellEntry],
    ['application-workbench', applicationWorkbenchEntry],
    ['application-detail', applicationDetailEntry],
  ]
  let chunks: Map<string, string> | undefined
  return (id, { getModuleInfo }) => {
    if (!chunks) {
      const assignments = new Map<string, string>()
      const assign = (moduleId: string, chunk: string) => {
        if (assignments.has(moduleId)) return
        // Vendor barrels re-export symbols that only later routes use. Preserve
        // Rollup's normal tree shaking and chunking for those dependencies.
        if (
          moduleId.includes('/node_modules/') &&
          !(chunk === 'workbench-session' && /\/node_modules\/(?:react|zustand)\//.test(moduleId))
        ) {
          return
        }
        const info = getModuleInfo(moduleId)
        if (!info || info.isExternal) return
        assignments.set(moduleId, chunk)
        for (const dependency of info.importedIds) assign(dependency, chunk)
      }
      // Keep the static dependencies of each startup step together. Dynamic route imports
      // continue to split along their existing boundaries.
      for (const [chunk, entry] of entries) assign(entry, chunk)
      for (const [, entry] of entries) {
        if (entry !== workbenchSessionEntry) assignments.delete(entry)
      }
      chunks = assignments
    }
    return chunks.get(id)
  }
}

export default defineConfig(() => ({
  base,
  plugins: [
    react(),
    {
      name: 'soha-workbench-session-entry',
      apply: 'build',
      transformIndexHtml: {
        order: 'post',
        handler(_, { bundle }) {
          const chunks = Object.values(bundle ?? {})
          const entry = chunks.find(
            (item) =>
              item.type === 'chunk' &&
              item.isEntry &&
              item.facadeModuleId === workbenchSessionEntry,
          )
          if (!entry) return []
          const routeModules = [
            applicationReadsEntry,
            appShellEntry,
            applicationWorkbenchEntry,
          ].flatMap((moduleId) => {
            const module = chunks.find(
              (item) => item.type === 'chunk' && item.facadeModuleId === moduleId,
            )
            return module ? [base + module.fileName] : []
          })
          const layoutModules = new Set<string>()
          const collectStaticModules = (fileName: string) => {
            const module = bundle?.[fileName]
            if (!module || module.type !== 'chunk' || layoutModules.has(fileName)) return
            layoutModules.add(fileName)
            for (const dependency of module.imports) collectStaticModules(dependency)
          }
          routeModules.forEach((href) => collectStaticModules(href.slice(base.length)))
          return [
            {
              tag: 'meta',
              attrs: {
                name: 'soha-workbench-layout-dependencies',
                content: Array.from(layoutModules, (fileName) => base + fileName).join(' '),
              },
              injectTo: 'head-prepend',
            },
            {
              tag: 'script',
              attrs: {
                type: 'module',
                async: true,
                crossorigin: '',
                src: base + entry.fileName,
              },
              injectTo: 'head-prepend',
            },
          ]
        },
      },
    },
    {
      name: 'soha-compressed-web-assets',
      apply: 'build',
      async writeBundle({ dir }, bundle) {
        if (!dir) return
        const index = await readFile(path.join(dir, 'index.html'), 'utf8')
        // Warm two substantial startup modules during session script download.
        // Leave the remaining HTTP/1.1 connections open for the refresh and CSS.
        const initialAssets = Array.from(
          index.matchAll(/<link rel="modulepreload" crossorigin href="([^"]+)">/g),
          ([, href]) => href,
        )
          .flatMap((href) => {
            const asset = bundle[href.slice(base.length)]
            return asset?.type === 'chunk' && asset.facadeModuleId !== workbenchSessionEntry
              ? [{ href, bytes: asset.code.length }]
              : []
          })
          .sort((a, b) => b.bytes - a.bytes)
          .slice(0, 2)
          .map(({ href }) => href)
        await writeFile(
          path.join(dir, 'application-workbench.html'),
          createWorkbenchDocument(index, initialAssets),
        )
        for (const fileName of Object.keys(bundle)) {
          if (!fileName.startsWith('assets/') || !/\.(js|css)$/.test(fileName)) continue
          const asset = path.join(dir, fileName)
          const source = await readFile(asset)
          if (source.length < 1024) continue
          const compressed = gzipSync(source)
          if (compressed.length < source.length) {
            await writeFile(asset + '.gz', compressed)
          }
        }
      },
    },
  ],
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
    rollupOptions: {
      input: [path.resolve(__dirname, 'index.html'), workbenchSessionEntry],
      output: { onlyExplicitManualChunks: true, manualChunks: workbenchChunks() },
    },
  },
}))
