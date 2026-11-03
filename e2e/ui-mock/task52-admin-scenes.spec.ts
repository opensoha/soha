import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

const plugin = {
  id: 'qa-plugin',
  name: '交付助手扩展',
  publisher: 'Soha QA',
  type: 'skill',
  version: '1.0.0',
  source: 'static',
  sourceId: 'qa-local',
  sourceUrl: `https://market.example.test/${'long-catalog-segment-'.repeat(16)}index.json`,
  installed: false,
  manifest: {
    description: '交付检查与诊断工具',
    permissions: { required: Array.from({ length: 7 }, (_, i) => `qa.permission.${i}`) },
    assets: { skills: [`skills/${'long-path-segment-'.repeat(16)}SKILL.md`] },
    capabilities: { tools: ['qa.inspect', 'qa.report'] },
    compatibility: { soha: '>=0.1.0', contracts: '>=0.1.24' },
    metadata: { preservedField: 'raw-manifest-proof' },
    configSchema: {
      type: 'object',
      properties: { endpoint: { type: 'string', title: '服务地址' } },
    },
  },
}
const installed = {
  ...plugin,
  status: 'enabled',
  installed: true,
  installedAt: '2026-10-16T01:00:00Z',
  updatedAt: '2026-10-16T01:00:00Z',
  installedBy: 'qa',
  configuredSecretRefs: {},
  configured: true,
  checksumStatus: 'verified',
}
const source = {
  id: 'gitlab-qa',
  name: '研发代码源',
  category: 'source_control',
  providerType: 'gitlab',
  enabled: true,
  version: 4,
  configuration: [{ key: 'base_url', value: 'https://git.example.test/api/v4' }],
  credentialKeys: ['token'],
  healthStatus: 'healthy',
}
const app = {
  id: 'qa-console',
  name: '研发服务门户',
  slug: 'qa-console',
  providerType: 'link',
  status: 'active',
  portalVisible: true,
  tags: ['研发'],
  assignments: [],
  launchUrl: 'https://console.example.test',
}
const security = {
  principal: {
    userId: 'qa-user',
    userName: '质量验证账户',
    email: 'qa@example.test',
    roles: ['qa'],
    teams: ['研发'],
    projects: [],
    tags: [],
  },
  mfaEnabled: false,
  activeSession: 2,
  linkedSources: ['oidc'],
}
const announcement = {
  id: 'qa-notice',
  title: '维护窗口通知',
  content: '服务滚动维护，请关注运行记录。',
  level: 'info',
  status: 'draft',
  sticky: false,
}
const menu = {
  id: 'qa-menu',
  labelZh: '研发资源入口',
  labelEn: 'QA resources',
  path: '/qa-resources',
  enabled: true,
  sortOrder: 1,
  visibilityMode: 'explicit',
  roleIds: ['qa'],
  children: [],
}
const sessions = ['session-a', 'session-b'].map((id, i) => ({
  id,
  userId: `user-${i}`,
  userName: `验证会话-${i + 1}`,
  email: `qa-${i}@example.test`,
  providerType: 'oidc',
  status: 'active',
  createdAt: '2026-10-16T01:00:00Z',
  lastSeenAt: '2026-10-16T02:00:00Z',
  expiresAt: '2026-10-17T02:00:00Z',
}))
const runtimeItem = {
  key: 'qa.connection',
  label: '服务连接地址',
  description: '后台服务连接配置',
  category: '连接',
  valueType: 'string',
  effectiveValue: 'before',
  source: 'config_file',
  applyMode: 'reconfigure',
  editable: true,
  sensitive: false,
}
const cases = [
  { route: '/plugins/marketplace', path: '/plugins/marketplace', name: plugin.name },
  {
    route: '/plugins/marketplace/qa-plugin',
    path: '/plugins/marketplace/qa-plugin',
    name: plugin.name,
  },
  { route: '/plugins/installed/qa-plugin', path: '/plugins/qa-plugin', name: plugin.name },
  { route: '/portal', path: '/portal/bootstrap', name: app.name },
  {
    route: '/portal/applications/qa-console',
    path: '/portal/applications/qa-console',
    name: app.name,
  },
  { route: '/portal/security', path: '/portal/security', name: security.principal.email },
  { route: '/settings/login', path: '/settings/identity', name: '研发统一登录' },
  { route: '/settings/source-control', path: '/system-integrations', name: source.name },
  {
    route: '/settings/runtime-configuration',
    path: '/settings/runtime-config',
    name: runtimeItem.label,
  },
  { route: '/system/online-users', path: '/auth/sessions', name: sessions[0].userName },
  { route: '/system/announcements', path: '/announcements', name: announcement.title },
  { route: '/system/menus', path: '/menus', name: menu.labelZh },
]

