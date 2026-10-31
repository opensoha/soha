import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

const base = '/api/v1'
const application = {
  id: 'app-console',
  name: '研发服务入口',
  slug: 'engineering',
  providerType: 'link',
  launchUrl: 'https://console.example.test',
  status: 'disabled',
  tags: ['研发'],
  authorizedRoles: [],
  authorizedTeams: [],
  authorizedUsers: [],
  createdAt: '2026-10-16T00:00:00Z',
  updatedAt: '2026-10-16T01:00:00Z',
}
const secret = {
  id: 'secret-deploy',
  name: '部署工具凭据',
  kind: 'opaque',
  scopeType: 'workspace',
  scopeId: 'default',
  bindings: [{ targetType: 'capability', targetRef: 'deploy.plan' }],
  currentVersion: 3,
  status: 'active',
  updatedAt: '2026-10-16T01:00:00Z',
}
const profile = {
  id: 'vpn-office',
  revision: 3,
  publishedRevision: 2,
  configuration: {
    name: '研发网络接入',
    enabled: true,
    preferredPathMode: 'relay',
    allowedPathModes: ['relay'],
    gatewayIds: [],
    networkSpaceIds: [],
  },
}
const proxy = {
  id: 'edge-proxy',
  name: '研发出口代理',
  engine: 'mihomo',
  host: 'edge-gateway-01',
  status: 'online',
  enabled: true,
  desiredRevision: 4,
  observedRevision: 3,
  lastSeenAt: '2026-10-16T01:00:00Z',
  runtimeVersion: '1.0',
  capabilities: [],
}
const provider = {
  id: 'provider-main',
  applicationId: application.id,
  name: '研发统一登录',
  type: 'oidc',
  enabled: true,
  status: 'enabled',
  config: {},
  createdAt: '2026-10-16T00:00:00Z',
  updatedAt: '2026-10-16T01:00:00Z',
}
const outpost = {
  id: 'outpost-main',
  name: '研发接入代理',
  mode: 'external',
  status: 'online',
  runtimeStatus: 'ready',
  endpoint: 'https://gateway.example.test',
  providerIds: [],
  createdAt: '2026-10-16T00:00:00Z',
  updatedAt: '2026-10-16T01:00:00Z',
}
const cases = [
  { route: '/identity/applications', path: '/identity/applications', name: application.name },
  { route: '/identity/providers', path: '/identity/providers', name: provider.name },
  { route: '/identity/outposts', path: '/identity/outposts', name: outpost.name },
  { route: '/settings/secrets', path: '/secrets', name: secret.name },
  {
    route: '/network-access/vpn/profiles',
    path: '/network-access/vpn/profiles',
    name: profile.configuration.name,
  },
  {
    route: '/network-access/proxy-instances',
    path: '/network-access/proxy-instances',
    name: proxy.name,
  },
]

