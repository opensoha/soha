/** @vitest-environment jsdom */

import { act } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PermissionSnapshot } from '@/types'
import { AccessPoliciesPage } from './policies/page'
import { AccessRolesPage } from './roles/page'
import { AccessTeamsPage } from './teams/page'
import { ApiError } from '@/services/api-error'
import permissionCatalog from '@opensoha/contracts/auth/permission-catalog.json'

const testState = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: [],
    visibleMenuIds: [],
    visibleMenus: [],
  } as PermissionSnapshot,
  responses: {} as Record<string, unknown>,
}))

const apiPostMock = vi.hoisted(() => vi.fn())
const apiPutMock = vi.hoisted(() => vi.fn())
const apiDeleteMock = vi.hoisted(() => vi.fn())

const apiGetMock = vi.hoisted(() =>
  vi.fn((path: string) => Promise.resolve({ data: testState.responses[path] ?? [] })),
)

vi.mock('@/features/auth/permission-snapshot', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth/permission-snapshot')>(
    '@/features/auth/permission-snapshot',
  )
  return {
    ...actual,
    usePermissionSnapshot: () => ({
      data: { data: testState.snapshot },
      isLoading: false,
    }),
  }
})

vi.mock('@/services/api-client', () => ({
  api: {
    get: apiGetMock,
    post: apiPostMock,
    put: apiPutMock,
    delete: apiDeleteMock,
  },
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function setSnapshot(
  permissionKeys: string[],
  visibleMenuIds: string[] = [],
  visibleMenus: PermissionSnapshot['visibleMenus'] = [],
) {
  testState.snapshot = {
    permissionKeys,
    visibleMenuIds,
    visibleMenus,
  }
}

function setDefaultResponses() {
  testState.responses = {
    '/access/users': [],
    '/access/roles': [],
    '/access/teams': [],
    '/access/policies': [],
    '/applications': [],
    '/application-environments': [],
    '/clusters': [],
  }
}

async function renderWithProviders(node: React.ReactNode, route: string) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)

  const root = createRoot(container)
  roots.push(root)

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  })

  await act(async () => {
    root.render(
      <AntdApp>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter initialEntries={[route]}>{node}</MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })

  await act(async () => {
    await vi.advanceTimersByTimeAsync(1)
  })

  return container
}

function getButtonTexts(container: HTMLElement) {
  return Array.from(container.querySelectorAll('button'))
    .map((node) => node.textContent?.trim() ?? '')
    .filter(Boolean)
}

async function clickButton(container: HTMLElement, label: string) {
  const button = Array.from(container.querySelectorAll('button')).find(
    (node) => node.textContent?.replace(/\s/g, '') === label || node.ariaLabel === label,
  )
  expect(button, label).toBeDefined()
  await act(async () => {
    button!.click()
    await vi.advanceTimersByTimeAsync(300)
  })
}

