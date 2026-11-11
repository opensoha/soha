/** @vitest-environment jsdom */

import { createRoot } from 'react-dom/client'
import { act } from 'react'
import type { ReactNode } from 'react'
import { describe, expect, it, beforeAll, afterEach, vi } from 'vitest'
import { UsageSnapshotPanel, extractUsageSnapshot } from './usage-snapshot'

function render(node: ReactNode) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  return {
    container,
    root,
    async mount() {
      await act(async () => {
        root.render(<>{node}</>)
      })
    },
    unmount() {
      root.unmount()
      container.remove()
    },
  }
}

async function flush() {
  await act(async () => {
    await Promise.resolve()
    await new Promise((resolve) => setTimeout(resolve, 0))
    await Promise.resolve()
  })
}

describe('usage snapshot diff', () => {
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
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
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('extracts before/after snapshots from nested payloads', () => {
    const result = extractUsageSnapshot({
      usageSnapshot: {
        before: { templateId: 'tpl-a' },
        after: { templateId: 'tpl-b' },
      },
    })

    expect(result.before?.templateId).toBe('tpl-a')
    expect(result.after?.templateId).toBe('tpl-b')
    expect(result.current?.templateId).toBe('tpl-b')
  })

  it('renders a structured diff for binding, approval and runtime changes', async () => {
    const snapshot = {
      usageSnapshot: {
        before: {
          templateKind: 'build',
          templateId: 'tpl-1',
          usageCount: 3,
          applicationCount: 1,
          environmentCount: 1,
          productionEnvironmentCount: 0,
          approvalBindingCount: 0,
          targetCount: 1,
          riskLevel: 'low',
          recommendedAction: 'save_with_standard_review',
          bindings: [
            {
              applicationId: 'app-1',
              applicationName: 'Mall API',
              environmentId: 'env-test',
              environmentName: '测试环境',
              requiresApproval: false,
              isProduction: false,
              targetCount: 1,
              riskLevel: 'low',
            },
          ],
          lastExecutionSummary: {
            source: 'runtime',
            stateCounts: { succeeded: 2, failed: 0, running: 1, pending: 0 },
            statusCounts: { completed: 2, running: 1 },
            kindCounts: { build: 2 },
            latestAt: '2026-06-15T08:00:00Z',
          },
        },
        after: {
          templateKind: 'build',
          templateId: 'tpl-1',
          usageCount: 5,
          applicationCount: 2,
          environmentCount: 2,
          productionEnvironmentCount: 1,
          approvalBindingCount: 1,
          targetCount: 3,
          riskLevel: 'high',
          recommendedAction: 'copy_template_before_editing',
          riskReasons: ['1 production environment bindings'],
          bindings: [
            {
              applicationId: 'app-1',
              applicationName: 'Mall API',
              environmentId: 'env-prod',
              environmentName: '生产环境',
              requiresApproval: true,
              isProduction: true,
              targetCount: 2,
              riskLevel: 'high',
            },
            {
              applicationId: 'app-2',
              applicationName: 'ERP Front Main',
              environmentId: 'env-test',
              environmentName: '测试环境',
              requiresApproval: false,
              isProduction: false,
              targetCount: 1,
              riskLevel: 'low',
            },
          ],
          lastExecutionSummary: {
            source: 'runtime',
            stateCounts: { succeeded: 2, failed: 3, running: 0, pending: 0 },
            statusCounts: { completed: 2, failed: 3 },
            kindCounts: { build: 1, release_bundle: 1 },
            latestAt: '2026-06-15T10:00:00Z',
          },
        },
      },
    }

    const view = render(<UsageSnapshotPanel metadata={snapshot as Record<string, unknown>} />)
    await view.mount()
    expect(document.body.textContent).toContain('应用环境绑定变化')
    expect(document.body.textContent).toContain('生产')
    expect(document.body.textContent).toContain('审批门禁')
    expect(document.body.textContent).toContain('失败执行 0 → 3')
    expect(document.body.textContent).toContain('最近执行时间 2026-06-15T08:00:00Z → 2026-06-15T10:00:00Z')
    expect(document.body.textContent).toContain('riskLevel')
    expect(document.body.textContent).toContain('recommendedAction')
    view.unmount()
  })

  it('keeps single snapshots readable without diff sections', async () => {
    const view = render(
      <UsageSnapshotPanel
        metadata={{
          usageSnapshot: {
            templateKind: 'workflow',
            templateId: 'wf-1',
            usageCount: 1,
            applicationCount: 1,
            environmentCount: 1,
            productionEnvironmentCount: 0,
            approvalBindingCount: 0,
            targetCount: 1,
            riskLevel: 'low',
            recommendedAction: 'save_with_standard_review',
            bindings: [
              {
                applicationId: 'app-1',
                applicationName: 'ERP Front Main',
                environmentId: 'env-test',
                environmentName: '测试环境',
                requiresApproval: false,
                isProduction: false,
                targetCount: 1,
                riskLevel: 'low',
              },
            ],
          },
        } as Record<string, unknown>}
      />,
    )

    await view.mount()
    await flush()
    expect(document.body.textContent).toContain('Usage Snapshot')
    expect(document.body.textContent).toContain('ERP Front Main')
    expect(document.body.textContent).toContain('原始 JSON')
    view.unmount()
  })
})
