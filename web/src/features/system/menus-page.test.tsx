/** @vitest-environment jsdom */

import { act } from 'react'
import type { ReactNode } from 'react'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PermissionSnapshot } from '@/types'
import { MenusPage } from './system-pages'

const testState = vi.hoisted(() => ({
  snapshot: {
    permissionKeys: ['system.menus.view', 'system.menus.manage'],
    visibleMenuIds: ['menus'],
    visibleMenus: [],
  } as PermissionSnapshot,
  responses: {} as Record<string, unknown>,
}))

const apiGetMock = vi.hoisted(() => vi.fn((path: string) => Promise.resolve({ data: testState.responses[path] ?? [] })))
const apiPutMock = vi.hoisted(() => vi.fn(() => Promise.resolve({ data: {} })))

vi.mock('@/features/auth/permission-snapshot', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth/permission-snapshot')>('@/features/auth/permission-snapshot')
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
    post: vi.fn(),
    put: apiPutMock,
    delete: vi.fn(),
  },
}))

vi.mock('@/components/admin-table', () => ({
  AdminTable: ({ dataSource, columns, title, headerExtra, toolbar }: { dataSource: unknown[]; columns: any[]; title?: ReactNode; headerExtra?: ReactNode; toolbar?: ReactNode }) => (
    <div data-testid="admin-table">
      {title ? <div>{title}</div> : null}
      {headerExtra ? <div>{headerExtra}</div> : null}
      {toolbar ? <div>{toolbar}</div> : null}
      {(function renderRows(items: Array<Record<string, unknown>>, prefix = ''): ReactNode {
        return items.map((record, rowIndex) => (
          <div key={`${prefix}${String(record.id ?? rowIndex)}`} data-testid={`row-${String(record.id ?? rowIndex)}`}>
            {columns.map((column, columnIndex) => {
              const dataIndex = column?.dataIndex
              const value = typeof dataIndex === 'string' ? record[dataIndex] : undefined
              const content = column?.render ? column.render(value, record, rowIndex) : String(value ?? '')
              return <div key={`${String(dataIndex ?? columnIndex)}:${columnIndex}`}>{content}</div>
            })}
            {Array.isArray(record.children) ? renderRows(record.children as Array<Record<string, unknown>>, `${String(record.id ?? rowIndex)}:`) : null}
          </div>
        ))
      })(dataSource as Array<Record<string, unknown>>)}
    </div>
  ),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function setDefaultResponses() {
  testState.responses = {
    '/menus': [
      {
        id: 'system',
        labelZh: '系统',
        labelEn: 'System',
        path: '/system',
        iconKey: 'panels-top-left',
        section: 'control',
        sortOrder: 225,
        enabled: true,
      },
      {
        id: 'configuration',
        labelZh: '配置',
        labelEn: 'Configuration',
        path: '/configuration',
        iconKey: 'cog',
        section: 'Dashboard',
        sortOrder: 40,
        enabled: true,
      },
      {
        id: 'virtualization-workbench',
        labelZh: '虚拟化管理工作台',
        labelEn: 'Virtualization',
        path: '/virtualization',
        iconKey: 'server',
        section: 'Dashboard',
        sortOrder: 50,
        enabled: true,
      },
    ],
    '/access/roles': [],
  }
}

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
    await Promise.resolve()
  })
}

async function renderWithProviders(node: ReactNode, route: string) {
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
          <MemoryRouter
            initialEntries={[route]}
            future={{
              v7_startTransition: true,
              v7_relativeSplatPath: true,
            }}
          >
            {node}
          </MemoryRouter>
        </QueryClientProvider>
      </AntdApp>,
    )
  })

  await flushAsyncWork()

  return container
}

function getRowEditButton(rowId: string) {
  const row = document.querySelector(`[data-testid="row-${rowId}"]`)
  if (!(row instanceof HTMLElement)) {
    throw new Error(`row not found: ${rowId}`)
  }

  const button = row.querySelector('button')
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error(`edit button not found: ${rowId}`)
  }

  return button
}

function getVisibleInputValues() {
  return Array.from(document.querySelectorAll('input'))
    .map((node) => node.value.trim())
    .filter(Boolean)
}

async function clickButton(node: HTMLButtonElement) {
  await act(async () => {
    node.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })
  await flushAsyncWork()
}

async function clickButtonByText(text: string) {
  const button = Array.from(document.querySelectorAll('button'))
    .find((node) => node.textContent?.replace(/\s+/g, '').includes(text))
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error(`button not found: ${text}`)
  }
  await clickButton(button)
}

describe('menus page modal state', () => {
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

    Object.defineProperty(window.HTMLElement.prototype, 'scrollIntoView', {
      writable: true,
      value: vi.fn(),
    })

    const originalGetComputedStyle = window.getComputedStyle.bind(window)
    Object.defineProperty(window, 'getComputedStyle', {
      writable: true,
      value: vi.fn().mockImplementation((element: Element) => originalGetComputedStyle(element)),
    })

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  beforeEach(() => {
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
    vi.clearAllMocks()
  })

  it('rebinds modal field values after updating one menu and opening another', async () => {
    await renderWithProviders(<MenusPage />, '/system/menus')

    await clickButton(getRowEditButton('system'))
    expect(getVisibleInputValues()).toContain('系统')
    expect(getVisibleInputValues()).toContain('/system')

    await clickButtonByText('更新')
    expect(apiPutMock).toHaveBeenCalledWith('/menus/system', expect.any(Object))

    await clickButton(getRowEditButton('configuration'))

    const inputValues = getVisibleInputValues()
    expect(inputValues).toContain('配置')
    expect(inputValues).toContain('Configuration')
    expect(inputValues).toContain('/configuration')
    expect(inputValues).not.toContain('系统')
    expect(inputValues).not.toContain('/system')
  })

  it('shows derived workbench ownership for menu rows and edit state', async () => {
    await renderWithProviders(<MenusPage />, '/system/menus')

    expect(document.body.textContent).toContain('k8s工作台')
    expect(document.body.textContent).toContain('虚拟化管理工作台')
    expect(document.body.textContent).toContain('设置中心')

    await clickButton(getRowEditButton('configuration'))

    expect(document.body.textContent).toContain('当前菜单会在 k8s工作台 的导航树内展示。')

    await clickButton(getRowEditButton('virtualization-workbench'))

    expect(document.body.textContent).toContain('当前菜单会在 虚拟化管理工作台 的导航树内展示。')
  })

  it('submits an empty section as an ungrouped menu', async () => {
    testState.responses['/menus'] = [
      {
        id: 'configuration',
        labelZh: '配置',
        labelEn: 'Configuration',
        path: '/configuration',
        iconKey: 'cog',
        section: '',
        sortOrder: 40,
        enabled: true,
      },
    ]
    await renderWithProviders(<MenusPage />, '/system/menus')

    expect(document.body.textContent).toContain('工作台视图')
    expect(document.body.textContent).toContain('未分组')

    await clickButton(getRowEditButton('configuration'))
    await clickButtonByText('更新')

    expect(apiPutMock).toHaveBeenCalledWith('/menus/configuration', expect.objectContaining({ section: '' }))
  })
})
