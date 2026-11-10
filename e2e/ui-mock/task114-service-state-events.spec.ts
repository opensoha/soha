import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

const target = '/applications/retail?applicationEnvironmentId=retail-test&serviceId=order-api'
const digest = '94188161c9ea4c4beb911c9831f510859930137e681727d0b7d84df35b7225ff'
const uid = '23018505-c915-43a2-8c54-fdbc93dd0a39'
const longNamespace = 'delivery-integration-diagnostics-namespace-with-a-long-name'
const binding = {
  id: 'retail-test',
  applicationId: 'retail',
  environmentId: 'test',
  clusterId: 'cluster-a',
  namespace: 'retail-test',
  targets: [
    {
      id: 'target-order-api',
      clusterId: 'cluster-a',
      namespace: 'retail-test',
      workloadKind: 'Deployment',
      workloadName: 'order-api',
      enabled: true,
      executorKind: 'manifest_ssa',
      configRef: 'binding-manifest',
      metadata: { serviceId: 'order-api' },
    },
  ],
}

async function setup(
  context: BrowserContext,
  page: Page,
  canPublish = false,
  canWorkflow = false,
  podState: 'normal' | 'empty' | 'failed' | 'forbidden' = 'normal',
) {
  let eventReads = 0
  let eventStatus = 200
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname.replace(/^\/api\/v1/, '')
    const data = (value: unknown) => ({ body: { data: value } })
    if (path === '/access/permission-snapshot' && podState === 'forbidden') {
      const response = task114Response(url, method)!
      const body = structuredClone(response.body) as { data: { permissionKeys: string[] } }
      body.data.permissionKeys = body.data.permissionKeys.filter(
        (key) => key !== 'platform.pods.view',
      )
      return { ...response, body }
    }
    if (path === '/access/permission-snapshot' && canPublish) {
      const response = task114Response(url, method)!
      const body = structuredClone(response.body) as { data: { permissionKeys: string[] } }
      body.data.permissionKeys.push('delivery.releases.trigger')
      if (canWorkflow) body.data.permissionKeys.push('delivery.workflows.view')
      return { ...response, body }
    }
    if (path.endsWith('/workloads/order-api/runtime') && podState === 'failed')
      return {
        status: 503,
        body: { error: { code: 'runtime_unavailable', message: 'Pod 读取失败' } },
      }
    if (path === '/application-environments') return data([binding])
    if (path === '/clusters/cluster-a/events') {
      eventReads++
      if (eventStatus !== 200)
        return {
          status: eventStatus,
          body: { error: { code: 'event_read_failed', message: '事件暂时无法读取' } },
        }
      return data([
        {
          name: 'older',
          namespace: 'retail-test',
          type: 'Normal',
          reason: 'Started',
          involvedKind: 'Pod',
          involvedName: 'order-api-pod-0',
          message: 'Started container main',
          count: 1,
          ageSeconds: 3600,
        },
        {
          name: 'readiness',
          namespace: 'retail-test',
          type: 'Warning',
          reason: 'Unhealthy',
          involvedKind: 'Pod',
          involvedName: 'order-api-pod-0',
          message: 'Readiness probe failed: dial tcp 10.217.0.9:5432: i/o timeout',
          count: 2,
          ageSeconds: 1080,
        },
      ])
    }
    if (/\/workloads\/pods\/order-api-pod-\d+\/detail$/.test(path))
      return data({
        name: path.split('/').at(-2),
        namespace: 'retail-test',
        uid: `uid-${path.split('/').at(-2)}`,
        phase: 'Running',
        containers: [],
      })
    const response = task114Response(url, method)
    if (response && path === '/applications/retail/runtime') {
      const body = structuredClone(response.body) as {
        data: {
          environments: Array<{ applicationEnvironmentId: string; manifestDeployments?: unknown[] }>
        }
      }
      const env = body.data.environments.find(
        (item) => item.applicationEnvironmentId === 'retail-test',
      )!
      env.manifestDeployments = [
        {
          id: 'manifest-deployment',
          packageId: 'manifest-package',
          bindingId: 'binding-manifest',
          generation: 1,
          spec: { desiredRevision: 1, desiredDigest: digest },
          status: {
            phase: 'converged',
            observedGeneration: 1,
            appliedRevision: 1,
            appliedDigest: digest,
            lastExecutionTaskId: 'observe-task',
            conditions: [{ type: 'Healthy', status: 'true', observedGeneration: 1 }],
            inventory: [
              {
                apiVersion: 'v1',
                kind: 'PersistentVolumeClaim',
                name: 'voting-db',
                namespace: longNamespace,
                health: 'healthy',
                uid,
              },
            ],
          },
        },
      ]
      return { ...response, body }
    }
    if (response && path.endsWith('/runtime')) {
      const body = structuredClone(response.body) as {
        data: { pods?: Array<{ name: string; uid?: string }> }
      }
      if (body.data.pods)
        body.data.pods =
          podState === 'empty'
            ? []
            : body.data.pods.map((pod) => ({ ...pod, uid: `uid-${pod.name}` }))
      return { ...response, body }
    }
    return response
  })
  return {
    evidence,
    reads: () => eventReads,
    failEvents: (status: number) => {
      eventStatus = status
    },
  }
}