async function mocks(context: BrowserContext, page: Page) {
  const state = {
    status: 200,
    failedPath: '',
    writes: [] as string[],
    publish: false,
    writeStatus: 503,
    proxyReads: 0,
  }
  const permissions = [
    'workbench.security.view',
    'workbench.settings.view',
    'settings.system-integrations.view',
    'software.package.view',
    'software.package.create',
    'identity.providers.view',
    'identity.providers.create',
    'identity.providers.update',
    'identity.outposts.view',
    'identity.applications.view',
    'identity.applications.create',
    'identity.applications.update',
    'identity.applications.delete',
    'secret.view',
    'secret.create',
    'secret.update',
    'secret.rotate',
    'secret.revoke',
    'network_access.vpn_profiles.view',
    'network_access.vpn_profiles.create',
    'network_access.vpn_profiles.update',
    'network_access.vpn_profiles.publish',
    'network_access.proxy_instances.view',
    'network_access.proxy_instances.update',
  ]
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname.slice(base.length)
    if (method === 'GET' && path === '/network-access/proxy-instances') state.proxyReads += 1
    if (path === '/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: permissions,
            visibleMenuIds: [
              'identity-applications',
              'identity-providers',
              'identity-outposts',
              'settings-secrets',
              'network-access-vpn-profiles',
              'network-access-proxy-instances',
            ],
            visibleMenus: cases.map((item, i) => ({
              id: [
                'identity-applications',
                'identity-providers',
                'identity-outposts',
                'settings-secrets',
                'network-access-vpn-profiles',
                'network-access-proxy-instances',
              ][i],
              path: item.route,
              labelZh: [
                '应用目录',
                '认证接入',
                '接入代理',
                'Secret Store',
                'VPN 连接方案',
                '代理实例',
              ][i],
            })),
          },
        },
      }
    if (method === 'GET' && path === state.failedPath && state.status !== 200)
      return {
        status: state.status,
        body: {
          error: {
            code: 'unavailable',
            message: '请求暂时不可用',
            request_id: 'scene-original-request',
          },
        },
      }
    if (
      method === 'POST' &&
      [
        '/secrets/secret-deploy/versions',
        '/software/packages',
        '/network-access/vpn/profiles/vpn-office/publish',
      ].includes(path)
    ) {
      state.writes.push(`${method} ${path}`)
      if (path.endsWith('/publish')) {
        state.publish = true
        return { body: { data: { ...profile, publishedRevision: 3 } } }
      }
      return {
        status: state.writeStatus,
        body: {
          error: {
            code: 'unavailable',
            message: '请求暂时不可用',
            request_id: 'scene-original-write',
          },
        },
      }
    }
    if (path === '/identity/applications') return { body: { data: [application] } }
    if (path === '/identity/providers') return { body: { data: [provider] } }
    if (path === '/identity/providers/provider-main') return { body: { data: provider } }
    if (path === '/identity/providers/provider-main/setup')
      return {
        body: {
          data: {
            configurationStatus: 'incomplete',
            missingFields: ['public_url'],
            issues: [],
            endpoints: {},
            validation: { status: 'unverified' },
          },
        },
      }
    if (path === '/identity/outposts') return { body: { data: [outpost] } }
    if (path === '/identity/capabilities')
      return {
        body: {
          data: {
            stepUp: { available: false },
            samlApplicationProvider: { available: false },
            outpost: {
              embeddedRuntime: { available: true },
              agentRuntime: { available: true },
              kubernetesArtifact: { available: true },
              externalProtocol: { available: true },
            },
          },
        },
      }
    if (path === '/system-integrations')
      return {
        body: {
          items: [
            {
              id: 'qa-storage',
              name: 'QA 对象存储',
              category: 'storage',
              providerType: 's3',
              enabled: true,
              configuration: [],
              credentialKeys: [],
              healthStatus: 'unknown',
              version: 1,
            },
          ],
        },
      }
    if (path === '/software/packages') return { body: { items: [] } }
    if (path === '/software/storage')
      return { body: { data: { backend: 's3', objectCount: 0, totalBytes: 0, items: [] } } }
    if (path === '/secrets') return { body: { items: [secret] } }
    if (path === '/secrets/secret-deploy/references') return { body: { items: [] } }
    if (path === '/secrets/secret-deploy/versions')
      return {
        body: {
          items: [
            {
              version: 3,
              status: 'active',
              createdAt: '2026-10-16T01:00:00Z',
              createdBy: 'operator',
            },
          ],
        },
      }
    if (path === '/network-access/vpn/profiles')
      return { body: { data: [{ ...profile, publishedRevision: state.publish ? 3 : 2 }] } }
    if (path === '/network-access/proxy-instances') return { body: { items: [proxy] } }
  })
  return { state, evidence }
}

