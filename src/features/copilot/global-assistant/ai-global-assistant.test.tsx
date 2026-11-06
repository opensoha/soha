/** @vitest-environment jsdom */

import { api } from '@/services/api-client'
import { usePreferencesStore } from '@/stores/preferences-store'
import type { PermissionSnapshot } from '@/types'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App as AntdApp } from 'antd'
import { act, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  encodeAIContextForElement,
  inferSelectionKind,
  sanitizeSelectionText,
  workbenchScopeFromAIContext,
  type AIPageContext,
} from './ai-context'
import { useAIGlobalAssistant, useAIPageContext } from './ai-context-provider'
import { GlobalAIAssistantProvider, sourceWorkbenchFromPath } from './ai-global-assistant-provider'
import { buildGlobalAssistantPrompt } from './ai-prompts'
import { clampFloatPosition, snapFloatPosition } from './draggable-float-shell'

vi.mock('@/services/api-client', () => ({
  api: {
    serviceScope: () => new URL('/api/v1', window.location.origin).toString(),
    get: vi.fn(),
    delete: vi.fn(),
    post: vi.fn(),
    postWithSignal: vi.fn(),
  },
}))

const permissionSnapshot: PermissionSnapshot = {
  permissionKeys: ['observe.ai.chat'],
  visibleMenuIds: [],
  visibleMenus: [],
}

const serviceContext: AIPageContext = {
  sourceWorkbench: 'platform',
  sourceRoute: '/network/services/payment-api?namespace=payments',
  sourceTitle: 'Service payment-api',
  entityKind: 'kubernetes.service',
  entityName: 'payment-api',
  clusterId: 'prod',
  namespace: 'payments',
  service: 'payment-api',
  timeRangeMinutes: 60,
  promptHint: '排查 Service 访问异常。',
}

let containers: HTMLDivElement[] = []
let roots: Array<ReturnType<typeof createRoot>> = []

function Harness() {
  const assistant = useAIGlobalAssistant()
  useAIPageContext(serviceContext)

  return (
    <div className="soha-pro-content-host">
      <button
        type="button"
        onClick={() => {
          void assistant?.launchAssistant({ action: 'analyze-page' })
        }}
      >
        ask
      </button>
      <button
        type="button"
        data-testid="explain"
        onClick={() =>
          void assistant?.launchAssistant({
            action: 'explain-selection',
            selection: { kind: 'plain', text: 'hello' },
          })
        }
      >
        explain
      </button>
      <div
        data-testid="row-context"
        data-ai-context={encodeAIContextForElement({
          sourceWorkbench: 'platform',
          sourceRoute: '/workloads/pods/demo-pod?namespace=payments',
          sourceTitle: 'Pod demo-pod',
          entityKind: 'kubernetes.pod',
          entityName: 'demo-pod',
          clusterId: 'prod',
          namespace: 'payments',
          pod: 'demo-pod',
          timeRangeMinutes: 60,
        })}
      >
        demo-pod
      </div>
      <p data-testid="selectable-text">ERROR password=secret token=abc payment-api timeout</p>
      <input data-testid="plain-input" />
    </div>
  )
}

function ToggleableProviderHarness() {
  const [enabled, setEnabled] = useState(true)
  return (
    <>
      <button
        data-testid="toggle-assistant"
        type="button"
        onClick={() => setEnabled((value) => !value)}
      >
        toggle
      </button>
      <GlobalAIAssistantProvider enabled={enabled} permissionSnapshot={permissionSnapshot}>
        <Harness />
      </GlobalAIAssistantProvider>
    </>
  )
}

async function renderProvider({
  enabled = true,
  nativeCompanionWindow = false,
}: { enabled?: boolean; nativeCompanionWindow?: boolean } = {}) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/network/services/payment-api?namespace=payments']}>
          <AntdApp>
            <GlobalAIAssistantProvider
              enabled={enabled}
              nativeCompanionWindow={nativeCompanionWindow}
              permissionSnapshot={permissionSnapshot}
            >
              <Harness />
            </GlobalAIAssistantProvider>
          </AntdApp>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })

  await act(async () => {
    await Promise.resolve()
  })
  return container
}