async function mocks(context: BrowserContext, page: Page, detail = plugin) {
  const state = {
    failedPath: '',
    status: 200,
    writeStatus: 503,
    version: 4,
    writes: [] as { method: string; path: string; body: Record<string, unknown> }[],
    batch: false,
    applicationStatus: '',
    applicationReads: 0,
    canInstall: true,
    marketplacePlugins: [detail],
    installedPlugins: [installed],
  }
  const evidence = await installMocks(context, page, (url, method, request) => {
    const path = url.pathname.replace('/api/v1', '')
    const failure = (status: number) => ({
      status,
      body: {
        error: { code: 'qa_failure', message: '验证请求暂不可用', request_id: 'qa-admin-request' },
      },
    })
    if (path === '/settings/runtime-config/applications/application-5') state.applicationReads += 1
    if (method === 'GET' && path === state.failedPath && state.status !== 200)
      return failure(state.status)
    if (path === '/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: [
              'plugin.view',
              'plugin.install',
              'plugin.manage',
              'plugin.configure',
              'plugin.lifecycle',
              'plugin.remove',
              'plugin.upgrade',
              'identity.portal.view',
              'identity.applications.view',
              'settings.branding.view',
              'settings.branding.update',
              'settings.identity.view',
              'settings.identity.update',
              'settings.system-integrations.view',
              'settings.system-integrations.create',
              'settings.system-integrations.update',
              'settings.system-integrations.test',
              'settings.system-integrations.delete',
              'settings.runtime-config.view',
              'settings.runtime-config.apply',
              'settings.runtime-config.validate',
              'settings.runtime-config.rollback',
              'system.online-users.view',
              'system.online-users.revoke',
              'system.announcements.view',
              'system.announcements.create',
              'system.announcements.update',
              'system.announcements.publish',
              'system.announcements.withdraw',
              'system.announcements.delete',
              'system.menus.view',
              'system.menus.create',
              'system.menus.update',
              'system.menus.delete',
              'workbench.settings.view',
              'workbench.home.view',
            ].filter((key) => state.canInstall || key !== 'plugin.install'),
            visibleMenuIds: [
              'settings-extensions-marketplace',
              'home-workbench',
              'announcements',
              'menus',
              'settings-overview',
              'settings-login',
              'settings-branding',
              'settings-source-control',
              'settings-runtime-configuration',
              'system-online-users',
              'system-announcements',
              'system-menus',
            ],
            visibleMenus: cases
              .filter((c) => !c.route.startsWith('/portal'))
              .map((c, i) => ({
                id: `qa-${i}`,
                path: c.route,
                labelZh:
                  [
                    '插件市场',
                    '插件详情',
                    '插件管理',
                    '登录设置',
                    '代码源',
                    '运行时配置',
                    '在线会话',
                    '公告管理',
                    '菜单管理',
                  ][i] || '管理页面',
              })),
          },
        },
      }
    if (path === '/auth/refresh') return undefined
    if (method !== 'GET') {
      state.writes.push({ method, path, body: request.postDataJSON() ?? {} })
      if (state.batch && path === '/auth/sessions/session-a/revoke') return { body: {} }
      if (state.batch && path === '/auth/sessions/session-b/revoke')
        return failure(state.writeStatus)
      if (
        path === '/settings/runtime-config/validate' &&
        request.postDataJSON().expectedVersion !== state.version
      )
        return failure(409)
      if (path === '/settings/runtime-config/validate')
        return {
          body: {
            data: {
              valid: true,
              expectedVersion: 4,
              currentVersion: state.version,
              changes: [],
              issues: [],
              requiresRestart: false,
            },
          },
        }
      if (
        ['/settings/runtime-config/apply', '/settings/runtime-config/rollback'].includes(path) &&
        state.applicationStatus
      ) {
        const body = request.postDataJSON()
        const date = '2026-10-16T01:00:00Z'
        return {
          body: {
            data: {
              revision: {
                id: 'revision-5',
                version: 5,
                status: state.applicationStatus,
                changes: body.changes ?? [],
                actor: 'qa',
                createdAt: date,
              },
              application: {
                id: 'application-5',
                revisionId: 'revision-5',
                version: 5,
                status: state.applicationStatus,
                items: [],
                createdAt: date,
                updatedAt: date,
              },
            },
          },
        }
      }
      return failure(state.writeStatus)
    }
    if (path === '/plugins/marketplace') {
      const query = url.searchParams.get('q') ?? ''
      const type = url.searchParams.get('type')
      return {
        body: {
          data: state.marketplacePlugins.filter(
            (item) => item.name.includes(query) && (!type || item.type === type),
          ),
        },
      }
    }
    if (path === '/plugins/marketplace/qa-plugin') return { body: { data: detail } }
    if (path === '/plugins/installed') return { body: { data: state.installedPlugins } }
    if (path === '/plugins/qa-plugin') return { body: { data: installed } }
    if (path === '/plugins/qa-plugin/manifest') return { body: { data: plugin.manifest } }
    if (path === '/extensions/runtime') return { body: { data: [] } }
    if (path === '/portal/bootstrap')
      return { body: { data: { applications: [app], favorites: [], recent: [], security } } }
    if (path === '/portal/applications/qa-console') return { body: { data: app } }
    if (path === '/portal/security') return { body: { data: security } }
    if (path === '/identity/capabilities')
      return {
        body: {
          data: {
            totp: { available: true },
            webauthn: { available: false },
            recoveryCodes: { available: true },
            stepUp: { available: false },
            samlLoginSource: { available: false },
          },
        },
      }
    if (path === '/identity/mfa/credentials') return { body: { items: [] } }
    if (path === '/settings/identity')
      return {
        body: {
          data: {
            localPasswordLoginEnabled: true,
            providers: [
              {
                id: 'login-qa',
                name: '研发统一登录',
                type: 'oidc',
                enabled: true,
                issuer: 'https://id.example.test',
                clientId: 'qa',
              },
            ],
            defaultProviderId: 'login-qa',
          },
        },
      }
    if (path === '/system-integrations') return { body: { items: [source] } }
    if (path === '/system-integrations/gitlab-qa')
      return { body: { data: { ...source, version: state.version } } }
    if (path === '/settings/runtime-config')
      return {
        body: { data: { version: state.version, items: [runtimeItem], pendingRestart: false } },
      }
    if (path === '/settings/runtime-config/history')
      return {
        body: {
          items: [
            {
              id: 'revision-3',
              version: 3,
              status: 'applied',
              changes: [{ key: runtimeItem.key, value: 'old' }],
              actor: 'qa',
              createdAt: '2026-10-15T01:00:00Z',
            },
          ],
        },
      }
    if (path === '/settings/runtime-config/applications/application-5')
      return {
        body: {
          data: {
            id: 'application-5',
            revisionId: 'revision-5',
            version: 5,
            status: state.applicationStatus,
            items: [],
            createdAt: '2026-10-16T01:00:00Z',
            updatedAt: '2026-10-16T02:00:00Z',
          },
        },
      }
    if (path === '/auth/sessions') return { body: { data: sessions } }
    if (path === '/announcements/inbox') return { body: { data: { items: [], unreadCount: 0 } } }
    if (path === '/announcements') return { body: { data: [announcement] } }
    if (path === '/announcements/qa-notice/receipts')
      return { body: { data: { items: [], total: 0, readCount: 0, unreadCount: 0 } } }
    if (path === '/menus') return { body: { data: [menu] } }
    if (path === '/access/roles') return { body: { data: [] } }
    if (path === '/settings/branding')
      return {
        body: { data: { appTitle: 'Soha QA', sidebarTitle: 'Soha QA', slogan: '研发资源控制台' } },
      }
  })
  page.on('console', (message) => {
    if (message.type() === 'error' && /Maximum update depth exceeded/.test(message.text()))
      evidence.fatal.push('React update loop')
  })
  return { state, evidence }
}

