import { test, expect, type BrowserContext, type Page } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { verifyGeometry } from '../fixtures/geometry'

async function governanceMocks(context: BrowserContext, page: Page) {
  const state = {
    readStatus: 200,
    failedPath: '',
    revokedPermissions: [] as string[],
    writeStatus: 503,
    previewStatus: 200,
    writes: 0,
    syncWrites: 0,
    reads: 0,
    runReads: 0,
  }
  const failure = (status: number) => ({
    status,
    body: {
      error: {
        code: 'unavailable',
        message: '治理请求暂时不可用',
        request_id: 'b4-request-original',
      },
    },
  })
  const roles = [
    {
      id: 'role-ops',
      name: '生产只读与故障诊断',
      scope: 'custom',
      permissionKeys: ['platform.deployment.view'],
      capabilities: [],
      userCount: 3,
    },
  ]
  const teams = [
    {
      id: 'team-platform',
      name: '平台工程团队',
      slug: 'platform',
      path: '/platform',
      metadata: {},
      userCount: 3,
    },
  ]
  const users = [
    {
      id: 'user-ops',
      username: 'operations',
      displayName: '平台值班员',
      email: 'operations@example.test',
      status: 'active',
      roles: ['role-ops'],
      teams: ['team-platform'],
      tags: [],
      projects: [],
      loginSources: [{ type: 'local' }],
    },
  ]
  const connection = {
    id: 'directory-main',
    name: '企业通讯录 · 组织与人员',
    providerType: 'feishu',
    enabled: true,
    status: 'healthy',
    capabilities: ['organizations', 'events'],
    policy: {
      syncOrganizations: true,
      syncPeople: false,
      mode: 'scheduled_and_realtime',
      provisionMode: 'review_before_link',
      userDisablePolicy: 'managed_only',
      missingObjectPolicy: 'archive',
    },
  }
  const evidence = await installMocks(context, page, (url, method) => {
    const path = url.pathname
    if (path === '/api/v1/access/permission-snapshot')
      return {
        body: {
          data: {
            permissionKeys: [
              'workbench.settings.view',
              'access.users.view',
              'access.users.create',
              'access.users.update',
              'access.users.delete',
              'access.users.replace-roles',
              'access.users.replace-teams',
              'access.roles.view',
              'access.roles.create',
              'access.roles.update',
              'access.roles.delete',
              'access.groups.view',
              'access.groups.create',
              'access.groups.update',
              'access.groups.delete',
              'access.policies.view',
              'access.policies.create',
              'access.policies.update',
              'access.policies.delete',
              'access.scope-grants.view',
              'access.scope-grants.create',
              'access.scope-grants.update',
              'access.scope-grants.delete',
              'access.directory.view',
              'access.directory.create',
              'access.directory.update',
              'access.directory.validate',
              'access.directory.sync',
              'access.directory.people.resolve',
              'settings.identity.view',
              'delivery.applications.view',
              'delivery.application-environments.view',
              'platform.clusters.view',
            ].filter((permission) => !state.revokedPermissions.includes(permission)),
            visibleMenuIds: [
              'access-users',
              'access-roles',
              'access-teams',
              'access-policies',
              'access-directory-sync',
            ],
            visibleMenus: ['users', 'roles', 'teams', 'policies', 'directory-sync'].map((name) => ({
              id: `access-${name}`,
              path: `/access/${name}`,
              labelZh: {
                users: '用户',
                roles: '角色',
                teams: '组织',
                policies: '策略',
                'directory-sync': '目录同步',
              }[name],
            })),
          },
        },
      }
    if (path === '/api/v1/access/permissions')
      return {
        body: {
          data: {
            permissions: [
              {
                key: 'platform.deployment.view',
                name: '查看 Deployment',
                description: '读取 Deployment',
                resource: 'deployment',
                action: 'view',
              },
            ],
          },
        },
      }
    if (path === '/api/v1/settings/identity')
      return {
        body: {
          data: {
            providers: [{ id: 'feishu-main', name: '企业飞书', type: 'feishu', enabled: true }],
          },
        },
      }
    if (path === '/api/v1/identity/users/user-ops/mfa/reset') {
      state.writes++
      return failure(state.writeStatus)
    }
    if (path.startsWith('/api/v1/access/')) {
      if (method === 'GET') {
        state.reads++
        if (state.readStatus !== 200 && (!state.failedPath || path.endsWith(state.failedPath)))
          return failure(state.readStatus)
        if (path === '/api/v1/access/users') return { body: { data: users } }
        if (path === '/api/v1/access/roles') return { body: { data: roles } }
        if (path === '/api/v1/access/teams') return { body: { data: teams } }
        if (path === '/api/v1/access/policies')
          return {
            body: {
              data: [
                {
                  id: 'policy-main',
                  name: '生产命名空间只读',
                  effect: 'allow',
                  priority: 10,
                  actions: ['view'],
                  subjects: { roles: ['role-ops'] },
                  namespaces: { names: ['production'] },
                  reason: '生产环境审阅',
                },
              ],
            },
          }
        if (path.endsWith('/scope-grants'))
          return {
            body: {
              data: [
                {
                  id: 'grant-platform',
                  subjectId: 'user-ops',
                  subjectType: 'user',
                  scopeType: 'platform',
                  clusterIds: ['cluster-a'],
                  role: 'role-ops',
                  effect: 'allow',
                  enabled: true,
                },
              ],
            },
          }
        if (path === '/api/v1/access/directory-connections') return { body: { data: [connection] } }
        if (path.endsWith('/runs')) {
          state.runReads++
          return {
            body: {
              data: [
                {
                  id: 'run-directory-1',
                  connectionId: connection.id,
                  trigger: 'manual',
                  status: 'queued',
                },
              ],
            },
          }
        }
        if (path.endsWith('/runtime-status'))
          return {
            body: {
              data: {
                connectionId: connection.id,
                callbackConfigured: true,
                callbackStatus: 'verified',
                queuedEvents: 2,
                failedEvents: 1,
                needsFullReconcile: true,
                reconcileReason: '检测到上游事件序列缺口',
              },
            },
          }
        if (path.endsWith('/events'))
          return {
            body: {
              data: [
                {
                  id: 'event-1',
                  providerEventId: 'upstream-1001',
                  eventType: 'department.updated',
                  occurredAt: '2026-10-16T04:30:00Z',
                  status: 'failed',
                  attempts: 1,
                  errorSummary: '等待上游连接恢复',
                },
              ],
            },
          }
        if (path === '/api/v1/access/directory-conflicts') return { body: { data: [] } }
      }
      if (method === 'POST' && path.endsWith('/sync/preview'))
        return state.previewStatus === 200
          ? {
              body: {
                data: {
                  connectionId: connection.id,
                  organizations: { create: 2, update: 3, archive: 0 },
                  generatedAt: '2026-10-16T04:30:00Z',
                },
              },
            }
          : failure(state.previewStatus)
      if (method === 'POST' && path.endsWith('/validate'))
        return { body: { data: { valid: true, message: '连接验证通过' } } }
      if (method !== 'GET') {
        state.writes++
        if (path.endsWith('/sync')) state.syncWrites++
        return state.writeStatus === 200
          ? {
              body: {
                data: {
                  id: 'run-directory-1',
                  connectionId: connection.id,
                  trigger: 'manual',
                  status: 'queued',
                },
              },
            }
          : failure(state.writeStatus)
      }
    }
    return undefined
  })
  return { state, evidence }
}

