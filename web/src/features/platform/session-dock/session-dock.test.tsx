/** @vitest-environment jsdom */

import { act, useState, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { podQueries } from '../workloads/pods/queries'
import {
  RealtimeSessionDockProvider,
  RealtimeSessionDockTrigger,
  useRealtimeSessionDock,
} from './session-dock'
import { createSessionDockStore } from './store'
import type { RealtimeSessionWorkbench } from './types'

const { getPodDetailMock } = vi.hoisted(() => ({ getPodDetailMock: vi.fn() }))

vi.mock('../workloads/pods/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../workloads/pods/api')>()),
  getPodDetail: getPodDetailMock,
}))

const authorization = vi.hoisted(() => ({ allowed: true, refetch: vi.fn() }))
const clusterCapability = vi.hoisted(() => ({
  disabled: false,
  isLoading: false,
  status: 'available' as 'available' | 'partial',
  reason: '当前模式仅支持非交互式 exec。',
}))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({
    data: { data: {} },
    isLoading: false,
    refetch: authorization.refetch,
  }),
  hasPermission: () => authorization.allowed,
  hasAllowedAction: (actions: string[] | undefined, action: string) =>
    actions?.includes(action) ?? false,
}))
vi.mock('../cluster-capabilities', () => ({
  useClusterCapabilityForCluster: () => ({ ...clusterCapability }),
}))

vi.mock('@/i18n', () => ({
  localeText: (_locale: string, zh: string) => zh,
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))

vi.mock('@/components/pod-terminal', () => ({
  PodTerminal: ({
    clusterId,
    container,
    podName,
    shell,
    toolbarContent,
  }: {
    clusterId: string
    container?: string
    podName: string
    shell?: string
    toolbarContent?: ReactNode
  }) => (
    <div
      data-container={container ?? ''}
      data-shell={shell ?? ''}
      data-testid={`terminal:${clusterId}:${podName}`}
    >
      {toolbarContent}
      terminal
    </div>
  ),
}))

vi.mock('@/components/pod-log-viewer', () => ({
  PodLogViewer: ({
    clusterId,
    podName,
    container,
    containerOptions,
    onContainerChange,
  }: {
    clusterId: string
    podName: string
    container?: string
    containerOptions?: Array<{ value: string; label: string }>
    onContainerChange?: (value: string) => void
  }) => (
    <div data-testid={`logs:${clusterId}:${podName}`} data-container={container}>
      <select
        aria-label="日志容器"
        value={container}
        onChange={(event) => onContainerChange?.(event.target.value)}
      >
        {containerOptions?.map(({ value, label }) => (
          <option key={value} value={value}>
            {label}
          </option>
        ))}
      </select>
      logs
    </div>
  ),
}))

function SessionHarness() {
  const { openSession } = useRealtimeSessionDock()
  return (
    <>
      <button
        type="button"
        onClick={() =>
          openSession({
            clusterId: 'cluster-a',
            container: 'api',
            kind: 'terminal',
            namespace: 'team-a',
            podName: 'pod-a',
            podUid: 'uid-pod-a',
            shell: '/bin/sh',
          })
        }
      >
        terminal-a
      </button>
      <button
        type="button"
        onClick={() =>
          openSession({
            clusterId: 'cluster-a',
            kind: 'logs',
            namespace: 'team-a',
            podName: 'pod-b',
            podUid: 'uid-pod-b',
          })
        }
      >
        logs-a
      </button>
      <button
        type="button"
        onClick={() =>
          openSession({
            clusterId: 'cluster-b',
            kind: 'terminal',
            namespace: 'team-b',
            podName: 'pod-c',
            podUid: 'uid-pod-c',
          })
        }
      >
        terminal-b
      </button>
    </>
  )
}

function OwnerHarness() {
  const [owner, setOwner] = useState('user-a')
  const [visible, setVisible] = useState(true)
  const [scope, setScope] = useState('app-a/test')
  const [workbench, setWorkbench] = useState<RealtimeSessionWorkbench>('platform')
  return (
    <>
      <button onClick={() => setOwner('user-b')}>switch-user</button>
      <button onClick={() => setVisible((value) => !value)}>toggle-visible</button>
      <button
        onClick={() => setWorkbench((value) => (value === 'platform' ? 'delivery' : 'platform'))}
      >
        switch-workbench
      </button>
      <button
        onClick={() => setScope((value) => (value === 'app-a/test' ? 'app-a/prod' : 'app-a/test'))}
      >
        switch-environment
      </button>
      <RealtimeSessionDockProvider
        ownerKey={owner}
        visible={visible}
        workbench={workbench}
        scopeKey={workbench === 'delivery' ? scope : undefined}
      >
        <input aria-label="page-local-draft" defaultValue="" />
        <RealtimeSessionDockTrigger />
        <SessionHarness />
      </RealtimeSessionDockProvider>
    </>
  )
}

const roots: Root[] = []

beforeAll(() => {
  class ResizeObserverMock {
    observe() {}
    unobserve() {}
    disconnect() {}
  }

  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({
      matches: false,
      media: '',
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('ResizeObserver', ResizeObserverMock)
  Object.defineProperty(HTMLElement.prototype, 'setPointerCapture', {
    configurable: true,
    value: vi.fn(),
  })
})

beforeEach(() => {
  authorization.refetch.mockImplementation(async () => ({ data: { data: {} }, isError: false }))
  authorization.allowed = true
  clusterCapability.isLoading = false
  clusterCapability.status = 'available'
  localStorage.clear()
  getPodDetailMock.mockReset()
  getPodDetailMock.mockImplementation(async ({ name }: { name: string }) => ({
    uid: 'uid-' + name,
    containers:
      name === 'pod-a'
        ? [
            { name: 'api' },
            { name: 'sidecar', role: 'sidecar' },
            { name: 'complete', role: 'init', state: 'terminated:Completed' },
          ]
        : name === 'pod-b'
          ? [
              { name: 'prepare', role: 'init', state: 'terminated:Completed' },
              { name: 'sidecar', role: 'sidecar' },
              { name: 'worker', role: 'main' },
            ]
          : [
              { name: 'complete', state: 'terminated:Completed' },
              { name: 'prepare', role: 'init' },
              { name: 'worker' },
            ],
    allowedActions: ['logs', 'exec'],
  }))
  usePlatformScopeStore.setState({ clusterId: 'cluster-a', namespace: 'team-a' })
})

afterEach(async () => {
  await act(async () => {
    roots.splice(0).forEach((root) => root.unmount())
  })
  document.body.innerHTML = ''
})

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
  })
}