async function renderToggleableProvider() {
  const container = document.createElement('div')
  document.body.appendChild(container)
  containers.push(container)
  const root = createRoot(container)
  roots.push(root)
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })

  await act(async () => {
    root.render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={['/network/services/payment-api?namespace=payments']}>
          <AntdApp>
            <ToggleableProviderHarness />
          </AntdApp>
        </MemoryRouter>
      </QueryClientProvider>,
    )
  })
  return container
}

async function waitForText(container: HTMLElement, text: string) {
  await vi.waitFor(
    async () => {
      await act(async () => {
        await Promise.resolve()
      })
      expect(container.textContent).toContain(text)
    },
    { timeout: 5_000 },
  )
}

function sseResponse(content: string) {
  const encoder = new TextEncoder()
  const stream = new ReadableStream({
    start(controller) {
      controller.enqueue(
        encoder.encode(
          `data: ${JSON.stringify({
            id: 'event-1',
            sessionId: 'session-global',
            sequence: 1,
            createdAt: '2026-07-06T00:00:00Z',
            type: 'message.done',
            role: 'assistant',
            messageId: 'message-ai',
            content,
          })}\n\n`,
        ),
      )
      controller.close()
    },
  })
  return Promise.resolve(new Response(stream, { status: 200 }))
}

function mockSelection(anchorNode: Node, text: string) {
  vi.spyOn(window, 'getSelection').mockReturnValue({
    anchorNode,
    getRangeAt: () => ({
      getBoundingClientRect: () => ({
        left: 80,
        top: 80,
        width: 120,
        height: 20,
        right: 200,
        bottom: 100,
        x: 80,
        y: 80,
        toJSON: () => ({}),
      }),
      getClientRects: () => [
        {
          left: 80,
          top: 80,
          width: 120,
          height: 20,
          right: 200,
          bottom: 100,
          x: 80,
          y: 80,
          toJSON: () => ({}),
        },
      ],
    }),
    isCollapsed: false,
    rangeCount: 1,
    toString: () => text,
  } as unknown as Selection)
}

describe('global AI assistant utilities', () => {
  it('classifies K8s route families as the platform workbench', () => {
    for (const path of [
      '/',
      '/clusters/demo',
      '/cluster-resources/nodes',
      '/workloads/replicasets',
      '/configuration/secrets',
      '/network/services',
      '/storage/persistentvolumes',
      '/platform-access-control/roles',
      '/resource-creation',
      '/manifests',
      '/extensions/apis/apps',
      '/helm/releases',
    ]) {
      expect(sourceWorkbenchFromPath(path)).toBe('platform')
    }
    expect(sourceWorkbenchFromPath('/monitoring-workbench')).toBe('monitoring')
    expect(sourceWorkbenchFromPath('/ai-workbench/overview')).toBe('ai')
  })

  it('sanitizes selected text and maps page context into Workbench scope', () => {
    expect(sanitizeSelectionText('Authorization: Bearer abc\npassword=secret')).toContain(
      '[REDACTED]',
    )
    expect(inferSelectionKind('ERROR failed to connect')).toBe('error')
    expect(workbenchScopeFromAIContext(serviceContext)).toEqual({
      clusterId: 'prod',
      namespace: 'payments',
      service: 'payment-api',
      timeRangeMinutes: 60,
    })
  })

  it('builds an explicit context prompt without relying on DOM scraping', () => {
    const prompt = buildGlobalAssistantPrompt('troubleshoot-selection', serviceContext, {
      kind: 'error',
      text: 'ERROR timeout',
    })

    expect(prompt).toContain('sourceWorkbench: platform')
    expect(prompt).toContain('service: payment-api')
    expect(prompt).toContain('选中内容类型: error')
    expect(prompt).toContain('高风险操作只给建议')
  })

  it('clamps and snaps draggable FloatButton positions', () => {
    expect(
      clampFloatPosition(
        { x: -20, y: 900 },
        { width: 400, height: 300 },
        { width: 48, height: 48 },
      ),
    ).toEqual({
      x: 24,
      y: 228,
    })
    expect(
      clampFloatPosition(
        { x: 24, y: 120 },
        { width: 1280, height: 720, left: 200 },
        { width: 176, height: 210 },
      ),
    ).toEqual({
      x: 224,
      y: 120,
    })
    expect(
      snapFloatPosition({ x: 340, y: 120 }, { width: 400, height: 300 }, { width: 48, height: 48 }),
    ).toEqual({
      x: 328,
      y: 120,
      edge: 'right',
    })
  })
})

