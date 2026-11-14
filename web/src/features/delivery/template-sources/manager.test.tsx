/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { App } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { deliveryApi } from '../api'
import { deliveryKeys } from '../keys'
import type {
  DeliveryDocumentSourceInfo,
  DeliveryTemplateSource,
  DeliveryTemplateSourceAssociation,
  DeliveryTemplateSyncRun,
} from '../types'
import { TemplateSourceManager } from './manager'
import { DocumentSourcePanel } from './source-panel'

vi.mock('../api', () => ({
  deliveryApi: {
    documents: { source: vi.fn() },
    repositories: { list: vi.fn() },
    templateSources: {
      list: vi.fn(),
      detail: vi.fn(),
      create: vi.fn(),
      update: vi.fn(),
      objects: vi.fn(),
      sync: vi.fn(),
      runs: vi.fn(),
      run: vi.fn(),
      apply: vi.fn(),
      detach: vi.fn(),
      remove: vi.fn(),
    },
  },
}))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: () => true,
}))

const originalSource: DeliveryTemplateSource = {
  id: 'source-1',
  name: 'Release library',
  repositoryId: 'repository-1',
  refType: 'branch',
  refValue: 'main',
  path: 'templates',
  kinds: ['BuildTemplate'],
  enabled: true,
  generation: 7,
  createdAt: '2026-09-13T00:00:00Z',
  updatedAt: '2026-09-13T00:00:00Z',
}
const association: DeliveryTemplateSourceAssociation = {
  sourceId: 'source-1',
  kind: 'BuildTemplate',
  objectId: 'build-1',
  key: 'image',
  path: 'templates/image.soha.yaml',
  lastImportedRevision: 3,
  resolvedCommit: 'a'.repeat(40),
  sourceDigest: 'sha256:source',
  normalizedSpecDigest: 'sha256:normalized',
  syncRunId: 'run-1',
  removed: false,
}
const ready: DeliveryTemplateSyncRun = {
  id: 'run-1',
  sourceId: 'source-1',
  sourceGeneration: 7,
  status: 'ready',
  actorId: 'user-1',
  createdAt: '2026-09-13T00:00:00Z',
  updatedAt: '2026-09-13T00:00:00Z',
  resolvedCommit: 'a'.repeat(40),
  preview: {
    id: 'preview-1',
    valid: true,
    candidateDigest: 'sha256:fixed',
    expiresAt: '2099-01-01T00:00:00Z',
    diagnostics: [],
    candidates: [
      {
        path: 'templates/image.soha.yaml',
        document: {
          apiVersion: 'delivery.soha.io/v1alpha1',
          kind: 'BuildTemplate',
          metadata: { name: 'image' },
          spec: { buildCommands: ['echo build'] },
        },
        action: 'update',
        targetId: 'build-1',
        expectedRevision: 3,
        sourceDigest: 'sha256:source',
        normalizedSpecDigest: 'sha256:normalized',
        changedPaths: ['/spec/buildCommands/0'],
      },
    ],
  },
  removed: [{ ...association, objectId: 'old-1', key: 'old', path: 'templates/old.soha.yaml' }],
}
let root: Root
let host: HTMLDivElement
let client: QueryClient
let source: DeliveryTemplateSource
let run: DeliveryTemplateSyncRun | undefined

beforeEach(() => {
  ;(globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  window.matchMedia = vi
    .fn()
    .mockReturnValue({ matches: false, addListener: vi.fn(), removeListener: vi.fn() })
  const getStyle = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getStyle(element))
  host = document.createElement('div')
  document.body.append(host)
  root = createRoot(host)
  client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  source = structuredClone(originalSource)
  run = undefined
  vi.mocked(deliveryApi.templateSources.list).mockImplementation(async () => [source])
  vi.mocked(deliveryApi.templateSources.detail).mockImplementation(async () => source)
  vi.mocked(deliveryApi.templateSources.runs).mockImplementation(async () => (run ? [run] : []))
  vi.mocked(deliveryApi.templateSources.run).mockImplementation(async () => run!)
  vi.mocked(deliveryApi.templateSources.objects).mockResolvedValue([association])
})
afterEach(async () => {
  await act(async () => root.unmount())
  client.clear()
  host.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.resetAllMocks()
})
async function settle() {
  for (let index = 0; index < 4; index++)
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 15))
    })
}
async function render(node: ReactNode) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <App>{node}</App>
      </QueryClientProvider>,
    ),
  )
  await settle()
}
function button(label: string) {
  const found = [...document.querySelectorAll('button')].find(
    (item) => item.textContent?.replace(/\s/g, '') === label.replace(/\s/g, ''),
  )
  expect(found, label).toBeDefined()
  return found!
}
async function click(label: string) {
  await act(async () => button(label).click())
  await settle()
}