async function verifyTagContrast(page: Page) {
  const ratios = await page
    .locator(
      '.ant-tag-success, .ant-tag-warning, .ant-tag-green, .ant-tag-gold, .soha-plugin-market-card-detail',
    )
    .evaluateAll((tags) => {
      const parse = (color: string) => {
        const channels = color.match(/[\d.]+/g)!.map(Number)
        return color.startsWith('color(srgb ')
          ? channels.map((value, i) => (i < 3 ? value * 255 : value))
          : channels
      }
      const blend = (front: number[], back: number[]) =>
        front
          .slice(0, 3)
          .map((value, i) => value * (front[3] ?? 1) + back[i] * (1 - (front[3] ?? 1)))
      const luminance = (color: number[]) =>
        color
          .map((value) => value / 255)
          .map((value) => (value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4))
          .reduce((sum, value, i) => sum + value * [0.2126, 0.7152, 0.0722][i], 0)
      return tags.map((tag) => {
        const ancestors: Element[] = []
        for (let node: Element | null = tag; node; node = node.parentElement)
          ancestors.unshift(node)
        const background = ancestors.reduce(
          (color, node) => blend(parse(getComputedStyle(node).backgroundColor), color),
          [255, 255, 255],
        )
        const foreground = blend(parse(getComputedStyle(tag).color), background)
        const levels = [luminance(foreground), luminance(background)].sort((a, b) => b - a)
        return { label: tag.textContent, ratio: (levels[0] + 0.05) / (levels[1] + 0.05) }
      })
    })
  expect(ratios.length).toBeGreaterThan(0)
  for (const item of ratios) expect(item.ratio, item.label ?? 'Tag').toBeGreaterThanOrEqual(4.5)
}