async function click(element: Element | null | undefined) {
  expect(element).toBeTruthy()
  await act(async () => {
    element?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })
  await flushAsyncWork()
  await flushAsyncWork()
}

async function selectOption(select: Element | null | undefined, optionLabel: string) {
  expect(select).not.toBeNull()
  await act(async () => {
    select
      ?.querySelector('.ant-select-content')
      ?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
  })
  await flushAsyncWork()
  const options = Array.from(document.body.querySelectorAll('.ant-select-item-option'))
  const option = options.find((item) => item.textContent === optionLabel)
  expect(option).not.toBeUndefined()
  await act(async () => {
    option?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    option?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })
  await flushAsyncWork()
  return options.map((item) => item.textContent)
}

function pointerEvent(type: string, clientY: number) {
  const event = new MouseEvent(type, { bubbles: true, button: 0, clientY })
  Object.defineProperty(event, 'pointerId', { value: 1 })
  return event
}

async function renderDock({
  preloadPodDetails = true,
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  }),
} = {}) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  queryClient.setQueryData(
    podQueries.detail({ clusterId: 'cluster-a', namespace: 'team-a' }, 'pod-b').queryKey,
    {
      name: 'pod-b',
      namespace: 'team-a',
      phase: 'Running',
      containers: [
        { name: 'prepare', role: 'init', image: 'test:1', ready: true, restartCount: 0 },
        { name: 'sidecar', role: 'sidecar', image: 'test:1', ready: true, restartCount: 0 },
        { name: 'worker', role: 'main', image: 'test:1', ready: true, restartCount: 0 },
      ],
      allowedActions: ['logs', 'exec'],
    },
  )
  if (preloadPodDetails) {
    queryClient.setQueryData(
      podQueries.detail({ clusterId: 'cluster-a', namespace: 'team-a' }, 'pod-a').queryKey,
      {
        allowedActions: ['logs', 'exec'],
        containers: [
          { image: 'api:latest', name: 'api', ready: true, restartCount: 0 },
          { image: 'sidecar:latest', name: 'sidecar', ready: true, restartCount: 0 },
        ],
        name: 'pod-a',
        namespace: 'team-a',
        phase: 'Running',
      },
    )
    queryClient.setQueryData(
      podQueries.detail({ clusterId: 'cluster-b', namespace: 'team-b' }, 'pod-c').queryKey,
      {
        allowedActions: ['logs', 'exec'],
        containers: [
          { name: 'prepare', role: 'init', image: 'test:1', ready: true, restartCount: 0 },
          { name: 'worker', image: 'test:1', ready: true, restartCount: 0 },
        ],
        name: 'pod-c',
        namespace: 'team-b',
        phase: 'Running',
      },
    )
  }
  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <OwnerHarness />
      </QueryClientProvider>,
    )
  })
  return container
}