describe('GlobalAIAssistantProvider', () => {
  beforeAll(() => {
    ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    class IntersectionObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
      takeRecords() {
        return []
      }
    }
    Object.defineProperty(window, 'ResizeObserver', { writable: true, value: ResizeObserverMock })
    Object.defineProperty(window, 'IntersectionObserver', {
      writable: true,
      value: IntersectionObserverMock,
    })
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation(() => ({
        addEventListener: vi.fn(),
        addListener: vi.fn(),
        dispatchEvent: vi.fn(),
        matches: false,
        media: '',
        onchange: null,
        removeEventListener: vi.fn(),
        removeListener: vi.fn(),
      })),
    })
  })

  beforeEach(() => {
    vi.mocked(api.get).mockImplementation(async (path) => {
      if (path === '/copilot/workbench/catalog')
        return {
          data: {
            adapters: [],
            dataSources: [],
            analysisProfiles: [],
            agentProviders: [
              {
                id: 'hermes',
                kind: 'hermes',
                name: 'Hermes',
                enabled: true,
                capabilities: ['general', 'root_cause'],
                runtimeStatus: { state: 'ready', queuedRuns: 0, runningRuns: 0, recentFailures: 0 },
              },
            ],
          },
        } as never
      return { data: [] } as never
    })

    vi.clearAllMocks()
    usePreferencesStore.setState({ companionMode: 'icon' })
    vi.mocked(api.postWithSignal).mockResolvedValue({
      data: {
        id: 'session-global',
        title: 'Service payment-api',
        updatedAt: '2026-07-06T00:00:00Z',
        metadata: { mode: 'root_cause' },
      },
    })
    vi.spyOn(window, 'fetch').mockImplementation(() => sseResponse('已完成当前服务分析。'))
    window.localStorage.clear()
  })

  afterEach(() => {
    roots.forEach((root) => root.unmount())
    roots = []
    containers.forEach((container) => container.remove())
    containers = []
    vi.restoreAllMocks()
  })

  it('creates a Workbench session and renders streamed assistant output', async () => {
    const container = await renderProvider()
    const askButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'ask',
    )
    expect(askButton).toBeTruthy()

    await act(async () => {
      askButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(api.postWithSignal).toHaveBeenCalledWith(
      '/copilot/sessions',
      expect.objectContaining({
        mode: 'root_cause',
        scope: expect.objectContaining({
          clusterId: 'prod',
          namespace: 'payments',
          service: 'payment-api',
        }),
        tags: ['global-assistant'],
      }),
      expect.any(AbortSignal),
    )
    await waitForText(container, '已完成当前服务分析。')
    expect(String(container.textContent)).toContain('已完成当前服务分析。')
  }, 15_000)

  it('uses general chat for explaining a selection', async () => {
    const container = await renderProvider()
    await act(async () => {
      container.querySelector<HTMLButtonElement>('[data-testid="explain"]')?.click()
    })
    await waitForText(container, '已完成当前服务分析。')
    expect(api.postWithSignal).toHaveBeenCalledWith(
      '/copilot/sessions',
      expect.objectContaining({ mode: 'general', agentProviderId: 'hermes' }),
      expect.any(AbortSignal),
    )
    const init = vi.mocked(window.fetch).mock.calls[0]?.[1]
    expect(JSON.parse(String(init?.body)).mode).toBe('general')
  })

  it.each([false, true])(
    'follows a queued external run and supports cancellation (%s)',
    async (cancel) => {
      const get = vi.mocked(api.get).getMockImplementation()!
      vi.mocked(api.get).mockImplementation(async (path, ...args) => {
        if (String(path).startsWith('/copilot/agent-runs?'))
          return {
            data: [
              {
                id: 'run-global',
                providerId: 'hermes',
                providerKind: 'hermes',
                capabilityId: 'root_cause',
                status: cancel ? 'running' : 'completed',
                output: { summary: '真实外部助手回复' },
              },
            ],
          } as never
        return get(path, ...args)
      })
      vi.mocked(api.post).mockResolvedValue({
        data: {
          id: 'run-global',
          sessionId: 'session-global',
          providerId: 'hermes',
          providerKind: 'hermes',
          status: 'canceled',
        },
      })
      vi.mocked(window.fetch).mockResolvedValue(
        new Response(
          `data: ${JSON.stringify({
            type: 'agent.status',
            id: 'queued-1',
            sessionId: 'session-global',
            sequence: 1,
            createdAt: '2026-07-06T00:00:00Z',
            providerId: 'hermes',
            status: 'queued',
            runId: 'run-global',
          })}\n\ndata: ${JSON.stringify({ type: 'message.done', id: 'queued-message', sessionId: 'session-global', sequence: 2, createdAt: '2026-07-06T00:00:00Z', messageId: 'run-global:queued', role: 'assistant', content: '已提交给助手。' })}\n\n`,
          { status: 200 },
        ),
      )
      const container = await renderProvider()
      await act(async () => {
        Array.from(container.querySelectorAll('button'))
          .find((button) => button.textContent === 'ask')
          ?.click()
      })
      if (cancel) {
        await waitForText(container, '助手正在处理当前请求。')
        const button = Array.from(container.querySelectorAll('button')).find((item) =>
          item.textContent?.includes('Stop loading'),
        )
        expect(button).toBeTruthy()
        await act(async () => {
          button?.click()
        })
        await vi.waitFor(() =>
          expect(api.post).toHaveBeenCalledWith('/copilot/agent-runs/run-global/cancel', {}),
        )
        expect(api.delete).not.toHaveBeenCalled()
      } else {
        await waitForText(container, '真实外部助手回复')
        expect(api.get).toHaveBeenCalledWith('/copilot/agent-runs?sessionId=session-global')
      }
    },
  )

  it('keeps sending locked after an uncertain cancel, supports status reads and explicit cancellation retry', async () => {
    const get = vi.mocked(api.get).getMockImplementation()!
    let status = 'running'
    let failRead = false
    vi.mocked(api.get).mockImplementation(async (path, ...args) => {
      if (String(path).startsWith('/copilot/agent-runs?')) {
        if (failRead) throw new Error('状态读取暂不可用')
        return {
          data: [
            {
              id: 'run-global',
              sessionId: 'session-global',
              providerId: 'hermes',
              providerKind: 'hermes',
              status,
            },
            {
              id: 'another-run',
              sessionId: 'session-global',
              providerId: 'hermes',
              providerKind: 'hermes',
              status: 'running',
            },
          ],
        } as never
      }
      return get(path, ...args)
    })
    vi.mocked(api.post)
      .mockRejectedValueOnce(new Error('取消回执丢失'))
      .mockImplementation(async () => {
        status = 'canceled'
        return {
          data: {
            id: 'run-global',
            sessionId: 'session-global',
            providerId: 'hermes',
            providerKind: 'hermes',
            status,
          },
        } as never
      })
    vi.mocked(window.fetch).mockResolvedValue(
      new Response(
        `data: ${JSON.stringify({ type: 'agent.status', id: 'queued-global', sessionId: 'session-global', sequence: 1, createdAt: '2026-10-16T00:00:00Z', providerId: 'hermes', status: 'queued', runId: 'run-global' })}\n\ndata: ${JSON.stringify({ type: 'message.done', id: 'queued-message-global', sessionId: 'session-global', sequence: 2, createdAt: '2026-10-16T00:00:00Z', messageId: 'queued-message', role: 'assistant', content: '已提交给助手。' })}\n\n`,
        { status: 200 },
      ),
    )
    const container = await renderProvider()
    const click = async (text: string) => {
      await act(async () => {
        Array.from(container.querySelectorAll('button'))
          .find((b) => b.textContent?.replace(/ /g, '').includes(text))
          ?.click()
      })
    }
    await click('ask')
    await waitForText(container, '助手正在处理当前请求。')
    await click('Stoploading')
    await waitForText(container, '取消回执丢失')
    expect(api.post).toHaveBeenCalledTimes(1)
    const submissions = vi.mocked(window.fetch).mock.calls.length
    await click('ask')
    expect(window.fetch).toHaveBeenCalledTimes(submissions)
    failRead = true
    await click('读取状态')
    await waitForText(container, '状态读取暂不可用')
    expect(api.post).toHaveBeenCalledTimes(1)
    failRead = false
    await click('读取状态')
    await waitForText(container, '任务仍在运行')
    await click('请求取消')
    await waitForText(container, '任务 run-global 已确认终态：canceled')
    expect(api.post).toHaveBeenCalledTimes(2)
    expect(
      vi
        .mocked(api.post)
        .mock.calls.every(([path]) => path === '/copilot/agent-runs/run-global/cancel'),
    ).toBe(true)
    expect(container.textContent).not.toContain('后台任务取消待确认')
    expect(api.delete).not.toHaveBeenCalled()
  }, 15000)

  it('reads candidates without canceling any run when the submitted stream has no run ID', async () => {
    const get = vi.mocked(api.get).getMockImplementation()!
    vi.mocked(api.get).mockImplementation(async (path, ...args) =>
      String(path).startsWith('/copilot/agent-runs?')
        ? ({
            data: [
              {
                id: 'unrelated-run',
                sessionId: 'session-global',
                status: 'running',
                providerKind: 'hermes',
              },
            ],
          } as never)
        : get(path, ...args),
    )
    vi.mocked(window.fetch).mockImplementation(
      (_input, init) =>
        new Promise<Response>((_resolve, reject) =>
          init?.signal?.addEventListener(
            'abort',
            () => reject(new DOMException('Stopped', 'AbortError')),
            { once: true },
          ),
        ),
    )
    const container = await renderProvider()
    await act(async () => {
      Array.from(container.querySelectorAll('button'))
        .find((b) => b.textContent === 'ask')
        ?.click()
    })
    await vi.waitFor(() => expect(window.fetch).toHaveBeenCalled())
    await act(async () => {
      Array.from(container.querySelectorAll('button'))
        .find((b) => b.textContent?.includes('Stop loading'))
        ?.click()
    })
    await waitForText(container, '尚未取得本次任务 ID')
    expect(api.get).toHaveBeenCalledWith('/copilot/agent-runs?sessionId=session-global')
    expect(api.post).not.toHaveBeenCalled()
    expect(api.delete).not.toHaveBeenCalled()
    expect(container.querySelector('[aria-label="待核对助手任务"]')).toBeTruthy()
  })

  it('reuses the shared session in the native companion window', async () => {
    window.localStorage.setItem(
      'soha.ai.global-assistant.session',
      JSON.stringify({
        contextKey: 'shared-main-window',
        session: {
          id: 'session-shared',
          title: 'Shared assistant',
          updatedAt: '2026-07-06T00:00:00Z',
          metadata: { mode: 'root_cause' },
        },
      }),
    )
    const container = await renderProvider({ nativeCompanionWindow: true })
    const askButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'ask',
    )

    await act(async () => {
      askButton?.click()
    })
    await waitForText(container, '已完成当前服务分析。')

    expect(api.postWithSignal).not.toHaveBeenCalled()
    expect(window.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/copilot/sessions/session-shared/messages/stream'),
      expect.any(Object),
    )
  })

  it('keeps a submitted session and its running stream when the panel closes', async () => {
    let streamSignal: AbortSignal | undefined
    vi.spyOn(window, 'fetch').mockImplementation((_input, init) => {
      streamSignal = init?.signal ?? undefined
      return new Promise<Response>((_resolve, reject) => {
        streamSignal?.addEventListener(
          'abort',
          () => reject(new DOMException('The operation was aborted.', 'AbortError')),
          { once: true },
        )
      })
    })
    vi.mocked(api.delete).mockResolvedValue(undefined)
    const container = await renderProvider()
    const askButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'ask',
    )

    await act(async () => {
      askButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
    })
    const closeButton = container.querySelector<HTMLButtonElement>(
      'button[aria-label="关闭 AI 助手"]',
    )
    expect(closeButton).not.toBeNull()

    await act(async () => {
      closeButton?.click()
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(streamSignal?.aborted).toBe(false)
    expect(api.delete).not.toHaveBeenCalled()
    expect(container.querySelector('[data-testid="soha-ai-panel"]')).toBeNull()
  })

  it('shows a selection toolbar only after an explicit text selection', async () => {
    const container = await renderProvider()
    const selectable = container.querySelector('[data-testid="selectable-text"]')
    expect(selectable?.firstChild).toBeTruthy()
    mockSelection(selectable!.firstChild!, 'ERROR password=secret')

    await act(async () => {
      document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }))
      await new Promise((resolve) => window.setTimeout(resolve, 0))
    })

    expect(container.querySelector('[data-testid="soha-ai-selection-toolbar"]')).toBeTruthy()
    expect(window.fetch).not.toHaveBeenCalled()
  })

  it('does not intercept native context menus in inputs', async () => {
    const container = await renderProvider()
    const input = container.querySelector('[data-testid="plain-input"]')
    const event = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 40,
      clientY: 40,
    })

    input?.dispatchEvent(event)

    expect(event.defaultPrevented).toBe(false)
    expect(container.querySelector('[data-testid="soha-ai-context-menu"]')).toBeNull()
  })

  it('removes every global assistant entrypoint when the capability is disabled', async () => {
    const container = await renderProvider({ enabled: false })
    const row = container.querySelector('[data-testid="row-context"]')
    const selectable = container.querySelector('[data-testid="selectable-text"]')
    expect(selectable?.firstChild).toBeTruthy()
    mockSelection(selectable!.firstChild!, 'ERROR payment-api timeout')

    await act(async () => {
      document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }))
      await new Promise((resolve) => window.setTimeout(resolve, 0))
    })

    const event = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 80,
      clientY: 80,
    })
    row?.dispatchEvent(event)

    expect(container.querySelector('.soha-ai-float-button')).toBeNull()
    expect(container.querySelector('[data-testid="soha-ai-selection-toolbar"]')).toBeNull()
    expect(container.querySelector('[data-testid="soha-ai-context-menu"]')).toBeNull()
    expect(event.defaultPrevented).toBe(false)

    const askButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'ask',
    )
    await act(async () => {
      askButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
    })
    expect(api.post).not.toHaveBeenCalled()
    expect(api.postWithSignal).not.toHaveBeenCalled()
  })

  it('clears stale selection state while toggling the capability off and on', async () => {
    const container = await renderToggleableProvider()
    const selectable = container.querySelector('[data-testid="selectable-text"]')
    expect(selectable?.firstChild).toBeTruthy()
    mockSelection(selectable!.firstChild!, 'ERROR payment-api timeout')

    await act(async () => {
      document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }))
      await new Promise((resolve) => window.setTimeout(resolve, 0))
    })
    expect(container.querySelector('[data-testid="soha-ai-selection-toolbar"]')).toBeTruthy()

    const toggle = container.querySelector('[data-testid="toggle-assistant"]')
    await act(async () => {
      toggle?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    expect(container.querySelector('.soha-ai-float-button')).toBeNull()
    expect(container.querySelector('[data-testid="soha-ai-selection-toolbar"]')).toBeNull()

    await act(async () => {
      toggle?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    })
    expect(container.querySelector('.soha-ai-float-button')).toBeTruthy()
    expect(container.querySelector('[data-testid="soha-ai-selection-toolbar"]')).toBeNull()
  })

  it('uses row-level AI context from the controlled context menu', async () => {
    const container = await renderProvider()
    const row = container.querySelector('[data-testid="row-context"]')
    vi.spyOn(window, 'getSelection').mockReturnValue(null)
    const event = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 80,
      clientY: 80,
    })

    await act(async () => {
      row?.dispatchEvent(event)
      await Promise.resolve()
    })

    expect(event.defaultPrevented).toBe(true)
    expect(container.querySelector('[data-testid="soha-ai-context-menu"]')).toBeTruthy()

    const troubleshootButton = Array.from(container.querySelectorAll('button')).find((button) =>
      button.textContent?.includes('AI 排查这个资源'),
    )
    expect(troubleshootButton).toBeTruthy()

    await act(async () => {
      troubleshootButton?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
      await Promise.resolve()
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(api.postWithSignal).toHaveBeenCalledWith(
      '/copilot/sessions',
      expect.objectContaining({
        scope: expect.objectContaining({
          clusterId: 'prod',
          namespace: 'payments',
          pod: 'demo-pod',
        }),
      }),
      expect.any(AbortSignal),
    )
  })
})
