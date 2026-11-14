import { test, expect, catalogState } from '../fixtures/real-app'

test('A01/E02/E03: UI login and real scoped catalog read', async ({ page, real }) => {
  const catalog = await real.api.get(real.catalogPath)
  expect(catalog.ok()).toBe(true)
  const names = (await catalog.json()).items.map((item: { name: string }) => item.name)
  await page.goto(real.target.baseURL + '/extensions')
  await expect(page.locator('#root')).not.toBeEmpty()
  if (real.target.lease) expect(names).toContain(real.target.lease.id)
  if (real.target.role === 'readonly')
    await expect(page.getByRole('button', { name: '删除 CRD 定义', exact: true })).toHaveCount(0)
})

test('A01/U01: invalid identity and logged-out session are rejected', async ({
  playwright,
  real,
}) => {
  const invalid = await playwright.request.newContext({
    baseURL: real.target.baseURL,
    extraHTTPHeaders: { Authorization: 'Bearer invalid-quality-test-token' },
    maxRedirects: 0,
  })
  try {
    expect((await invalid.get(real.catalogPath)).status()).toBe(401)
  } finally {
    await invalid.dispose()
  }
  expect((await real.api.post('/api/v1/auth/logout')).ok()).toBe(true)
  expect((await real.api.get(real.catalogPath)).status()).toBe(401)
})

test('A02/A04: rejected deletion has no side effect', async ({ real }) => {
  const { lease, runId } = real.target
  if (!lease?.id || !lease?.uid || lease.runId !== runId)
    throw new Error('BLOCKED: exact test-owned lease required')
  const read = async () => {
    const response = await real.api.get(real.catalogPath)
    expect(response.ok()).toBe(true)
    return (await response.json()).items
  }
  const before = await read()
  expect(before.find((item: { name: string }) => item.name === lease.id)?.uid).toBe(lease.uid)
  const response = await real.api.delete(
    `${real.catalogPath}/${encodeURIComponent(lease.id)}?expectedUid=${encodeURIComponent(lease.uid + '-wrong')}`,
  )
  expect(response.status()).toBe(real.target.role === 'readonly' ? 403 : 409)
  expect(catalogState(await read())).toEqual(catalogState(before))
})

test('A03: denied cluster scope is rejected without disclosing catalog data', async ({ real }) => {
  const denied = real.target.deniedClusterId
  if (!denied || denied === real.target.clusterId)
    throw new Error('BLOCKED: provider-attested denied cluster scope required')
  const response = await real.api.get(
    `/api/v1/clusters/${encodeURIComponent(denied)}/extensions/crds`,
  )
  expect(response.status()).toBe(403)
  const body = await response.json()
  expect(Array.isArray(body.data)).toBe(false)
})
