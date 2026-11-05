/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, expect, it, vi } from 'vitest'
import { PlatformOperationsPanel } from './operations-panel'

interface EvidenceStreamOptions {
  enabled?: boolean
  onEvent: () => void
  onFallback?: () => void
  onResyncRequired?: () => unknown
}

const state = vi.hoisted(() => ({
  stream: undefined as EvidenceStreamOptions | undefined,
  nodesFailed: true,
  eventsFailed: true,
  nodesRefetch: vi.fn(),
  eventsRefetch: vi.fn(),
  otherRefetch: vi.fn(),
}))

vi.mock('@tanstack/react-query', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-query')>()),
  useQuery: (options: { queryKey: readonly unknown[]; enabled?: boolean }) => {
    const nodes = options.queryKey[1] === 'cluster-resources'
    const events = options.queryKey[0] === 'overview-events'
    const failed = (nodes && state.nodesFailed) || (events && state.eventsFailed)
    return {
      data: [],
      error: failed ? new Error('Evidence unavailable') : null,
      isError: failed,
      isLoading: false,
      isFetching: false,
      isEnabled: options.enabled !== false,
      refetch: nodes ? state.nodesRefetch : events ? state.eventsRefetch : state.otherRefetch,
    }
  },
}))

vi.mock('@/features/copilot', () => ({ useAIGlobalAssistant: () => null }))
vi.mock('@/features/platform/shared/resource-stream', () => ({
  useKubernetesResourceStream: (options: EvidenceStreamOptions) => {
    state.stream = options
    return { status: options.enabled ? 'live' : 'idle' }
  },
}))
vi.mock('@/features/platform/shared/resource-stream-status', () => ({
  ResourceStreamStatus: () => null,
}))
vi.mock('@/components/request-feedback', () => ({ ReadFeedback: () => null }))
vi.mock('@/components/management-list', () => ({ ManagementState: () => <div /> }))
vi.mock('@/components/status-tag', () => ({ StatusTag: () => null }))
vi.mock('antd', () => ({
  Card: ({ children, extra }: { children: ReactNode; extra: ReactNode }) => (
    <div>
      {extra}
      {children}
    </div>
  ),
  Space: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  Button: ({ children }: { children: ReactNode }) => <button>{children}</button>,
  Typography: { Text: ({ children }: { children: ReactNode }) => <span>{children}</span> },
  Progress: () => null,
  Spin: () => null,
  Collapse: () => null,
}))

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

async function renderPanel() {
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
  await updatePanel()
}

async function updatePanel(refreshToken = 0) {
  await act(async () =>
    root.render(
      <MemoryRouter>
        <PlatformOperationsPanel
          clusterId="cluster-a"
          localeCode="zh_CN"
          refreshToken={refreshToken}
          refresh={{
            enabled: true,
            hidden: false,
            setEnabled: vi.fn(),
            interval: () => false,
          }}
          permissions={{
            nodes: true,
            events: true,
            audit: false,
            operations: false,
            hpas: false,
            networkPolicies: false,
            mutatingWebhooks: false,
            validatingWebhooks: false,
            podDisruptionBudgets: false,
          }}
        />
      </MemoryRouter>,
    ),
  )
}

beforeAll(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})
beforeEach(() => {
  state.stream = undefined
  state.nodesFailed = true
  state.eventsFailed = true
  state.nodesRefetch.mockReset()
  state.eventsRefetch.mockReset()
  state.otherRefetch.mockReset()
})
afterEach(async () => {
  await act(async () => root?.unmount())
  container?.remove()
})

it.each(['onEvent', 'onFallback', 'onResyncRequired'] as const)(
  'does not automatically reread failed node and event queries through %s',
  async (callback) => {
    await renderPanel()
    await act(async () => {
      if (state.stream?.enabled !== false) await state.stream?.[callback]?.()
    })
    expect(state.nodesRefetch).not.toHaveBeenCalled()
    expect(state.eventsRefetch).not.toHaveBeenCalled()
  },
)

it('still rereads healthy node and event queries when a resource signal arrives', async () => {
  state.nodesFailed = false
  state.eventsFailed = false
  await renderPanel()
  expect(state.stream?.enabled).toBe(true)
  await act(async () => state.stream?.onEvent())
  expect(state.nodesRefetch).toHaveBeenCalledOnce()
  expect(state.eventsRefetch).toHaveBeenCalledOnce()
})

it.each(['onEvent', 'onFallback', 'onResyncRequired'] as const)(
  'keeps a healthy event query refreshing through %s when the node query failed',
  async (callback) => {
    state.eventsFailed = false
    await renderPanel()
    expect(state.stream?.enabled).toBe(true)
    await act(async () => {
      await state.stream?.[callback]?.()
    })
    expect(state.nodesRefetch).not.toHaveBeenCalled()
    expect(state.eventsRefetch).toHaveBeenCalledOnce()
  },
)

it.each(['onEvent', 'onFallback', 'onResyncRequired'] as const)(
  'keeps a healthy node query refreshing through %s when the event query failed',
  async (callback) => {
    state.nodesFailed = false
    await renderPanel()
    expect(state.stream?.enabled).toBe(true)
    await act(async () => {
      await state.stream?.[callback]?.()
    })
    expect(state.nodesRefetch).toHaveBeenCalledOnce()
    expect(state.eventsRefetch).not.toHaveBeenCalled()
  },
)

it('uses manual refresh to recover failed evidence queries and resumes automatic signals', async () => {
  await renderPanel()
  await act(async () => state.stream?.onEvent())
  expect(state.nodesRefetch).not.toHaveBeenCalled()
  expect(state.eventsRefetch).not.toHaveBeenCalled()

  await updatePanel(1)
  await act(async () => state.stream?.onEvent())
  expect(state.nodesRefetch).toHaveBeenCalledOnce()
  expect(state.eventsRefetch).toHaveBeenCalledOnce()

  state.nodesRefetch.mockImplementation(() => {
    state.nodesFailed = false
  })
  state.eventsRefetch.mockImplementation(() => {
    state.eventsFailed = false
  })
  await updatePanel(2)
  expect(state.nodesRefetch).toHaveBeenCalledTimes(2)
  expect(state.eventsRefetch).toHaveBeenCalledTimes(2)
  // The query observer publishes the successful manual reads without another refresh token.
  await updatePanel(2)
  await act(async () => state.stream?.onEvent())
  expect(state.nodesRefetch).toHaveBeenCalledTimes(3)
  expect(state.eventsRefetch).toHaveBeenCalledTimes(3)
})