for (const [width, theme] of [
  [1440, 'light'],
  [1024, 'light'],
  [768, 'dark'],
  [320, 'dark'],
] as const) {
  test(`Task114 managed state preserves full identifiers and Pod event context at ${width}px`, async ({
    context,
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 })
    await context.addInitScript(
      (themeMode) =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode, localeCode: 'zh_CN' }, version: 0 }),
        ),
      theme,
    )
    const { evidence, reads, failEvents } = await setup(context, page)
    await page.goto(`${target}&serviceTab=events`)
    const state = page.getByRole('region', { name: '状态', exact: true })
    await expect(state).toContainText('资源已就绪')
    await expect(state).toContainText('voting-db')
    const stateBox = (await state.boundingBox())!
    expect(stateBox.x).toBeGreaterThanOrEqual(0)
    expect(stateBox.x + stateBox.width).toBeLessThanOrEqual(width)
    await expect(state.getByRole('link', { name: /查看最近执行任务/ })).toHaveAttribute(
      'href',
      '/delivery/execution-tasks/observe-task',
    )
    const summary = state.locator('.manifest-deployment-state__digest > summary')
    await summary.focus()
    await page.keyboard.press('Enter')
    await expect(state.getByText(digest, { exact: true })).toBeVisible()
    const uidSummary = state.locator('summary').filter({ hasText: 'UID' })
    await uidSummary.focus()
    await page.keyboard.press('Enter')
    await expect(state.getByText(uid, { exact: true })).toBeVisible()
    const uidWidth = (await state.getByText(uid, { exact: true }).boundingBox())!.width
    const uidCellWidth = (await state.locator('td').filter({ hasText: uid }).boundingBox())!.width
    expect(uidWidth).toBeLessThanOrEqual(uidCellWidth)
    const namespaceWidth = (await state.getByText(longNamespace, { exact: true }).boundingBox())!
      .width
    const namespaceCellWidth = (await state
      .locator('td')
      .filter({ hasText: longNamespace })
      .boundingBox())!.width
    expect(namespaceWidth).toBeLessThanOrEqual(namespaceCellWidth)
    expect((await state.boundingBox())!.x).toBeGreaterThanOrEqual(0)
    expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false)
    await page.screenshot({
      path: info.outputPath(`state-${width}-${theme}.png`),
      fullPage: true,
      animations: 'disabled',
    })
    await expect(state).not.toContainText('Pod 重启记录与历史事件在实例内查看。')
    await expect(
      state.getByRole('button', { name: '查看 Pod 重启与事件', exact: true }),
    ).toHaveCount(0)
    if (width === 320) {
      await page.locator('.soha-service-detail .ant-tabs-nav-more').hover()
      await page.getByRole('option', { name: /^Pods/ }).click()
    } else {
      await page.getByRole('tab', { name: /^Pods/ }).click()
    }
    const pod = page.locator('.soha-pod-card').filter({ hasText: 'order-api-pod-0' })
    await pod.getByRole('button', { name: '事件', exact: true }).click()
    const drawer = page.locator('.ant-drawer-open')
    const records = drawer.locator('.soha-pod-detail-timeline-item')
    await expect(records).toHaveCount(2)
    await expect(records.first()).toContainText('Unhealthy')
    await drawer.getByRole('button', { name: '事件范围说明', exact: true }).focus()
    await expect(page.getByRole('tooltip')).toContainText('历史告警不代表当前健康状态')
    await page.keyboard.press('Tab')
    await expect(records.locator('time[datetime]')).toHaveCount(2)
    const eventToolbar = drawer.locator('.soha-pod-detail-event-toolbar')
    await eventToolbar.getByText('Warning', { exact: true }).click()
    await expect(records).toHaveCount(1)
    await expect(records.first()).toContainText('Unhealthy')
    await eventToolbar.getByText('全部', { exact: true }).click()
    await eventToolbar.getByRole('textbox', { name: '搜索事件', exact: true }).fill('Started')
    await expect(records).toHaveCount(1)
    await expect(records.first()).toContainText('Started')
    await eventToolbar.getByRole('textbox', { name: '搜索事件', exact: true }).fill('')
    await expect(records).toHaveCount(2)
    await expect(drawer.locator('.soha-pod-drawer-context')).toContainText('retail-test')
    const before = reads()
    await drawer.getByRole('button', { name: '刷新 Pod 事件', exact: true }).click()
    await expect.poll(reads).toBeGreaterThan(before)
    await expect(records).toHaveCount(2)
    await page.screenshot({
      path: info.outputPath(`events-${width}-${theme}.png`),
      fullPage: true,
      animations: 'disabled',
    })
    failEvents(503)
    await drawer.getByRole('button', { name: '刷新 Pod 事件', exact: true }).click()
    await expect(drawer).toContainText('Pod 事件未更新，正在展示上次读取结果')
    await expect(records).toHaveCount(2)
    failEvents(200)
    await drawer.getByRole('button', { name: '重试读取Pod 事件', exact: true }).click()
    await expect(drawer).not.toContainText('未更新，正在展示上次读取结果')
    await expect(records).toHaveCount(2)
    failEvents(403)
    await drawer.getByRole('button', { name: '刷新 Pod 事件', exact: true }).click()
    await expect(drawer).toContainText('Pod 事件无访问权限')
    await expect(records).toHaveCount(0)
    await drawer.locator('.ant-drawer-close').click()
    await expect(drawer).toHaveCount(0)
    await expect(page).not.toHaveURL(/tool=events/)
    verifyBrowserEvidence(evidence)
  })
}

