/** @vitest-environment jsdom */
import { act } from 'react'
import { ApiError } from '@/services/api-error'
import type { NetworkProxyRuntimeCommandInput } from '@opensoha/contracts/gen/ts/sohaapi'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { beforeAll, beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import { SourcesPanel } from './sources-panel'
import { RuntimePanel } from './runtime-panel'
import { DeploymentPanel } from './deployment-panel'
import { RulesPanel } from './rules-panel'
import type { NetworkProxyInstance } from '@opensoha/contracts/gen/ts/sohaapi'
const api = vi.hoisted(() => ({
  sources: vi.fn(),
  bundles: vi.fn(),
  rules: vi.fn(),
  deployment: vi.fn(),
  queue: vi.fn(),
  command: vi.fn(),
  cancel: vi.fn(),
}))
vi.mock('./workspace-api', () => ({ workspaceApi: api }))
let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
async function render(element: React.ReactNode) {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  await act(async () => {
    root.render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <App>{element}</App>
      </QueryClientProvider>,
    )
  })
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
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
  api.sources.mockResolvedValue({ items: [] })
  api.bundles.mockResolvedValue({ items: [] })
  api.rules.mockResolvedValue({
    orgRevision: 2,
    personalRevision: 3,
    rules: [],
    effectiveRules: [],
    conflicts: [],
  })
  api.deployment.mockResolvedValue({ revision: 0, state: 'unbound', members: [] })
})
afterEach(async () => {
  await act(async () => root?.unmount())
  container?.remove()
})
describe('proxy workspace visible boundaries', () => {
  it('keeps source writes absent for read-only users', async () => {
    await render(<SourcesPanel id="a" canUpdate={false} />)
    expect(container.textContent).toContain('来源与发布')
    expect(container.textContent).not.toContain('新增来源')
    expect(container.textContent).not.toContain('保存来源')
  })
  it('offers three explicit delivery modes with an initially safe mode', async () => {
    await render(<SourcesPanel id="a" canUpdate />)
    expect(container.textContent).toContain('逐节点内网映射')
    expect(container.textContent).toContain('添加节点')
    expect(container.textContent).toContain('转换预览（主动读取订阅）')
  })
  it('does not optimistically enable an undeclared runtime capability', async () => {
    await render(
      <RuntimePanel
        instance={
          {
            id: 'a',
            name: 'A',
            engine: 'v2ray',
            processEpoch: 'epoch',
            capabilities: [],
            enabled: true,
            status: 'online',
            desiredRevision: 1,
            observedRevision: 1,
            createdAt: '2026-11-08T00:00:00Z',
            updatedAt: '2026-11-08T00:00:00Z',
          } as NetworkProxyInstance
        }
        canUpdate
      />,
    )
    const button = [...container.querySelectorAll('button')].find(
      (b) => b.textContent === '读取策略组',
    )
    expect(button?.disabled).toBe(true)
    expect(api.queue).not.toHaveBeenCalled()
  })
  it('explains missing approved deployment rather than accepting infrastructure identities', async () => {
    await render(<DeploymentPanel id="a" canUpdate />)
    expect(container.textContent).toContain('待安装者配置批准的部署目标')
    expect(container.querySelector('input:not([role="spinbutton"])')).toBeNull()
    expect(
      [...container.querySelectorAll('button')].find(
        (b) => b.textContent?.replace(/\s/g, '') === '启动',
      )?.disabled,
    ).toBe(true)
  })
  it('displays safe effective rule and conflict metadata', async () => {
    api.rules.mockResolvedValue({
      orgRevision: 2,
      personalRevision: 3,
      rules: [],
      effectiveRules: [],
      conflicts: [
        { ruleId: 'personal-1', blockingRuleId: 'required-1', reasonCode: 'masked_by_required' },
      ],
    })
    await render(<RulesPanel id="a" canUpdate={false} />)
    expect(container.textContent).toContain('personal-1')
    expect(container.textContent).toContain('required-1')
    expect(container.textContent).toContain('masked_by_required')
  })
})

it.each(['known reject', 'unknown result', 'read-only cancel'])(
  'scopes runtime command recovery and cancellation: %s',
  async (scenario) => {
    const commandFor = (
      input: NetworkProxyRuntimeCommandInput,
      status: 'pending' | 'succeeded',
    ) => ({
      commandId: input.requestId,
      requestId: input.requestId,
      processEpoch: input.processEpoch,
      kind: input.kind,
      expiresAt: input.expiresAt,
      status,
    })
    api.queue.mockImplementation(async (_id: string, input: NetworkProxyRuntimeCommandInput) => {
      if (scenario === 'known reject') throw new ApiError(409, 'epoch changed', { method: 'POST' })
      if (scenario === 'unknown result')
        throw new ApiError(0, 'lost command response', { method: 'POST' })
      return commandFor(input, 'pending')
    })
    api.command.mockImplementation(async () => {
      const result = commandFor(
        api.queue.mock.calls[0][1],
        scenario === 'read-only cancel' ? 'pending' : 'succeeded',
      )
      return scenario === 'unknown result' ? { ...result, kind: 'probe' } : result
    })
    await render(
      <RuntimePanel
        instance={
          {
            id: 'a',
            name: 'A',
            engine: 'v2ray',
            processEpoch: 'epoch',
            capabilities: ['groups'],
          } as NetworkProxyInstance
        }
        canUpdate={scenario !== 'read-only cancel'}
      />,
    )
    const button = (name: string) =>
      [...container.querySelectorAll('button')].find(
        (b) => b.textContent?.replace(/\s/g, '') === name,
      )
    const run = button('读取策略组')!
    await act(async () => run.click())
    if (scenario === 'known reject') {
      await act(async () =>
        vi.waitFor(() => expect(container.textContent).toContain('epoch changed')),
      )
      expect(run.disabled).toBe(false)
      expect(api.command).not.toHaveBeenCalled()
      return
    }
    const input = api.queue.mock.calls[0][1] as NetworkProxyRuntimeCommandInput
    await act(async () =>
      vi.waitFor(() => expect(api.command).toHaveBeenCalledWith('a', input.requestId)),
    )
    expect(run.disabled).toBe(true)
    if (scenario === 'read-only cancel') {
      expect(button('读取原回执')).toBeDefined()
      expect(button('取消请求')).toBeUndefined()
      expect(api.cancel).not.toHaveBeenCalled()
      return
    }
    expect(container.textContent).toContain('运行回执 · 未知')
    api.command.mockResolvedValue(commandFor(input, 'succeeded'))
    await act(async () => button('读取原回执')!.click())
    await vi.waitFor(async () => {
      await act(async () => new Promise((resolve) => setTimeout(resolve, 0)))
      expect(run.disabled).toBe(false)
    })
    expect(container.textContent).toContain('运行回执 · succeeded')
    expect(api.queue).toHaveBeenCalledTimes(1)
  },
)