for (const mode of ['light', 'dark', 'narrow'] as const) {
  test(`B4 governance preserves search, old evidence and permissions (${mode})`, async ({
    context,
    page,
  }, info) => {
    if (mode === 'dark')
      await context.addInitScript(() =>
        localStorage.setItem(
          'soha-prefs',
          JSON.stringify({ state: { themeMode: 'dark' }, version: 0 }),
        ),
      )
    if (mode === 'narrow') {
      await page.setViewportSize({ width: 390, height: 1000 })
      await page.emulateMedia({ reducedMotion: 'reduce' })
    }
    const { state, evidence } = await governanceMocks(context, page)
    await page.goto('/access/users')
    await expect(page.getByText('平台值班员', { exact: true })).toBeVisible()
    const keyword = page.getByPlaceholder('搜索用户名、显示名、邮箱、角色或组织')
    await keyword.fill('operations')
    state.readStatus = 503
    await page.getByRole('button', { name: '刷新用户', exact: true }).click()
    await expect(page.getByText('列表未更新，正在展示上次读取结果')).toBeVisible()
    await expect(keyword).toHaveValue('operations')
    await expect(page.getByText('平台值班员', { exact: true })).toBeVisible()
    await page.screenshot({ path: info.outputPath(`b4-users-${mode}.png`), fullPage: true })
    await verifyGeometry(page, page.getByRole('button', { name: '刷新用户', exact: true }))
    state.readStatus = 403
    await page.getByRole('button', { name: '刷新用户', exact: true }).click()
    await expect(page.getByText('列表无访问权限')).toBeVisible()
    await expect(page.getByText('平台值班员', { exact: true })).toHaveCount(0)
    await expect(page.getByText('平台工程团队', { exact: true })).toHaveCount(0)
    verifyBrowserEvidence(evidence)
  })
}