for (const width of [1200, 390]) {
  test(`R12 Pods is the service landing and explicit runtime keeps resource relations at ${width}px`, async ({
    context,
    page,
  }, info) => {
    await page.setViewportSize({ width, height: 1000 })
    const { evidence } = await setup(context, page)
    await page.goto(target)
    await expect(page.getByRole('tab', { name: /^Pods/ })).toHaveAttribute('aria-selected', 'true')
    await expect(page.locator('.soha-pod-card')).toHaveCount(2)
    await expect(page.getByRole('region', { name: '服务运行指标', exact: true })).toHaveCount(0)
    const metadata = page.locator('.soha-service-location')
    await expect(metadata).toContainText('就绪 2/2')
    await expect(metadata).toContainText('累计重启 1')
    if (width === 1200) {
      const bounds = await metadata.boundingBox()
      expect(bounds!.height).toBeLessThanOrEqual(24)
      const tags = metadata.locator('.ant-tag')
      await expect(tags).toHaveCount(3)
      await expect(metadata.locator('.soha-metadata-tag').last()).toHaveText('托管 YAML')
      const boxes = await tags.evaluateAll((items) =>
        items.map((item) => ({
          height: item.getBoundingClientRect().height,
          top: item.getBoundingClientRect().top,
        })),
      )
      expect(boxes.every((box) => box.height === 20)).toBe(true)
      expect(new Set(boxes.map((box) => box.top)).size).toBe(1)
    }
    await expect(page.locator('.soha-service-runtime-overview')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '部署配置', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: /发布清单$/ })).toHaveCount(0)
    const tabs = page.locator('.soha-service-summary [role="tab"]')
    await expect(tabs.first()).toContainText('Pods')
    await expect(tabs.last()).toHaveText('资源关系')
    await page.screenshot({
      path: info.outputPath(`r12-pods-${width}.png`),
      fullPage: true,
      animations: 'disabled',
    })
    await page.goto(`${target}&serviceTab=runtime`)
    await expect(page.getByRole('tab', { name: '资源关系', exact: true })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await expect(page.locator('.soha-service-runtime-overview')).toBeVisible()
    await expect(page.getByRole('region', { name: '服务运行指标', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '部署配置', exact: true })).toHaveCount(0)
    verifyBrowserEvidence(evidence)
  })
}

