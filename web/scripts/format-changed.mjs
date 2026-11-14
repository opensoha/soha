import { existsSync, realpathSync, statSync } from 'node:fs'
import { execFileSync, spawnSync } from 'node:child_process'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'
import { createRequire } from 'node:module'

const supportedExtension = /\.(?:css|html|js|json|md|mjs|ts|tsx|ya?ml)$/i
const ignoredPrefixes = ['.codex-tmp/', '.tmp/', 'dist/', 'node_modules/']

const trackedAllowlist = new Set([
  '.github/workflows/ci.yml',
  'README.md',
  'eslint.config.js',
  'index.html',
  'package-lock.json',
  'package.json',
  'postcss.config.js',
  'scripts/analyze-route-baseline.mjs',
  'scripts/check-bundle-budget.mjs',
  'scripts/check-frontend-boundaries.mjs',
  'scripts/check-frontend-boundaries.test.mjs',
  'scripts/check-route-registry.mjs',
  'src/App.tsx',
  'src/components/app-error-boundary.tsx',
  'src/components/global-api-error-handler.tsx',
  'src/components/overview-visuals.tsx',
  'src/features/auth/routes.ts',
  'src/features/identity/index.ts',
  'src/main.tsx',
  'src/routes/definitions.ts',
  'src/routes/index.tsx',
  'src/routes/registry.ts',
  'src/routes/route-types.ts',
  'src/routes/route-validation.test.ts',
  'src/routes/router-baseline.test.tsx',
  'src/routes/router.tsx',
  'src/services/api-client.test.ts',
  'src/services/api-client.ts',
  'src/services/api-error.ts',
  'src/styles/globals.css',
  'src/types/scope.test.ts',
  'src/types/scope.ts',
  'tailwind.config.ts',
  'tsconfig.json',
  'vite.config.ts',
])
const trackedAllowedPrefixes = [
  'scripts/baselines/',
  'src/components/overview/',
  'src/features/copilot/global-assistant/',
  'src/features/identity/applications/',
  'src/features/identity/shared/',
]
export function changedFiles(root) {
  root = realpathSync(root)
  const gitRoot = execFileSync('git', ['rev-parse', '--show-toplevel'], {
    cwd: root,
    encoding: 'utf8',
  }).trim()
  const prefix = path.relative(gitRoot, root).split(path.sep).join('/')
  const sourcePrefix = prefix ? `${prefix}/` : ''
  const gitPaths = (args) =>
    execFileSync('git', args, { cwd: gitRoot, encoding: 'utf8' })
      .split('\0')
      .filter((file) => file && file.startsWith(sourcePrefix))
      .map((file) => file.slice(sourcePrefix.length))
  const tracked = gitPaths(['diff', '--name-only', '-z', 'HEAD', '--', prefix || '.']).filter(
    (file) =>
      trackedAllowlist.has(file) ||
      trackedAllowedPrefixes.some((allowed) => file.startsWith(allowed)),
  )
  const untracked = gitPaths([
    'ls-files',
    '--others',
    '--exclude-standard',
    '-z',
    '--',
    prefix || '.',
  ])
  return [...new Set([...tracked, ...untracked])]
    .filter((file) => supportedExtension.test(file))
    .filter((file) => !ignoredPrefixes.some((ignored) => file.startsWith(ignored)))
    .filter((file) => {
      const absolutePath = path.join(root, file)
      return existsSync(absolutePath) && statSync(absolutePath).isFile()
    })
    .sort()
}

function main() {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
  const mode = process.argv.includes('--write') ? '--write' : '--check'
  const files = changedFiles(root)
  if (files.length === 0) {
    console.log('No changed files require formatting.')
    return
  }
  console.log(`${mode === '--write' ? 'Formatting' : 'Checking'} ${files.length} changed files.`)
  // Resolve from this script so npm's hoisted workspace installation is supported.
  const require = createRequire(import.meta.url)
  const prettier = path.join(
    path.dirname(require.resolve('prettier/package.json')),
    'bin/prettier.cjs',
  )
  const result = spawnSync(process.execPath, [prettier, mode, ...files], {
    cwd: root,
    stdio: 'inherit',
  })
  process.exitCode = result.status ?? 1
}

if (
  process.argv[1] &&
  existsSync(process.argv[1]) &&
  realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)
)
  main()
