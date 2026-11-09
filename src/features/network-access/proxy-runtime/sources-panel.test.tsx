/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { beforeAll, beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import { SourcesPanel } from './sources-panel'
import { ApiError } from '@/services/api-error'
import type { NetworkProxySourceInput } from '@opensoha/contracts/gen/ts/sohaapi'
const api = vi.hoisted(() => ({
  sources: vi.fn(),
  bundles: vi.fn(),
  rules: vi.fn(),
  preview: vi.fn(),
  saveSource: vi.fn(),
}))
vi.mock('./workspace-api', () => ({ workspaceApi: api }))
let container: HTMLDivElement, root: ReturnType<typeof createRoot>, client: QueryClient
let previews: NetworkProxySourceInput[], saves: NetworkProxySourceInput[]
const source = {
  sourceId: 'existing',
  revision: 3,
  name: 'Old source',
  format: 'mihomo_yaml',
  deliveryMode: 'mapped_nodes',
  configured: true,
  nodeCount: 1,
  updatedAt: '2026-11-08T00:00:00Z',
}
beforeAll(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({
      matches: false,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  const computed = window.getComputedStyle.bind(window)
  vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => computed(element))
})
beforeEach(() => {
  vi.clearAllMocks()
  previews = []
  saves = []
  api.sources.mockResolvedValue({ items: [] })
  api.bundles.mockResolvedValue({ items: [] })
  api.rules.mockResolvedValue({
    orgRevision: 1,
    personalRevision: 1,
    rules: [],
    effectiveRules: [],
    conflicts: [],
  })
  api.preview.mockImplementation(async (_id, input) => {
    previews.push(structuredClone(input))
    return {
      supported: true,
      sourceRevision: input.expectedRevision,
      nodes: [],
      groups: [],
      errors: [],
    }
  })
  api.saveSource.mockImplementation(async (_id, input) => {
    saves.push(structuredClone(input))
    return { ...source, name: input.name }
  })
})
afterEach(async () => {
  await act(async () => root?.unmount())
  client?.clear()
  container?.remove()
})
async function render() {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <App>
          <SourcesPanel id="a" canUpdate />
        </App>
      </QueryClientProvider>,
    ),
  )
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
async function click(text: string) {
  const button = [...container.querySelectorAll('button')].find(
    (b) => b.textContent?.replace(/\s/g, '') === text.replace(/\s/g, ''),
  )
  expect(button).toBeDefined()
  await act(async () => button!.click())
}
function field(text: string) {
  const label = [...container.querySelectorAll('label')].find((l) => l.textContent === text)
  expect(label).toBeDefined()
  return document.getElementById(label!.htmlFor) as HTMLInputElement | HTMLTextAreaElement
}
async function fill(text: string, value: string) {
  const input = field(text)
  await act(async () => {
    const prototype =
      input instanceof HTMLTextAreaElement
        ? HTMLTextAreaElement.prototype
        : HTMLInputElement.prototype
    Object.getOwnPropertyDescriptor(prototype, 'value')!.set!.call(input, value)
    input.dispatchEvent(new Event('input', { bubbles: true }))
    input.dispatchEvent(new Event('change', { bubbles: true }))
  })
}
async function preview() {
  await click('转换预览（主动读取订阅）')
  await act(async () => vi.waitFor(() => expect(previews).toHaveLength(1)))
}
async function save() {
  await click('保存来源')
  await act(async () => vi.waitFor(() => expect(api.saveSource).toHaveBeenCalled()))
}
describe('source draft survives preview and save failures', () => {
  it('saves the same new manual-node draft after successful preview, then clears secrets', async () => {
    await render()
    await fill('来源名称', 'New source')
    await click('添加节点')
    await fill('逻辑节点 ID', 'node-a')
    await fill('名称', 'Node A')
    await fill('服务器', 'proxy.private')
    await fill('端口', '1080')
    await fill('秘密', 'new-secret')
    await preview()
    expect(field('秘密').value).toBe('new-secret')
    expect(
      JSON.stringify(
        client
          .getMutationCache()
          .getAll()
          .map((m) => m.state.variables),
      ),
    ).not.toContain('new-secret')
    expect(field('服务器').value).toBe('proxy.private')
    await save()
    expect(saves[0]).toEqual(previews[0])
    expect(saves[0].nodes?.[0].secret).toBe('new-secret')
    await act(async () =>
      vi.waitFor(() => expect(container.querySelector('input[id="nodes_0_secret"]')).toBeNull()),
    )
    expect(
      JSON.stringify(
        client
          .getQueryCache()
          .getAll()
          .map((q) => q.state.data),
      ),
    ).not.toContain('new-secret')
  })
  it('preserves replacement materials while previewing an existing source and saves that draft', async () => {
    api.sources.mockResolvedValue({ items: [source] })
    await render()
    await act(async () => vi.waitFor(() => expect(container.textContent).toContain('Old source')))
    await click('编辑')
    await fill('订阅地址（写入后不回显）', 'https://private.example/new?token=secret')
    await fill('或粘贴配置（保存成功后清除）', 'replacement-material')
    await preview()
    expect(field('或粘贴配置（保存成功后清除）').value).toBe('replacement-material')
    await save()
    expect(saves[0]).toEqual(previews[0])
    expect(saves[0].expectedRevision).toBe(3)
    expect(saves[0].content).toBe('replacement-material')
    expect(api.saveSource).toHaveBeenCalledWith('a', expect.anything(), 'existing')
    await act(async () => vi.waitFor(() => expect(container.textContent).toContain('新增来源')))
    expect(
      JSON.stringify(
        client
          .getQueryCache()
          .getAll()
          .map((q) => q.state.data),
      ),
    ).not.toContain('replacement-material')
  })
  it('keeps a rejected save editable and clears material only after a successful corrected save', async () => {
    api.sources.mockResolvedValue({ items: [source] })
    api.saveSource.mockImplementationOnce(async (_id, input) => {
      saves.push(structuredClone(input))
      throw new ApiError(400, 'invalid source')
    })
    await render()
    await act(async () => vi.waitFor(() => expect(container.textContent).toContain('Old source')))
    await click('编辑')
    await fill('或粘贴配置（保存成功后清除）', 'invalid-draft')
    await save()
    expect(field('或粘贴配置（保存成功后清除）').value).toBe('invalid-draft')
    await fill('或粘贴配置（保存成功后清除）', 'fixed-draft')
    await click('保存来源')
    await act(async () => vi.waitFor(() => expect(saves).toHaveLength(2)))
    expect(saves[1].content).toBe('fixed-draft')
    await act(async () => vi.waitFor(() => expect(container.textContent).toContain('新增来源')))
  })
})