test('B4 role draft survives uncertain save and reopen without another write', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await governanceMocks(context, page)
  await page.goto('/access/roles')
  await page.getByRole('button', { name: '添加角色', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('角色名称', { exact: true }).fill('生产巡检角色草稿')
  await dialog.getByRole('button', { name: '创建', exact: true }).click()
  await expect(dialog.getByText('保存：请求结果不明')).toBeVisible()
  await expect(dialog.getByText('b4-request-original', { exact: false })).toBeVisible()
  await expect(dialog.getByRole('button', { name: '创建', exact: true })).toBeDisabled()
  await expect
    .poll(() =>
      dialog.evaluate(
        (element) =>
          element.getAnimations().filter((animation) => animation.playState === 'running').length,
      ),
    )
    .toBe(0)
  await page.screenshot({ path: info.outputPath('b4-role-draft.png'), fullPage: true })
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('button', { name: '查看待核对草稿', exact: true }).click()
  await expect(dialog.getByLabel('角色名称', { exact: true })).toHaveValue('生产巡检角色草稿')
  await dialog.getByRole('button', { name: '核对列表记录', exact: true }).click()
  await expect.poll(() => state.reads).toBeGreaterThan(1)
  expect(state.writes).toBe(1)
  verifyBrowserEvidence(evidence)
})

test('B4 MFA reset keeps confirmation and locks an unknown original target', async ({
  context,
  page,
}) => {
  const { state, evidence } = await governanceMocks(context, page)
  await page.goto('/access/users')
  await page.getByRole('button', { name: '重置 MFA', exact: true }).click()
  await expect(page.getByText('确认重置 operations 的 MFA？')).toBeVisible()
  expect(state.writes).toBe(0)
  await page.getByRole('button', { name: '确认重置 MFA', exact: true }).click()
  await expect(page.getByText('重置 MFA：请求结果不明')).toBeVisible()
  await expect(page.getByRole('button', { name: '重置 MFA', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '刷新用户', exact: true }).click()
  expect(state.writes).toBe(1)
  await expect(page.getByText('user-ops', { exact: true })).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('B4 scope editor retains the draft and uses only reads to check an unknown save', async ({
  context,
  page,
}) => {
  const { state, evidence } = await governanceMocks(context, page)
  await page.goto('/access/users')
  await page.getByRole('button', { name: '授权范围', exact: true }).click()
  await page.getByRole('button', { name: '编辑授权项', exact: true }).click()
  const editor = page
    .getByRole('dialog')
    .filter({ has: page.getByRole('button', { name: '更新', exact: true }) })
  await editor.getByRole('button', { name: '更新', exact: true }).click()
  await expect(editor.getByText('保存：请求结果不明')).toBeVisible()
  await expect(editor.getByRole('button', { name: '更新', exact: true })).toBeDisabled()
  await editor.getByRole('button', { name: '核对列表记录', exact: true }).click()
  expect(state.writes).toBe(1)
  verifyBrowserEvidence(evidence)
})

test('B4 directory preview retries reads, then exposes the returned running evidence', async ({
  context,
  page,
}, info) => {
  const { state, evidence } = await governanceMocks(context, page)
  await page.goto('/access/directory-sync')
  state.previewStatus = 503
  await page.getByRole('button', { name: '预览变更', exact: true }).click()
  const preview = page.getByRole('dialog')
  await expect(preview.getByText('读取同步预览：操作请求失败')).toBeVisible()
  expect(state.writes).toBe(0)
  state.previewStatus = 200
  await preview.getByRole('button', { name: '重试读取预览', exact: true }).click()
  await expect(preview.getByText('当前仅为预览，尚未写入组织或用户数据')).toBeVisible()
  state.writeStatus = 200
  await preview.getByRole('button', { name: '立即同步', exact: true }).click()
  const runtime = page.getByRole('dialog', {
    name: '企业通讯录 · 组织与人员 · 运行与事件',
    exact: true,
  })
  await expect(runtime.getByText('已返回同步运行记录')).toBeVisible()
  await expect(runtime.getByText('run-directory-1', { exact: true })).toBeVisible()
  await expect(runtime.getByText('同步已完成，组织数据已更新')).toHaveCount(0)
  await expect(runtime.getByRole('tab', { name: '运行历史' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await runtime.getByRole('tab', { name: '实时事件' }).click()
  await runtime.getByRole('button', { name: '查看运行记录', exact: true }).click()
  await expect(runtime.getByRole('tab', { name: '运行历史' })).toHaveAttribute(
    'aria-selected',
    'true',
  )
  await expect(
    runtime.getByRole('tabpanel', { name: '运行历史' }).getByText('已排队', { exact: true }),
  ).toBeVisible()
  await page.screenshot({ path: info.outputPath('b4-directory-run.png'), fullPage: true })
  expect(state.syncWrites).toBe(1)
  verifyBrowserEvidence(evidence)
})

test('B4 unknown directory submission stays bound to its target and refresh never syncs', async ({
  context,
  page,
}) => {
  const { state, evidence } = await governanceMocks(context, page)
  await page.goto('/access/directory-sync')
  await page.getByRole('button', { name: '立即同步', exact: true }).click()
  await expect(page.getByText('目录同步：请求结果不明')).toBeVisible()
  await expect(page.getByRole('button', { name: '立即同步', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '查看同步记录', exact: true }).click()
  const runtime = page.getByRole('dialog', {
    name: '企业通讯录 · 组织与人员 · 运行与事件',
    exact: true,
  })
  await runtime.getByRole('button', { name: '刷新运行记录', exact: true }).click()
  await expect.poll(() => state.runReads).toBeGreaterThan(1)
  expect(state.syncWrites).toBe(1)
  state.readStatus = 403
  state.failedPath = '/runtime-status'
  await runtime.getByRole('button', { name: '刷新运行记录', exact: true }).click()
  await expect(runtime.getByText('目录事件状态无访问权限')).toBeVisible()
  await expect(runtime.getByText('待处理 2')).toHaveCount(0)
  await expect(runtime.getByText('department.updated', { exact: true })).toBeVisible()
  verifyBrowserEvidence(evidence)
})

test('B4 rejects only the failed directory cache while keeping independently readable users', async ({
  context,
  page,
}) => {
  const { state, evidence } = await governanceMocks(context, page)
  await page.goto('/access/users')
  await expect(page.getByText('生产只读与故障诊断', { exact: true })).toBeVisible()
  state.failedPath = '/roles'
  state.readStatus = 403
  await page.getByRole('button', { name: '刷新用户', exact: true }).click()
  await expect(page.getByText('角色目录无访问权限')).toBeVisible()
  await expect(page.getByText('平台值班员', { exact: true })).toBeVisible()
  await expect(page.getByText('生产只读与故障诊断', { exact: true })).toHaveCount(0)
  state.failedPath = '/teams'
  await page.getByRole('button', { name: '刷新用户', exact: true }).click()
  await expect(page.getByText('组织目录无访问权限')).toBeVisible()
  await expect(page.getByText('平台工程团队', { exact: true })).toHaveCount(0)
  verifyBrowserEvidence(evidence)
})
test('B4 role editor supports keyboard submission and reduced motion on narrow screens', async ({
  context,
  page,
}) => {
  const { state, evidence } = await governanceMocks(context, page)
  await page.setViewportSize({ width: 390, height: 1000 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/access/roles')
  const addRole = page.getByRole('button', { name: '添加角色', exact: true })
  await expect(addRole).toBeEnabled()
  await addRole.press('Enter')
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await dialog.getByLabel('角色名称', { exact: true }).fill('键盘编辑草稿')
  const createRole = dialog.getByRole('button', { name: '创建', exact: true })
  await expect(createRole).toBeEnabled()
  await createRole.press('Enter')
  await expect(dialog.getByText('保存：请求结果不明')).toBeVisible()
  await expect(dialog.locator('.soha-operation-feedback')).toHaveAttribute(
    'data-motion-disabled',
    'true',
  )
  await verifyGeometry(page, dialog.getByRole('button', { name: '取消', exact: true }))
  expect(state.writes).toBe(1)
  verifyBrowserEvidence(evidence)
})

test('B4 cached grant read failure is visible inside the editor and blocks saving', async ({
  context,
  page,
}) => {
  const { state, evidence } = await governanceMocks(context, page)
  await page.goto('/access/users')
  await page.getByRole('button', { name: '授权范围', exact: true }).click()
  await expect(page.getByRole('button', { name: '编辑授权项', exact: true })).toBeVisible()
  state.failedPath = '/scope-grants'
  state.readStatus = 503
  await page.getByRole('button', { name: '刷新授权记录', exact: true }).click()
  await expect(page.getByText('列表未更新，正在展示上次读取结果')).toBeVisible()
  await page.getByRole('button', { name: '编辑授权项', exact: true }).click()
  const editor = page
    .getByRole('dialog')
    .filter({ has: page.getByRole('button', { name: '更新', exact: true }) })
  await expect(editor.getByText('授权记录未更新，正在展示上次读取结果')).toBeVisible()
  await expect(editor.getByRole('button', { name: '更新', exact: true })).toBeDisabled()
  expect(state.writes).toBe(0)
  verifyBrowserEvidence(evidence)
})
test('B4 a known grant rejection preserves the edited switch and permits correction', async ({
  context,
  page,
}) => {
  const { state, evidence } = await governanceMocks(context, page)
  state.writeStatus = 409
  await page.goto('/access/users')
  await page.getByRole('button', { name: '授权范围', exact: true }).click()
  await page.getByRole('button', { name: '编辑授权项', exact: true }).click()
  const editor = page
    .getByRole('dialog')
    .filter({ has: page.getByRole('button', { name: '更新', exact: true }) })
  await editor.getByRole('switch').click()
  await editor.getByRole('button', { name: '更新', exact: true }).click()
  await expect(editor.getByText('保存：操作请求失败')).toBeVisible()
  await expect(editor.getByRole('switch')).toHaveAttribute('aria-checked', 'false')
  await expect(editor.getByRole('button', { name: '更新', exact: true })).toBeEnabled()
  expect(state.writes).toBe(1)
  verifyBrowserEvidence(evidence)
})
test('B4 directory credential draft stays private and reopens its original unknown request', async ({
  context,
  page,
}) => {
  const { state, evidence } = await governanceMocks(context, page)
  await page.goto('/access/directory-sync')
  await page.getByRole('button', { name: '新增目录连接', exact: true }).click()
  const editor = page.getByRole('dialog')
  await editor.getByLabel('连接名称', { exact: true }).fill('LDAP 连接草稿')
  const provider = editor.getByLabel('目录类型', { exact: true })
  await provider.click()
  for (let i = 0; i < 3; i++) await provider.press('ArrowDown')
  await provider.press('Enter')
  await editor
    .getByLabel('LDAP Endpoint', { exact: true })
    .fill('ldaps://directory.example.test:636')
  await editor.getByLabel('Base DN', { exact: true }).fill('dc=example,dc=test')
  await editor.getByLabel('Bind Password', { exact: true }).fill('synthetic-secret-canary')
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await expect(editor.getByText('保存目录连接：请求结果不明')).toBeVisible()
  await expect(editor.locator('.soha-operation-feedback')).not.toContainText(
    'synthetic-secret-canary',
  )
  await expect(editor.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  await editor.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('button', { name: '查看待核对草稿', exact: true }).click()
  await expect(editor.getByLabel('连接名称', { exact: true })).toHaveValue('LDAP 连接草稿')
  expect(state.writes).toBe(1)
  verifyBrowserEvidence(evidence)
})

test('B4 stale directory records explain why saving is paused inside the connection editor', async ({
  context,
  page,
}) => {
  const { state, evidence } = await governanceMocks(context, page)
  await page.goto('/access/directory-sync')
  await expect(page.getByRole('button', { name: '编辑目录连接', exact: true })).toBeVisible()
  state.readStatus = 503
  state.failedPath = '/directory-connections'
  await page.getByRole('button', { name: '刷新目录同步', exact: true }).click()
  await expect(page.getByText('列表未更新，正在展示上次读取结果')).toBeVisible()
  await page.getByRole('button', { name: '编辑目录连接', exact: true }).click()
  const editor = page.getByRole('dialog')
  await expect(editor.getByText('目录连接未更新，正在展示上次读取结果')).toBeVisible()
  await expect(editor.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  expect(state.writes).toBe(0)
  verifyBrowserEvidence(evidence)
})

test('B4 revoked directory permissions hide cached options across the same client session', async ({
  context,
  page,
}) => {
  const { state, evidence } = await governanceMocks(context, page)
  await page.goto('/access/teams')
  await page.getByRole('button', { name: '编辑组织', exact: true }).click()
  let editor = page.getByRole('dialog')
  await editor.getByLabel('映射来源', { exact: true }).click()
  await expect(page.getByText(/企业飞书/).filter({ visible: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await editor.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('menuitem', { name: /用户$/ }).click()
  await expect(page.getByText('生产只读与故障诊断', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '编辑用户', exact: true }).click()
  editor = page.getByRole('dialog')
  state.writeStatus = 200
  state.revokedPermissions = [
    'access.roles.view',
    'settings.identity.view',
    'delivery.applications.view',
    'platform.clusters.view',
  ]
  await editor.getByRole('button', { name: '更新', exact: true }).click()
  await expect(editor).toBeHidden()
  await expect(page.getByText('生产只读与故障诊断', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '授权范围', exact: true }).click()
  await page.getByRole('button', { name: '编辑授权项', exact: true }).click()
  editor = page
    .getByRole('dialog')
    .filter({ has: page.getByRole('button', { name: '更新', exact: true }) })
  await expect(editor.getByLabel('范围内角色')).toBeDisabled()
  await expect(editor.getByText('生产只读与故障诊断', { exact: true })).toHaveCount(0)
  await editor.getByRole('button', { name: '取消', exact: true }).click()
  await expect(editor).toBeHidden()
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.getByRole('menuitem', { name: /组织$/ }).click()
  await page.getByRole('button', { name: '编辑组织', exact: true }).click()
  editor = page.getByRole('dialog')
  await editor.getByLabel('映射来源', { exact: true }).click()
  await expect(page.getByText(/企业飞书/)).toHaveCount(0)
  expect(state.writes).toBe(1)
  verifyBrowserEvidence(evidence)
})
