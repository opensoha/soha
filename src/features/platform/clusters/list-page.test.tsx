/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  act,
  Children,
  isValidElement,
  StrictMode,
  type ComponentProps,
  type InputHTMLAttributes,
  type ReactNode,
  type TextareaHTMLAttributes,
} from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { ClustersPage } from './list-page'

const storeMocks = vi.hoisted(() => ({ setClusterId: vi.fn() }))
const authMocks = vi.hoisted(() => ({ permissions: new Set<string>() }))
const formLifecycleMocks = vi.hoisted(() => ({
  destroyOnHidden: undefined as boolean | undefined,
  preserve: undefined as boolean | undefined,
  disabled: undefined as boolean | undefined,
  submit: undefined as ((values: Record<string, unknown>) => void) | undefined,
  realForm: false,
}))
const selectMocks = vi.hoisted(() => ({ setAgentMode: undefined as (() => void) | undefined }))
const apiGetMock = vi.hoisted(() =>
  vi.fn(async (path: string) => {
    if (path === '/clusters') {
      return {
        data: [
          {
            id: 'cluster-a',
            name: 'Primary',
            region: 'gke',
            environment: 'prod',
            labels: {},
            connectionMode: 'agent',
            version: 'v1.30',
            health: { status: 'healthy' },
          },
        ],
      }
    }
    if (path === '/clusters/cluster-a/detail') {
      return {
        data: {
          summary: { id: 'cluster-a' },
          connection: { mode: 'agent', endpoint: 'http://agent' },
          monitoring: { prometheus: {} },
        },
      }
    }
    return { data: [] }
  }),
)
const apiDeleteMock = vi.hoisted(() => vi.fn(async () => ({ data: null })))
const apiPostMock = vi.hoisted(() =>
  vi.fn(async (_path: string, _body?: unknown): Promise<{ data: unknown }> => ({ data: {} })),
)