for (const canWorkflow of [false, true])
  test(`R11 manifest release is a permission-scoped header action (workflow: ${canWorkflow}) and opens preparation only`, async ({
    context,
    page,
  }, info) => {
    const { evidence } = await setup(context, page, true, canWorkflow)
    await page.goto(target)
    const header = page.locator('.soha-service-summary')
    if (canWorkflow) {
      await header.getByRole('button', { name: '更多发布方式', exact: true }).click()
    }
    const release = canWorkflow
      ? page.getByRole('menuitem', { name: '发布清单', exact: true })
      : header.getByRole('button', { name: /发布清单$/ })
    await expect(release).toBeVisible()
    await expect(page.getByRole('button', { name: '部署配置', exact: true })).toHaveCount(0)
    await page.screenshot({
      path: info.outputPath('r11-release-header.png'),
      fullPage: true,
      animations: 'disabled',
    })
    await release.click()
    await expect(page.getByRole('dialog')).toContainText('资源包版本')
    await expect(page.getByRole('dialog')).toContainText('发布清单')
    verifyBrowserEvidence(evidence)
  })

for (const state of ['empty', 'failed', 'forbidden'] as const) {
  test(`R12 header distinguishes ${state} Pod inventory from healthy counts`, async ({
    context,
    page,
  }) => {
    const { evidence } = await setup(context, page, false, false, state)
    await page.goto(target)
    const metadata = page.locator('.soha-service-location')
    await expect(metadata).toContainText(state === 'empty' ? '就绪 0/0' : '就绪 —')
    await expect(metadata).toContainText(state === 'empty' ? '累计重启 0' : '累计重启 —')
    // Application runtime snapshots retain their existing application-view permission;
    // platform.pods.view gates the new header summary and Pod inspection actions.
    await expect(page.locator('.soha-pod-card')).toHaveCount(state === 'forbidden' ? 2 : 0)
    verifyBrowserEvidence(evidence)
  })
}

test('R12 header resets Pod statistics when switching environment', async ({ context, page }) => {
  const { evidence } = await setup(context, page)
  await page.goto(target)
  const metadata = page.locator('.soha-service-location')
  await expect(metadata).toContainText('就绪 2/2')
  await page
    .getByRole('navigation', { name: '选择环境', exact: true })
    .getByRole('button', { name: /生产环境/ })
    .click()
  await expect(metadata).toContainText('就绪 —')
  await expect(metadata).toContainText('累计重启 —')
  verifyBrowserEvidence(evidence)
})
