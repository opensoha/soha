/** @vitest-environment jsdom */

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { StoragePvcDetailPage } from './persistent-volume-claims/detail-page'
import { StoragePvDetailPage } from './persistent-volumes/detail-page'
import { StorageClassDetailPage } from './storage-classes/detail-page'

const testState = vi.hoisted(() => ({
  editorModuleLoads: 0,
  scope: { clusterId: 'cluster-a', namespace: 'selected-ns', setClusterId: vi.fn() },
  permissions: ['view', 'update', 'delete'].map(
    (action) => `platform.storage.persistent-volume-claims.${action}`,
  ),
  allowedActions: ['update', 'delete'] as string[] | undefined,
  detailName: 'claim-a',
  yamlName: 'claim-a',
  generation: 1,
  changeDraft: undefined as ((value: string) => void) | undefined,
}))
const apiGetMock = vi.hoisted(() =>
  vi.fn(async (path: string) => {
    if (path.includes('/resources/graph?')) {
      const { searchParams } = new URL(path, 'http://localhost')
      const kind = searchParams.get('kind')
      const namespace = searchParams.get('namespace') ?? undefined
      return {
        data: {
          clusterId: 'cluster-a',
          rootId: 'root',
          generatedAt: '',
          warnings: [],
          evidence: [],
          nodes: [
            {
              id: 'root',
              resource: {
                clusterId: 'cluster-a',
                kind,
                name: searchParams.get('name'),
                apiVersion: kind === 'StorageClass' ? 'storage.k8s.io/v1' : 'v1',
                scopeMode: kind === 'PersistentVolumeClaim' ? 'namespace' : 'cluster',
                namespace,
              },
            },
          ],
          edges: [],
        },
      }
    }
    if (path.includes('/yaml?') || path.endsWith('/yaml')) {
      if (path.includes('/persistentvolumes/')) {
        return {
          data: {
            content: `apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv-a
spec:
  csi:
    driver: csi.example.io
    volumeHandle: volume-a
  mountOptions:
    - noatime
`,
          },
        }
      }
      if (path.includes('/storageclasses/')) {
        return {
          data: {
            content: `apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: fast
  annotations:
    storageclass.kubernetes.io/is-default-class: 'true'
mountOptions:
  - discard
`,
          },
        }
      }
      const namespace = new URL(path, 'http://localhost').searchParams.get('namespace')
      return {
        data: {
          content: `apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ${testState.yamlName}
  namespace: ${namespace}
  generation: ${testState.generation}
spec:
  volumeMode: Filesystem
status:
  conditions: []
`,
        },
      }
    }
    if (path.includes('/persistentvolumeclaims/')) {
      return {
        data: {
          name: testState.detailName,
          allowedActions: testState.allowedActions,
          namespace: new URL(path, 'http://localhost').searchParams.get('namespace'),
          status: 'Bound',
          volumeName: 'pv-a',
          storageClass: 'fast',
          pods: [{ name: 'api-0', namespace: 'selected-ns', phase: 'Running' }],
          ageSeconds: 60,
        },
      }
    }
    if (path.includes('/persistentvolumes/')) {
      return {
        data: {
          name: 'pv-a',
          status: 'Bound',
          storageClass: 'fast',
          claimRef: 'selected-ns/claim-a',
          claimNamespace: 'selected-ns',
          claimName: 'claim-a',
          ageSeconds: 60,
        },
      }
    }
    return {
      data: {
        name: 'fast',
        provisioner: 'csi.example.io',
        allowVolumeExpansion: true,
        volumes: [{ name: 'pv-a', status: 'Bound' }],
        claims: [{ name: 'claim-a', namespace: 'selected-ns', status: 'Bound' }],
        ageSeconds: 60,
      },
    }
  }),
)

const apiDeleteMock = vi.hoisted(() => vi.fn<(...args: unknown[]) => Promise<unknown>>())