for (const item of cases) {
  test(`${item.route} retains labeled stale reads and hides denied data`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await mocks(context, page)
    await page.goto(item.route)
    if (item.route === '/system/menus')
      await page.getByRole('button', { name: '展开行', exact: true }).click()
    await expect(page.getByText(item.name, { exact: true }).first()).toBeVisible()
    state.failedPath = item.path
    state.status = 503
    await page.getByRole('button', { name: /刷新/ }).last().click()
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

for (const view of [
  { width: 1440, dark: false },
  { width: 1166, dark: true },
  { width: 390, dark: false },
  { width: 390, dark: true },
]) {
  test(`plugin marketplace catalog layout ${view.width} ${view.dark ? 'dark' : 'light'}`, async ({
    context,
    page,
  }, info) => {
    await page.setViewportSize({ width: view.width, height: 1209 })
    const item = {
      ...plugin,
      name: `交付助手扩展${'长名称LongName'.repeat(12)}`,
      publisher: 'LongPublisher'.repeat(8),
      sourceId: 'long-source-'.repeat(12),
      verified: true,
      riskLevel: 'read',
    }
    const { state, evidence } = await mocks(context, page, item)
    state.installedPlugins = []
    state.marketplacePlugins = [
      item,
      { ...item, id: 'qa-mutating', name: '变更扩展', type: 'connector', riskLevel: 'mutate' },
    ]
    await page.goto('/plugins/marketplace')
    if (view.dark) await page.getByRole('button', { name: '切换到深色模式' }).click()
    await expect(page.locator('h1')).toHaveText('插件市场')
    expect(
      await page.locator('h1').evaluate((element) => element.getBoundingClientRect().height),
    ).toBeLessThanOrEqual(1)
    await expect(page.getByRole('heading', { name: item.name, level: 2 })).toBeVisible()
    await verifyGeometry(page, page.getByRole('button', { name: /安\s*装$/ }).first())
    await expect(page.getByText('2 个插件', { exact: true })).toBeVisible()
    const card = page.locator('.soha-plugin-market-card').first()
    await expect(card.getByText('查看详情', { exact: true })).toBeVisible()
    await expect(card.getByText('版本 1.0.0', { exact: true })).toBeVisible()
    await expect(card.getByText(`来源 ${item.sourceId}`, { exact: true })).toBeVisible()
    await expect(card.locator('.soha-plugin-market-card-source')).not.toHaveCSS('cursor', 'pointer')
    await expect(page.getByRole('button', { name: /更多筛选$/ })).toBeVisible()
    await page.screenshot({
      path: info.outputPath('plugin-marketplace.png'),
      fullPage: true,
      animations: 'disabled',
    })
    // Measure the settled theme after the screenshot has finished CSS transitions.
    await verifyTagContrast(page)
    const link = page.getByRole('link', { name: `查看 ${item.name} 详情`, exact: true })
    await link.focus()
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL(/\/plugins\/marketplace\/qa-plugin\?/)
    await expect(page.getByRole('heading', { name: item.name, level: 1 })).toBeVisible()
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })

  test(`plugin detail hierarchy and long values ${view.width} ${view.dark ? 'dark' : 'light'}`, async ({
    context,
    page,
  }, info) => {
    await page.setViewportSize({ width: view.width, height: 1209 })
    const detail = { ...plugin, name: `交付助手扩展${'长名称'.repeat(24)}` }
    const { state, evidence } = await mocks(context, page, detail)
    await page.goto('/plugins/marketplace/qa-plugin?sourceId=qa-local&version=1.0.0')
    if (view.dark) await page.getByRole('button', { name: '切换到深色模式' }).click()
    await expect(page.getByRole('heading', { name: detail.name, level: 1 })).toBeVisible()
    await expect(page.getByRole('heading', { name: '扩展', exact: true })).toHaveCount(0)
    await verifyGeometry(page, page.getByRole('button', { name: /安\s*装$/ }))
    await expect(page.getByText('qa.permission.6', { exact: true })).toBeVisible()
    await expect(page.getByText(plugin.manifest.assets.skills[0], { exact: true })).toBeVisible()
    const raw = page.getByRole('button', { name: '原始 Manifest', exact: true })
    await expect(raw).toHaveAttribute('aria-expanded', 'false')
    await raw.focus()
    await page.keyboard.press('Enter')
    await expect(raw).toHaveAttribute('aria-expanded', 'true')
    await expect(page.locator('.soha-plugin-json')).toContainText('raw-manifest-proof')
    await page.keyboard.press('Enter')
    await expect(raw).toHaveAttribute('aria-expanded', 'false')
    const previousReads = evidence.requests.length
    await page.getByRole('button', { name: '刷新插件详情' }).click()
    await expect.poll(() => evidence.requests.length).toBeGreaterThan(previousReads)
    expect(
      evidence.requests.some(
        (path) =>
          path.includes('/plugins/marketplace/qa-plugin?') &&
          path.includes('sourceId=qa-local') &&
          path.includes('version=1.0.0'),
      ),
    ).toBe(true)
    await page.screenshot({
      path: info.outputPath('plugin-detail.png'),
      fullPage: true,
      animations: 'disabled',
    })
    await page.getByRole('button', { name: '返回市场', exact: true }).click()
    await expect(page).toHaveURL(/\/plugins\/marketplace$/)
    await page.goto('/plugins/installed/qa-plugin')
    await expect(page.getByRole('heading', { name: plugin.name, level: 1 })).toBeVisible()
    await verifyGeometry(page, page.getByRole('button', { name: /移\s*除$/ }))
    await expect(page.getByRole('button', { name: '原始 Manifest', exact: true })).toHaveCount(1)
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

for (const dark of [false, true]) {
  test(`shared tag contrast remains readable outside Plugins in ${dark ? 'dark' : 'light'}`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await mocks(context, page)
    await page.goto('/system/menus')
    if (dark) await page.getByRole('button', { name: '切换到深色模式' }).click()
    await page.getByRole('button', { name: '展开行', exact: true }).click()
    await expect(page.getByText(menu.labelZh, { exact: true })).toBeVisible()
    await expect(page.locator('.soha-status-tag.ant-tag-success').first()).toBeVisible()
    await verifyTagContrast(page)
    expect(state.writes).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}

test('plugin type scope displays all initially and after reset, and submits the selected type', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  state.marketplacePlugins = [
    plugin,
    { ...plugin, id: 'qa-connector', type: 'connector', name: '连接器' },
  ]
  await page.goto('/plugins/marketplace')
  const select = page
    .locator('.ant-select')
    .filter({ has: page.getByRole('combobox', { name: /类型/ }) })
  await expect(select).toContainText('全部类型')
  await page.getByRole('combobox', { name: /类型/ }).click()
  await page.locator('.ant-select-dropdown').getByText('Skill', { exact: true }).click()
  await page.getByRole('button', { name: /查\s*询/ }).click()
  await expect(page.locator('.soha-plugin-market-card')).toHaveCount(1)
  await expect(page.getByRole('heading', { name: plugin.name, level: 2 })).toBeVisible()
  expect(
    evidence.requests.some(
      (path) => new URL(path, test.info().project.use.baseURL).searchParams.get('type') === 'skill',
    ),
  ).toBe(true)
  await page.getByRole('button', { name: /重\s*置/ }).click()
  await expect(select).toContainText('全部类型')
  await expect(page.locator('.soha-plugin-market-card')).toHaveCount(2)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('plugin marketplace preserves query, local pagination, installation filters and management navigation', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  state.installedPlugins = []
  state.marketplacePlugins = Array.from({ length: 13 }, (_, i) => ({
    ...plugin,
    id: i ? `qa-plugin-${i}` : plugin.id,
    name: `${plugin.name} ${i + 1}`,
  }))
  await page.goto('/plugins/marketplace')
  await expect(page.locator('.soha-plugin-market-card')).toHaveCount(12)
  await expect(page.getByText('13 个插件', { exact: true })).toBeVisible()
  await page.locator('.ant-pagination-item-2').click()
  await expect(page.locator('.soha-plugin-market-card')).toHaveCount(1)
  await expect(page.getByRole('heading', { name: `${plugin.name} 13`, level: 2 })).toBeVisible()
  await page.getByPlaceholder('搜索 ID、名称、发布者').fill(`${plugin.name} 1`)
  await page.getByRole('button', { name: /查\s*询/ }).click()
  await expect(page.locator('.soha-plugin-market-card')).toHaveCount(5)
  await expect(page.getByText('5 个插件', { exact: true })).toBeVisible()
  expect(
    evidence.requests.some(
      (path) =>
        new URL(path, test.info().project.use.baseURL).searchParams.get('q') === `${plugin.name} 1`,
    ),
  ).toBe(true)
  await page.getByRole('button', { name: /重\s*置/ }).click()
  await expect(page.locator('.soha-plugin-market-card')).toHaveCount(12)
  await page.getByRole('radiogroup').getByText('已安装', { exact: true }).click()
  await expect(page.getByText('尚未安装插件', { exact: true })).toBeVisible()
  await expect(page.getByText('0 个插件', { exact: true })).toBeVisible()
  await page.getByRole('radiogroup').getByText('未安装', { exact: true }).click()
  await expect(page.locator('.soha-plugin-market-card')).toHaveCount(12)
  state.marketplacePlugins = [{ ...plugin, installed: true }]
  state.installedPlugins = [installed]
  await page.getByRole('button', { name: '刷新插件列表' }).click()
  await expect(page.getByText('没有匹配插件', { exact: true })).toBeVisible()
  await page.getByRole('radiogroup').getByText('已安装', { exact: true }).click()
  await expect(page.getByRole('button', { name: /管\s*理/ })).toBeVisible()
  await page.getByRole('button', { name: /管\s*理/ }).click()
  await expect(page).toHaveURL(/\/plugins\/installed\/qa-plugin$/)
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('plugin marketplace keeps installation disabled without permission', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  state.canInstall = false
  state.installedPlugins = []
  await page.goto('/plugins/marketplace')
  await expect(page.getByRole('button', { name: /安\s*装$/ })).toBeDisabled()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

test('plugin detail keeps installation disabled without permission', async ({ context, page }) => {
  const { state, evidence } = await mocks(context, page)
  state.canInstall = false
  await page.goto('/plugins/marketplace/qa-plugin')
  await expect(page.getByRole('heading', { name: plugin.name, level: 1 })).toBeVisible()
  await expect(page.getByRole('button', { name: /安\s*装$/ })).toBeDisabled()
  expect(state.writes).toEqual([])
  verifyBrowserEvidence(evidence)
})

for (const route of ['/plugins/marketplace', '/plugins/marketplace/qa-plugin']) {
  test(`plugin install keeps the reviewed target and freezes unknown submission from ${route}`, async ({
    context,
    page,
  }, info) => {
    const { state, evidence } = await mocks(context, page)
    await page.goto(route)
    await page.getByRole('button', { name: /安\s*装$/ }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('button', { name: /安\s*装/ }).click()
    await expect(dialog.getByText(/请求结果不明/)).toBeVisible()
    await expect(dialog.getByRole('button', { name: /安\s*装/ })).toBeDisabled()
    await expect(dialog.getByRole('button', { name: /取.*消/ })).toBeDisabled()
    await expect(dialog).toContainText(plugin.name)
    expect(state.writes).toHaveLength(1)
    await page.screenshot({
      path: info.outputPath('plugins-review-feedback.png'),
      fullPage: true,
      animations: 'disabled',
    })
    verifyBrowserEvidence(evidence)
  })
}

test('code source refresh preserves private draft and original expectedVersion', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await mocks(context, page)
  state.writeStatus = 409
  await page.goto('/settings/source-control/gitlab-qa')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('连接名称', { exact: true }).fill('未保存的连接草稿')
  const token = dialog.getByLabel(/访问令牌/)
  await token.fill('synthetic-private-draft')
  state.version = 5
  await dialog.getByRole('button', { name: '刷新当前代码源连接' }).click()
  await dialog.getByRole('button', { name: /保\s*存/ }).click()
  await expect(dialog.getByText(/操作请求失败/)).toBeVisible()
  await expect(token).toHaveValue('synthetic-private-draft')
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].body.expectedVersion).toBe(4)
  expect(
    (await dialog.locator('.soha-operation-feedback').allTextContents()).join(''),
  ).not.toContain('synthetic-private-draft')
  await page.screenshot({
    path: info.outputPath('source-draft-feedback.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('runtime snapshot refresh preserves draft, original version and frozen apply review', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/settings/runtime-configuration')
  const editor = page.getByRole('textbox', { name: runtimeItem.label, exact: true })
  await editor.fill('after')
  state.version = 5
  await page.getByRole('button', { name: /刷\s*新$/ }).click()
  await expect(editor).toHaveValue('after')
  await expect(page.getByText(/服务端已更新为 v5/)).toBeVisible()
  await page.getByRole('button', { name: /应用 1 项/ }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '应用配置' }).click()
  await expect(dialog.getByText(/操作请求失败/)).toBeVisible()
  expect(state.writes.map((w) => w.path)).toEqual(['/settings/runtime-config/validate'])
  expect(state.writes.every((w) => w.body.expectedVersion === 4)).toBe(true)
  await expect(dialog.getByRole('button', { name: '应用配置' })).toBeEnabled()
  await page.screenshot({
    path: info.outputPath('runtime-version-feedback.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('batch revoke retains failed IDs and pauses unknown outcomes', async ({ context, page }) => {
  const { state, evidence } = await mocks(context, page)
  state.batch = true
  await page.goto('/system/online-users')
  await page.getByRole('checkbox', { name: 'Select all' }).check()
  await page.getByRole('button', { name: '批量下线 (2)' }).click()
  await expect(page.locator('.soha-operation-feedback')).toContainText('成功 1，失败 1')
  await expect(page.locator('.soha-operation-feedback')).toContainText('请求结果不明')
  await expect(page.getByRole('button', { name: '批量下线 (1)' })).toBeDisabled()
  expect(state.writes.map((w) => w.path)).toEqual([
    '/auth/sessions/session-a/revoke',
    '/auth/sessions/session-b/revoke',
  ])
  verifyBrowserEvidence(evidence)
})

test('source unknown writes block Escape, close and repeat submission', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/settings/source-control/gitlab-qa')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel(/访问令牌/).fill('synthetic-unknown-draft')
  await dialog.getByRole('button', { name: /保\s*存/ }).click()
  await expect(dialog.getByText(/请求结果不明/)).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  await expect(dialog.locator('.ant-modal-close')).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: /保\s*存/ })).toBeDisabled()
  await expect(dialog.getByRole('button', { name: /取\s*消/ })).toBeDisabled()
  expect(state.writes).toHaveLength(1)
  verifyBrowserEvidence(evidence)
})

test('source write refusal clears the private draft and leaves a visible receipt', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  state.writeStatus = 403
  await page.goto('/settings/source-control/gitlab-qa')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel(/访问令牌/).fill('synthetic-denied-draft')
  await dialog.getByRole('button', { name: /保\s*存/ }).click()
  await expect(dialog).toHaveCount(0)
  await expect(page.locator('.soha-operation-feedback')).toContainText('操作请求失败')
  await expect(page.locator('input[type="password"]')).toHaveCount(0)
  expect(state.writes).toHaveLength(1)
  verifyBrowserEvidence(evidence)
})

for (const status of [403, 503]) {
  test(`login provider save status ${status} preserves or clears the private draft explicitly`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await mocks(context, page)
    state.writeStatus = status
    await page.goto('/settings/login')
    await page.getByRole('button', { name: '编辑登录源', exact: true }).click()
    const dialog = page.getByRole('dialog')
    const secret = dialog.getByLabel('Client Secret', { exact: true })
    await secret.fill('synthetic-login-private-draft')
    await dialog.getByRole('button', { name: '下一步', exact: true }).click()
    await dialog.getByRole('button', { name: /保\s*存/ }).click()
    if (status === 403) {
      await expect(dialog).toHaveCount(0)
      await expect(page.locator('input[type="password"]')).toHaveCount(0)
      await expect(page.locator('.soha-operation-feedback')).toContainText('操作请求失败')
    } else {
      await expect(dialog.locator('.soha-operation-feedback')).toContainText('请求结果不明')
      await expect(secret).toHaveValue('synthetic-login-private-draft')
      await page.keyboard.press('Escape')
      await expect(dialog).toBeVisible()
      await expect(dialog.getByRole('button', { name: /保\s*存/ })).toBeDisabled()
    }
    expect(
      (await page.locator('.soha-operation-feedback').allTextContents()).join(''),
    ).not.toContain('synthetic-login-private-draft')
    expect(state.writes).toHaveLength(1)
    verifyBrowserEvidence(evidence)
  })
}

test('plugin configuration keeps its draft after an unknown result', async ({ context, page }) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/plugins/installed/qa-plugin')
  await page.getByRole('button', { name: /配\s*置$/ }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('服务地址', { exact: true }).fill('https://draft.example.test')
  await dialog.getByRole('button', { name: /保\s*存/ }).click()
  await expect(dialog.locator('.soha-operation-feedback')).toContainText('请求结果不明')
  await expect(dialog.getByLabel('服务地址', { exact: true })).toHaveValue(
    'https://draft.example.test',
  )
  await expect(dialog.getByRole('button', { name: /保\s*存/ })).toBeDisabled()
  expect(state.writes).toHaveLength(1)
  verifyBrowserEvidence(evidence)
})

test('runtime unknown apply retains a frozen confirmation and prevents duplicates', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/settings/runtime-configuration')
  await page.getByRole('textbox', { name: runtimeItem.label, exact: true }).fill('after')
  await page.getByRole('button', { name: /应用 1 项/ }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: '应用配置' }).click()
  await expect(dialog.getByText(/请求结果不明/)).toBeVisible()
  await expect(dialog.getByRole('button', { name: '应用配置' })).toBeDisabled()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  expect(state.writes.map((w) => w.path)).toEqual([
    '/settings/runtime-config/validate',
    '/settings/runtime-config/apply',
  ])
  expect(state.writes.every((w) => w.body.expectedVersion === 4)).toBe(true)
  verifyBrowserEvidence(evidence)
})

for (const status of ['failed', 'partially_applied']) {
  test(`runtime application ${status} keeps the draft and a link to the returned revision`, async ({
    context,
    page,
  }) => {
    const { state, evidence } = await mocks(context, page)
    state.applicationStatus = status
    await page.goto('/settings/runtime-configuration')
    const editor = page.getByRole('textbox', { name: runtimeItem.label, exact: true })
    await editor.fill('failed-runtime-draft')
    await page.getByRole('button', { name: /应用 1 项/ }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByRole('button', { name: '应用配置' }).click()
    await expect(
      dialog.locator('.soha-operation-feedback').filter({ hasText: '应用运行时配置' }),
    ).toContainText(`配置应用状态：${status}`)
    await expect(dialog.getByRole('button', { name: '应用配置' })).toBeDisabled()
    await expect(editor).toHaveValue('failed-runtime-draft')
    await dialog.getByRole('button', { name: '查看返回版本' }).click()
    await expect(page.locator('.ant-drawer-open')).toContainText('配置版本 v5')
    expect(state.writes.map((w) => w.path)).toEqual([
      '/settings/runtime-config/validate',
      '/settings/runtime-config/apply',
    ])
    verifyBrowserEvidence(evidence)
  })
}

test('rollback failure keeps its reviewed version and the returned application record', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  state.applicationStatus = 'failed'
  await page.goto('/settings/runtime-configuration')
  await page.getByRole('tab', { name: '变更历史', exact: true }).click()
  await page.getByRole('button', { name: '查看版本 v3', exact: true }).click()
  await page.getByRole('button', { name: /回滚到此版本/ }).click()
  const dialog = page.getByRole('dialog', { name: '回滚到版本 v3？', exact: true })
  await dialog.getByRole('button', { name: '确认回滚', exact: true }).click()
  await expect(dialog.locator('.soha-operation-feedback')).toContainText('配置应用状态：failed')
  await expect(dialog.getByRole('button', { name: '确认回滚', exact: true })).toBeDisabled()
  expect(state.writes).toHaveLength(1)
  expect(state.writes[0].body).toMatchObject({ expectedVersion: 4, targetVersion: 3 })
  await dialog.getByRole('button', { name: '查看返回版本' }).click()
  await expect(page.locator('.ant-drawer-open .soha-runtime-config-application')).toContainText(
    'application-5',
  )
  verifyBrowserEvidence(evidence)
})

test('returned configuration application polls its actual ID and pauses failed reads', async ({
  context,
  page,
}) => {
  const { state, evidence } = await mocks(context, page)
  state.applicationStatus = 'pending'
  await page.clock.install()
  await page.goto('/settings/runtime-configuration')
  await page.getByRole('textbox', { name: runtimeItem.label, exact: true }).fill('after')
  await page.getByRole('button', { name: /应用 1 项/ }).click()
  await page.getByRole('dialog').getByRole('button', { name: '应用配置', exact: true }).click()
  const record = page.locator('.ant-drawer-open .soha-runtime-config-application')
  await expect(record).toContainText('pending')
  state.failedPath = '/settings/runtime-config/applications/application-5'
  state.status = 503
  await record.getByRole('button', { name: '刷新配置应用记录', exact: true }).click()
  await expect(record).toContainText('未更新，正在展示上次读取结果')
  const reads = state.applicationReads
  await page.clock.fastForward(6000)
  expect(state.applicationReads).toBe(reads)
  state.status = 200
  state.applicationStatus = 'applied'
  await record.getByRole('button', { name: /重试读取/ }).click()
  await expect(record).toContainText('applied')
  const completedReads = state.applicationReads
  await page.clock.fastForward(6000)
  expect(state.applicationReads).toBe(completedReads)
  verifyBrowserEvidence(evidence)
})

test('branding preserves its changed form and shows unknown save feedback', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/settings/branding')
  await page.getByLabel('网页标题', { exact: true }).fill('研发控制台')
  await page.getByRole('button', { name: /保\s*存/ }).click()
  await expect(page.locator('.soha-operation-feedback')).toContainText('请求结果不明')
  await expect(page.getByLabel('网页标题', { exact: true })).toHaveValue('研发控制台')
  await expect(page.getByRole('button', { name: /保\s*存/ })).toBeDisabled()
  expect(state.writes).toHaveLength(1)
  await page.screenshot({
    path: info.outputPath('branding-feedback.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('announcement unknown save preserves editor and locks close and repeat actions', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await mocks(context, page)
  await page.goto('/system/announcements')
  await page.getByRole('button', { name: '编辑公告', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('标题', { exact: true }).fill('维护通知草稿')
  await dialog.getByRole('button', { name: /更\s*新/ }).click()
  await expect(dialog.getByText(/请求结果不明/)).toBeVisible()
  await expect(dialog.getByLabel('标题', { exact: true })).toHaveValue('维护通知草稿')
  await expect(dialog.getByRole('button', { name: /取.*消/ })).toBeDisabled()
  expect(state.writes).toHaveLength(1)
  await verifyGeometry(page, dialog)
  await page.screenshot({
    path: info.outputPath('announcement-feedback.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})

test('Portal dark and narrow security view supports keyboard and reduced motion', async ({
  context,
  page,
}, info) => {
  const { evidence } = await mocks(context, page)
  await page.goto('/settings/source-control')
  await page.getByRole('button', { name: '切换到深色模式' }).click()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/portal/security')
  await expect(page.getByText(security.principal.email).first()).toBeVisible()
  await verifyGeometry(page, page.getByRole('button', { name: '刷新安全记录' }))
  await page.keyboard.press('Tab')
  await page.screenshot({
    path: info.outputPath('portal-security-narrow.png'),
    fullPage: true,
    animations: 'disabled',
  })
  verifyBrowserEvidence(evidence)
})
