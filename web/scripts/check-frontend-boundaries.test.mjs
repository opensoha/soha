import { spawnSync } from 'node:child_process'
import { copyFile, mkdir, mkdtemp, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

const projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceScript = join(projectRoot, 'scripts/check-frontend-boundaries.mjs')

let fixtureRoot
let fixtureScript

beforeEach(async () => {
  fixtureRoot = await mkdtemp(join(tmpdir(), 'soha-web-boundaries-'))
  fixtureScript = join(fixtureRoot, 'scripts/check-frontend-boundaries.mjs')

  await mkdir(dirname(fixtureScript), { recursive: true })
  await copyFile(sourceScript, fixtureScript)
  await symlink(join(projectRoot, 'node_modules'), join(fixtureRoot, 'node_modules'), 'dir')
  await writeBaseline('empty-baseline.json', {})
})

afterEach(async () => {
  await rm(fixtureRoot, { force: true, recursive: true })
})

describe('check-frontend-boundaries', () => {
  it('rejects new page api-client imports and bare query key arrays', async () => {
    await writeSource(
      'src/features/orders/orders-page.tsx',
      `import { api } from '@/services/api-client'

export function OrdersPage() {
  const options = { queryKey: ['orders'] }
  return api && options ? null : null
}
`,
    )

    const result = runCheck('--baseline', 'empty-baseline.json', '--enforce', '--json')
    const report = parseReport(result)

    expect(result.status).toBe(1)
    expect(report.summary.byRule).toMatchObject({
      'page-direct-api-client': 1,
      'bare-query-key': 1,
    })
    expect(report.baseline.newViolations).toBe(2)
    expect(report.newViolations.map(({ rule }) => rule).sort()).toEqual([
      'bare-query-key',
      'page-direct-api-client',
    ])
  })

  it('allows baseline debt but rejects extra occurrences with the same fingerprint', async () => {
    const pagePath = 'src/features/orders/orders-page.tsx'
    await writeSource(
      pagePath,
      `import { api } from '@/services/api-client'

export const first = { queryKey: ['orders'] }
export const OrdersPage = () => api && first
`,
    )

    const baselineResult = runCheck('--write-baseline', 'existing-baseline.json', '--json')
    expect(baselineResult.status).toBe(0)

    const unchangedResult = runCheck('--baseline', 'existing-baseline.json', '--enforce', '--json')
    expect(unchangedResult.status).toBe(0)
    expect(parseReport(unchangedResult).baseline.newViolations).toBe(0)

    await writeSource(
      pagePath,
      `import { api } from '@/services/api-client'

export const first = { queryKey: ['orders'] }
export const second = { queryKey: ['orders'] }
export const loadApiAgain = () => import('@/services/api-client')
export const OrdersPage = () => api && first && second
`,
    )

    const changedResult = runCheck('--baseline', 'existing-baseline.json', '--enforce', '--json')
    const changedReport = parseReport(changedResult)

    expect(changedResult.status).toBe(1)
    expect(changedReport.baseline).toMatchObject({
      existingViolations: 2,
      newViolations: 2,
      resolvedViolations: 0,
    })
    expect(changedReport.newViolations.map(({ rule }) => rule).sort()).toEqual([
      'bare-query-key',
      'page-direct-api-client',
    ])
  })

  it('detects supported bare query key forms outside key factories', async () => {
    await writeSource(
      'src/features/orders/orders-queries.ts',
      `export const propertyForm = { queryKey: ['property'] }

export function variableForm() {
  const queryKey = ['variable']
  return queryKey
}

export function assignmentForm() {
  let queryKey = propertyForm.queryKey
  queryKey = ['assignment']
  return queryKey
}
`,
    )
    await writeSource(
      'src/features/orders/orders-keys.ts',
      `export const ordersKeys = {
  all: { queryKey: ['orders'] },
  list: () => ['orders', 'list'] as const,
}

export const queryKey = ['orders', 'legacy']
`,
    )

    const result = runCheck('--baseline', 'empty-baseline.json', '--enforce', '--json')
    const report = parseReport(result)

    expect(result.status).toBe(1)
    expect(report.summary.byRule['bare-query-key']).toBe(3)
    expect(report.newViolations).toHaveLength(3)
    expect(report.newViolations.every(({ file }) => file.endsWith('orders-queries.ts'))).toBe(true)
  })
})

async function writeSource(path, source) {
  const destination = join(fixtureRoot, path)
  await mkdir(dirname(destination), { recursive: true })
  await writeFile(destination, source, 'utf8')
}

async function writeBaseline(path, counts) {
  await writeFile(
    join(fixtureRoot, path),
    `${JSON.stringify({ version: 1, counts }, null, 2)}\n`,
    'utf8',
  )
}

function runCheck(...args) {
  return spawnSync(process.execPath, [fixtureScript, ...args], {
    cwd: fixtureRoot,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  })
}

function parseReport(result) {
  expect(result.error).toBeUndefined()
  expect(result.stderr).toBe('')
  expect(result.stdout).not.toBe('')
  return JSON.parse(result.stdout)
}