async function fillInput(input: HTMLInputElement, value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

const roleFixture = {
  id: 'role-1',
  name: 'Support',
  scope: 'custom',
  permissionKeys: ['access.roles.view', 'future.permission.view'],
  userCount: 2,
}

describe('frontend access authorization splits', () => {
  beforeAll(() => {
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        matches: false,
        media: '',
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    })
    const getComputedStyle = window.getComputedStyle.bind(window)
    Object.defineProperty(window, 'getComputedStyle', {
      value: (element: Element) => getComputedStyle(element),
    })
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  beforeEach(() => {
    vi.useFakeTimers()
    setSnapshot([])
    setDefaultResponses()
  })

  afterEach(async () => {
    await act(async () => {
      for (const root of roots) {
        root.unmount()
      }
    })
    roots = []
    for (const container of containers) {
      container.remove()
    }
    containers = []
    vi.useRealTimers()
    vi.clearAllMocks()
  })

  it('shows the role page and hides manage actions for view-only role access', async () => {
    setSnapshot(
      ['access.roles.view'],
      ['access-roles'],
      [{ id: 'access-roles', path: '/access/roles' }],
    )

    const container = await renderWithProviders(<AccessRolesPage />, '/access/roles')

    expect(container.textContent).toContain('关键词')
    expect(getButtonTexts(container)).not.toContain('添加角色')
  })

  it('does not load roles or the permission catalog without role view access', async () => {
    const container = await renderWithProviders(<AccessRolesPage />, '/access/roles')

    expect(container.textContent).toContain('当前账号没有角色管理权限')
    expect(apiGetMock).not.toHaveBeenCalledWith('/access/roles')
    expect(apiGetMock).not.toHaveBeenCalledWith('/access/permissions')
  })

  it('shows the role create action when the create permission is present', async () => {
    setSnapshot(
      ['access.roles.view', 'access.roles.create'],
      ['access-roles'],
      [{ id: 'access-roles', path: '/access/roles' }],
    )

    const container = await renderWithProviders(<AccessRolesPage />, '/access/roles')

    expect(container.textContent).toContain('关键词')
    expect(getButtonTexts(container)).toContain('添加角色')
  })

  it('filters roles by name, scope and permission key, and resets without losing table controls', async () => {
    setSnapshot(['access.roles.view'])
    testState.responses['/access/roles'] = [
      roleFixture,
      { ...roleFixture, id: 'role-2', name: 'Auditor', scope: 'system', permissionKeys: [] },
    ]
    const container = await renderWithProviders(<AccessRolesPage />, '/access/roles')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    const table = container.querySelector('table')!
    const search = container.querySelector<HTMLInputElement>(
      'input[placeholder="搜索角色、范围或权限键"]',
    )!
    expect(table.textContent).toContain('Support')
    expect(table.textContent).toContain('Auditor')
    for (const keyword of ['support', 'custom', 'future.permission.view']) {
      await fillInput(search, keyword)
      expect(table.textContent).toContain('Support')
      expect(table.textContent).not.toContain('Auditor')
    }
    await fillInput(search, 'no-matching-role')
    expect(table.textContent).not.toContain('Support')
    await clickButton(container, '重置')
    expect(search.value).toBe('')
    expect(table.textContent).toContain('Support')
    expect(table.textContent).toContain('Auditor')
    await clickButton(container, '切换角色表格密度')
    expect(container.querySelector('.ant-table-medium')).not.toBeNull()
    await clickButton(container, '切换角色表格密度')
    expect(container.querySelector('.ant-table-small')).not.toBeNull()
    expect(container.querySelector('[aria-label="编辑角色"]')).toBeNull()
    expect(container.querySelector('[aria-label="删除角色"]')).toBeNull()
  })

  it('keeps edited permissions and compatibility keys through a failed update and refreshes after retry', async () => {
    setSnapshot(['access.roles.view', 'access.roles.update'])
    testState.responses['/access/roles'] = [roleFixture]
    testState.responses['/access/permissions'] = {
      permissions: permissionCatalog.permissions.filter((entry) =>
        ['access.roles.view', 'access.roles.update'].includes(entry.key),
      ),
    }
    apiPutMock.mockRejectedValueOnce(
      new ApiError(409, '角色已被更新', { requestId: 'req-role-edit' }),
    )
    const container = await renderWithProviders(<AccessRolesPage />, '/access/roles')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    await clickButton(container, '编辑角色')
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
    const name = dialog.querySelector<HTMLInputElement>('input[id="name"]')!
    expect(name.value).toBe('Support')
    expect(dialog.textContent).toContain('自定义角色')
    expect(dialog.textContent).toContain('兼容权限 1（只读）')
    await clickButton(dialog, '设置中心1/2')
    const actions = Array.from(
      dialog.querySelectorAll<HTMLInputElement>('.is-actions-list input[type="checkbox"]'),
    )
    expect(actions).toHaveLength(2)
    expect(actions.filter((input) => input.checked)).toHaveLength(1)
    await act(async () => {
      actions.find((input) => !input.checked)!.click()
    })
    await fillInput(name, ' Support updated ')
    await clickButton(dialog, '更新')
    const values = {
      name: 'Support updated',
      scope: 'custom',
      capabilities: [],
      permissionKeys: ['access.roles.update', 'access.roles.view', 'future.permission.view'],
    }
    expect(apiPutMock).toHaveBeenCalledWith('/access/roles/role-1', values)
    expect(dialog.textContent).toContain('角色已被更新')
    expect(dialog.textContent).toContain('req-role-edit')
    expect(name.value).toBe(' Support updated ')
    expect(actions.every((input) => input.checked)).toBe(true)
    apiPutMock.mockImplementationOnce(async () => {
      testState.responses['/access/roles'] = [{ ...roleFixture, ...values }]
      return { data: {} }
    })
    await clickButton(dialog, '更新')
    expect(apiPutMock).toHaveBeenCalledTimes(2)
    expect(document.querySelector('[role="dialog"]')).toBeNull()
    expect(container.querySelector('table')?.textContent).toContain('Support updated')
    expect(apiGetMock.mock.calls.filter(([path]) => path === '/access/roles')).toHaveLength(2)
  })

  it('only deletes a role after confirmation and refreshes the list', async () => {
    setSnapshot(['access.roles.view', 'access.roles.delete'])
    testState.responses['/access/roles'] = [roleFixture]
    apiDeleteMock.mockImplementationOnce(async () => {
      testState.responses['/access/roles'] = []
      return { data: {} }
    })
    const container = await renderWithProviders(<AccessRolesPage />, '/access/roles')
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300)
    })
    await clickButton(container, '删除角色')
    expect(document.body.textContent).toContain('确认删除？')
    expect(apiDeleteMock).not.toHaveBeenCalled()
    await clickButton(document.querySelector<HTMLElement>('.ant-popconfirm')!, 'Cancel')
    expect(apiDeleteMock).not.toHaveBeenCalled()
    expect(container.querySelector('table')?.textContent).toContain('Support')
    await clickButton(container, '删除角色')
    await clickButton(document.querySelector<HTMLElement>('.ant-popconfirm')!, 'OK')
    expect(apiDeleteMock).toHaveBeenCalledExactlyOnceWith('/access/roles/role-1')
    expect(container.querySelector('table')?.textContent).not.toContain('Support')
    expect(apiGetMock.mock.calls.filter(([path]) => path === '/access/roles')).toHaveLength(2)
  })

  it.each([
    ['roles', '角色', 'access.roles', AccessRolesPage],
    ['teams', '组织', 'access.groups', AccessTeamsPage],
  ] as const)(
    'preserves %s input after a failed save and prevents closing while saving',
    async (resource, label, permission, Page) => {
      setSnapshot([`${permission}.view`, `${permission}.create`])
      let rejectSave!: (error: Error) => void
      apiPostMock.mockImplementationOnce(
        () =>
          new Promise((_resolve, reject) => {
            rejectSave = reject
          }),
      )
      const container = await renderWithProviders(<Page />, `/access/${resource}`)
      await act(async () => {
        Array.from(container.querySelectorAll('button'))
          .find((button) => button.textContent?.trim() === `添加${label}`)!
          .click()
      })
      const dialog = document.querySelector<HTMLElement>('[role="dialog"]')!
      const field = dialog.querySelector<HTMLInputElement>('input[id="name"]')!
      const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
      await act(async () => {
        setValue.call(field, 'Test resource')
        field.dispatchEvent(new Event('input', { bubbles: true }))
      })
      const submit = Array.from(dialog.querySelectorAll('button')).find(
        (button) => button.textContent?.replace(/\s/g, '') === '创建',
      )!
      await act(async () => {
        submit.click()
      })
      await act(async () => {
        await vi.advanceTimersByTimeAsync(300)
      })
      expect(apiPostMock).toHaveBeenCalledTimes(1)
      expect(dialog.textContent).toContain('保存：正在提交请求')
      expect(field.disabled).toBe(true)
      expect(dialog.querySelector<HTMLButtonElement>('.ant-modal-close')?.disabled).toBe(true)
      const cancel = Array.from(dialog.querySelectorAll('button')).find(
        (button) => button.textContent?.replace(/\s/g, '') === '取消',
      )!
      expect(cancel.disabled).toBe(true)
      await act(async () => {
        cancel.click()
        dialog.dispatchEvent(
          new KeyboardEvent('keydown', { key: 'Escape', keyCode: 27, bubbles: true }),
        )
      })
      expect(document.querySelector('[role="dialog"]')).toBe(dialog)
      await act(async () => {
        rejectSave(new ApiError(409, '名称已存在', { requestId: 'req-save' }))
        await vi.advanceTimersByTimeAsync(300)
      })
      expect(dialog.textContent).toContain('保存：操作请求失败')
      expect(dialog.textContent).toContain('名称已存在')
      expect(dialog.textContent).toContain('req-save')
      expect(field.value).toBe('Test resource')
      expect(field.disabled).toBe(false)
      expect(document.querySelector('.ant-message-error')).toBeNull()
      apiPostMock.mockResolvedValueOnce({ data: {} })
      await act(async () => {
        submit.click()
      })
      await act(async () => {
        await vi.advanceTimersByTimeAsync(300)
      })
      expect(apiPostMock).toHaveBeenCalledTimes(2)
      expect(document.querySelector('[role="dialog"]')).toBeNull()
    },
  )

  it('keeps common policy fields visible and places low-frequency conditions in advanced options', async () => {
    setSnapshot(['access.policies.view', 'access.policies.create'])

    const container = await renderWithProviders(<AccessPoliciesPage />, '/access/policies')
    const createButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.trim() === '添加策略',
    )

    await act(async () => {
      createButton?.click()
    })

    expect(document.body.textContent).toContain('适用主体')
    expect(document.body.textContent).toContain('目标范围')
    expect(document.body.textContent).toContain('高级条件')
    expect(document.body.textContent).not.toContain('主体标签')
    expect(document.body.textContent).not.toContain('归属组织')
    expect(document.body.textContent).not.toContain('资源名称')

    const advancedButton = document.body.querySelector<HTMLElement>(
      '[role="button"][aria-expanded="false"]',
    )
    await act(async () => {
      advancedButton?.click()
    })

    expect(document.body.textContent).toContain('主体标签')
    expect(document.body.textContent).toContain('集群地域')
    expect(document.body.textContent).toContain('请求来源')
    expect(document.body.textContent).toContain('审批状态')
  })

  it('does not load policy role and team options without their view permissions', async () => {
    setSnapshot(['access.policies.view'])

    await renderWithProviders(<AccessPoliciesPage />, '/access/policies')

    expect(apiGetMock).toHaveBeenCalledWith('/access/policies')
    expect(apiGetMock).not.toHaveBeenCalledWith('/access/roles')
    expect(apiGetMock).not.toHaveBeenCalledWith('/access/teams')
  })

  it('loads policy team options with the group view permission', async () => {
    setSnapshot(['access.policies.view', 'access.groups.view'])

    await renderWithProviders(<AccessPoliciesPage />, '/access/policies')

    expect(apiGetMock).toHaveBeenCalledWith('/access/teams')
  })
})
