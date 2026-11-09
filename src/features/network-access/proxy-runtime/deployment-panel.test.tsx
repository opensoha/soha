/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { beforeAll, beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import { DeploymentPanel } from './deployment-panel'
const api = vi.hoisted(() => ({ deployment: vi.fn(), bind: vi.fn(), control: vi.fn() }))
vi.mock('./workspace-api', () => ({ workspaceApi: api }))
let root: ReturnType<typeof createRoot>, container: HTMLDivElement, client: QueryClient
const bound = {
  revision: 4,
  slotId: 'approved-a',
  objectUID: 'uid-accepted',
  templateHash: 'sha256:accepted-pin',
  owner: 'soha-managed',
  ownerEpoch: 1,
  state: 'conflict',
  reasonCode: 'rebind_required',
  members: [],
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
  vi.spyOn(window, 'getComputedStyle').mockImplementation((e) => computed(e))
})
beforeEach(() => {
  vi.clearAllMocks()
  api.deployment.mockResolvedValue(bound)
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
  await paint()
}
async function paint(canUpdate = true) {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <App>
          <DeploymentPanel id="a" canUpdate={canUpdate} />
        </App>
      </QueryClientProvider>,
    ),
  )
  await act(async () => {
    await new Promise((r) => setTimeout(r, 20))
  })
}
const button = (name: string) =>
  [...container.querySelectorAll<HTMLButtonElement>('button')].find(
    (b) => b.textContent?.replace(/\s/g, '') === name,
  )
describe('controlled deployment upgrade guidance', () => {
  it('shows the accepted pin and blocks start/scale when rebind is required', async () => {
    await render()
    expect(container.textContent).toContain('uid-accepted')
    expect(container.textContent).toContain('sha256:accepted-pin')
    expect(container.textContent).toContain('批准目标已发生变更，需要重新绑定')
    expect(button('启动')?.disabled).toBe(true)
    expect(button('调整副本')?.disabled).toBe(true)
    expect(api.control).not.toHaveBeenCalled()
  })
  it('opens manual rebind without treating the form as approval or submitting automatically', async () => {
    await render()
    await act(async () => button('绑定安装者批准的目标')!.click())
    expect(container.textContent).toContain('填表和接管勾选不是批准证明')
    expect(container.querySelector<HTMLInputElement>('input[type="checkbox"]')?.checked).toBe(false)
    expect(container.querySelector<HTMLInputElement>('#objectUID')?.value).toBe('')
    expect(api.bind).not.toHaveBeenCalled()
    expect(button('提交绑定')).toBeDefined()
    expect(container.textContent).toContain('--kernel-preflight <私有配置文件>')
  })
  it('distinguishes unknown observation from stopped-zero and still requires a trusted receipt', async () => {
    api.deployment.mockResolvedValue({
      ...bound,
      state: 'unknown',
      reasonCode: 'observation_unknown',
    })
    await render()
    expect(container.textContent).toContain('观察副本：未知')
    expect(container.textContent).toContain('尚未观察到已停止且副本为 0')
    expect(button('启动')?.disabled).toBe(true)
    await act(async () => {
      client.setQueryData(['network-access', 'proxy-workspace', 'a', 'deployment'], {
        ...bound,
        state: 'stopped',
        observedReplicas: 0,
        reasonCode: undefined,
      })
    })
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20))
    })
    expect(container.textContent).toContain('观察为已停止、0 副本；仍须核对原停止操作的成功回执')
    expect(api.control).not.toHaveBeenCalled()
  })
})

it.each(['frozen count', 'changed version', 'changed observation', 'revoked permission'])(
  'binds deployment confirmation to its original intent: %s',
  async (scenario) => {
    const ready = { ...bound, state: 'ready', observedReplicas: 2, reasonCode: undefined }
    api.deployment.mockResolvedValue(ready)
    api.control.mockResolvedValue({ operationId: 'op-1', status: 'pending' })
    await render()
    const uuid = vi.spyOn(crypto, 'randomUUID')
    try {
      await act(async () => button('调整副本')!.click())
      expect(api.control).not.toHaveBeenCalled()
      const requestId = uuid.mock.results[0]?.value
      expect(requestId).toEqual(expect.any(String))
      expect(document.body.textContent).toContain('版本 4 · 目标副本 1')
      if (scenario === 'changed version' || scenario === 'changed observation') {
        await act(async () =>
          client.setQueryData(
            ['network-access', 'proxy-workspace', 'a', 'deployment'],
            scenario === 'changed version'
              ? { ...ready, revision: 5 }
              : { ...ready, observedReplicas: 3 },
          ),
        )
        await act(async () => new Promise((r) => setTimeout(r, 20)))
      } else if (scenario === 'revoked permission') await paint(false)
      else {
        const input = container.querySelector<HTMLInputElement>('input[aria-label="目标副本"]')!
        await act(async () => {
          Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(
            input,
            '2',
          )
          input.dispatchEvent(new Event('input', { bubbles: true }))
          input.dispatchEvent(new Event('change', { bubbles: true }))
        })
        expect(input.value).toBe('2')
      }
      const confirm = document.querySelector<HTMLButtonElement>('.ant-popconfirm .ant-btn-primary')
      expect(confirm).not.toBeNull()
      await act(async () => confirm?.click())
      if (scenario === 'frozen count')
        expect(api.control).toHaveBeenCalledWith(
          'a',
          expect.objectContaining({ action: 'scale', replicas: 1, expectedRevision: 4, requestId }),
        )
      else {
        expect(api.control).not.toHaveBeenCalled()
        expect(container.textContent).toContain('重新确认')
      }
    } finally {
      uuid.mockRestore()
    }
  },
)
