/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { expect, it, vi } from 'vitest'
import { PlatformOperationsPanel } from './operations-panel'
const state = vi.hoisted(() => ({
  launch: vi.fn(),
  event: {
    name: 'protected-event',
    reason: 'SensitiveReason',
    message: 'protected payload',
    involvedKind: 'Pod',
    involvedName: 'protected-pod',
    namespace: 'team',
    type: 'Warning',
    lastTimestamp: '2026-10-18T00:00:00Z',
  },
}))
vi.mock('@tanstack/react-query', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-query')>()),
  useQuery: (options: { queryKey: unknown[]; enabled?: boolean }) => ({
    data: options.queryKey[0] === 'overview-events' ? [state.event] : [],
    error: null,
    isError: false,
    isLoading: false,
    isFetching: false,
    isEnabled: options.enabled,
    refetch: vi.fn(),
  }),
}))
vi.mock('@/features/copilot', () => ({
  useAIGlobalAssistant: () => ({ launchAssistant: state.launch }),
}))
vi.mock('@/features/platform/shared/resource-stream', () => ({
  useKubernetesResourceStream: () => ({ status: 'disabled' }),
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
  Button: ({ children, onClick }: { children: ReactNode; onClick: () => void }) => (
    <button onClick={onClick}>{children}</button>
  ),
  Typography: { Text: ({ children }: { children: ReactNode }) => <span>{children}</span> },
  Progress: () => null,
  Spin: () => null,
  Collapse: () => null,
}))
it('removes disabled cached events from the timeline and AI context after permission loss', () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const render = (events: boolean) =>
    act(() =>
      root.render(
        <MemoryRouter>
          <PlatformOperationsPanel
            clusterId="cluster-a"
            localeCode="zh_CN"
            permissions={{
              events,
              audit: true,
              operations: false,
              nodes: false,
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
  try {
    render(true)
    expect(container.textContent).toContain('protected payload')
    render(false)
    expect(container.textContent).not.toContain('protected payload')
    const button = [...container.querySelectorAll('button')].find(
      (b) => b.textContent === 'AI 排障',
    )!
    act(() => button.click())
    expect(state.launch).toHaveBeenCalledOnce()
    expect(JSON.stringify(state.launch.mock.calls[0])).not.toContain('protected payload')
  } finally {
    act(() => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  }
})
