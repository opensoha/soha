/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { PlatformAccessControlRoleDetailPage } from './access-control/roles/detail-page'

const testState = vi.hoisted(() => ({
  responses: {} as Record<string, unknown>,
  scope: {
    clusterId: 'cluster-a' as string | null,
    namespace: 'default' as string | null,
    setClusterId: vi.fn(),
    setNamespace: vi.fn(),
  },
}))

const apiGetMock = vi.hoisted(() =>
  vi.fn((path: string) => Promise.resolve({ data: testState.responses[path] ?? [] })),
)
const apiPutMock = vi.hoisted(() => vi.fn())

vi.mock('@/features/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth')>()),
  usePermissionSnapshot: () => ({
    data: {
      data: {
        permissionKeys: ['platform.access-control.roles.view', 'platform.access-control.roles.update'],
      },
    },
  }),
}))

vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))

vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  localeText: (localeCode: string, chinese: string, english: string) =>
    localeCode === 'zh_CN' ? chinese : english,
  useI18n: () => ({
    localeCode: 'en_US' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))

vi.mock('@/services/api-client', () => ({
  api: {
    get: apiGetMock,
    put: apiPutMock,
  },
}))

vi.mock('@/components/k8s-yaml-editor', () => ({
  K8sYamlEditor: ({
    value,
    readOnly,
    applyDisabled,
    onApply,
  }: {
    value: string
    readOnly?: boolean
    applyDisabled?: boolean
    onApply: () => void
  }) => (
    <div data-testid="yaml-editor" data-read-only={String(readOnly)}>
      <pre>{value}</pre>
      <button disabled={readOnly || applyDisabled} onClick={onApply}>
        Apply Role YAML
      </button>
    </div>
  ),
}))

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function setResponses(responses: Record<string, unknown>) {
  testState.responses = responses
}

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await Promise.resolve()
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function renderWithProviders(node: ReactNode) {
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
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/platform-access-control/roles/demo?namespace=default']}>
          <Routes>
            <Route path="/platform-access-control/roles/:name" element={node} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await flushAsyncWork()

  return container
}

describe('RBAC detail YAML tab', () => {
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

    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  })

  beforeEach(() => {
    testState.scope.clusterId = 'cluster-a'
    testState.scope.namespace = 'default'
    setResponses({
      '/clusters': [
        {
          id: 'cluster-a',
          name: 'Agent Cluster',
          connectionMode: 'agent',
          region: 'dev',
          environment: 'test',
          labels: {},
          version: 'v1.30.0',
          health: { status: 'healthy' },
        },
      ],
      '/clusters/capabilities': [
        {
          key: 'resource.yaml.apply',
          label: 'YAML apply and delete',
          category: 'configuration',
          direct: { status: 'available' },
          agent: {
            status: 'unsupported',
            notes: ['This Agent does not support YAML apply'],
          },
        },
        {
          key: 'resource.yaml.view',
          direct: { status: 'available' },
          agent: { status: 'available' },
        },
      ],
      '/clusters/cluster-a/access-control/roles/demo/detail?namespace=default': {
        annotations: {},
        createdAt: '2026-06-10T00:00:00Z',
        labels: {},
        name: 'demo',
        namespace: 'default',
        allowedActions: ['view', 'update'],
        ruleSummaries: ['get pods'],
        rules: 1,
      },
      '/clusters/cluster-a/access-control/roles/demo/yaml?namespace=default': {
        content: 'apiVersion: rbac.authorization.k8s.io/v1\nkind: Role\nmetadata:\n  name: demo\n  namespace: default\nrules: []\n',
      },
    })
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

  it('reads authorized Role YAML while unsupported Agent apply remains blocked', async () => {
    const container = await renderWithProviders(<PlatformAccessControlRoleDetailPage />)

    const yamlTab = Array.from(container.querySelectorAll('[role="tab"]')).find(
      (item) => item.textContent === 'YAML',
    )
    if (!yamlTab) {
      throw new Error('YAML tab not found')
    }

    await act(async () => {
      yamlTab.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    await flushAsyncWork()

    await vi.waitFor(() => {
      const editor = container.querySelector('[data-testid="yaml-editor"]')
      expect(editor?.textContent).toContain('kind: Role')
      expect(editor?.getAttribute('data-read-only')).toBe('false')
    })
    expect(apiGetMock.mock.calls.map(([path]) => path)).toContain(
      '/clusters/cluster-a/access-control/roles/demo/yaml?namespace=default',
    )
    const apply = container.querySelector<HTMLButtonElement>('[data-testid="yaml-editor"] button')!
    expect(apply.disabled).toBe(true)
    await act(async () => apply.click())
    expect(apiPutMock).not.toHaveBeenCalled()
  })
})
