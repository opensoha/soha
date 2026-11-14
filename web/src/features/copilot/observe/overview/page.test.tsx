/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PermissionSnapshot } from '@/types'
import { AIObserveOverviewPage } from './page'

const useQueryMock = vi.hoisted(() => vi.fn())
const snapshot = {
  permissionKeys: [
    'observe.ai.view',
    'observe.ai.chat',
    'ai.knowledge.view',
    'ai.gateway.view',
    'ai.gateway.relay.view',
    'ai.gateway.approvals.view',
    'ai.evaluations.view',
    'ai.operations.view',
  ],
  visibleMenuIds: [],
  visibleMenus: [],
} as PermissionSnapshot

vi.mock('@tanstack/react-query', async () => {
  const actual =
    await vi.importActual<typeof import('@tanstack/react-query')>('@tanstack/react-query')
  return { ...actual, useQuery: useQueryMock }
})

vi.mock('@/features/auth/permission-snapshot', async () => {
  const actual = await vi.importActual<typeof import('@/features/auth/permission-snapshot')>(
    '@/features/auth/permission-snapshot',
  )
  return {
    ...actual,
    usePermissionSnapshot: () => ({
      data: { data: snapshot },
      isLoading: false,
      isError: false,
      refetch: vi.fn(),
    }),
  }
})

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>

beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const results = new Map<string, unknown>([
    ['copilot-workbench-sessions', []],
    ['copilot-workbench-catalog', { agentProviders: [], skillsRegistry: [], capabilities: [] }],
    ['copilot-agent-runs', [{ id: 'run-1', status: 'failed' }]],
    ['ai,knowledge,bases', [{ id: 'base-1', status: 'degraded' }]],
    ['ai-gateway,capabilities', { summary: { toolCount: 2, skillCount: 1 } }],
    ['ai-gateway,relay,metrics', { requestsToday: 12, successRate: 0.99 }],
    ['ai,evaluations,runs', [{ id: 'evaluation-1', status: 'failed' }]],
    ['ai-gateway,approval-requests', [{ id: 'approval-1', status: 'pending' }]],
    ['ai,production-operations,snapshots', [{ id: 'operation-1', status: 'queued' }]],
  ])
  useQueryMock.mockImplementation(({ queryKey }: { queryKey: unknown[] }) => {
    const key = queryKey.filter((part) => typeof part === 'string').join(',')
    const match = [...results.entries()].find(([prefix]) => key.startsWith(prefix))
    return {
      data: { data: match?.[1] ?? [] },
      isLoading: false,
      isError: false,
    }
  })

  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
})

afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  useQueryMock.mockReset()
  vi.unstubAllGlobals()
})

describe('AIObserveOverviewPage', () => {
  it('links real operational exceptions to their handling pages', async () => {
    await act(async () => {
      root.render(
        <MemoryRouter>
          <AIObserveOverviewPage />
        </MemoryRouter>,
      )
    })

    const operationalChips = [...container.querySelectorAll('.soha-overview-chip')].slice(-5)
    expect(operationalChips).toHaveLength(5)
    expect(operationalChips.map((chip) => chip.textContent)).toEqual([
      '知识库1异常知识库',
      '评测回归1失败评测',
      '审批请求1待审批',
      'Agent Runs1异常运行',
      '生产操作1进行中或待执行',
    ])
    expect(operationalChips.every((chip) => chip.classList.contains('is-warning'))).toBe(true)
    expect(container.querySelector('.soha-ai-overview-action')).toBeNull()
    expect(container.querySelector('.soha-overview-section-bar')).toBeNull()
    expect(container.querySelector('a[href="/ai-gateway/governance?tab=approvals"]')).not.toBeNull()
  })
})
