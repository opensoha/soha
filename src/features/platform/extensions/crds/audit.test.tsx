/** @vitest-environment jsdom */

import { transferableAbortController } from 'node:util'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { App as AntdApp } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { ApiError } from '@/services/api-error'
import { CRDApiGroupDetailPage } from './api-group-detail-page'
import { deleteCustomResource } from './api'
import type {
  ApplyCustomResourceVariables,
  CRD,
  CRDResourceInstance,
  CustomResourceTarget,
} from './types'
import type { KubernetesResourceRef } from '@opensoha/contracts/gen/ts/sohaapi'

type RelationshipProps = {
  kind: string
  name: string
  scope: { clusterId: string; namespace: string | null }
  rootAPI: { apiVersion: string; resource: string }
  resolveCustomDetailPath: (resource: KubernetesResourceRef) => string | null
}

const fixtures = vi.hoisted(() => ({
  reads: [] as Array<{ action: string; clusterId: string }>,
  writes: [] as ApplyCustomResourceVariables[],
  relationships: [] as RelationshipProps[],
  instanceActions: ['view', 'update'],
  apply: undefined as undefined | Promise<{ content: string }>,
  applyError: undefined as Error | undefined,
  definition: {
    name: 'widgets.quality.example',
    group: 'quality.example',
    kind: 'Widget',
    plural: 'widgets',
    version: 'v1',
    scope: 'Namespaced',
  } as CRD,
}))

function recordRead(action: string, clusterId: string) {
  if (!['cluster-a', 'cluster-b'].includes(clusterId))
    throw new Error(`Unknown cluster fixture: ${clusterId}`)
  fixtures.reads.push({ action, clusterId })
}

vi.mock('./api', () => ({
  listCRDs: async (clusterId: string) => {
    recordRead('catalog', clusterId)
    return [fixtures.definition]
  },
  listCustomResources: async (clusterId: string, crd: CRD) => {
    recordRead('resources', clusterId)
    if (crd.name !== fixtures.definition.name) throw new Error(`Unknown CRD fixture: ${crd.name}`)
    return [
      {
        name: 'same-name',
        namespace: 'monitoring',
        uid: `${clusterId}-uid`,
        allowedActions: fixtures.instanceActions,
      },
    ]
  },
  getCustomResourceAccess: async (clusterId: string) => {
    recordRead('access', clusterId)
    return { allowedActions: ['create'] }
  },
  getCustomResourceYAML: async (target: CustomResourceTarget) => {
    recordRead('yaml', target.clusterId)
    if (
      target.crd.name !== fixtures.definition.name ||
      target.resourceName !== 'same-name' ||
      target.namespace !== 'monitoring'
    )
      throw new Error('Unknown YAML target fixture')
    return {
      content: `apiVersion: quality.example/v1\nkind: Widget\nmetadata:\n  name: ${target.resourceName}\n  namespace: monitoring\n`,
    }
  },
  applyCustomResource: async (variables: ApplyCustomResourceVariables) => {
    if (
      variables.crd.name !== fixtures.definition.name ||
      (variables.mode === 'edit'
        ? variables.resourceName !== 'same-name'
        : variables.resourceName !== undefined) ||
      variables.namespace !== 'monitoring'
    )
      throw new Error('Unknown apply target fixture')
    fixtures.writes.push(variables)
    if (fixtures.applyError) throw fixtures.applyError
    return fixtures.apply ?? { content: variables.content }
  },
  deleteCustomResource: vi.fn(() => {
    throw new Error('Deletion is outside this audit test')
  }),
  deleteCRDDefinition: vi.fn(() => {
    throw new Error('Definition deletion is outside this audit test')
  }),
}))
vi.mock('@/features/platform/cluster-capabilities', () => ({
  useClusterCapability: () => ({ disabled: false, isLoading: false, reason: '' }),
}))
vi.mock('@/features/platform/cluster-resources/queries', () => ({
  namespaceQueries: {
    list: (scope: { clusterId: string }) => ({
      queryKey: ['audit-namespaces', scope.clusterId],
      queryFn: async () => [{ name: 'monitoring' }],
    }),
  },
}))
vi.mock('@/features/copilot', () => ({ useAIPageContext: () => {} }))
vi.mock('@/features/platform/shared/resource-insights/workload-relationships', () => ({
  WorkloadRelationships: (props: RelationshipProps) => {
    fixtures.relationships.push(props)
    return <div data-testid="relationship-panel" />
  },
}))
vi.mock('@/features/auth', async (original) => ({
  ...(await original<typeof import('@/features/auth')>()),
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: ['platform.extensions.custom-resources.create'] } },
  }),
}))
vi.mock('@/components/k8s-yaml-editor', () => ({
  K8sYamlEditor: ({
    value,
    onChange,
    onApply,
    applyDisabled,
    readOnly,
  }: {
    value: string
    onChange: (value: string) => void
    onApply: () => void
    applyDisabled?: boolean
    readOnly?: boolean
  }) => (
    <div>
      <textarea
        aria-label="Audit YAML draft"
        value={value}
        readOnly={readOnly}
        onInput={(event) => onChange(event.currentTarget.value)}
      />
      <button disabled={applyDisabled || readOnly} onClick={onApply}>
        Audit apply
      </button>
    </div>
  ),
}))
vi.mock('@/features/platform/shared/kubernetes-resource-table', () => ({
  KubernetesResourceTable: ({
    dataSource,
    columns,
    headerExtra,
  }: {
    dataSource: CRDResourceInstance[]
    columns: Array<{
      key?: string
      dataIndex?: keyof CRDResourceInstance
      render?: (value: unknown, record: CRDResourceInstance, index: number) => ReactNode
    }>
    headerExtra?: ReactNode
  }) => (
    <div>
      {headerExtra}
      {dataSource.map((record, index) => (
        <div key={`${record.namespace}/${record.name}`}>
          {columns.map((column, i) => (
            <span key={column.key ?? i}>
              {column.render
                ? column.render(
                    column.dataIndex ? record[column.dataIndex] : undefined,
                    record,
                    index,
                  )
                : null}
            </span>
          ))}
        </div>
      ))}
    </div>
  ),
}))