vi.mock('@/services/api-client', () => ({
  api: {
    get: apiGetMock,
    put: vi.fn(async () => ({ data: { content: 'updated' } })),
    delete: apiDeleteMock,
  },
}))
vi.mock('@/features/auth', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/features/auth')>()),
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: testState.permissions } },
    isPending: false,
  }),
}))
vi.mock('@/features/copilot', () => ({ useAIPageContext: vi.fn() }))
vi.mock('@/components/k8s-yaml-editor', () => {
  testState.editorModuleLoads += 1
  return {
    K8sYamlEditor: ({
      value,
      readOnly,
      applyDisabled,
      onChange,
      onApply,
      serverChanged,
    }: {
      value: string
      readOnly: boolean
      applyDisabled: boolean
      onChange: (value: string) => void
      onApply: () => void
      serverChanged: boolean
    }) => {
      testState.changeDraft = onChange
      return (
        <div data-testid="yaml-editor">
          <textarea
            aria-label="YAML draft"
            value={value}
            readOnly={readOnly}
            onChange={(event) => onChange(event.target.value)}
          />
          <button disabled={readOnly || applyDisabled} onClick={onApply}>
            Apply test YAML
          </button>
          {serverChanged ? <span>Server changed</span> : null}
        </div>
      )
    },
  }
})
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => testState.scope,
}))
vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))
vi.mock('@/components/status-tag', async (original) => ({
  ...(await original<typeof import('@/components/status-tag')>()),
  BooleanTag: ({ value }: { value: boolean }) => <span>{String(value)}</span>,
  StatusTag: ({ value }: { value: string }) => <span>{value}</span>,
}))
vi.mock('@/components/management-list', async (original) => ({
  ...(await original<typeof import('@/components/management-list')>()),
  ManagementState: ({ title, description }: { title?: ReactNode; description?: ReactNode }) => (
    <div>
      {title}
      {description}
    </div>
  ),
}))

const mountedRoots: Root[] = []

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
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
})

beforeEach(() => {
  vi.clearAllMocks()
  apiDeleteMock.mockReset()
  testState.scope.clusterId = 'cluster-a'
  testState.scope.namespace = 'selected-ns'
  testState.permissions = ['view', 'update', 'delete'].map(
    (action) => `platform.storage.persistent-volume-claims.${action}`,
  )
  testState.allowedActions = ['update', 'delete']
  testState.detailName = 'claim-a'
  testState.yamlName = 'claim-a'
  testState.generation = 1
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

async function renderDetail(path: string, route: string, page: ReactNode) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  mountedRoots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  })
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path={route} element={page} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })
  await flushAsyncWork()
  await flushAsyncWork()
  return container
}

function clickTab(container: HTMLElement, name: string) {
  const tab = Array.from(container.querySelectorAll<HTMLElement>('[role="tab"]')).find(
    (item) => item.textContent?.trim() === name,
  )
  expect(tab).toBeDefined()
  tab?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
}