it('retries lost read/apply responses using the same candidate and shows removed-file impact', async () => {
  vi.mocked(deliveryApi.templateSources.sync)
    .mockImplementationOnce(async () => {
      run = structuredClone(ready)
      source = { ...source, lastSyncRunId: ready.id }
      throw new Error('response lost')
    })
    .mockImplementationOnce(async () => run!)
  vi.mocked(deliveryApi.templateSources.apply)
    .mockRejectedValueOnce(new Error('apply response lost'))
    .mockImplementationOnce(async () => {
      run = { ...ready, status: 'applied', result: { previewId: 'preview-1', objects: [] } }
      source = { ...source, generation: 8 }
      return run
    })
  await render(<TemplateSourceManager onClose={() => {}} />)
  await click('读取并预览')
  await click('恢复本次读取结果')
  expect(vi.mocked(deliveryApi.templateSources.sync).mock.calls[0]).toEqual(
    vi.mocked(deliveryApi.templateSources.sync).mock.calls[1],
  )
  expect(document.body.textContent).toContain('/spec/buildCommands/0')
  expect(document.body.textContent).toContain('templates/old.soha.yaml')
  await click('确认导入此提交')
  await click('确认导入此提交')
  const calls = vi.mocked(deliveryApi.templateSources.apply).mock.calls
  expect(calls).toHaveLength(2)
  expect(calls[0]).toEqual(calls[1])
  expect(calls[0]).toEqual([
    'source-1',
    'run-1',
    { expectedGeneration: 7, candidateDigest: 'sha256:fixed', idempotencyKey: 'web-apply-run-1' },
  ])
  expect(document.body.textContent).toContain('已导入')
  expect(document.body.textContent).not.toContain('请检查下方差异后确认导入')
  expect(document.body.textContent).not.toContain('候选有效至')
  expect(document.body.textContent).not.toContain('工作流配置已保存')
  expect(deliveryApi.templateSources.remove).not.toHaveBeenCalled()
})

it('requires a concrete detach review and keeps the generation that was reviewed', async () => {
  vi.mocked(deliveryApi.templateSources.detach).mockResolvedValue(undefined)
  await render(<TemplateSourceManager onClose={() => {}} />)
  const tab = [...document.querySelectorAll<HTMLElement>('[role="tab"]')].find(
    (item) => item.textContent === '关联对象',
  )!
  await act(async () => tab.click())
  await settle()
  await click('解除关联')
  expect(document.body.textContent).toContain('解除 image 的 Git 关联')
  expect(deliveryApi.templateSources.detach).not.toHaveBeenCalled()
  source = { ...source, generation: 8 }
  await act(async () => client.invalidateQueries({ queryKey: deliveryKeys.all }))
  await settle()
  await click('确认解除')
  expect(deliveryApi.templateSources.detach).toHaveBeenCalledWith(
    'source-1',
    'BuildTemplate',
    'build-1',
    { expectedGeneration: 7, disposition: 'keep' },
  )
})

it('renders historical provenance after detachment and does not replace it with current association', async () => {
  const info: DeliveryDocumentSourceInfo = {
    repository: {
      id: 'repository-old',
      name: 'Original repository',
      url: 'ssh://git@example.test/catalog.git',
    },
    provenance: {
      ...association,
      repositoryId: 'repository-old',
      version: 2,
      treeDigest: 'tree-old',
    },
  }
  vi.mocked(deliveryApi.documents.source).mockResolvedValue(info)
  await render(<DocumentSourcePanel kind="BuildTemplate" id="build-1" version={2} />)
  expect(deliveryApi.documents.source).toHaveBeenCalledWith('BuildTemplate', 'build-1', 2)
  expect(document.body.textContent).toContain('Soha 管理')
  expect(document.body.textContent).toContain('Original repository')
  await click('查看仓库')
  expect(document.body.textContent).toContain('ssh://git@example.test/catalog.git')
  expect(document.body.textContent).toContain('v2 版本来源')
})