let root: Root | undefined
let container: HTMLDivElement
let client: QueryClient
let router: ReturnType<typeof createMemoryRouter>
let navigateBeforeGuard: typeof router.navigate
const detail = '/extensions/apis/quality.example'
const resource =
  '?clusterId=cluster-a&kind=widgets.quality.example&namespace=&resource=same-name&resourceNamespace=monitoring&tab=yaml'

async function settle() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
async function render(search = resource) {
  router = createMemoryRouter(
    [
      { path: '/extensions/apis/:groupName', element: <CRDApiGroupDetailPage /> },
      { path: '/extensions', element: <div data-testid="catalog-route">Catalog route</div> },
    ],
    { initialEntries: [detail + search] },
  )
  navigateBeforeGuard = router.navigate.bind(router)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  await act(async () =>
    root!.render(
      <AntdApp>
        <QueryClientProvider client={client}>
          <I18nProvider>
            <RouterProvider router={router} />
          </I18nProvider>
        </QueryClientProvider>
      </AntdApp>,
    ),
  )
  await settle()
}
async function button(text: string, within: ParentNode = document) {
  let target: HTMLButtonElement | undefined
  await vi.waitFor(() => {
    target = Array.from(within.querySelectorAll('button')).find(
      (node) => node.textContent?.replace(/\s/g, '') === text.replace(/\s/g, ''),
    )
    expect(target, `button ${text}`).toBeDefined()
    expect(target?.disabled, `button ${text} is ready`).toBe(false)
  })
  await act(async () => target!.click())
  await settle()
}
async function relationshipTab() {
  const target = [...container.querySelectorAll<HTMLElement>('[role="tab"]')].find(
    (node) => !node.closest('[hidden]') && node.textContent === '关系',
  )
  expect(target).toBeDefined()
  await act(async () => target!.click())
  await settle()
}
async function dirtyEditor() {
  await button('编辑 YAML')
  await vi.waitFor(() =>
    expect(document.querySelector('textarea[aria-label="Audit YAML draft"]')).not.toBeNull(),
  )
  await act(async () => {
    const input = document.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="Audit YAML draft"]',
    )!
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
      input,
      input.value + '# unsaved-audit-draft\n',
    )
    input.dispatchEvent(new Event('input', { bubbles: true }))
  })
  await settle()
}
async function clickCatalogBack() {
  const back = container.querySelector<HTMLAnchorElement>('a[aria-label="返回API Group列表"]')
  expect(back).not.toBeNull()
  await act(async () => back!.click())
  await settle()
}