for (const item of cases) {
  test(`${item.route} preserves cached reads and removes denied data`, async ({
    context,
    page,
  }, info) => {
    const { state, evidence } = await mocks(context, page)
    await page.goto(item.route)
    await expect(page.getByText(item.name, { exact: true }).first()).toBeVisible()
    if (item.route === '/identity/providers') {
      await page.screenshot({
        path: info.outputPath('identity-providers.png'),
        fullPage: true,
        animations: 'disabled',
      })
    }
    state.failedPath = item.path
    state.status = 503
    await page.getByRole('button', { name: /刷新/ }).first().click()
    await expect(page.getByText(/未更新，正在展示上次读取结果/).first()).toBeVisible()
    await expect(page.getByText(item.name, { exact: true }).first()).toBeVisible()
    state.status = 403
    await page
      .getByRole('button', { name: /重试读取/ })
      .first()
      .click()
    await expect(page.getByText(/无访问权限/).first()).toBeVisible()
    await expect(page.getByText(item.name, { exact: true })).toHaveCount(0)
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

test('Secret rotation keeps the draft and pauses duplicate writes after an unknown result', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/settings/secrets')
  await page.getByRole('button', { name: '轮换 Secret', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('input[type="password"]').fill('synthetic-canary-value')
  await dialog.getByRole('button', { name: /确.*定/ }).click()
  await expect(dialog.getByText(/请求结果不明/)).toBeVisible()
  await expect(dialog.locator('input[type="password"]')).toHaveValue('synthetic-canary-value')
  await expect(dialog.locator('input[type="password"]')).toBeDisabled()
  await expect(dialog.getByRole('button', { name: /确.*定/ })).toBeDisabled()
  await expect(dialog.getByRole('button', { name: /取.*消/ })).toBeDisabled()
  expect(
    (await page.locator('.soha-operation-feedback').allTextContents()).join(' '),
  ).not.toContain('synthetic-canary-value')
  expect(state.writes).toEqual(['POST /secrets/secret-deploy/versions'])
  await page.screenshot({
    path: info.outputPath('task52-secrets-feedback.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('VPN publishes a reviewed revision without claiming terminal application', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/network-access/vpn/profiles')
  await expect(page.locator('body')).toContainText('已发布 r2')
  await page.getByRole('button', { name: /^发\s*布$/ }).click()
  await expect(page.getByRole('tooltip')).toContainText('会话')
  await page
    .getByRole('tooltip')
    .getByRole('button', { name: /确.*定/ })
    .click()
  await expect(page.locator('body')).toContainText('已发布 r3')
  await expect(page.locator('.soha-operation-feedback')).toContainText('终端应用结果以运行记录为准')
  expect(state.writes).toEqual(['POST /network-access/vpn/profiles/vpn-office/publish'])
  await expect(page.locator('.ant-message-notice')).toHaveCount(0)
  await page.screenshot({
    path: info.outputPath('task52-vpn-revisions.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('Secret relations, narrow layout, keyboard and reduced motion remain usable', async ({
  context,
  page,
}, info) => {
  const { evidence } = await mocks(context, page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/settings/secrets')
  await page.getByRole('button', { name: '使用 Secret', exact: true }).click()
  await expect(page.getByRole('dialog').getByRole('textbox')).toHaveValue(
    'soha://secrets/secret-deploy',
  )
  await verifyGeometry(page, page.getByRole('dialog'))
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: /新建 Secret/ }).click()
  await page
    .getByRole('dialog')
    .getByLabel('名称', { exact: true })
    .fill('较长的自动化部署凭据名称')
  await page.keyboard.press('Tab')
  await expect(page.getByRole('dialog')).toBeVisible()
  await verifyGeometry(page, page.getByRole('dialog').getByRole('button', { name: /确.*定/ }))
  await page.screenshot({
    path: info.outputPath('task52-secrets-narrow.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('dark network revision view remains usable at 125 percent zoom', async ({
  context,
  page,
}, info) => {
  const { evidence } = await mocks(context, page)
  await page.goto('/network-access/vpn/profiles')
  await page.getByRole('button', { name: '切换到深色模式' }).click()
  await expect(page.locator('body')).toContainText(profile.configuration.name)
  await page.addStyleTag({ content: 'html {zoom: 1.25}' })
  await verifyGeometry(page, page.getByRole('button', { name: /^发\s*布$/ }))
  await page.screenshot({
    path: info.outputPath('task52-vpn-dark.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('write permission refusal clears sensitive draft while preserving separately authorized metadata', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  state.writeStatus = 403
  await page.goto('/settings/secrets')
  await page.getByRole('button', { name: '轮换 Secret', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.locator('input[type="password"]').fill('synthetic-denied-draft')
  await dialog.getByRole('button', { name: /确.*定/ }).click()
  await expect(dialog).toHaveCount(0)
  await expect(page.locator('.soha-operation-feedback')).toContainText('操作请求失败')
  await expect(page.getByText(secret.name, { exact: true })).toBeVisible()
  expect(await page.locator('input[type="password"]').count()).toBe(0)
  expect(state.writes).toHaveLength(1)
  verifyBrowserEvidence(evidence)
})

test('failed proxy polling pauses until an explicit read succeeds', async ({ context, page }) => {
  const { state, evidence } = await mocks(context, page)
  await page.clock.install()
  await page.goto('/network-access/proxy-instances')
  await expect(page.getByText(proxy.name, { exact: true })).toBeVisible()
  state.failedPath = '/network-access/proxy-instances'
  state.status = 503
  await page.getByRole('button', { name: /刷新/ }).click()
  await expect(page.getByText(/未更新，正在展示上次读取结果/)).toBeVisible()
  const failedReads = state.proxyReads
  await page.clock.fastForward(31000)
  expect(state.proxyReads).toBe(failedReads)
  state.status = 200
  await page.getByRole('button', { name: /重试读取/ }).click()
  await expect(page.getByText(/未更新，正在展示上次读取结果/)).toHaveCount(0)
  await page.clock.fastForward(16000)
  await expect.poll(() => state.proxyReads).toBeGreaterThan(failedReads + 1)
  verifyBrowserEvidence(evidence)
})

test('software batch stops after an unknown item and keeps unsubmitted files separate', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/internal-workbench/software')
  await page.getByRole('button', { name: /上传软件/ }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByText('批量发布', { exact: true }).click()
  await expect(dialog.getByRole('radio', { name: '批量发布', exact: true })).toBeChecked()
  await dialog.getByLabel('目标存储', { exact: true }).click()
  await dialog.getByLabel('目标存储', { exact: true }).press('Enter')
  await dialog.locator('input[type="file"]').setInputFiles([
    {
      name: 'first-linux-amd64.tar.gz',
      mimeType: 'application/gzip',
      buffer: Buffer.from('first'),
    },
    {
      name: 'second-linux-amd64.tar.gz',
      mimeType: 'application/gzip',
      buffer: Buffer.from('second'),
    },
  ])
  const packages = dialog.locator('.soha-software-batch-package')
  await expect(packages).toHaveCount(2)
  for (let i = 0; i < 2; i++) {
    await packages.nth(i).getByLabel('版本', { exact: true }).fill('1.0.0')
    await packages.nth(i).getByLabel('发布者', { exact: true }).fill('Soha QA')
    const platform = packages.nth(i).getByLabel('平台', { exact: true })
    await platform.click()
    const popupId = await platform.getAttribute('aria-controls')
    await page
      .locator('.ant-select-dropdown')
      .filter({ has: page.locator(`[id="${popupId}"]`) })
      .getByText('Linux', { exact: true })
      .click()
  }
  await dialog.getByRole('button', { name: /批量上传并发布/ }).click()
  await expect(dialog.locator('.soha-operation-feedback')).toContainText('请求结果不明')
  await expect(page.locator('.ant-message')).toContainText('1 个未提交')
  await expect(dialog.getByRole('button', { name: /批量上传并发布/ })).toBeDisabled()
  await expect(dialog.getByRole('radio', { name: '单个发布', exact: true })).toBeDisabled()
  expect(state.writes).toEqual(['POST /software/packages'])
  verifyBrowserEvidence(evidence)
})

test('Secret version history preserves cached rows with a warning and hides refused reads', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.clock.install()
  await page.goto('/settings/secrets')
  await page.getByRole('button', { name: '查看 Secret 版本', exact: true }).click()
  const drawer = page.locator('.ant-drawer-open')
  await expect(drawer.getByText('v3', { exact: true })).toBeVisible()
  await drawer.locator('.ant-drawer-close').click()
  await expect(drawer).toHaveCount(0)
  state.failedPath = '/secrets/secret-deploy/versions'
  state.status = 503
  await page.clock.fastForward(31000)
  await page.getByRole('button', { name: '查看 Secret 版本', exact: true }).click()
  await expect(drawer.getByText(/版本记录未更新/)).toBeVisible()
  await expect(drawer.getByText('v3', { exact: true })).toBeVisible()
  state.status = 403
  await drawer.getByRole('button', { name: /重试读取/ }).click()
  await expect(drawer.getByText(/版本记录无访问权限/)).toBeVisible()
  await expect(drawer.getByText('v3', { exact: true })).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})
