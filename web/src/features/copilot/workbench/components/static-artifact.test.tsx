/** @vitest-environment jsdom */

import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { App } from 'antd'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { permissionSnapshotQueryKey } from '@/features/auth'
import { I18nProvider } from '@/i18n'
import { resolveArtifactPublishIds } from '../artifacts'
import { StaticArtifactView } from './static-artifact'

const apiMocks = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/services/api-client', () => ({ api: apiMocks }))

const mountedRoots: Root[] = []

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn(() => ({
      matches: false,
      media: '',
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(async () => {
  await act(async () => {
    for (const root of mountedRoots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function flushAsync() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}

describe('StaticArtifactView hosted app publishing', () => {
  it('gates publish button on permission and non-empty content, and sends artifact reference without raw body', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    mountedRoots.push(root)

    const artifact = {
      kind: 'report',
      runId: 'artifact:9988-7766',
      title: '部门周报汇总看板',
      summary: '对话生成的静态草稿。',
      dataSourceSnapshot: {
        format: 'text',
        content: '<!doctype html><html><body><h1>SECRET_BODY_DO_NOT_ECHO</h1></body></html>',
        sessionId: 'sess-chat-42',
      },
    }

    await act(async () => {
      root.render(
        <App>
          <QueryClientProvider client={queryClient}>
            <I18nProvider>
              <StaticArtifactView artifact={artifact} sources={[]} onSource={() => {}} />
            </I18nProvider>
          </QueryClientProvider>
        </App>,
      )
    })
    await flushAsync()

    // Without permission, publish button is absent
    expect(
      Array.from(container.querySelectorAll('button')).find(
        (b) => b.textContent?.trim() === '发布为托管应用',
      ),
    ).toBeUndefined()

    // Grant permission
    await act(async () => {
      queryClient.setQueryData(permissionSnapshotQueryKey, {
        data: {
          permissionKeys: ['apphosting.apps.publish'],
          visibleMenuIds: [],
          visibleMenus: [],
        },
      })
    })
    await flushAsync()

    const publishTrigger = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === '发布为托管应用',
    )
    expect(publishTrigger).toBeDefined()

    apiMocks.get.mockResolvedValue({ data: { items: [], total: 0 } })
    await act(async () => {
      publishTrigger?.click()
    })
    await flushAsync()

    const nameInput = document.querySelector<HTMLInputElement>('input[aria-label="托管应用名称"]')!
    expect(nameInput.value).toBe('部门周报汇总看板')

    apiMocks.post.mockResolvedValue({
      data: {
        created: true,
        app: {
          id: 'app-art-1',
          name: '部门周报汇总看板',
          kind: 'static',
          dnsLabel: 'weekly-a1b2',
          host: 'weekly-a1b2.apps.soha.internal',
          url: 'https://weekly-a1b2.apps.soha.internal',
          spaFallback: false,
          ownerUserId: 'usr-1',
          maintainerUserIds: [],
          visibility: { mode: 'private' },
          status: 'online',
          routeGeneration: 1,
          createdAt: '2026-11-21T00:00:00Z',
          updatedAt: '2026-11-21T00:00:00Z',
        },
        revision: {
          id: 'rev-art-1',
          appId: 'app-art-1',
          revisionNumber: 1,
          kind: 'static',
          status: 'ready',
          bundleSha256: 'fedcba0987654321',
          fileCount: 1,
          totalBytes: 512,
          spaFallback: false,
          createdBy: 'usr-1',
          createdAt: '2026-11-21T00:00:00Z',
        },
      },
    })

    const confirmBtn = Array.from(document.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('确认发布'),
    )!
    await act(async () => {
      confirmBtn.click()
    })
    await flushAsync()

    expect(apiMocks.post).toHaveBeenCalledTimes(1)
    const [calledPath, calledPayload] = apiMocks.post.mock.calls[0]
    expect(calledPath).toBe('/app-hosting/workspace-deployments')
    expect(calledPayload).toMatchObject({
      name: '部门周报汇总看板',
      spaFallback: false,
      source: {
        kind: 'conversation_artifact',
        sessionId: 'sess-chat-42',
        artifactId: 'artifact:9988-7766',
      },
    })
    expect(JSON.stringify(calledPayload)).not.toContain('SECRET_BODY_DO_NOT_ECHO')
  })

  it('resolves exact message/personal artifact IDs from controller entries without leaking unrelated URL session', async () => {
    const targetArtifact = {
      kind: 'report',
      runId: '',
      title: '无 runId 会话报表',
      summary: '多产物测试',
      dataSourceSnapshot: { format: 'text', content: '<!doctype html><h1>Report</h1>' },
    }
    const sessionEntry = {
      key: 'msg-77:report:1',
      artifact: targetArtifact,
      index: 0,
      message: {
        id: 'msg-77',
        sessionId: 'sess-real-77',
        role: 'assistant' as const,
        content: '',
        createdAt: '2026-11-21T00:00:00Z',
        metadata: {
          analysisArtifacts: [{ kind: 'stream', runId: 's', title: '', summary: '' }, targetArtifact],
        },
      },
    }
    expect(resolveArtifactPublishIds(sessionEntry, undefined, { id: 'sess-fallback' } as never)).toEqual(
      { sessionId: 'sess-real-77', artifactId: 'message:msg-77:1' },
    )

    const personalEntry = {
      key: 'task:run-88:0',
      artifact: targetArtifact,
      index: 0,
      message: {
        id: 'task:run-88:0',
        sessionId: '',
        role: 'assistant' as const,
        content: '',
        createdAt: '2026-11-21T00:00:00Z',
      },
    }
    expect(
      resolveArtifactPublishIds(personalEntry, personalEntry, { id: 'sess-unrelated' } as never),
    ).toEqual({ sessionId: '', artifactId: 'task:run-88:0' })

    window.history.replaceState({}, '', '/ai-workbench?session=sess-unrelated')
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    queryClient.setQueryData(permissionSnapshotQueryKey, {
      data: { permissionKeys: ['apphosting.apps.publish'], visibleMenuIds: [], visibleMenus: [] },
    })
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    mountedRoots.push(root)

    await act(async () => {
      root.render(
        <App>
          <QueryClientProvider client={queryClient}>
            <I18nProvider>
              <StaticArtifactView
                artifact={targetArtifact}
                sessionId=""
                artifactId="task:run-88:0"
                sources={[]}
                onSource={() => {}}
              />
            </I18nProvider>
          </QueryClientProvider>
        </App>,
      )
    })
    await flushAsync()

    apiMocks.get.mockResolvedValue({ data: { items: [], total: 0 } })
    apiMocks.post.mockResolvedValue({
      data: {
        created: true,
        app: {
          id: 'app-p-1',
          name: '无 runId 会话报表',
          kind: 'static',
          dnsLabel: 'rep-p1',
          host: 'rep-p1.apps.soha.internal',
          url: 'https://rep-p1.apps.soha.internal',
          spaFallback: false,
          ownerUserId: 'usr-1',
          maintainerUserIds: [],
          visibility: { mode: 'private' },
          status: 'online',
          routeGeneration: 1,
          createdAt: '2026-11-21T00:00:00Z',
          updatedAt: '2026-11-21T00:00:00Z',
        },
        revision: {
          id: 'rev-p1',
          appId: 'app-p-1',
          revisionNumber: 1,
          kind: 'static',
          status: 'ready',
          bundleSha256: '11223344',
          fileCount: 1,
          totalBytes: 128,
          spaFallback: false,
          createdBy: 'usr-1',
          createdAt: '2026-11-21T00:00:00Z',
        },
      },
    })

    const trigger = Array.from(container.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === '发布为托管应用',
    )!
    await act(async () => {
      trigger.click()
    })
    await flushAsync()
    const confirm = Array.from(document.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('确认发布'),
    )!
    await act(async () => {
      confirm.click()
    })
    await flushAsync()

    const [, payload] = apiMocks.post.mock.calls[0]
    expect(payload.source).toEqual({
      kind: 'conversation_artifact',
      artifactId: 'task:run-88:0',
    })
    window.history.replaceState({}, '', '/')
  })
})