describe('CRD target and navigation audit', () => {
  beforeAll(() => {
    // React Router constructs a Node Request; its signal must use the same realm.
    const nativeController = transferableAbortController()
    vi.stubGlobal('AbortController', nativeController.constructor)
    vi.stubGlobal('AbortSignal', nativeController.signal.constructor)
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    )
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn(() => ({
        matches: false,
        addListener() {},
        removeListener() {},
        addEventListener() {},
        removeEventListener() {},
      })),
    })
  })
  beforeEach(() => {
    localStorage.clear()
    fixtures.reads = []
    fixtures.writes = []
    fixtures.relationships = []
    fixtures.instanceActions = ['view', 'update']
    fixtures.apply = undefined
    fixtures.applyError = undefined
    vi.mocked(deleteCustomResource).mockReset()
    usePlatformScopeStore.setState({ clusterId: 'cluster-a', namespace: null })
  })
  afterEach(async () => {
    await act(async () => root?.unmount())
    router?.dispose()
    client?.clear()
    container?.remove()
    document.querySelectorAll('.ant-modal-root').forEach((node) => node.remove())
    root = undefined
  })

  it('honors a copied cluster-a deep link even when the persisted selection is cluster-b', async () => {
    usePlatformScopeStore.setState({ clusterId: 'cluster-b', namespace: null })
    await render()
    await vi.waitFor(() => expect(fixtures.reads.some((read) => read.action === 'yaml')).toBe(true))
    expect(fixtures.reads.filter((read) => read.clusterId === 'cluster-b')).toEqual([])
    expect(
      fixtures.reads.some((read) => read.action === 'yaml' && read.clusterId === 'cluster-a'),
    ).toBe(true)
    expect(fixtures.writes).toEqual([])
  })

  it.each(['return to the list', 'keep a newer navigation'])(
    'waits for deletion to settle and can %s',
    async (expectedNavigation) => {
      fixtures.instanceActions.push('delete')
      let finishDelete: () => void = () => {}
      vi.mocked(deleteCustomResource).mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            finishDelete = resolve
          }),
      )
      await render(resource.replace('tab=yaml', 'tab=overview') + '&q=quality')
      const trigger = await vi.waitFor(() => {
        const found = container.querySelector<HTMLButtonElement>('button[aria-label="删除"]')
        expect(found).not.toBeNull()
        return found!
      })
      await act(async () => trigger.click())
      const confirmation = await vi.waitFor(() => {
        const found = document.querySelector('.ant-popconfirm')
        expect(found).not.toBeNull()
        return found!
      })
      await button('删除', confirmation)
      expect(new URLSearchParams(router.state.location.search).get('resource')).toBe('same-name')
      expect(deleteCustomResource).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({
          clusterId: 'cluster-a',
          namespace: 'monitoring',
          resourceName: 'same-name',
          expectedUid: 'cluster-a-uid',
        }),
        expect.anything(),
      )
      const newerLocation = `${detail}${resource}&q=recent`
      if (expectedNavigation === 'keep a newer navigation') {
        // Simulate navigation accepted independently of the guarded confirmation flow.
        await act(async () => navigateBeforeGuard(newerLocation))
        await settle()
      }
      await act(async () => finishDelete())
      await settle()
      if (expectedNavigation === 'keep a newer navigation') {
        expect(router.state.location.pathname + router.state.location.search).toBe(newerLocation)
      } else {
        expect(Object.fromEntries(new URLSearchParams(router.state.location.search))).toEqual({
          clusterId: 'cluster-a',
          kind: 'widgets.quality.example',
          namespace: '',
          tab: 'instances',
          q: 'quality',
        })
      }
    },
  )

  it('mounts relationships on demand and preserves the list scope through verified detail routes', async () => {
    await render('?clusterId=cluster-a&kind=widgets.quality.example&namespace=&q=quality')
    expect(fixtures.relationships).toEqual([])
    await relationshipTab()
    await vi.waitFor(() => expect(fixtures.relationships.length).toBeGreaterThan(0))
    const definitionPanel = fixtures.relationships.at(-1)!
    expect(definitionPanel).toMatchObject({
      kind: 'CustomResourceDefinition',
      name: 'widgets.quality.example',
      scope: { clusterId: 'cluster-a', namespace: null },
      rootAPI: {
        apiVersion: 'apiextensions.k8s.io/v1',
        resource: 'customresourcedefinitions',
      },
    })
    const instance: KubernetesResourceRef = {
      clusterId: 'cluster-a',
      apiVersion: 'quality.example/v1',
      kind: 'Widget',
      name: 'same-name',
      namespace: 'monitoring',
      scopeMode: 'namespace',
      uid: 'cluster-a-uid',
    }
    for (const foreignIdentity of [
      { clusterId: 'cluster-b' },
      { apiVersion: 'other.example/v1' },
      { apiVersion: 'quality.example/v2' },
      { kind: 'Unknown' },
      { name: 'unobserved' },
      { namespace: 'other' },
      { scopeMode: 'cluster' as const },
      { uid: 'replaced-uid' },
    ]) {
      expect(
        definitionPanel.resolveCustomDetailPath({ ...instance, ...foreignIdentity }),
      ).toBeNull()
    }
    const path = definitionPanel.resolveCustomDetailPath(instance)
    expect(path).not.toBeNull()
    await act(async () => router.navigate(path!))
    await settle()
    expect(container.querySelector('[data-testid="relationship-panel"]')).toBeNull()
    expect(router.state.location.pathname).toBe(detail)
    const search = new URLSearchParams(router.state.location.search)
    expect(Object.fromEntries(search)).toEqual({
      clusterId: 'cluster-a',
      kind: 'widgets.quality.example',
      namespace: '',
      q: 'quality',
      resource: 'same-name',
      resourceNamespace: 'monitoring',
      tab: 'overview',
    })
    await relationshipTab()
    expect(fixtures.relationships.at(-1)).toMatchObject({
      kind: 'Widget',
      name: 'same-name',
      scope: { clusterId: 'cluster-a', namespace: 'monitoring' },
      rootAPI: { apiVersion: 'quality.example/v1', resource: 'widgets' },
    })
    const definitionPath = fixtures.relationships.at(-1)?.resolveCustomDetailPath({
      clusterId: 'cluster-a',
      apiVersion: 'apiextensions.k8s.io/v1',
      kind: 'CustomResourceDefinition',
      name: 'widgets.quality.example',
      scopeMode: 'cluster',
    })
    expect(definitionPath).toBe(
      `${detail}?clusterId=cluster-a&kind=widgets.quality.example&namespace=&q=quality&tab=definition`,
    )
    expect(fixtures.writes).toEqual([])
  })

  it('protects a dirty YAML draft before entering relationships', async () => {
    await render()
    await dirtyEditor()
    await relationshipTab()
    expect(document.querySelector('.ant-modal-confirm')).not.toBeNull()
    expect(document.querySelector('textarea')?.value).toContain('unsaved-audit-draft')
    expect(fixtures.relationships).toEqual([])
    await button('放弃更改并切换')
    await vi.waitFor(() => expect(fixtures.relationships.length).toBeGreaterThan(0))
    expect(new URLSearchParams(router.state.location.search).get('tab')).toBe('relationships')
    expect(fixtures.writes).toEqual([])
  })

  it('does not mount an instance graph without instance read access', async () => {
    fixtures.instanceActions = []
    await render(resource.replace('tab=yaml', 'tab=relationships'))
    await vi.waitFor(() => expect(container.textContent).toContain('当前授权不允许查看此实例内容'))
    expect(fixtures.relationships).toEqual([])
  })

  it('keeps unsaved YAML and asks before the API Group back link leaves the editor', async () => {
    await render()
    await dirtyEditor()
    await clickCatalogBack()
    expect(router.state.location.pathname).toBe(detail)
    expect(document.querySelector('textarea')?.value).toContain('unsaved-audit-draft')
    expect(document.querySelector('.ant-modal-confirm')).not.toBeNull()
    expect(fixtures.writes).toEqual([])
  })

  it('establishes that a dirty editor protects its own return button and browser unload', async () => {
    await render()
    await dirtyEditor()
    const unload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(unload)
    expect(unload.defaultPrevented).toBe(true)
    await button('返回实例列表')
    expect(router.state.location.search).toContain('resource=same-name')
    expect(document.querySelector('.ant-modal-confirm')).not.toBeNull()
    expect(document.querySelector('textarea')?.value).toContain('unsaved-audit-draft')
    expect(fixtures.writes).toEqual([])
  })

  it('blocks browser-history navigation while preserving the dirty resource', async () => {
    await render('?clusterId=cluster-a&kind=widgets.quality.example&namespace=&tab=instances')
    await act(async () => router.navigate(detail + resource))
    await settle()
    await dirtyEditor()
    await act(async () => router.navigate(-1))
    await settle()
    expect(router.state.location.search).toContain('resource=same-name')
    expect(document.querySelector('textarea')?.value).toContain('unsaved-audit-draft')
    expect(document.querySelector('.ant-modal-confirm')).not.toBeNull()
  })

  it('retains the resource and its pending apply when the API Group back link is clicked', async () => {
    let release!: (result: { content: string }) => void
    fixtures.apply = new Promise((resolve) => {
      release = resolve
    })
    try {
      await render()
      await dirtyEditor()
      await button('Audit apply')
      await vi.waitFor(() => expect(fixtures.writes).toHaveLength(1))
      expect(client.isMutating()).toBe(1)
      expect(
        Array.from(document.querySelectorAll('button')).find(
          (node) => node.textContent === 'Audit apply',
        )?.disabled,
      ).toBe(true)
      expect(router.state.navigation.state).toBe('idle')
      await clickCatalogBack()
      expect(router.state.navigation.state).toBe('idle')
      expect(client.isMutating()).toBe(1)
      expect(router.state.location.pathname).toBe(detail)
      expect(document.querySelector('textarea')?.value).toContain('unsaved-audit-draft')
      expect(fixtures.writes[0]).toMatchObject({
        clusterId: 'cluster-a',
        resourceName: 'same-name',
        namespace: 'monitoring',
        mode: 'edit',
      })
    } finally {
      release?.({ content: 'apiVersion: quality.example/v1\nkind: Widget\n' })
      await settle()
    }
  })

  it('warns on browser unload for a dirty creation modal as well as an existing resource', async () => {
    await render('?clusterId=cluster-a&kind=widgets.quality.example&namespace=&tab=instances')
    await button('新建实例')
    await vi.waitFor(() => expect(document.querySelector('textarea')).not.toBeNull())
    await act(async () => {
      const input = document.querySelector('textarea')!
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        input,
        input.value + '# dirty-new-resource\n',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await settle()
    const unload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(unload)
    expect(unload.defaultPrevented).toBe(true)
    expect(fixtures.writes).toEqual([])
  })

  it('confirms a dirty global cluster switch once, restores on cancel, and updates the URL on acceptance', async () => {
    await render()
    await dirtyEditor()
    await act(async () => usePlatformScopeStore.getState().setClusterId('cluster-b'))
    await settle()
    expect(usePlatformScopeStore.getState().clusterId).toBe('cluster-a')
    expect(document.querySelector('textarea')?.value).toContain('unsaved-audit-draft')
    expect(fixtures.reads.filter((read) => read.clusterId === 'cluster-b')).toEqual([])
    expect(document.querySelectorAll('.ant-modal-confirm')).toHaveLength(1)
    await button('取消', document.querySelector('.ant-modal-confirm')!)
    await vi.waitFor(() => expect(document.querySelectorAll('.ant-modal-confirm')).toHaveLength(0))
    expect(usePlatformScopeStore.getState().clusterId).toBe('cluster-a')
    await act(async () => usePlatformScopeStore.getState().setClusterId('cluster-b'))
    await settle()
    await button('放弃更改并切换')
    await vi.waitFor(() => expect(usePlatformScopeStore.getState().clusterId).toBe('cluster-b'))
    const params = new URLSearchParams(router.state.location.search)
    expect(params.get('clusterId')).toBe('cluster-b')
    expect(params.has('resource')).toBe(false)
    expect(params.has('resourceNamespace')).toBe(false)
    expect(document.querySelector('textarea')).toBeNull()
    expect(fixtures.writes).toEqual([])
  })

  it('blocks cluster changes while an apply is pending without retargeting or replaying it', async () => {
    let release!: (result: { content: string }) => void
    fixtures.apply = new Promise((resolve) => {
      release = resolve
    })
    try {
      await render()
      await dirtyEditor()
      await button('Audit apply')
      await vi.waitFor(() => expect(client.isMutating()).toBe(1))
      await act(async () => usePlatformScopeStore.getState().setClusterId('cluster-b'))
      await settle()
      expect(usePlatformScopeStore.getState().clusterId).toBe('cluster-a')
      expect(document.querySelector('textarea')?.value).toContain('unsaved-audit-draft')
      expect(document.querySelector('.ant-modal-confirm')).toBeNull()
      expect(fixtures.reads.filter((read) => read.clusterId === 'cluster-b')).toEqual([])
      expect(fixtures.writes).toHaveLength(1)
      expect(fixtures.writes[0].clusterId).toBe('cluster-a')
    } finally {
      release?.({ content: 'apiVersion: quality.example/v1\nkind: Widget\n' })
      await settle()
    }
  })

  it('retains the uncertain apply receipt and fence across navigation and cluster-switch attempts', async () => {
    fixtures.applyError = new ApiError(503, 'Synthetic unknown apply', {
      method: 'PUT',
      path: '/synthetic-audit-yaml',
      requestId: 'audit-unknown-request',
    })
    await render()
    await dirtyEditor()
    await button('Audit apply')
    await vi.waitFor(() => expect(container.textContent).toContain('audit-unknown-request'))
    await clickCatalogBack()
    await act(async () => usePlatformScopeStore.getState().setClusterId('cluster-b'))
    await settle()
    expect(router.state.location.pathname).toBe(detail)
    expect(usePlatformScopeStore.getState().clusterId).toBe('cluster-a')
    expect(container.textContent).toContain('audit-unknown-request')
    expect(document.querySelector('textarea')?.value).toContain('unsaved-audit-draft')
    expect(fixtures.writes).toHaveLength(1)
  })

  it('keeps a creation draft in its opening namespace when the shared list namespace changes', async () => {
    usePlatformScopeStore.setState({ namespace: 'monitoring' })
    await render('?clusterId=cluster-a&kind=widgets.quality.example&tab=instances')
    await button('新建实例')
    await vi.waitFor(() =>
      expect(document.querySelector('textarea')?.value).toContain('namespace: monitoring'),
    )
    await act(async () => {
      const input = document.querySelector('textarea')!
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(
        input,
        input.value + '# keep-creation-scope\n',
      )
      input.dispatchEvent(new Event('input', { bubbles: true }))
      usePlatformScopeStore.getState().setNamespace('another-namespace')
    })
    await settle()
    expect(document.querySelector('textarea')?.value).toContain('namespace: monitoring')
    expect(document.querySelector('textarea')?.value).toContain('keep-creation-scope')
    await button('Audit apply')
    await vi.waitFor(() => expect(fixtures.writes).toHaveLength(1))
    expect(fixtures.writes[0]).toMatchObject({
      clusterId: 'cluster-a',
      namespace: 'monitoring',
      mode: 'create',
    })
  })

  it('retains a pending creation target across namespace, cluster and route changes', async () => {
    let release!: (result: { content: string }) => void
    fixtures.apply = new Promise((resolve) => {
      release = resolve
    })
    usePlatformScopeStore.setState({ namespace: 'monitoring' })
    try {
      await render('?clusterId=cluster-a&kind=widgets.quality.example&tab=instances')
      await button('新建实例')
      await vi.waitFor(() =>
        expect(document.querySelector('textarea')?.value).toContain('namespace: monitoring'),
      )
      await button('Audit apply')
      await vi.waitFor(() => expect(client.isMutating()).toBe(1))
      await act(async () => {
        usePlatformScopeStore.getState().setNamespace('another-namespace')
        usePlatformScopeStore.getState().setClusterId('cluster-b')
      })
      await settle()
      await clickCatalogBack()
      expect(usePlatformScopeStore.getState().clusterId).toBe('cluster-a')
      expect(router.state.location.pathname).toBe(detail)
      expect(document.querySelector('textarea')?.value).toContain('namespace: monitoring')
      expect(fixtures.writes).toHaveLength(1)
      expect(fixtures.writes[0]).toMatchObject({
        clusterId: 'cluster-a',
        namespace: 'monitoring',
        mode: 'create',
      })
    } finally {
      release?.({ content: 'apiVersion: quality.example/v1\nkind: Widget\n' })
      await settle()
    }
  })
})
