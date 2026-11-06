/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, it, expect, vi } from 'vitest'
import type { PodDetail } from '@/types'
import { PodDetailResources } from './pod-detail-resources'
vi.mock('@/features/platform/shared/resource-insights/workload-relationships', () => ({
  WorkloadRelationships: (props: {
    kind: string
    name: string
    scope: { namespace: string }
    active: boolean
  }) => (
    <div
      data-kind={props.kind}
      data-name={props.name}
      data-namespace={props.scope.namespace}
      data-active={String(props.active)}
    />
  ),
}))
describe('Pod relationship leaf', () => {
  it('uses the detail namespace and passes visibility without synthesizing detail references', async () => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    const container = document.createElement('div')
    const root = createRoot(container)
    await act(async () =>
      root.render(
        <PodDetailResources
          pod={
            {
              name: 'api',
              namespace: 'actual',
              relatedResources: [{ kind: 'Service', name: 'not-a-graph-node' }],
            } as PodDetail
          }
          scope={{ clusterId: 'a', namespace: 'all' }}
          active={false}
        />,
      ),
    )
    expect(container.firstElementChild?.getAttribute('data-kind')).toBe('Pod')
    expect(container.firstElementChild?.getAttribute('data-namespace')).toBe('actual')
    expect(container.firstElementChild?.getAttribute('data-active')).toBe('false')
    expect(container.textContent).not.toContain('not-a-graph-node')
    await act(async () => root.unmount())
    vi.unstubAllGlobals()
  })
})
