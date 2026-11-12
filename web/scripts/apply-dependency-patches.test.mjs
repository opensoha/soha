import { createHash } from 'node:crypto'
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, expect, it, vi } from 'vitest'
import { applyDependencyPatches, dependencyPatchPlan as plan } from './apply-dependency-patches.mjs'

const source = fileURLToPath(new URL('../..', import.meta.url))
const directories = []
const sha256 = (content) => createHash('sha256').update(content).digest('hex')
function fixture() {
  const root = mkdtempSync(join(tmpdir(), 'soha-dependency-patches-'))
  directories.push(root)
  const put = (path, content) => {
    mkdirSync(dirname(join(root, path)), { recursive: true })
    writeFileSync(join(root, path), content)
  }
  put('package-lock.json', readFileSync(join(source, 'package-lock.json')))
  for (const pkg of plan.packages)
    put(`${pkg.path}/package.json`, readFileSync(join(source, pkg.path, 'package.json')))
  for (const file of plan.files) {
    let content = readFileSync(join(source, file.path), 'utf8')
    if (sha256(content) === file.after)
      for (const key of [...file.edits].reverse()) {
        const edit = plan.edits[key]
        // The removed sourceMappingURL was the original file suffix.
        content = edit.after ? content.replace(edit.after, edit.before) : content + edit.before
      }
    expect(sha256(content)).toBe(file.before)
    put(file.path, content)
  }
  return root
}
afterEach(() => {
  vi.unstubAllEnvs()
  for (const directory of directories.splice(0)) rmSync(directory, { recursive: true, force: true })
})

it('applies every pinned runtime/declaration edit and is idempotent', () => {
  const root = fixture()
  expect(applyDependencyPatches(root)).toEqual({ patched: 50, verified: 0, midscene: 'patched' })
  for (const file of plan.files)
    expect(sha256(readFileSync(join(root, file.path)))).toBe(file.after)
  expect(applyDependencyPatches(root)).toEqual({ patched: 0, verified: 50, midscene: 'patched' })
})

it.each(['unknown', 'missing'])('rejects a %s file before writing any other file', (mode) => {
  const root = fixture()
  const path = join(root, plan.files.at(-1).path)
  if (mode === 'unknown') writeFileSync(path, 'unexpected upstream content')
  else rmSync(path)
  expect(() => applyDependencyPatches(root)).toThrow()
  expect(sha256(readFileSync(join(root, plan.files[0].path)))).toBe(plan.files[0].before)
})

it('rejects package version or lock integrity drift', () => {
  const root = fixture()
  const path = join(root, plan.packages[0].path, 'package.json')
  const original = readFileSync(path, 'utf8')
  writeFileSync(path, original.replace('4.6.7', '4.6.8'))
  expect(() => applyDependencyPatches(root)).toThrow('upstream package changed')
  writeFileSync(path, original)
  const lockPath = join(root, 'package-lock.json')
  const lock = JSON.parse(readFileSync(lockPath, 'utf8'))
  lock.packages[plan.packages[0].path].integrity = 'sha512-unexpected'
  writeFileSync(lockPath, JSON.stringify(lock))
  expect(() => applyDependencyPatches(root)).toThrow('upstream package changed')
})

it('rejects a partial Midscene installation', () => {
  const root = fixture()
  rmSync(join(root, 'node_modules/@midscene/shared'), { recursive: true })
  expect(() => applyDependencyPatches(root)).toThrow('partial Midscene installation')
})

it('patches Zod and the selector engine when only Midscene packages are omitted', () => {
  const root = fixture()
  rmSync(join(root, 'node_modules/@midscene'), { recursive: true })
  expect(applyDependencyPatches(root)).toEqual({ patched: 3, verified: 0, midscene: 'omitted' })
  expect(applyDependencyPatches(root)).toEqual({ patched: 0, verified: 3, midscene: 'omitted' })
})

it('permits selector absence only for explicit omit-dev with jsdom also absent', () => {
  vi.stubEnv('NODE_ENV', 'test')
  vi.stubEnv('npm_config_omit', '')
  const root = fixture()
  rmSync(join(root, 'node_modules/@midscene'), { recursive: true })
  rmSync(join(root, 'node_modules/nwsapi'), { recursive: true })
  expect(() => applyDependencyPatches(root)).toThrow()
  vi.stubEnv('npm_config_omit', 'dev')
  expect(applyDependencyPatches(root)).toEqual({ patched: 2, verified: 0, midscene: 'omitted' })
  expect(applyDependencyPatches(root)).toEqual({ patched: 0, verified: 2, midscene: 'omitted' })
  mkdirSync(join(root, 'node_modules/jsdom'))
  expect(() => applyDependencyPatches(root)).toThrow()
})

it('rejects selector package identity drift before applying any patch', () => {
  const root = fixture()
  const pkg = plan.packages.find((pkg) => pkg.path === 'node_modules/nwsapi')
  const path = join(root, pkg.path, 'package.json')
  const original = readFileSync(path, 'utf8')
  writeFileSync(path, original.replace('2.2.28', '2.2.29'))
  expect(() => applyDependencyPatches(root)).toThrow('upstream package changed')
  writeFileSync(path, original)
  const lockPath = join(root, 'package-lock.json')
  const lock = JSON.parse(readFileSync(lockPath, 'utf8'))
  lock.packages[pkg.path].integrity = 'sha512-unexpected'
  writeFileSync(lockPath, JSON.stringify(lock))
  expect(() => applyDependencyPatches(root)).toThrow('upstream package changed')
  expect(sha256(readFileSync(join(root, plan.files[0].path)))).toBe(plan.files[0].before)
})
