import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import type { Page } from '@playwright/test'
import { test, expect } from '../fixtures/real-app'

const controlPath = fileURLToPath(
  new URL(
    '../../../docs/tasks/114-enterprise-delivery-workbench/evidence/m12/control.py',
    import.meta.url,
  ),
)
function control(action: string, mode: string, ...args: string[]) {
  return JSON.parse(
    execFileSync('python3', [controlPath, action, mode.replace('e2e-', ''), ...args], {
      encoding: 'utf8',
    }),
  )
}

test('Task114 real application resources and explicit CR source access', async ({ page, real }) => {
  const { target } = real
  const env = target.fixtures.environments.test
  await page.goto(
    `${target.baseURL}/applications/${target.fixtures.application.id}?tab=services&applicationEnvironmentId=${env.imported.applicationEnvironmentId}&serviceId=${env.nativeServiceId}`,
  )
  await expect(page.getByRole('navigation', { name: '选择环境', exact: true })).toBeVisible()
  await expect(
    page.locator('.soha-pod-card-list > li').filter({ hasText: 'native-api-' }).first(),
  ).toBeVisible()
  await expect(page.getByRole('tab', { name: '指标', exact: true })).toBeVisible()
  await page.goto(
    `${target.baseURL}/applications/${target.fixtures.application.id}?tab=services&applicationEnvironmentId=${env.imported.applicationEnvironmentId}&serviceId=${env.rootServiceId}`,
  )
  const relations = page.getByRole('region', { name: '运行资源关系', exact: true })
  await expect(relations).toContainText('Widget')
  await expect(relations).toContainText('operator-api')
  await expect(relations).toContainText('共享引用')
  const source = page.getByRole('button', { name: '查看源 YAML', exact: true })
  if (target.role === 'test-writer') {
    await source.click()
    await expect(page.getByLabel('源配置 YAML')).toContainText('TASK114_SYNTHETIC_ROOT_CANARY')
    await page
      .getByRole('navigation', { name: '选择环境', exact: true })
      .getByRole('button', {
        name: new RegExp(target.fixtures.environments.dev.imported.environment.name),
      })
      .click()
    await expect(page.getByLabel('源配置 YAML')).toHaveCount(0)
    expect(await page.evaluate(() => Object.values(localStorage).join(''))).not.toContain(
      'TASK114_SYNTHETIC_ROOT_CANARY',
    )
  } else await expect(source).toHaveCount(0)
  await page.getByRole('button', { name: '切换应用', exact: true }).click()
  await expect(
    page
      .getByRole('region', { name: '应用切换面板', exact: true })
      .getByRole('button', { name: new RegExp(target.fixtures.otherApplication.name) }),
  ).toBeVisible()
})

test('Task114 real existing workload import wizard preserves preview identity', async ({
  page,
  real,
}) => {
  const { target, api } = real
  expect(process.env.SOHA_E2E_MUTATIONS).toBe('1')
  expect(target.role).toBe('test-writer')
  await page.goto(target.baseURL + '/applications')
  await page.getByRole('button', { name: '接入已有资源', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '导入集群服务' })
  await dialog.getByLabel('集群', { exact: true }).click()
  await page
    .locator('.ant-select-item-option')
    .filter({ hasText: 'Quality ' + target.mode.replace('e2e-', '') })
    .click()
  await dialog.getByLabel('命名空间', { exact: true }).click()
  await page.locator('.ant-select-item-option').filter({ hasText: target.namespace }).click()
  await dialog.getByRole('button', { name: '下一步', exact: true }).click()
  await dialog.getByRole('row').filter({ hasText: 'ui-import' }).getByRole('checkbox').check()
  await dialog.getByRole('button', { name: '下一步', exact: true }).click()
  await expect(dialog.getByLabel('源身份预览')).toContainText('ui-import')
  await expect(dialog.getByRole('radio', { name: '托管发布', exact: true })).toBeDisabled()
  await dialog.getByLabel('应用', { exact: true }).click()
  await page
    .locator('.ant-select-item-option')
    .filter({ hasText: `${target.fixtures.application.name} (${target.fixtures.application.key})` })
    .click()
  await dialog.getByLabel('环境', { exact: true }).fill('test')
  await dialog.getByLabel('环境', { exact: true }).press('Enter')
  const imported = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/api/v1/application-environments/imports' &&
      response.request().method() === 'POST',
  )
  await dialog.getByRole('button', { name: '导入 1 个服务', exact: true }).click()
  const response = await imported
  expect(response.status()).toBe(200)
  const input = response.request().postDataJSON()
  expect(input.ownershipMode).toBe('observe_only')
  expect(input.workloads).toHaveLength(1)
  const workload = input.workloads[0]
  expect(workload.expectedUid).toBeTruthy()
  expect(workload.expectedSpecDigest).toMatch(/^[a-f0-9]{64}$/)
  const candidates = await api.get(
    '/api/v1/application-environments/target-candidates?' +
      new URLSearchParams({
        clusterId: target.clusterId,
        namespace: target.namespace,
        workloadKind: 'Deployment',
      }),
  )
  expect(candidates.status()).toBe(200)
  const current = (await candidates.json()).items.find(
    (item: { workloadName: string }) => item.workloadName === 'ui-import',
  )
  expect(current.source.resource.uid).toBe(workload.expectedUid)
  expect(current.source.specDigest).toBe(workload.expectedSpecDigest)
})

