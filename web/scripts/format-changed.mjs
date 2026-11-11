import { existsSync, statSync } from 'node:fs'
import { execFileSync, spawnSync } from 'node:child_process'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const mode = process.argv.includes('--write') ? '--write' : '--check'
const supportedExtension = /\.(?:css|html|js|json|md|mjs|ts|tsx|ya?ml)$/i
const ignoredPrefixes = ['.codex-tmp/', '.tmp/', 'dist/', 'node_modules/']

function gitPaths(args) {
  const output = execFileSync('git', args, { cwd: root, encoding: 'buffer' })
  return output.toString('utf8').split('\0').filter(Boolean)
}

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
const tracked = gitPaths(['diff', '--name-only', '-z', 'HEAD']).filter(
  (file) =>
    trackedAllowlist.has(file) || trackedAllowedPrefixes.some((prefix) => file.startsWith(prefix)),
)
const untracked = gitPaths(['ls-files', '--others', '--exclude-standard', '-z'])
const candidates = new Set([...tracked, ...untracked])

const files = [...candidates]
  .filter((file) => supportedExtension.test(file))
  .filter((file) => !ignoredPrefixes.some((prefix) => file.startsWith(prefix)))
  .filter((file) => {
    const absolutePath = path.join(root, file)
    return existsSync(absolutePath) && statSync(absolutePath).isFile()
  })
  .sort()

if (files.length === 0) {
  console.log('No changed files require formatting.')
  process.exit(0)
}

console.log(`${mode === '--write' ? 'Formatting' : 'Checking'} ${files.length} changed files.`)
const prettier = path.join(root, 'node_modules', '.bin', 'prettier')
const result = spawnSync(prettier, [mode, ...files], { cwd: root, stdio: 'inherit' })
process.exit(result.status ?? 1)
