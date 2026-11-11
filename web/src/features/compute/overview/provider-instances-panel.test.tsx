/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { App } from 'antd'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useMutation, useQuery } from '@tanstack/react-query'
import { ProviderInstancesPanel } from './provider-instances-panel'
import { ApiError } from '@/services/api-error'

vi.mock('@tanstack/react-query', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-query')>()),
  useMutation: vi.fn(),
  useQuery: vi.fn(),
  useQueryClient: () => ({}),
}))

const roots: Array<ReturnType<typeof createRoot>> = []

function LocationProbe() {
  const location = useLocation()
  return (
    <output>
      {location.pathname}
      {location.search}
    </output>
  )
}

function render(node: ReactNode) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  act(() =>
    root.render(
      <App>
        <MemoryRouter>{node}</MemoryRouter>
      </App>,
    ),
  )
  return container
}

function providerQuery(overrides: Record<string, unknown> = {}) {
  return {
    data: {
      items: [
        {
          instanceRef: 'connection-1',
          displayName: 'pve-lab',
          accessMode: 'direct',
          lastObservedAt: '2026-08-24T12:00:00Z',
          snapshot: { domain: 'virtualization', providerKey: 'pve', generation: 1 },
          health: { status: 'healthy' },
        },
      ],
    },
    isError: false,
    isFetching: false,
    isLoading: false,
    refetch: vi.fn(),
    ...overrides,
  }
}

afterEach(() => {
  roots.splice(0).forEach((root) => act(() => root.unmount()))
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

describe('provider instances panel', () => {
  it.each(['health', 'discover'])(
    'shows a persistent %s failure beside its original instance',
    (action) => {
      vi.mocked(useQuery).mockReturnValue(providerQuery() as never)
      vi.mocked(useMutation).mockImplementation((options) => {
        const active = options.mutationKey?.includes(action)
        return {
          isPending: false,
          isSuccess: false,
          submittedAt: active ? 20 : 0,
          error: active ? new ApiError(503, 'provider request failed', { method: 'POST' }) : null,
          variables: active ? { instanceRef: 'connection-1' } : undefined,
          mutate: vi.fn(),
        } as never
      })
      const container = render(
        <ProviderInstancesPanel canViewTask canDiscover canTest enabled localeCode="zh_CN" />,
      )
      const feedback = container.querySelector('.soha-operation-feedback')
      expect(feedback?.textContent).toContain('provider request failed')
      expect(feedback?.textContent).toContain('connection-1')
      expect(feedback?.textContent).toContain(action === 'health' ? '操作请求失败' : '请求结果不明')
    },
  )

  it.each([
    { canTest: true, canDiscover: false, visible: '检查连接健康', hidden: '发现并同步资源' },
    { canTest: false, canDiscover: true, visible: '发现并同步资源', hidden: '检查连接健康' },
  ])(
    'gates provider actions by their exact permission',
    ({ canTest, canDiscover, visible, hidden }) => {
      vi.mocked(useQuery).mockReturnValue(providerQuery() as never)
      vi.mocked(useMutation).mockReturnValue({ isPending: false, mutate: vi.fn() } as never)

      const container = render(
        <ProviderInstancesPanel
          canDiscover={canDiscover}
          canTest={canTest}
          enabled
          localeCode="zh_CN"
        />,
      )

      expect(container.querySelector(`button[aria-label="${visible}"]`)).not.toBeNull()
      expect(container.querySelector(`button[aria-label="${hidden}"]`)).toBeNull()
    },
  )

  it('retries a failed provider query', () => {
    const refetch = vi.fn()
    vi.mocked(useQuery).mockReturnValue(
      providerQuery({ data: undefined, isError: true, refetch }) as never,
    )
    vi.mocked(useMutation).mockReturnValue({ isPending: false, mutate: vi.fn() } as never)

    const container = render(
      <ProviderInstancesPanel canDiscover={false} canTest={false} enabled localeCode="zh_CN" />,
    )
    const retry = container.querySelector<HTMLButtonElement>(
      'button[aria-label="重试读取提供方实例"]',
    )

    expect(retry).toBeDefined()
    act(() => retry?.click())
    expect(refetch).toHaveBeenCalledTimes(1)
  })

  it('shows unhealthy probe details without navigating to task history', () => {
    const health = vi.fn()
    vi.mocked(useQuery).mockReturnValue(providerQuery() as never)
    vi.mocked(useMutation).mockReturnValue({ isPending: false, mutate: health } as never)
    const container = render(
      <>
        <ProviderInstancesPanel canTest canDiscover={false} enabled localeCode="zh_CN" />
        <LocationProbe />
      </>,
    )
    act(() =>
      container.querySelector<HTMLButtonElement>('button[aria-label="检查连接健康"]')?.click(),
    )
    act(() =>
      health.mock.calls[0][1].onSuccess({
        data: { healthy: false, status: 'degraded', message: 'KubeVirt CRD unavailable' },
      }),
    )
    expect(document.body.textContent).toContain('连接异常')
    expect(document.body.textContent).toContain('KubeVirt CRD unavailable')
    expect(container.querySelector('output')?.textContent).toBe('/')
  })

  it.each([
    { action: 'health', label: '检查连接健康', input: { expectedGeneration: 1 } },
    {
      action: 'discover',
      label: '发现并同步资源',
      input: { expectedGeneration: 1, maxItems: 1000 },
    },
  ])(
    'keeps the $action action bound to its instance and resulting task',
    ({ action, label, input }) => {
      const health = vi.fn()
      const discover = vi.fn()
      vi.mocked(useQuery).mockReturnValue(providerQuery() as never)
      vi.mocked(useMutation).mockImplementation(
        (options) =>
          ({
            isPending: false,
            mutate: options.mutationKey?.includes('health') ? health : discover,
          }) as never,
      )
      const container = render(
        <>
          <ProviderInstancesPanel canViewTask canDiscover canTest enabled localeCode="zh_CN" />
          <LocationProbe />
        </>,
      )

      expect(container.querySelector('time')?.getAttribute('datetime')).toBe('2026-08-24T12:00:00Z')
      act(() =>
        container.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)?.click(),
      )
      const mutation = action === 'health' ? health : discover
      expect(mutation).toHaveBeenCalledWith(
        {
          domain: 'virtualization',
          providerKey: 'pve',
          instanceRef: 'connection-1',
          input,
        },
        expect.objectContaining({ onSuccess: expect.any(Function) }),
      )
      expect(action === 'health' ? discover : health).not.toHaveBeenCalled()
      expect(mutation.mock.calls[0][1].onError).toBeUndefined()
      act(() =>
        mutation.mock.calls[0][1].onSuccess({
          data:
            action === 'health'
              ? { healthy: true, status: 'healthy', message: 'Provider reachable' }
              : {
                  domain: 'virtualization',
                  id: 'task-1',
                  normalizedStatus: 'succeeded',
                  result: { summary: 'Provider reachable' },
                },
        }),
      )
      expect(container.querySelector('output')?.textContent).toBe(
        action === 'health'
          ? '/'
          : '/compute/tasks/operations?domain=virtualization&taskId=task-1&view=logs',
      )
      if (action === 'health') {
        expect(document.body.textContent).toContain('连接正常')
        expect(document.body.textContent).toContain('Provider reachable')
      }
    },
  )
})