for (const kind of ['logs', 'terminal'] as const) {
  test(`Task114 real ${kind} persistent lifecycle and replacement UID`, async ({
    page,
    context,
    real,
  }) => {
    test.setTimeout(150_000)
    const { target, api } = real
    expect(process.env.SOHA_E2E_MUTATIONS).toBe('1')
    if (target.role !== 'test-writer')
      throw new Error('Writer-only scenario must be selected by runner')
    const env = target.fixtures.environments.test
    const url = `${target.baseURL}/applications/${target.fixtures.application.id}?tab=services&applicationEnvironmentId=${env.imported.applicationEnvironmentId}&serviceId=${env.nativeServiceId}`
    const sockets: Array<{ closed: boolean; url: string; output: string[] }> = []
    const attach = (current: Page) =>
      current.on('websocket', (socket) => {
        if (!/\/workloads\/pods\/[^/]+\/(logs\/stream|terminal)/.test(socket.url())) return
        const entry = { closed: false, url: socket.url(), output: [] as string[] }
        sockets.push(entry)
        socket.on('close', () => {
          entry.closed = true
        })
        socket.on('framereceived', (frame) => {
          entry.output.push(frame.payload.toString())
        })
      })
    attach(page)
    context.on('page', attach)
    await page.goto(url)
    const card = page.locator('.soha-pod-card-list > li').filter({ hasText: 'native-api-' }).first()
    await expect(card).toBeVisible()
    await card.getByRole('button', { name: kind === 'logs' ? '日志' : '终端', exact: true }).click()
    await expect.poll(() => sockets.length).toBe(1)
    await expect.poll(() => sockets[0].output.length).toBeGreaterThan(0)
    const socketURL = new URL(sockets[0].url)
    const oldUID = socketURL.searchParams.get('expectedUid')!
    expect(oldUID).toBeTruthy()
    const podName = decodeURIComponent(socketURL.pathname.split('/pods/')[1].split('/')[0])
    await expect
      .poll(() => control('streams', target.mode)[kind === 'terminal' ? 'exec' : 'logs'])
      .toBeGreaterThan(0)
    await page.getByRole('button', { name: '收起实时会话', exact: true }).click()
    await expect
      .poll(() => control('streams', target.mode)[kind === 'terminal' ? 'exec' : 'logs'])
      .toBeGreaterThan(0)
    expect(sockets[0].closed).toBe(false)
    await page.getByRole('button', { name: /实时会话 \(1\)/ }).click()
    expect(sockets).toHaveLength(1)
    await page
      .getByRole('navigation', { name: '选择环境', exact: true })
      .getByRole('button', {
        name: new RegExp(target.fixtures.environments.dev.imported.environment.name),
      })
      .click()
    await expect.poll(() => sockets[0].closed).toBe(true)
    await expect
      .poll(() => control('streams', target.mode)[kind === 'terminal' ? 'exec' : 'logs'], {
        timeout: 20_000,
        intervals: [1000, 2000],
      })
      .toBe(0)
    await page
      .getByRole('navigation', { name: '选择环境', exact: true })
      .getByRole('button', { name: new RegExp(env.imported.environment.name) })
      .click()
    await page.getByRole('button', { name: /实时会话 \(1\)/ }).click()
    await expect(page.getByRole('button', { name: '重新连接', exact: true })).toBeVisible()
    expect(sockets).toHaveLength(1)
    await page.getByRole('button', { name: '重新连接', exact: true }).click()
    await expect.poll(() => sockets.length).toBe(2)
    await expect.poll(() => sockets[1].output.length).toBeGreaterThan(0)
    await page.getByRole('button', { name: '切换应用', exact: true }).click()
    await page
      .getByRole('region', { name: '应用切换面板', exact: true })
      .getByRole('button', { name: new RegExp(target.fixtures.otherApplication.name) })
      .click()
    await expect.poll(() => sockets[1].closed).toBe(true)
    await page.goto(url)
    await page.getByRole('button', { name: /实时会话 \(1\)/ }).click()
    expect(sockets).toHaveLength(2)
    await page.getByRole('button', { name: '重新连接', exact: true }).click()
    await expect.poll(() => sockets.length).toBe(3)
    await expect.poll(() => sockets[2].output.length).toBeGreaterThan(0)
    await page.close()
    // Closing a page destroys Playwright's socket event source; prove upstream release independently.
    await expect
      .poll(() => control('streams', target.mode)[kind === 'terminal' ? 'exec' : 'logs'], {
        timeout: 20_000,
        intervals: [1000, 2000],
      })
      .toBe(0)
    const reopened = await context.newPage()
    await reopened.goto(url)
    await reopened.getByRole('button', { name: /实时会话 \(1\)/ }).click()
    await expect(reopened.getByRole('button', { name: '重新连接', exact: true })).toBeVisible()
    expect(sockets).toHaveLength(3)
    const replacement = control('replace-pod', target.mode, podName, oldUID)
    expect(replacement.newUid).not.toBe(oldUID)
    const stale = await api.get(
      `/api/v1/clusters/${target.clusterId}/workloads/pods/${podName}/logs?namespace=${env.namespace}&container=main&expectedUid=${oldUID}`,
    )
    expect(stale.status()).toBe(409)
    await reopened.getByRole('button', { name: '重新连接', exact: true }).click()
    await expect(reopened.locator('.soha-realtime-session-dock')).toContainText('Pod 已被替换')
    expect(sockets).toHaveLength(3)
    const storage = await reopened.evaluate(() => Object.values(localStorage).join(''))
    expect(storage).toContain(oldUID)
    expect(storage).not.toContain(replacement.newUid)
    expect(storage).not.toMatch(/task114-main|task114-sidecar|TASK114_SYNTHETIC_ROOT_CANARY/)
    await reopened.close()
  })
}

