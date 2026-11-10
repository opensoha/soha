/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { RootCRConfigurationDrawer } from './root-cr-configuration-drawer'
import { rootEnvironment, rootWorkload, rootYAML } from './root-cr-fixtures.test-data'
import { rootCRConfiguration, rootCRPackageInput } from './root-cr-configuration'
import type { ManifestPackage } from '../manifests/types'

const state = vi.hoisted(() => ({ denied: '', get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: (_: unknown, key: string) => key !== state.denied,
}))
vi.mock('@/services/api-client', () => ({
  api: { get: state.get, post: state.post, put: state.put },
}))
vi.mock('@/components/yaml-draft-diff-editor', () => ({
  YamlDraftDiffEditor: ({
    modified,
    onChange,
  }: {
    modified: string
    onChange: (value: string) => void
  }) => (
    <textarea
      aria-label="根 CR 配置草稿"
      value={modified}
      onChange={(event) => onChange(event.target.value)}
    />
  ),
}))
let root: ReturnType<typeof createRoot>
let client: QueryClient
let packages: ManifestPackage[] = []
const onPlan = vi.fn()
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const computed = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => computed(element))
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: () => ({
      matches: false,
      addListener() {},
      removeListener() {},
      addEventListener() {},
      removeEventListener() {},
    }),
  })
  state.denied = ''
  packages = []
  state.get.mockReset()
  state.post.mockReset()
  state.put.mockReset()
  onPlan.mockReset()
  state.get.mockImplementation(async (url: string) => {
    if (url.includes('/source')) return { data: { content: rootYAML } }
    if (url.includes('/revisions')) return { data: [] }
    if (url.includes('manifest-packages'))
      return { data: { items: packages, total: packages.length, page: 1, pageSize: 100 } }
    throw new Error('Unexpected GET ' + url)
  })
  state.post.mockImplementation(async (url: string, input: Record<string, unknown>) => {
    if (url.endsWith('/revisions')) {
      packages = [
        {
          ...packages[0],
          status: 'published',
          currentRevision: 1,
          updatedAt: '2026-10-22T03:00:00Z',
        },
      ]
      return { data: packages[0] }
    }
    if (url.endsWith('/manifest-packages')) {
      packages = [
        {
          ...input,
          id: 'package',
          status: 'draft',
          currentRevision: 0,
          bindings: [{ ...(input.bindings as object[])[0], id: 'manifest-binding' }],
          createdAt: '2026-10-22T02:00:00Z',
          updatedAt: '2026-10-22T02:00:00Z',
        } as ManifestPackage,
      ]
      return { data: packages[0] }
    }
    throw new Error('Unexpected POST ' + url)
  })
  state.put.mockResolvedValue({ data: {} })
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})
async function tick() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 30))
  })
}
async function render(binding = rootEnvironment) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <RootCRConfigurationDrawer
          workload={rootWorkload}
          binding={binding}
          onClose={() => {}}
          onOpenPlan={onPlan}
        />
      </QueryClientProvider>,
    ),
  )
  await tick()
  await tick()
}
async function click(label: string) {
  const button = [...document.querySelectorAll('button')].find(
    (item) => item.textContent?.replace(/\s/g, '') === label,
  )!
  expect(button).toBeTruthy()
  expect(button.disabled).toBe(false)
  await act(async () => button.click())
  await tick()
  await tick()
}

it('saves only configuration then explicitly records a version and publication control without deploying', async () => {
  await render()
  expect(document.body.textContent).toContain('当前仅观察')
  await click('保存草稿')
  expect(state.post.mock.calls[0][1].files[0].content).not.toMatch(
    /status:|finalizers:|resourceVersion:/,
  )
  await click('保存配置版本')
  expect(state.post.mock.calls[1]).toEqual([
    '/delivery/manifest-packages/package/revisions',
    { expectedUpdatedAt: '2026-10-22T02:00:00Z' },
  ])
  await click('允许Soha发布根配置')
  const input = state.put.mock.calls[0][1]
  expect(input.expectedUpdatedAt).toBe(rootEnvironment.updatedAt)
  expect(input.targets[0]).toMatchObject({
    executorKind: 'manifest_ssa',
    configRef: 'manifest-binding',
    metadata: { ownershipMode: 'managed', rootCRGeneration: 2 },
  })
  expect(
    state.post.mock.calls.some(
      ([url]) => String(url).includes('/plans') || String(url).includes('/desired-revision'),
    ),
  ).toBe(false)
})
it('hands a published root configuration to the existing application plan action', async () => {
  const input = rootCRPackageInput(
    rootEnvironment,
    rootWorkload,
    rootCRConfiguration(rootYAML, rootWorkload).content,
  )
  packages = [
    {
      ...input,
      id: 'package',
      status: 'published',
      currentRevision: 3,
      bindings: [{ ...input.bindings[0], id: 'manifest-binding' }],
      createdAt: 'now',
      updatedAt: 'now',
    },
  ]
  const binding = structuredClone(rootEnvironment)
  binding.targets![0].executorKind = 'manifest_ssa'
  binding.targets![0].configRef = 'manifest-binding'
  binding.targets![0].metadata = { ...binding.targets![0].metadata, ownershipMode: 'managed' }
  await render(binding)
  await click('生成发布计划')
  expect(onPlan).toHaveBeenCalledWith('target', 3)
  expect(state.post).not.toHaveBeenCalled()
})
it('does not request CR or copied Manifest configuration after sensitive permission denial', async () => {
  state.denied = 'platform.configuration.secret-data.view'
  await render()
  expect(state.get).not.toHaveBeenCalled()
  expect(document.body.textContent).toContain('敏感数据权限')
})

it('does not select an unrelated package or recover a UID file from another namespace', async () => {
  const input = rootCRPackageInput(
    rootEnvironment,
    rootWorkload,
    rootCRConfiguration(rootYAML, rootWorkload).content,
  )
  packages = [
    {
      ...input,
      id: 'unrelated',
      name: 'unrelated package',
      status: 'draft',
      currentRevision: 0,
      files: [{ path: 'settings.yaml', content: 'unrelated-configuration' }],
      bindings: [{ ...input.bindings[0], id: 'unrelated-binding' }],
      createdAt: 'now',
      updatedAt: 'now',
    },
    {
      ...input,
      id: 'other-namespace',
      status: 'draft',
      currentRevision: 0,
      files: [{ ...input.files[0], content: 'other-namespace-configuration' }],
      bindings: [{ ...input.bindings[0], id: 'other-binding', namespace: 'another-namespace' }],
      createdAt: 'now',
      updatedAt: 'now',
    },
  ]
  await render()
  const editor = document.querySelector('textarea')!
  expect(editor.value).toBe(rootCRConfiguration(rootYAML, rootWorkload).content)
  expect(state.get.mock.calls.some(([url]) => String(url).includes('/revisions'))).toBe(false)
  await click('保存草稿')
  expect(state.post.mock.calls[0][0]).toBe('/delivery/manifest-packages')
  expect(state.put).not.toHaveBeenCalled()
})
