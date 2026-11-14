/** @vitest-environment jsdom */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { AgentUpgradeModal } from './agent-upgrade-modal'
import { clusterKeys } from './keys'
import type { Cluster } from './types'

const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/services/api-client', () => ({ api }))
vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({ localeCode: 'zh_CN' }),
}))
vi.mock('antd', async (importOriginal) => {
  const Container = ({ children }: { children?: ReactNode }) => <div>{children}</div>
  return {
    ...(await importOriginal<typeof import('antd')>()),
    Alert: ({ title }: { title?: ReactNode }) => <div>{title}</div>,
    Button: ({ children, onClick }: { children?: ReactNode; onClick?: () => void }) => (
      <button onClick={onClick}>{children}</button>
    ),
    Descriptions: () => null,
    Form: Object.assign(Container, { Item: Container }),
    Input: ({
      value,
      disabled,
      onChange,
    }: {
      value?: string
      disabled?: boolean
      onChange?: React.ChangeEventHandler<HTMLInputElement>
    }) => <input value={value} disabled={disabled} onChange={onChange} />,
    Space: Container,
    Spin: () => null,
    Modal: ({
      children,
      onOk,
      okButtonProps,
    }: {
      children?: ReactNode
      onOk?: () => void
      okButtonProps?: { disabled?: boolean }
    }) => (
      <div>
        {children}
        <button id="upgrade" disabled={okButtonProps?.disabled} onClick={onOk}>
          更新 Agent
        </button>
      </div>
    ),
  }
})

const scope = { clusterId: 'one', namespace: null }
const current = {
  version: 'v0.1.7',
  image: 'ghcr.io/opensoha/soha-agent:v0.1.7',
  recommendedVersion: 'v0.1.8',
  rolloutStatus: 'healthy',
  message: '',
  canUpgrade: true,
}
let host: HTMLDivElement, root: Root, client: QueryClient
async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}
async function render() {
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <AgentUpgradeModal cluster={{ id: 'one', name: 'One' } as Cluster} onClose={() => {}} />
      </QueryClientProvider>,
    ),
  )
  await flush()
}

describe('Agent update completion', () => {
  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
    api.get.mockReset().mockResolvedValue({ data: current })
    api.post.mockReset().mockResolvedValue({
      data: { previousImage: current.image, targetImage: 'ghcr.io/opensoha/soha-agent:v0.1.8' },
    })
    host = document.createElement('div')
    document.body.append(host)
    root = createRoot(host)
    client = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
  })
  afterEach(async () => {
    await act(async () => root.unmount())
    client.clear()
    host.remove()
  })

  it('does not call acceptance completion until the target process reconnects and rollout is healthy', async () => {
    await render()
    await act(async () => host.querySelector<HTMLButtonElement>('#upgrade')!.click())
    await flush()
    expect(api.post).toHaveBeenCalledWith('/clusters/one/agent-upgrade', { version: 'v0.1.8' })
    expect(host.textContent).toContain('正在等待新版本')
    expect(host.textContent).not.toContain('更新完成')
    await act(async () =>
      client.setQueryData(clusterKeys.agentUpgrade(scope), {
        ...current,
        image: 'ghcr.io/opensoha/soha-agent:v0.1.8',
      }),
    )
    await flush()
    expect(host.textContent).not.toContain('更新完成')
    await act(async () =>
      client.setQueryData(clusterKeys.agentUpgrade(scope), {
        ...current,
        version: 'v0.1.8',
        image: 'ghcr.io/opensoha/soha-agent:v0.1.8',
      }),
    )
    await flush()
    expect(host.textContent).toContain('更新完成')
  })

  it('disables updates for legacy or externally managed Agents', async () => {
    api.get.mockResolvedValue({
      data: {
        ...current,
        canUpgrade: false,
        upgradeDisabledReason: 'Use the owning deployment tool',
      },
    })
    await render()
    expect(host.querySelector<HTMLButtonElement>('#upgrade')!.disabled).toBe(true)
    expect(host.textContent).toContain('Use the owning deployment tool')
    expect(api.post).not.toHaveBeenCalled()
  })

  it('does not default a newer Agent back to an older bundled recommendation', async () => {
    api.get.mockResolvedValue({
      data: { ...current, version: 'v0.1.10', image: 'ghcr.io/opensoha/soha-agent:v0.1.10' },
    })
    await render()
    expect(host.querySelector<HTMLInputElement>('input')!.value).toBe('v0.1.10')
    expect(host.querySelector<HTMLButtonElement>('#upgrade')!.disabled).toBe(true)
  })

  it('keeps a failed request visible for retry', async () => {
    api.post.mockRejectedValue(new Error('Agent connection unavailable'))
    await render()
    await act(async () => host.querySelector<HTMLButtonElement>('#upgrade')!.click())
    await flush()
    expect(host.textContent).toContain('Agent connection unavailable')
    expect(host.textContent).not.toContain('更新完成')
    expect(host.querySelector<HTMLButtonElement>('#upgrade')!.disabled).toBe(false)
  })
})
