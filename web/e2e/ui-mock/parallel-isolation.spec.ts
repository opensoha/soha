import { existsSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { test, expect, definitions } from '../fixtures/crd-app'
import { runContext } from '../../scripts/e2e-run-context.mjs'
import { verifyPage } from '../fixtures/mock-app'

test('parallel tasks isolate identical mock IDs, browser state and evidence', async ({
  page,
  catalog,
}, info) => {
  const run = runContext()
  await page.goto('/clusters')
  await verifyPage(page, '/clusters', ['prod-cluster'])
  const before = await page.evaluate(() => localStorage.getItem('parallel-task'))
  expect(before).toBeNull()
  const marker = run.dataNamespace ?? 'legacy'
  await page.evaluate((value) => localStorage.setItem('parallel-task', value), marker)
  expect(catalog.data).toHaveLength(18)
  catalog.failDelete = false
  const original = definitions[0]
  const response = await page.evaluate(async ({ name, uid }) => {
    const path = `/api/v1/clusters/cluster-a/extensions/crds/${name}?expectedUid=${uid}`
    return (await fetch(path, { method: 'DELETE' })).status
  }, original)
  expect(response).toBe(200)
  expect(catalog.data).toHaveLength(17)
  const proof = {
    taskId: run.taskId,
    executionId: run.executionId,
    marker,
    baseURL: run.baseURL,
    resourceId: original.name,
    remaining: catalog.data.length,
  }
  writeFileSync(join(run.root, 'parallel-ready.json'), JSON.stringify(proof))
  if (process.env.SOHA_E2E_PROBE_BARRIER)
    await expect
      .poll(() => existsSync(process.env.SOHA_E2E_PROBE_BARRIER!), { timeout: 30_000 })
      .toBe(true)
  expect(await page.evaluate(() => localStorage.getItem('parallel-task'))).toBe(marker)
  expect(catalog.data).toHaveLength(17)
  expect(catalog.deleteCalls).toHaveLength(1)
  await page.screenshot({ path: info.outputPath('parallel-candidate.png') })
  await info.attach('parallel-proof', {
    body: JSON.stringify(proof),
    contentType: 'application/json',
  })
})
