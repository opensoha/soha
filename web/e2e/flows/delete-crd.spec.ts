import { test, expect, catalogState } from '../fixtures/real-app'
import { validateCleanup } from '../../scripts/quality-guards.mjs'
import { execFileSync } from 'node:child_process'

test('A04/U10: replacement with the same name rejects an old UI confirmation', async ({
  page,
  real,
}) => {
  if (process.env.SOHA_E2E_MUTATIONS !== '1' || real.target.role !== 'test-writer')
    throw new Error('BLOCKED: dedicated mutation writer required')
  const { lease, runId } = real.target
  const read = async () => {
    const response = await real.api.get(real.catalogPath)
    expect(response.ok()).toBe(true)
    return (await response.json()).items
  }
  const item = (await read()).find((row: { name: string }) => row.name === lease.id)
  validateCleanup(lease, { runId, id: item?.name, uid: item?.uid }, runId)
  await page.goto(real.target.baseURL + '/extensions')
  await page.getByPlaceholder('搜索 API Group / CRD / Kind / Version').fill(lease.id)
  await page.getByRole('button', { name: '删除 CRD 定义', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('选择具体 CRD 定义').click()
  await page.getByTitle(`${item.name} · ${item.kind}`, { exact: true }).click()
  await dialog.getByLabel('输入完整 CRD 名称确认').fill(lease.id)
  const replacement = JSON.parse(
    execFileSync(
      'python3',
      [
        '../soha/scripts/quality-lab.py',
        'recreate',
        '--mode',
        real.target.mode === 'e2e-agent' ? 'agent' : 'direct',
        '--expected-run',
        runId,
        '--expected-id',
        lease.id,
        '--expected-uid',
        lease.uid,
      ],
      { encoding: 'utf8' },
    ),
  )
  expect(replacement.uid).not.toBe(lease.uid)
  const before = await read()
  const response = page.waitForResponse(
    (value) =>
      value.request().method() === 'DELETE' &&
      new URL(value.url()).pathname === `${real.catalogPath}/${lease.id}`,
  )
  await dialog.getByRole('button', { name: '删除定义及实例' }).click()
  expect((await response).status()).toBe(409)
  await expect(dialog.getByText('删除请求失败', { exact: true })).toBeVisible()
  await expect(dialog.getByLabel('输入完整 CRD 名称确认')).toHaveValue(lease.id)
  expect(catalogState(await read())).toEqual(catalogState(before))
})

test('E01/A04: UI deletes only the exact pre-provisioned test CRD', async ({ page, real }) => {
  if (process.env.SOHA_E2E_MUTATIONS !== '1' || real.target.role !== 'test-writer')
    throw new Error('BLOCKED: explicit mutation mode and dedicated writer required')
  const { lease, runId } = real.target
  const read = async () => {
    const response = await real.api.get(real.catalogPath)
    expect(response.ok()).toBe(true)
    return (await response.json()).items
  }
  const before = await read()
  const item = before.find((row: { name: string }) => row.name === lease?.id)
  validateCleanup(lease, { runId, id: item?.name, uid: item?.uid }, runId)
  await page.goto(real.target.baseURL + '/extensions')
  await page.getByPlaceholder('搜索 API Group / CRD / Kind / Version').fill(lease.id)
  await page.getByRole('button', { name: '删除 CRD 定义', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('选择具体 CRD 定义').click()
  await page.getByTitle(`${item.name} · ${item.kind}`, { exact: true }).click()
  await dialog.getByLabel('输入完整 CRD 名称确认').fill(lease.id)
  // A06: a bounded client transport failure cannot mutate Core; the same UI can retry.
  await page.route(
    `**${real.catalogPath}/${lease.id}?**`,
    (route) => route.abort('connectionfailed'),
    { times: 1 },
  )
  await dialog.getByRole('button', { name: '删除定义及实例' }).click()
  await expect(dialog.getByText('删除请求失败', { exact: true })).toBeVisible()
  await expect(dialog.getByLabel('输入完整 CRD 名称确认')).toHaveValue(lease.id)
  expect(catalogState(await read())).toEqual(catalogState(before))
  const responsePromise = page.waitForResponse(
    (response) =>
      response.request().method() === 'DELETE' &&
      new URL(response.url()).pathname === real.catalogPath + '/' + encodeURIComponent(lease.id),
  )
  await dialog.getByRole('button', { name: '删除定义及实例' }).click()
  expect((await responsePromise).ok()).toBe(true)
  await expect
    .poll(async () => (await read()).some((row: { uid: string }) => row.uid === lease.uid), {
      timeout: 30_000,
    })
    .toBe(false)
  const after = await read()
  expect(catalogState(after)).toEqual(
    catalogState(before.filter((row: { uid: string }) => row.uid !== lease.uid)),
  )
  // No broad cleanup on failure: this lease remains a precise remediation input.
})