test('Task114 real template UI history and wave adoption; API automatic policy and approval', async ({
  page,
  real,
}) => {
  test.setTimeout(180_000)
  const { target, api } = real
  expect(process.env.SOHA_E2E_MUTATIONS).toBe('1')
  if (target.role !== 'test-writer')
    throw new Error('Writer-only scenario must be selected by runner')
  const template = target.templates.template
  const listPath =
    '/api/v1/delivery/template-updates/targets?' +
    new URLSearchParams({ kind: 'deployment', templateId: template.id, pageSize: '100' })
  const list = async () => {
    const response = await api.get(listPath)
    expect(response.status()).toBe(200)
    return (await response.json()).data.items as Array<{
      id: string
      serviceName: string
      fromVersion: number
      eligible: boolean
      independentOverrides: string[]
    }>
  }
  const targets = await list()
  expect(targets).toHaveLength(10)
  expect(
    targets
      .filter((item) => item.serviceName === 'Template detached')
      .every((item) => !item.eligible),
  ).toBe(true)
  const manual = targets.filter((item) => item.serviceName === 'Template manual')
  expect(manual).toHaveLength(2)
  expect(manual.every((item) => item.independentOverrides.includes('environment.message'))).toBe(
    true,
  )
  await page.goto(target.baseURL + '/deployment-templates')
  await page
    .getByRole('row')
    .filter({ hasText: template.key })
    .getByRole('button', { name: '检查模板更新', exact: true })
    .click()
  const drawer = page.getByRole('dialog', { name: '模板采用 · ' + template.id, exact: true })
  const rows = drawer.getByRole('row').filter({ hasText: 'Template manual' })
  await expect(rows).toHaveCount(2)
  for (let index = 0; index < 2; index++)
    await rows.nth(index).locator('input[type=checkbox]').check()
  const previewRead = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/api/v1/delivery/template-updates/previews' &&
      response.request().method() === 'POST',
  )
  await drawer.getByRole('button', { name: '比较所选 2 项', exact: true }).click()
  const previewResponse = await previewRead
  expect(previewResponse.status()).toBe(200)
  await expect(drawer.getByRole('tab', { name: '累计差异 v1 → v4', exact: true })).toBeVisible()
  await drawer.getByRole('tab', { name: '逐版本差异（3 段）', exact: true }).click()
  await expect(drawer.getByRole('combobox', { name: '版本差异段', exact: true })).toBeVisible()
  const preview = (await previewResponse.json()).data
  expect(preview.configurationOnly).toBe(true)
  for (const candidate of preview.candidates) {
    expect(candidate.eligible).toBe(true)
    expect(
      candidate.versions.map((item: { fromVersion: number; toVersion: number }) => [
        item.fromVersion,
        item.toVersion,
      ]),
    ).toEqual([
      [1, 2],
      [2, 3],
      [3, 4],
    ])
    expect(candidate.cumulative.changes.length).toBeGreaterThan(0)
    expect(candidate.cumulative.renderStatus).toBe('available')
    expect(candidate.parameters.message).toBe('service-override')
  }
  await drawer.getByRole('spinbutton', { name: '每批采用数量', exact: true }).fill('1')
  const batchRead = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname ===
        `/api/v1/delivery/template-updates/previews/${preview.id}/batches` &&
      response.request().method() === 'POST',
  )
  await drawer.getByRole('button', { name: '分批采用 2 项配置', exact: true }).click()
  const batchResponse = await batchRead
  expect(batchResponse.status()).toBe(200)
  const batch = (await batchResponse.json()).data
  await expect
    .poll(
      async () =>
        (await (await api.get('/api/v1/delivery-batches/' + batch.id)).json()).data.status,
      { timeout: 60_000, intervals: [1000, 2000] },
    )
    .toBe('completed')
  const completed = (await (await api.get('/api/v1/delivery-batches/' + batch.id)).json()).data
  expect(completed.nodes.every((node: { executionTaskId?: string }) => !node.executionTaskId)).toBe(
    true,
  )
  for (const policy of ['automatic', 'approval']) {
    const candidate = targets.find((item) => item.serviceName === 'Template ' + policy)!
    const saved = await api.put('/api/v1/delivery/template-updates/policies', {
      data: {
        kind: 'deployment',
        templateId: template.id,
        targetId: candidate.id,
        mode: policy,
        paused: false,
        expectedVersion: 0,
      },
    })
    expect(saved.status()).toBe(200)
  }
  const policiesPath =
    '/api/v1/delivery/template-updates/policies?' +
    new URLSearchParams({ kind: 'deployment', templateId: template.id })
  const listPolicies = async () => {
    const response = await api.get(policiesPath)
    expect(response.status()).toBe(200)
    return (await response.json()).items
  }
  await expect
    .poll(
      async () =>
        (await listPolicies()).filter((item: { lastBatchId?: string }) => item.lastBatchId).length,
      { timeout: 60_000, intervals: [2000, 3000] },
    )
    .toBe(2)
  const policies = await listPolicies()
  const approval = policies.find((item: { mode: string }) => item.mode === 'approval')
  await expect
    .poll(
      async () =>
        (
          await (await api.get('/api/v1/delivery-batches/' + approval.lastBatchId)).json()
        ).data.nodes.some((node: { status: string }) => node.status === 'waiting_approval'),
      { timeout: 30_000, intervals: [1000, 2000] },
    )
    .toBe(true)
  const pending = (await (await api.get('/api/v1/delivery-batches/' + approval.lastBatchId)).json())
    .data
  const plan = pending.nodes.find(
    (node: { deliveryPlanId?: string }) => node.deliveryPlanId,
  ).deliveryPlanId
  expect((await list()).find((item) => item.id === approval.targetId)?.fromVersion).toBe(1)
  const approved = await api.post('/api/v1/delivery/plans/' + plan + '/approval', {
    data: { action: 'approve', comment: 'Task114 isolated acceptance' },
  })
  expect(approved.status()).toBe(200)
  for (const policy of policies)
    await expect
      .poll(
        async () =>
          (await (await api.get('/api/v1/delivery-batches/' + policy.lastBatchId)).json()).data
            .status,
        { timeout: 60_000, intervals: [1000, 2000] },
      )
      .toBe('completed')
  const updated = await list()
  expect(
    updated
      .filter((item) => item.serviceName === 'Template manual')
      .every((item) => item.fromVersion === 4),
  ).toBe(true)
  expect(
    updated
      .filter((item) => ['Template fixed', 'Template detached'].includes(item.serviceName))
      .every((item) => item.fromVersion === 1),
  ).toBe(true)
  for (const entry of Object.values(target.templates.services) as Array<{
    package: { id: string }
  }>) {
    const packageResponse = await api.get('/api/v1/delivery/manifest-packages/' + entry.package.id)
    expect(packageResponse.status()).toBe(200)
    const saved = (await packageResponse.json()).data
    expect(
      saved.bindings.every((binding: { templateParameters: { message: string } }) =>
        binding.templateParameters.message.startsWith('environment-'),
      ),
    ).toBe(true)
  }
})