describe('realtime session dock', () => {
  it('opens the empty dock on the first trigger click', async () => {
    const container = await renderDock()

    await click(container.querySelector('.soha-header-realtime-sessions'))
    await act(async () => {
      await vi.dynamicImportSettled()
    })

    const dock = container.querySelector<HTMLElement>('section[aria-label="实时会话"]')
    expect(dock?.hidden).toBe(false)
    expect(dock?.textContent).toContain('当前集群暂无实时会话')
  })

  it('keeps sessions mounted across tabs, minimizing and cluster changes', async () => {
    const container = await renderDock()
    const buttonByText = (value: string) =>
      Array.from(container.querySelectorAll('button')).find((button) =>
        button.textContent?.includes(value),
      )

    await click(buttonByText('terminal-a'))
    await click(buttonByText('logs-a'))
    await flushAsyncWork()
    await flushAsyncWork()

    const dock = container.querySelector<HTMLElement>('[aria-label="实时会话"]')
    const clusterA = container.querySelector<HTMLElement>('[data-cluster-id="cluster-a"]')
    expect(dock?.hidden).toBe(false)
    expect(clusterA?.querySelectorAll('[role="tab"]')).toHaveLength(2)
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).not.toBeNull()

    const resizeHandle = container.querySelector('[aria-label="调整实时会话高度"]')
    vi.spyOn(dock as HTMLElement, 'getBoundingClientRect').mockReturnValue({
      ...dock?.getBoundingClientRect(),
      height: 400,
    } as DOMRect)
    await act(async () => {
      resizeHandle?.dispatchEvent(pointerEvent('pointerdown', 400))
      window.dispatchEvent(pointerEvent('pointermove', 300))
      window.dispatchEvent(pointerEvent('pointerup', 300))
    })
    expect(dock?.style.getPropertyValue('--soha-realtime-session-height')).toBe('500px')

    await act(async () => {
      resizeHandle?.dispatchEvent(pointerEvent('pointerdown', 300))
      resizeHandle?.dispatchEvent(pointerEvent('lostpointercapture', 300))
      window.dispatchEvent(pointerEvent('pointermove', 250))
    })
    expect(dock?.style.getPropertyValue('--soha-realtime-session-height')).toBe('500px')

    await act(async () => {
      resizeHandle?.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'ArrowUp' }))
    })
    expect(dock?.style.getPropertyValue('--soha-realtime-session-height')).toBe('532px')

    await click(container.querySelector('[aria-label="最大化实时会话"]'))
    expect(dock?.classList.contains('is-maximized')).toBe(true)

    await click(container.querySelector('[aria-label="收起实时会话"]'))
    expect(container.querySelector<HTMLElement>('section[aria-label="实时会话"]')?.hidden).toBe(
      true,
    )
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).not.toBeNull()

    await click(container.querySelector('.soha-header-realtime-sessions'))
    await act(async () => {
      usePlatformScopeStore.getState().setClusterId('cluster-b')
    })
    await flushAsyncWork()
    expect(container.querySelector<HTMLElement>('[data-cluster-id="cluster-a"]')?.hidden).toBe(true)
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).not.toBeNull()

    await click(buttonByText('terminal-b'))
    const clusterB = container.querySelector<HTMLElement>('[data-cluster-id="cluster-b"]')
    expect(clusterB?.hidden).toBe(false)
    expect(container.querySelector('[data-testid="terminal:cluster-b:pod-c"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).not.toBeNull()

    await act(async () => {
      usePlatformScopeStore.getState().setClusterId('cluster-a')
    })
    await flushAsyncWork()
    const activeClusterA = container.querySelector<HTMLElement>('[data-cluster-id="cluster-a"]')
    const removeButtons = activeClusterA?.querySelectorAll('.ant-tabs-tab-remove')
    await click(removeButtons?.item(1))
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).toBeNull()
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).not.toBeNull()
  })

  it('shows application sessions from multiple clusters in one tab strip and selects the remaining tab on close', async () => {
    const container = await renderDock()
    const button = (label: string) =>
      Array.from(container.querySelectorAll('button')).find((item) => item.textContent === label)
    await click(button('switch-workbench'))
    await click(button('logs-a'))
    const log = container.querySelector('[data-testid="logs:cluster-a:pod-b"]')
    await click(button('terminal-b'))
    expect(container.querySelector('[aria-label="会话集群"]')).toBeNull()
    expect(container.querySelectorAll('[role="tablist"]')).toHaveLength(1)
    const tabs = container.querySelectorAll<HTMLElement>('[role="tab"]')
    expect(tabs).toHaveLength(2)
    expect(tabs[1].getAttribute('aria-selected')).toBe('true')
    expect(tabs[0].querySelector('[title]')?.getAttribute('title')).toContain(
      'cluster-a / team-a / pod-b',
    )
    expect(tabs[1].querySelector('[title]')?.getAttribute('title')).toContain(
      'cluster-b / team-b / pod-c',
    )
    await click(tabs[0])
    expect(tabs[0].getAttribute('aria-selected')).toBe('true')
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).toBe(log)
    expect(container.querySelector('[data-testid="terminal:cluster-b:pod-c"]')).not.toBeNull()
    await click(container.querySelector('.ant-tabs-tab-active .ant-tabs-tab-remove'))
    expect(container.querySelectorAll('[role="tab"]')).toHaveLength(1)
    expect(container.querySelector('[role="tab"][aria-selected="true"]')?.textContent).toContain(
      'pod-c',
    )
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).toBeNull()
  })

  it('isolates environment connections and restores tabs without remounting the page', async () => {
    const container = await renderDock()
    const button = (label: string) =>
      Array.from(container.querySelectorAll('button')).find((item) => item.textContent === label)
    await click(button('switch-workbench'))
    const draft = container.querySelector<HTMLInputElement>('[aria-label="page-local-draft"]')!
    draft.value = 'unsaved'
    await click(button('logs-a'))
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).not.toBeNull()
    await click(button('switch-environment'))
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).toBeNull()
    expect(
      container.querySelector('.soha-header-realtime-sessions')?.getAttribute('aria-label'),
    ).toBe('实时会话')
    expect(container.querySelector('[aria-label="page-local-draft"]')).toBe(draft)
    expect(draft.value).toBe('unsaved')
    await click(button('terminal-a'))
    await click(button('switch-environment'))
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).toBeNull()
    await click(container.querySelector('.soha-header-realtime-sessions'))
    await click(
      Array.from(container.querySelectorAll('button')).find(
        (item) => item.textContent === '重新连接',
      ),
    )
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).not.toBeNull()
    expect(container.querySelectorAll('[role="tab"]')).toHaveLength(1)
  })

  it('isolates workbench sessions, restores their tabs and drops connections on account change', async () => {
    const container = await renderDock()
    const button = (label: string) =>
      Array.from(container.querySelectorAll('button')).find((item) => item.textContent === label)
    await click(button('terminal-a'))
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).not.toBeNull()
    await click(button('switch-workbench'))
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).toBeNull()
    expect(
      container.querySelector('.soha-header-realtime-sessions')?.getAttribute('aria-label'),
    ).toBe('实时会话')
    await click(button('logs-a'))
    await flushAsyncWork()
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).not.toBeNull()
    expect(container.querySelector('[aria-label="会话集群"]')).toBeNull()
    // Platform scope changes must not redirect a delivery session.
    await act(async () => usePlatformScopeStore.getState().setClusterId('cluster-b'))
    expect(container.querySelector<HTMLElement>('[data-cluster-id="application"]')?.hidden).toBe(
      false,
    )
    await act(async () => usePlatformScopeStore.getState().setClusterId('cluster-a'))
    await click(button('switch-workbench'))
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).toBeNull()
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).toBeNull()
    await click(container.querySelector('.soha-header-realtime-sessions'))
    await click(
      Array.from(container.querySelectorAll('button')).find(
        (item) => item.textContent === '重新连接',
      ),
    )
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).not.toBeNull()
    expect(container.querySelectorAll('[role="tab"]')).toHaveLength(1)
    expect(container.querySelector('[aria-label="会话集群"]')).toBeNull()
    await click(button('switch-workbench'))
    await click(container.querySelector('.soha-header-realtime-sessions'))
    await click(
      Array.from(container.querySelectorAll('button')).find(
        (item) => item.textContent === '重新连接',
      ),
    )
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).not.toBeNull()
    expect(container.querySelectorAll('[role="tab"]')).toHaveLength(1)
    await click(button('switch-user'))
    expect(container.querySelector('[data-testid="logs:cluster-a:pod-b"]')).toBeNull()
    expect(
      container.querySelector('.soha-header-realtime-sessions')?.getAttribute('aria-label'),
    ).toBe('实时会话')
  })

  it('restores tabs without connecting until opened and rechecks permissions', async () => {
    const original = await renderDock()
    await click(
      Array.from(original.querySelectorAll('button')).find(
        (button) => button.textContent === 'terminal-a',
      ),
    )
    await act(async () => roots.pop()!.unmount())
    const restored = await renderDock()
    expect(
      restored.querySelector('.soha-header-realtime-sessions')?.getAttribute('aria-label'),
    ).toBe('实时会话 (1)')
    expect(restored.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).toBeNull()
    authorization.allowed = false
    await click(restored.querySelector('.soha-header-realtime-sessions'))
    expect(authorization.refetch).toHaveBeenCalled() // initial explicit open only
    await click(
      Array.from(restored.querySelectorAll('button')).find(
        (item) => item.textContent === '重新连接',
      ),
    )
    expect(restored.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).toBeNull()
    expect(restored.querySelector('.soha-management-state')).not.toBeNull()
  })

  it('unmounts a running session when permission is revoked', async () => {
    const container = await renderDock()
    await click(
      Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent === 'terminal-a',
      ),
    )
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).not.toBeNull()
    authorization.allowed = false
    await click(
      Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent === 'toggle-visible',
      ),
    )
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).toBeNull()
  })

  it.each(['policy', 'identity', 'termination', 'missing container'] as const)(
    'requires explicit reconnect after Pod %s changes',
    async (change) => {
      const queryClient = new QueryClient({
        defaultOptions: { queries: { retry: false, staleTime: Infinity } },
      })
      const container = await renderDock({ queryClient })
      const button = (label: string) =>
        Array.from(container.querySelectorAll('button')).find((item) => item.textContent === label)
      const terminal = () => container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')
      await click(button('terminal-a'))
      expect(terminal()).not.toBeNull()

      const key = podQueries.detail(
        { clusterId: 'cluster-a', namespace: 'team-a' },
        'pod-a',
      ).queryKey
      const original = queryClient.getQueryData(key)!
      const changes = {
        policy: { allowedActions: ['logs'] },
        identity: { uid: 'replacement-uid' },
        termination: {
          containers: original.containers?.map((item) =>
            item.name === 'api' ? { ...item, state: 'terminated:Completed' } : item,
          ),
        },
        'missing container': {
          containers: original.containers?.filter((item) => item.name !== 'api'),
        },
      }
      await act(async () => {
        queryClient.setQueryData(key, { ...original, ...changes[change] })
      })
      await flushAsyncWork()
      expect(terminal()).toBeNull()
      expect(container.textContent).toContain(
        change === 'termination'
          ? '当前容器已结束，无法连接终端。'
          : change === 'missing container'
            ? '原容器已不存在'
            : '会话权限或 Pod 身份已变化，请重新校验。',
      )

      await act(async () => {
        queryClient.setQueryData(key, original)
      })
      await flushAsyncWork()
      expect(terminal()).toBeNull()
      await click(button('重新校验并连接'))
      expect(terminal()).not.toBeNull()
    },
  )

  it('waits for initial terminal capability and requires revalidation after capability loss', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    clusterCapability.isLoading = true
    const container = await renderDock({ queryClient })
    const button = (label: string) =>
      Array.from(container.querySelectorAll('button')).find((item) => item.textContent === label)
    const terminal = () => container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')
    const key = podQueries.detail({ clusterId: 'cluster-a', namespace: 'team-a' }, 'pod-a').queryKey
    const observe = async (phase: string) => {
      await act(async () => {
        queryClient.setQueryData(key, (current) => ({ ...current!, phase }))
      })
      await flushAsyncWork()
    }

    await click(button('terminal-a'))
    expect(terminal()).toBeNull()
    expect(button('重新校验并连接')).toBeUndefined()
    clusterCapability.isLoading = false
    await observe('Running')
    expect(terminal()).not.toBeNull()

    clusterCapability.status = 'partial'
    await observe('Unknown')
    expect(terminal()).toBeNull()
    expect(button('重新校验并连接')?.disabled).toBe(true)
    expect(container.textContent).toContain(clusterCapability.reason)

    clusterCapability.status = 'available'
    await observe('Running')
    expect(terminal()).toBeNull()
    expect(button('重新校验并连接')?.disabled).toBe(false)
    await click(button('重新校验并连接'))
    expect(terminal()).not.toBeNull()
  })

  it('only follows the platform cluster selector, including after closing its last tab', async () => {
    const container = await renderDock()
    const button = (label: string) =>
      Array.from(container.querySelectorAll('button')).find((item) => item.textContent === label)
    await click(button('terminal-a'))
    await click(button('terminal-b'))
    expect(container.querySelector('[data-cluster-id="cluster-b"]')).toBeNull()
    expect(container.querySelector('[aria-label="会话集群"]')).toBeNull()
    await act(async () => usePlatformScopeStore.getState().setClusterId('cluster-b'))
    expect(
      container.querySelector('.soha-header-realtime-sessions')?.getAttribute('aria-label'),
    ).toBe('实时会话')
    await click(button('terminal-b'))
    await click(container.querySelector('[data-cluster-id="cluster-b"] .ant-tabs-tab-remove'))
    expect(container.querySelector<HTMLElement>('[data-cluster-id="cluster-a"]')?.hidden).toBe(true)
    expect(container.textContent).toContain('当前集群暂无实时会话')
    expect(usePlatformScopeStore.getState().clusterId).toBe('cluster-b')
    await act(async () => usePlatformScopeStore.getState().setClusterId('cluster-a'))
    expect(container.querySelector<HTMLElement>('[data-cluster-id="cluster-a"]')?.hidden).toBe(
      false,
    )
    expect(
      container.querySelector('.soha-header-realtime-sessions')?.getAttribute('aria-label'),
    ).toBe('实时会话 (1)')
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).not.toBeNull()
  })

  it('restores cluster tabs without overriding the current platform cluster', async () => {
    const original = await renderDock()
    const button = (label: string) =>
      Array.from(original.querySelectorAll('button')).find((item) => item.textContent === label)
    await click(button('terminal-a'))
    await act(async () => usePlatformScopeStore.getState().setClusterId('cluster-b'))
    await click(button('terminal-b'))
    await act(async () => roots.pop()!.unmount())
    createSessionDockStore('platform', 'user-a').setState({ selectedClusterId: 'cluster-b' })
    await act(async () => usePlatformScopeStore.getState().setClusterId('cluster-a'))
    const restored = await renderDock()
    await click(restored.querySelector('.soha-header-realtime-sessions'))
    expect(restored.querySelector<HTMLElement>('[data-cluster-id="cluster-a"]')?.hidden).toBe(false)
    expect(restored.querySelector<HTMLElement>('[data-cluster-id="cluster-b"]')?.hidden).toBe(true)
    expect(restored.querySelector('[data-testid="terminal:cluster-b:pod-c"]')).toBeNull()
    await act(async () => usePlatformScopeStore.getState().setClusterId('cluster-b'))
    expect(restored.querySelector<HTMLElement>('[data-cluster-id="cluster-b"]')?.hidden).toBe(false)
    expect(restored.querySelector('[data-testid="terminal:cluster-b:pod-c"]')).toBeNull()
    await click(
      Array.from(restored.querySelectorAll('[data-cluster-id="cluster-b"] button')).find(
        (button) => button.textContent === '重新连接',
      ),
    )
    expect(restored.querySelector('[data-testid="terminal:cluster-b:pod-c"]')).not.toBeNull()
  })

  it('switches a terminal container and shell in the current tab', async () => {
    const container = await renderDock()
    const terminalButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('terminal-a'),
    )

    await click(terminalButton)
    await act(async () => {
      await vi.dynamicImportSettled()
    })
    await flushAsyncWork()

    let terminal = container.querySelector<HTMLElement>('[data-testid="terminal:cluster-a:pod-a"]')
    expect(terminal?.textContent).toContain('容器:')
    expect(terminal?.textContent).toContain('Shell:')
    expect(terminal?.dataset.container).toBe('api')
    expect(terminal?.dataset.shell).toBe('/bin/sh')

    let selects = terminal?.querySelectorAll('.ant-select')
    expect(selects).toHaveLength(2)
    expect(await selectOption(selects?.item(0), 'sidecar')).not.toContain('complete')

    terminal = container.querySelector<HTMLElement>('[data-testid="terminal:cluster-a:pod-a"]')
    expect(terminal?.dataset.container).toBe('sidecar')
    expect(container.querySelectorAll('[data-cluster-id="cluster-a"] [role="tab"]')).toHaveLength(1)

    selects = terminal?.querySelectorAll('.ant-select')
    await selectOption(selects?.item(1), '/bin/ash')
    terminal = container.querySelector<HTMLElement>('[data-testid="terminal:cluster-a:pod-a"]')
    expect(terminal?.dataset.shell).toBe('/bin/ash')
    expect(container.querySelectorAll('[data-cluster-id="cluster-a"] [role="tab"]')).toHaveLength(1)
  })

  it('opens the main log container and switches containers within the same session tab', async () => {
    const container = await renderDock()
    await click(
      Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent === 'logs-a',
      ),
    )
    await act(async () => {
      await vi.dynamicImportSettled()
    })
    await flushAsyncWork()
    expect(
      container.querySelector<HTMLElement>('[data-testid="logs:cluster-a:pod-b"]')?.dataset
        .container,
    ).toBe('worker')
    expect(container.querySelector('[role="tab"]')?.textContent).toContain('worker')
    await act(async () => {
      const select = container.querySelector<HTMLSelectElement>('[aria-label="日志容器"]')!
      select.value = 'prepare'
      select.dispatchEvent(new Event('change', { bubbles: true }))
    })
    await flushAsyncWork()
    expect(
      container.querySelector<HTMLElement>('[data-testid="logs:cluster-a:pod-b"]')?.dataset
        .container,
    ).toBe('prepare')
    expect(container.querySelectorAll('[role="tab"]')).toHaveLength(1)
    expect(container.querySelector('[role="tab"]')?.textContent).toContain('prepare')
  })

  it('falls back to a regular container for terminal sessions without role metadata', async () => {
    const container = await renderDock()
    await act(async () => usePlatformScopeStore.getState().setClusterId('cluster-b'))
    await click(
      Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent === 'terminal-b',
      ),
    )
    await act(async () => {
      await vi.dynamicImportSettled()
    })
    await flushAsyncWork()
    expect(
      container.querySelector<HTMLElement>('[data-testid="terminal:cluster-b:pod-c"]')?.dataset
        .container,
    ).toBe('worker')
  })

  it('shows and retries container loading errors', async () => {
    getPodDetailMock.mockRejectedValue(new Error('unavailable'))
    const container = await renderDock({ preloadPodDetails: false })
    const terminalButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('terminal-a'),
    )

    await click(terminalButton)
    await act(async () => {
      await vi.dynamicImportSettled()
    })
    await flushAsyncWork()
    await flushAsyncWork()

    const terminal = container.querySelector<HTMLElement>(
      '[data-testid="terminal:cluster-a:pod-a"]',
    )
    expect(terminal).toBeNull()
    const retryButton = Array.from(container.querySelectorAll('button')).find(
      (button) =>
        button.textContent?.includes('Retry') ||
        button.textContent?.replace(/\s/g, '').includes('重试') ||
        button.textContent?.includes('重新校验并连接'),
    )
    expect(retryButton).not.toBeNull()
    const attempts = getPodDetailMock.mock.calls.length
    expect(attempts).toBeGreaterThanOrEqual(1)

    await click(retryButton)
    expect(getPodDetailMock.mock.calls.length).toBeGreaterThan(attempts)
  })
  it.each([
    {
      change: 'a same-name replacement',
      uid: 'replacement',
      state: 'running',
      message: 'Pod 已被替换',
    },
    {
      change: 'a terminated container',
      uid: 'uid-pod-a',
      state: 'terminated:Completed',
      message: '当前容器已结束',
    },
  ])(
    'requires explicit restore after pagehide and rejects $change on a fresh read',
    async ({ uid, state, message }) => {
      const container = await renderDock()
      await click(
        Array.from(container.querySelectorAll('button')).find(
          (button) => button.textContent === 'terminal-a',
        ),
      )
      expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).not.toBeNull()
      await act(async () => window.dispatchEvent(new Event('pagehide')))
      expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).toBeNull()
      await act(async () => window.dispatchEvent(new Event('pageshow')))
      expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).toBeNull()
      getPodDetailMock.mockResolvedValue({
        uid,
        containers: [{ name: 'api', state }],
        allowedActions: ['exec'],
      })
      await click(
        Array.from(container.querySelectorAll('button')).find(
          (button) => button.textContent === '重新连接',
        ),
      )
      expect(container.textContent).toContain(message)
      expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).toBeNull()
      const saved = Array.from({ length: localStorage.length }, (_, i) =>
        localStorage.getItem(localStorage.key(i)!),
      ).join('')
      expect(saved).toContain('uid-pod-a')
      expect(saved).not.toContain('replacement')
    },
  )

  it('does not adopt a legacy saved record without UID', async () => {
    createSessionDockStore('platform', 'user-a').setState({
      sessions: [
        {
          id: 'legacy',
          kind: 'terminal',
          clusterId: 'cluster-a',
          namespace: 'team-a',
          podName: 'pod-a',
          container: 'api',
          shell: '/bin/sh',
        },
      ],
    })
    const container = await renderDock()
    await click(container.querySelector('.soha-header-realtime-sessions'))
    await click(
      Array.from(container.querySelectorAll('button')).find(
        (button) => button.textContent === '重新连接',
      ),
    )
    expect(container.textContent).toContain('旧记录缺少 Pod UID')
    expect(container.querySelector('[data-testid="terminal:cluster-a:pod-a"]')).toBeNull()
  })
})
