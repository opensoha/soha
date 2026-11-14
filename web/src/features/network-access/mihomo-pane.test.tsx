/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { beforeAll, beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import { NetworkMihomoPane } from './mihomo-pane'
const state = vi.hoisted(() => ({ allowed: true }))
const api = vi.hoisted(() => ({
  listNetworkMihomoProfiles: vi.fn(),
  createNetworkMihomoProfile: vi.fn(),
  updateNetworkMihomoProfile: vi.fn(),
  listInstances: vi.fn(),
  bundles: vi.fn(),
}))
vi.mock('./api', async (original) => ({ ...(await original<typeof import('./api')>()), ...api }))
vi.mock('./proxy-runtime/api', () => ({ listInstances: api.listInstances }))
vi.mock('./proxy-runtime/workspace-api', () => ({ workspaceApi: { bundles: api.bundles } }))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({ data: { data: {} } }),
  hasPermission: () => state.allowed,
}))
const profile = {
  id: 'old',
  deviceId: 'device-a',
  name: 'Existing',
  mode: 'managed_follow',
  sourceType: 'managed_subscription',
  status: 'active',
  subscriptionConfigured: true,
  revision: 1,
  mixedPort: 7890,
  controllerPort: 9090,
  dnsMode: 'fake_ip',
  fakeIpRange: '198.18.0.0/15',
  selectorGroup: 'PROXY',
  selectedProxy: 'old-node',
  bypassCidrs: [],
  bypassHosts: [],
  failClosed: true,
  createdAt: '',
  updatedAt: '',
}
const bundle = {
  bundleId: 'bundle-a',
  state: 'activated',
  deliveryMode: 'mapped_nodes',
  sourceRevision: 4,
  orgRevision: 2,
  revision: 1,
  activeRevision: 1,
}
beforeEach(() => {
  vi.clearAllMocks()
  state.allowed = true
  api.listNetworkMihomoProfiles.mockResolvedValue([])
  api.listInstances.mockResolvedValue([{ id: 'runtime-a', name: 'Runtime A' }])
  api.bundles.mockResolvedValue({ items: [bundle] })
  api.createNetworkMihomoProfile.mockResolvedValue(profile)
  api.updateNetworkMihomoProfile.mockResolvedValue(profile)
})
let container: HTMLDivElement, root: ReturnType<typeof createRoot>, client: QueryClient

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
          <NetworkMihomoPane canCreate canUpdate canDelete={false} />
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
  const label = [...document.querySelectorAll('label')].find((l) => l.textContent === text)
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

async function select(input: HTMLInputElement, label: string) {
  await act(async () => input.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })))
  await act(async () => {
    await new Promise((r) => setTimeout(r, 10))
  })
  const option = [...document.querySelectorAll<HTMLElement>('.ant-select-item-option')].find((e) =>
    e.textContent?.includes(label),
  )
  expect(option).toBeDefined()
  await act(async () => option!.click())
}
async function chooseBundle() {
  await select(field('连接配置来源') as HTMLInputElement, '受管发布包')
  await select(document.querySelector('input[aria-label="发布包代理实例"]')!, 'Runtime A')
  await act(async () => {
    await new Promise((r) => setTimeout(r, 20))
  })
  await select(document.querySelector('input[aria-label="已激活发布包"]')!, 'bundle-a')
}
async function saveDialog() {
  const b = [...document.querySelectorAll<HTMLButtonElement>('.ant-modal button')].find(
    (b) => b.textContent?.replace(/\s/g, '') === '保存',
  )
  await act(async () => b!.click())
  await act(async () => {
    await new Promise((r) => setTimeout(r, 30))
  })
}
describe('Mihomo profile publication binding', () => {
  it('creates a profile from an activated bundle without legacy material', async () => {
    await render()
    await click('新增代理隧道')
    await fill('设备 ID', 'device-a')
    await fill('名称', 'Bound')
    await chooseBundle()
    await saveDialog()
    expect(api.createNetworkMihomoProfile.mock.calls[0][0]).toEqual(
      expect.objectContaining({
        bundleId: 'bundle-a',
        deliveryMode: 'mapped_nodes',
        sourceRevision: 4,
        orgRevision: 2,
        mode: 'managed_follow',
        failClosed: true,
      }),
    )
    const payload = api.createNetworkMihomoProfile.mock.calls[0][0]
    for (const key of ['sourceType', 'subscriptionUrl', 'manualNode', 'selectedProxy'])
      expect(payload).not.toHaveProperty(key)
  })
  it('preserves legacy subscription secrets when editing unrelated fields', async () => {
    api.listNetworkMihomoProfiles.mockResolvedValue([profile])
    await render()
    await act(async () =>
      document.querySelector<HTMLButtonElement>('button[aria-label="编辑 mihomo 配置"]')!.click(),
    )
    await fill('名称', 'Renamed')
    await saveDialog()
    expect(api.updateNetworkMihomoProfile.mock.calls[0][0]).toEqual({
      profileId: 'old',
      input: expect.objectContaining({
        name: 'Renamed',
        sourceType: 'managed_subscription',
        selectedProxy: 'old-node',
      }),
    })
    expect(api.updateNetworkMihomoProfile.mock.calls[0][0].input).not.toHaveProperty(
      'subscriptionUrl',
    )
  })
  it('does not fetch directories without permission and retains an existing binding', async () => {
    state.allowed = false
    api.listNetworkMihomoProfiles.mockResolvedValue([
      { ...profile, ...bundle, sourceType: undefined, selectedProxy: undefined },
    ])
    await render()
    await act(async () =>
      document.querySelector<HTMLButtonElement>('button[aria-label="编辑 mihomo 配置"]')!.click(),
    )
    expect(document.body.textContent).toContain('无代理实例查看权限')
    await fill('名称', 'Bound rename')
    await saveDialog()
    expect(api.listInstances).not.toHaveBeenCalled()
    expect(api.bundles).not.toHaveBeenCalled()
    expect(api.updateNetworkMihomoProfile.mock.calls[0][0].input.bundleId).toBe('bundle-a')
  })
  it('requires direct disclosure confirmation and resets it when switching sources', async () => {
    api.bundles.mockResolvedValue({ items: [{ ...bundle, deliveryMode: 'direct_subscription' }] })
    await render()
    await click('新增代理隧道')
    await fill('设备 ID', 'device-a')
    await fill('名称', 'Direct')
    await chooseBundle()
    await saveDialog()
    expect(api.createNetworkMihomoProfile).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('请选择已激活发布包')
    const checkbox = document.querySelector<HTMLInputElement>('.ant-modal input[type="checkbox"]')!
    await act(async () => checkbox.click())
    await select(field('连接配置来源') as HTMLInputElement, '既有直订阅')
    await select(field('连接配置来源') as HTMLInputElement, '受管发布包')
    await saveDialog()
    expect(api.createNetworkMihomoProfile).not.toHaveBeenCalled()
    await act(async () =>
      document.querySelector<HTMLInputElement>('.ant-modal input[type="checkbox"]')!.click(),
    )
    await saveDialog()
    expect(api.createNetworkMihomoProfile.mock.calls[0][0]).toEqual(
      expect.objectContaining({ deliveryMode: 'direct_subscription' }),
    )
  })
})