vi.mock('@/services/api-client', () => ({
  api: {
    delete: apiDeleteMock,
    get: apiGetMock,
    post: apiPostMock,
    put: vi.fn(async () => ({ data: {} })),
  },
}))
vi.mock('@/features/auth', () => ({
  hasPermission: (_snapshot: unknown, permissionKey: string) =>
    authMocks.permissions.has(permissionKey),
  usePermissionSnapshot: () => ({ data: { data: {} } }),
}))
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: (selector?: (state: typeof storeMocks) => unknown) =>
    selector ? selector(storeMocks) : storeMocks,
}))
vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))
vi.mock('@/components/management-list', () => ({
  ManagementBatchBar: ({ children }: { children?: ReactNode }) => <>{children}</>,
  ManagementDensityButton: () => null,
  ManagementIconButton: ({
    'aria-label': ariaLabel,
    onClick,
  }: {
    'aria-label': string
    onClick?: () => void
  }) => (
    <button aria-label={ariaLabel} onClick={onClick}>
      {ariaLabel}
    </button>
  ),
  ManagementKeywordField: () => null,
  ManagementQueryActions: () => null,
  ManagementQueryField: ({ children }: { children?: ReactNode }) => <>{children}</>,
  ManagementQueryPanel: ({ children }: { children?: ReactNode }) => <>{children}</>,
  ManagementRefreshButton: () => null,
  ManagementTableToolbar: ({ children }: { children?: ReactNode }) => <>{children}</>,
}))
vi.mock('@/components/admin-table', () => ({
  AdminTable: ({
    columns = [],
    dataSource = [],
    headerExtra,
  }: {
    columns?: Array<{
      dataIndex?: string | string[]
      key?: string
      render?: (value: unknown, record: Record<string, unknown>) => ReactNode
    }>
    dataSource?: Array<Record<string, unknown>>
    headerExtra?: ReactNode
  }) => (
    <div>
      {headerExtra}
      {dataSource.map((record, rowIndex) => (
        <div key={rowIndex}>
          {columns.map((column, columnIndex) => {
            const value = Array.isArray(column.dataIndex)
              ? column.dataIndex.reduce<unknown>(
                  (current, key) =>
                    current && typeof current === 'object'
                      ? (current as Record<string, unknown>)[key]
                      : undefined,
                  record,
                )
              : record[column.dataIndex ?? '']
            return (
              <span key={column.key ?? `${String(column.dataIndex)}-${columnIndex}`}>
                {column.render ? column.render(value, record) : (value as ReactNode)}
              </span>
            )
          })}
        </div>
      ))}
    </div>
  ),
}))
vi.mock('../shared/kubernetes-resource-actions-menu', () => ({
  KubernetesResourceActionsMenu: ({
    items,
    onSelect,
  }: {
    items: Array<{ key: string; label: ReactNode }>
    onSelect: (key: string) => void
  }) => (
    <>
      {items.map((item) => (
        <button key={item.key} aria-label={String(item.label)} onClick={() => onSelect(item.key)}>
          {item.label}
        </button>
      ))}
    </>
  ),
}))
vi.mock('antd', async (importOriginal) => {
  const actual = await importOriginal<typeof import('antd')>()
  const FormMock = ({
    children,
    onFinish,
    ...props
  }: Omit<import('antd').FormProps, 'children'> & { children?: ReactNode }) => {
    formLifecycleMocks.submit = onFinish
    formLifecycleMocks.preserve = props.preserve
    formLifecycleMocks.disabled = props.disabled
    if (formLifecycleMocks.realForm) {
      return (
        <actual.Form {...props} onFinish={onFinish}>
          {children}
        </actual.Form>
      )
    }
    return <div>{children}</div>
  }
  FormMock.useForm = actual.Form.useForm
  FormMock.List = (props: ComponentProps<typeof actual.Form.List>) =>
    formLifecycleMocks.realForm ? (
      <actual.Form.List {...props} />
    ) : (
      <>
        {props.children(
          [],
          { add: vi.fn(), remove: vi.fn(), move: vi.fn() },
          { errors: [], warnings: [] },
        )}
      </>
    )
  FormMock.Item = (props: import('antd').FormItemProps) =>
    formLifecycleMocks.realForm ? (
      <actual.Form.Item {...props} />
    ) : (
      <div>{props.children as ReactNode}</div>
    )
  const InputMock = Object.assign(
    (props: InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
    {
      Password: (props: InputHTMLAttributes<HTMLInputElement>) => (
        <input type="password" {...props} />
      ),
      TextArea: ({
        autoSize: _autoSize,
        ...props
      }: TextareaHTMLAttributes<HTMLTextAreaElement> & { autoSize?: unknown }) => (
        <textarea {...props} />
      ),
    },
  )
  const AppMock = Object.assign(({ children }: { children?: ReactNode }) => <>{children}</>, {
    useApp: () => ({ message: { error: vi.fn(), success: vi.fn() } }),
  })
  return {
    ...actual,
    App: AppMock,
    Form: FormMock,
    Input: InputMock,
    Modal: ({
      children,
      destroyOnHidden,
      open,
      title,
      onOk,
      footer,
    }: {
      children?: ReactNode
      destroyOnHidden?: boolean
      open?: boolean
      title?: ReactNode
      onOk?: () => void
      footer?: ReactNode
    }) => {
      formLifecycleMocks.destroyOnHidden = destroyOnHidden
      const formChild = Children.toArray(children).find(
        (child) =>
          isValidElement<{ preserve?: boolean }>(child) && child.props.preserve !== undefined,
      )
      if (isValidElement<{ preserve?: boolean }>(formChild))
        formLifecycleMocks.preserve = formChild.props.preserve
      return open ? (
        <div role="dialog" aria-label={typeof title === 'string' ? title : undefined}>
          {title}
          {onOk && (
            <button aria-label="确认删除集群" onClick={onOk}>
              确认删除
            </button>
          )}
          {children}
          {footer}
        </div>
      ) : null
    },
    Popconfirm: ({ children, onConfirm }: { children?: ReactNode; onConfirm?: () => void }) => (
      <span onClick={onConfirm}>{children}</span>
    ),
    Select: ({
      onChange,
      options = [],
      ...props
    }: {
      onChange?: (value: string) => void
      options?: Array<{ label: ReactNode; value: string }>
    } & import('antd').SelectProps) => {
      if (formLifecycleMocks.realForm)
        return <actual.Select {...props} options={options} onChange={onChange} />
      const isConnectionMode = options.some((option) => option.value === 'direct_kubeconfig')
      if (isConnectionMode) {
        selectMocks.setAgentMode = () => onChange?.('agent')
        return (
          <button type="button" aria-label="select-agent-mode" onClick={() => onChange?.('agent')}>
            Agent
          </button>
        )
      }
      return (
        <select aria-label="select" onChange={(event) => onChange?.(event.target.value)}>
          {options.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      )
    },
  }
})

const mountedRoots: Root[] = []

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn(() => ({
      matches: false,
      media: '',
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
})
beforeEach(() => {
  vi.clearAllMocks()
  authMocks.permissions = new Set([
    'platform.clusters.view',
    'platform.clusters.create',
    'platform.clusters.update',
    'platform.clusters.delete',
  ])
  formLifecycleMocks.destroyOnHidden = undefined
  formLifecycleMocks.preserve = undefined
  formLifecycleMocks.disabled = undefined
  formLifecycleMocks.submit = undefined
  formLifecycleMocks.realForm = false
  selectMocks.setAgentMode = undefined
})
afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function renderPage() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
  await act(async () => {
    root.render(
      <StrictMode>
        <QueryClientProvider client={queryClient}>
          <MemoryRouter>
            <ClustersPage />
          </MemoryRouter>
        </QueryClientProvider>
      </StrictMode>,
    )
  })
  await flushAsyncWork()
  return container
}

describe('clusters list page boundaries', () => {
  it('keeps new custom resource grants read-only through StrictMode field mounting', async () => {
    formLifecycleMocks.realForm = true
    const container = await renderPage()
    await act(async () =>
      container
        .querySelector('button[aria-label="编辑集群"]')
        ?.dispatchEvent(new MouseEvent('click', { bubbles: true })),
    )
    await flushAsyncWork()
    const addButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('添加授权'),
    )
    expect(addButton).toBeDefined()
    await act(async () => addButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()
    const labels = Array.from(container.querySelectorAll('.ant-select-selection-item')).map(
      (item) => item.getAttribute('title'),
    )
    expect(labels).toEqual(expect.arrayContaining(['get', 'list', 'watch']))
    expect(labels).not.toEqual(expect.arrayContaining(['create', 'update', 'patch', 'delete']))
  })

  it('discards create form state when the modal closes', async () => {
    const container = await renderPage()
    await act(async () =>
      container.querySelector<HTMLButtonElement>('.soha-clusters-create-button')?.click(),
    )
    await flushAsyncWork()

    expect(formLifecycleMocks.destroyOnHidden).toBe(true)
    expect(formLifecycleMocks.preserve).toBe(false)
  })

  it('preserves create fields and removes inbound connection fields in Agent mode', async () => {
    const container = await renderPage()
    const createButton = container.querySelector<HTMLButtonElement>('.soha-clusters-create-button')
    expect(createButton).not.toBeNull()
    await act(async () => createButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()

    const nameInput = container.querySelector('input')
    expect(nameInput).not.toBeNull()
    await act(async () => {
      if (!nameInput) return
      nameInput.value = 'soha-k3s-agent-1'
      nameInput.dispatchEvent(new Event('input', { bubbles: true }))
      nameInput.dispatchEvent(new Event('change', { bubbles: true }))
    })

    await act(async () => {
      selectMocks.setAgentMode?.()
    })
    await flushAsyncWork()

    expect(container.querySelector('input')?.value).toBe('soha-k3s-agent-1')
    expect(container.textContent).not.toContain('Agent Endpoint')
    expect(container.textContent).not.toContain('Agent Token')
  })

  it('shows and refreshes the Agent apply command after creation', async () => {
    const command = 'kubectl apply -f https://soha.test/agent.yaml'
    const refreshedCommand = 'kubectl apply -f https://soha.test/refreshed-agent.yaml'
    apiPostMock.mockImplementation(async (path: string) => {
      if (path === '/clusters') return { data: { id: 'cluster-created', connectionMode: 'agent' } }
      if (path === '/clusters/cluster-created/agent-installation') {
        return {
          data: {
            clusterId: 'cluster-created',
            command,
            expiresAt: '2026-08-17T18:00:00+08:00',
            manifestUrl: 'https://soha.test/agent.yaml',
          },
        }
      }
      return { data: {} }
    })

    const container = await renderPage()
    const createButton = container.querySelector<HTMLButtonElement>('.soha-clusters-create-button')
    await act(async () => createButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()
    await act(async () => selectMocks.setAgentMode?.())
    await flushAsyncWork()

    apiGetMock.mockImplementationOnce(() => new Promise(() => {}))
    await act(async () => {
      formLifecycleMocks.submit?.({
        connectionMode: 'agent',
        name: 'cluster-created',
        provider: 'standard_kubernetes',
      })
    })
    await flushAsyncWork()

    expect(apiPostMock).toHaveBeenNthCalledWith(
      1,
      '/clusters',
      expect.objectContaining({ connectionMode: 'agent', name: 'cluster-created' }),
    )
    expect(apiPostMock).toHaveBeenNthCalledWith(2, '/clusters/cluster-created/agent-installation')
    expect(container.textContent).toContain('Agent 安装命令')
    expect(container.querySelector('textarea')?.value).toBe(command)

    apiPostMock.mockResolvedValueOnce({
      data: {
        clusterId: 'cluster-created',
        command: refreshedCommand,
        expiresAt: '2026-08-17T19:00:00+08:00',
        manifestUrl: 'https://soha.test/refreshed-agent.yaml',
      },
    })
    const refreshButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === '刷新命令',
    )
    expect(refreshButton).toBeDefined()
    await act(async () => refreshButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()

    expect(apiPostMock).toHaveBeenNthCalledWith(3, '/clusters/cluster-created/agent-installation')
    expect(container.querySelector('textarea')?.value).toBe(refreshedCommand)
  })

  it('loads edit detail on demand and deletes through the capability mutation', async () => {
    const container = await renderPage()
    expect(apiGetMock).toHaveBeenCalledWith('/clusters')
    expect(apiGetMock).not.toHaveBeenCalledWith('/clusters/cluster-a/detail')
    expect(container.querySelector('.soha-status-tag')?.textContent).toBe('正常')
    expect(container.querySelector('.soha-metadata-tag')?.textContent).toBe('Agent')

    const editButton = container.querySelector('button[aria-label="编辑集群"]')
    await act(async () => editButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()
    expect(apiGetMock).toHaveBeenCalledWith('/clusters/cluster-a/detail')
    expect(container.textContent).toContain('编辑集群')

    const deleteButton = container.querySelector('button[aria-label="删除集群"]')
    await act(async () => deleteButton?.dispatchEvent(new MouseEvent('click', { bubbles: true })))
    await flushAsyncWork()
    expect(apiDeleteMock).not.toHaveBeenCalled()
    await act(async () =>
      container.querySelector<HTMLButtonElement>('button[aria-label="确认删除集群"]')?.click(),
    )
    await flushAsyncWork()
    expect(apiDeleteMock).toHaveBeenCalledWith('/clusters/cluster-a')
  })

  it('hides mutation controls without their exact permissions', async () => {
    authMocks.permissions = new Set(['platform.clusters.view'])
    const container = await renderPage()

    expect(container.querySelector('.soha-clusters-create-button')).toBeNull()
    expect(container.querySelector('button[aria-label="编辑集群"]')).toBeNull()
    expect(container.querySelector('button[aria-label="删除集群"]')).toBeNull()
  })

  it('locks the editor and rejects another submit while its create request is pending', async () => {
    let finishCreate!: (value: { data: unknown }) => void
    const pendingCreate = new Promise<{ data: unknown }>((resolve) => {
      finishCreate = resolve
    })
    apiPostMock.mockImplementationOnce(() => pendingCreate)
    const container = await renderPage()
    await act(async () =>
      container.querySelector<HTMLButtonElement>('.soha-clusters-create-button')?.click(),
    )
    await flushAsyncWork()
    const values = {
      name: 'pending-cluster',
      provider: 'standard_kubernetes',
      connectionMode: 'direct_kubeconfig',
      kubeconfig: 'new-cluster-credentials',
    }
    await act(async () => {
      formLifecycleMocks.submit?.(values)
    })
    await vi.waitFor(() => expect(apiPostMock).toHaveBeenCalledTimes(1))
    await vi.waitFor(() => expect(formLifecycleMocks.disabled).toBe(true))
    const editor = container.querySelector('[role="dialog"][aria-label="添加集群"]')
    expect(editor).not.toBeNull()
    const saveButton = editor!.querySelector<HTMLButtonElement>('button[type="submit"][form]')
    const cancelButton = Array.from(editor!.querySelectorAll<HTMLButtonElement>('button')).find(
      (button) => button.textContent?.replace(/\s/g, '') === '取消',
    )
    expect(saveButton).not.toBeNull()
    expect(cancelButton).toBeDefined()
    expect(saveButton?.disabled).toBe(true)
    expect(cancelButton?.disabled).toBe(true)
    await act(async () => {
      saveButton?.click()
      formLifecycleMocks.submit?.(values)
      cancelButton?.click()
    })
    expect(apiPostMock).toHaveBeenCalledTimes(1)
    expect(container.textContent).toContain('添加集群')
    await act(async () =>
      finishCreate({ data: { id: 'pending-cluster', connectionMode: 'direct_kubeconfig' } }),
    )
    await vi.waitFor(() => expect(container.textContent).not.toContain('添加集群'))
  })
})