describe('storage detail pages', () => {
  it.each([
    {
      kind: 'PersistentVolumeClaim',
      route: 'persistentvolumeclaims',
      name: 'claim-a',
      page: <StoragePvcDetailPage />,
      scopeQuery: 'namespace=url-ns&',
    },
    {
      kind: 'PersistentVolume',
      route: 'persistentvolumes',
      name: 'pv-a',
      page: <StoragePvDetailPage />,
      scopeQuery: '',
    },
    {
      kind: 'StorageClass',
      route: 'storageclasses',
      name: 'fast',
      page: <StorageClassDetailPage />,
      scopeQuery: '',
    },
  ])(
    'requests the exact $kind graph only on its relationships tab',
    async ({ kind, route, name, page, scopeQuery }) => {
      testState.permissions.push(
        'platform.storage.persistent-volumes.view',
        'platform.storage.storage-classes.view',
      )
      const container = await renderDetail(
        `/storage/${route}/${name}?clusterId=cluster-a&namespace=url-ns`,
        `/storage/${route}/:name`,
        page,
      )
      const graphRequests = () =>
        apiGetMock.mock.calls
          .map(([path]) => path)
          .filter((path) => path.includes('/resources/graph?'))
      expect(graphRequests()).toEqual([])
      await act(async () => clickTab(container, '关系'))
      await vi.waitFor(
        async () => {
          await flushAsyncWork()
          expect(container.querySelector('.soha-workload-relationships')).not.toBeNull()
          expect(container.querySelector('[data-id="root"]')).not.toBeNull()
          expect(graphRequests()).toEqual([
            `/clusters/cluster-a/resources/graph?${scopeQuery}kind=${kind}&name=${name}`,
          ])
        },
        { timeout: 2_000, interval: 10 },
      )
      await act(async () => clickTab(container, '概览'))
      expect(container.querySelector('.soha-workload-relationships')).toBeNull()
      expect(graphRequests()).toHaveLength(1)
    },
  )

  it('reads supplemental PVC configuration on overview but loads the editor only on YAML', async () => {
    const editorModuleLoads = testState.editorModuleLoads
    const container = await renderDetail(
      '/storage/persistentvolumeclaims/claim-a?namespace=url-ns',
      '/storage/persistentvolumeclaims/:name',
      <StoragePvcDetailPage />,
    )
    const paths = () => apiGetMock.mock.calls.map(([path]) => String(path))
    expect(paths()).toContain(
      '/clusters/cluster-a/storage/persistentvolumeclaims/claim-a/detail?namespace=url-ns',
    )
    expect(paths()).toContain(
      '/clusters/cluster-a/storage/persistentvolumeclaims/claim-a/yaml?namespace=url-ns',
    )
    expect(container.querySelector('[data-testid="yaml-editor"]')).toBeNull()
    expect(testState.editorModuleLoads).toBe(editorModuleLoads)
    expect(container.textContent).toContain('Filesystem')

    await act(async () => clickTab(container, 'YAML'))
    await flushAsyncWork()
    expect(paths()).toContain(
      '/clusters/cluster-a/storage/persistentvolumeclaims/claim-a/yaml?namespace=url-ns',
    )
    // The first visit resolves the real lazy panel module; later cases reuse its module cache.
    // Wait for that observable UI rather than assuming one macrotask completes a cold import.
    await vi.waitFor(
      async () => {
        await flushAsyncWork()
        expect(container.querySelector('[data-testid="yaml-editor"]')).not.toBeNull()
      },
      { timeout: 2_000, interval: 10 },
    )
    expect(testState.editorModuleLoads).toBeGreaterThan(0)
    expect(testState.scope.namespace).toBe('selected-ns')
  })

  it('reads cluster-scoped PV and StorageClass supplemental configuration and their relationship tabs', async () => {
    testState.permissions.push(
      'platform.storage.persistent-volumes.view',
      'platform.storage.storage-classes.view',
    )
    const editorModuleLoads = testState.editorModuleLoads
    const pv = await renderDetail(
      '/storage/persistentvolumes/pv-a',
      '/storage/persistentvolumes/:name',
      <StoragePvDetailPage />,
    )
    const storageClass = await renderDetail(
      '/storage/storageclasses/fast',
      '/storage/storageclasses/:name',
      <StorageClassDetailPage />,
    )
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/storage/persistentvolumes/pv-a/detail',
    )
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/storage/storageclasses/fast/detail',
    )
    expect(apiGetMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/storage/persistentvolumes/pv-a/yaml',
    )
    expect(apiGetMock).toHaveBeenCalledWith('/clusters/cluster-a/storage/storageclasses/fast/yaml')
    expect(apiGetMock.mock.calls.map(([path]) => path).sort()).toEqual(
      [
        '/clusters/cluster-a/storage/persistentvolumes/pv-a/detail',
        '/clusters/cluster-a/storage/persistentvolumes/pv-a/yaml',
        '/clusters/cluster-a/storage/storageclasses/fast/detail',
        '/clusters/cluster-a/storage/storageclasses/fast/yaml',
      ].sort(),
    )
    expect(pv.textContent).toContain('selected-ns/claim-a')
    expect(pv.textContent).toContain('volume-a')
    expect(pv.textContent).toContain('noatime')
    expect(storageClass.textContent).toContain('discard')
    expect(storageClass.textContent).not.toContain('补充配置未提供，默认状态未知')
    expect(pv.querySelector('[data-testid="yaml-editor"]')).toBeNull()
    expect(storageClass.querySelector('[data-testid="yaml-editor"]')).toBeNull()
    expect(testState.editorModuleLoads).toBe(editorModuleLoads)
    await act(async () => clickTab(storageClass, 'PVs'))
    await flushAsyncWork()
    expect(storageClass.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toBe(
      'PVs',
    )
    expect(storageClass.textContent).toContain('pv-a')
    expect(storageClass.textContent).toContain('显示 1 / 当前返回 1 条')
    await act(async () => clickTab(storageClass, 'PVCs'))
    await flushAsyncWork()
    expect(storageClass.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toBe(
      'PVCs',
    )
    expect(storageClass.textContent).toContain('claim-a')
    expect(storageClass.textContent).toContain('selected-ns')
    expect(storageClass.textContent).toContain('显示 1 / 当前返回 1 条')
  })

  it('shows pods using the PVC', async () => {
    const container = await renderDetail(
      '/storage/persistentvolumeclaims/claim-a?namespace=selected-ns',
      '/storage/persistentvolumeclaims/:name',
      <StoragePvcDetailPage />,
    )
    expect(container.textContent).toContain('当前返回 1 条引用')
    await act(async () => clickTab(container, 'Pods'))
    await flushAsyncWork()
    expect(container.textContent).toContain('api-0')
  })

  it.each([
    {
      label: 'missing allowedActions',
      actions: undefined,
      permissions: ['view', 'update', 'delete'],
    },
    { label: 'read permission only', actions: ['update', 'delete'], permissions: ['view'] },
  ])('keeps $label read-only', async ({ actions, permissions }) => {
    testState.allowedActions = actions
    testState.permissions = permissions.map(
      (action) => `platform.storage.persistent-volume-claims.${action}`,
    )
    const container = await renderDetail(
      '/storage/persistentvolumeclaims/claim-a?namespace=selected-ns',
      '/storage/persistentvolumeclaims/:name',
      <StoragePvcDetailPage />,
    )
    expect(container.textContent).not.toContain('编辑 YAML')
    expect(
      Array.from(container.querySelectorAll('button')).some(
        (button) => button.textContent === '删除',
      ),
    ).toBe(false)
    await act(async () => clickTab(container, 'YAML'))
    await flushAsyncWork()
    expect(container.querySelector<HTMLTextAreaElement>('textarea')?.readOnly).toBe(true)
    expect(
      Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent === 'Apply test YAML',
      )?.disabled,
    ).toBe(true)
  })

  it('does not read PVC detail or YAML without view permission', async () => {
    testState.permissions = []
    await renderDetail(
      '/storage/persistentvolumeclaims/claim-a?namespace=selected-ns',
      '/storage/persistentvolumeclaims/:name',
      <StoragePvcDetailPage />,
    )
    expect(apiGetMock).not.toHaveBeenCalled()
  })

  it('rejects mismatched detail identity', async () => {
    testState.detailName = 'different-claim'
    const container = await renderDetail(
      '/storage/persistentvolumeclaims/claim-a?namespace=selected-ns',
      '/storage/persistentvolumeclaims/:name',
      <StoragePvcDetailPage />,
    )
    expect(container.textContent).toContain('PVC 详情身份不匹配')
    expect(container.querySelector('h1')).toBeNull()
    expect(container.textContent).not.toContain('编辑 YAML')
  })

  it('rejects mismatched supplemental YAML and fences edits', async () => {
    testState.yamlName = 'different-claim'
    const container = await renderDetail(
      '/storage/persistentvolumeclaims/claim-a?namespace=selected-ns',
      '/storage/persistentvolumeclaims/:name',
      <StoragePvcDetailPage />,
    )
    expect(container.textContent).toContain('PVC 补充配置无法解析')
    await act(async () => clickTab(container, 'YAML'))
    await flushAsyncWork()
    expect(container.querySelector('[data-testid="yaml-editor"]')).toBeNull()
    expect(container.textContent).not.toContain('Apply test YAML')
  })

  it('preserves a dirty draft across tabs and a server refresh', async () => {
    const container = await renderDetail(
      '/storage/persistentvolumeclaims/claim-a?namespace=selected-ns&tab=yaml',
      '/storage/persistentvolumeclaims/:name',
      <StoragePvcDetailPage />,
    )
    await flushAsyncWork()
    const initial = container.querySelector<HTMLTextAreaElement>('textarea')?.value
    expect(initial).toContain('generation: 1')
    const draft = initial + '# local draft\n'
    await act(async () => testState.changeDraft?.(draft))
    await act(async () => clickTab(container, 'Pods'))
    await flushAsyncWork()
    testState.generation = 2
    await act(async () => clickTab(container, '概览'))
    await flushAsyncWork()
    await act(async () =>
      container.querySelector<HTMLButtonElement>('button[aria-label="刷新 PVC"]')?.click(),
    )
    await flushAsyncWork()
    await act(async () => clickTab(container, 'YAML'))
    await flushAsyncWork()
    expect(container.querySelector<HTMLTextAreaElement>('textarea')?.value).toBe(draft)
    expect(container.textContent).toContain('Server changed')
  })

  it('confirms the exact PVC namespace and name and permits cancellation without deleting', async () => {
    const container = await renderDetail(
      '/storage/persistentvolumeclaims/claim-a?namespace=selected-ns',
      '/storage/persistentvolumeclaims/:name',
      <StoragePvcDetailPage />,
    )
    const remove = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.replace(/\s/g, '') === '删除',
    )
    expect(remove).toBeDefined()
    await act(async () => remove?.click())
    await flushAsyncWork()
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.textContent).toContain('确认删除 claim-a？')
    expect(dialog?.textContent).toContain('selected-ns / claim-a')
    expect(apiDeleteMock).not.toHaveBeenCalled()
    const cancel = Array.from(dialog?.querySelectorAll('button') ?? []).find(
      (button) => button.textContent?.replace(/\s/g, '') === '取消',
    )
    await act(async () => cancel?.click())
    await flushAsyncWork()
    expect(apiDeleteMock).not.toHaveBeenCalled()
    expect(container.textContent).toContain('claim-a')
  })

  it('fences duplicate deletion while pending and retains the failed confirmation target', async () => {
    let rejectDelete!: (error: Error) => void
    apiDeleteMock.mockImplementationOnce(
      () =>
        new Promise((_resolve, reject) => {
          rejectDelete = reject
        }),
    )
    const container = await renderDetail(
      '/storage/persistentvolumeclaims/claim-a?namespace=selected-ns',
      '/storage/persistentvolumeclaims/:name',
      <StoragePvcDetailPage />,
    )
    const remove = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent?.replace(/\s/g, '') === '删除',
    )
    await act(async () => remove?.click())
    await flushAsyncWork()
    const confirm = document.querySelector<HTMLButtonElement>(
      '[role="dialog"] button[aria-label="删除"]',
    )
    expect(confirm).not.toBeNull()
    await act(async () => {
      confirm?.click()
      confirm?.click()
    })
    await flushAsyncWork()
    expect(apiDeleteMock).toHaveBeenCalledTimes(1)
    expect(apiDeleteMock).toHaveBeenCalledWith(
      '/clusters/cluster-a/storage/persistentvolumeclaims/claim-a?namespace=selected-ns',
    )
    expect(confirm?.disabled).toBe(true)
    expect(confirm?.getAttribute('aria-busy')).toBe('true')
    expect(remove?.disabled).toBe(true)
    await act(async () =>
      rejectDelete(new ApiError(409, 'Synthetic PVC delete conflict', { method: 'DELETE' })),
    )
    await flushAsyncWork()
    const dialog = document.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.textContent).toContain('确认删除 claim-a？')
    expect(dialog?.textContent).toContain('selected-ns / claim-a')
    expect(dialog?.textContent).toContain('Synthetic PVC delete conflict')
    expect(apiDeleteMock).toHaveBeenCalledTimes(1)
    expect(container.querySelector('h1')?.textContent).toBe('claim-a')
  })
})
