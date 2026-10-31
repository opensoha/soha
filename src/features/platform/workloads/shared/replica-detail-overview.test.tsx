/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { ControllerOverview } from './controller-overview'
import { MemoryRouter } from 'react-router-dom'

const details = vi.hoisted(() => ({
  replicasets: {
    name: 'web-rs',
    namespace: 'apps',
    desiredReplicas: 3,
    readyReplicas: 2,
    availableReplicas: 2,
    selector: { app: 'web' },
    pods: [{ name: 'web-1' }],
    relatedResources: [{ kind: 'Deployment', name: 'web', relation: 'owner' }],
  },
  replicationcontrollers: {
    name: 'legacy-web',
    namespace: 'apps',
    desiredReplicas: 2,
    currentReplicas: 2,
    readyReplicas: 1,
    availableReplicas: 1,
    selector: { app: 'legacy-web' },
    pods: [{ name: 'legacy-web-1' }, { name: 'legacy-web-2' }],
    relatedResources: [{ kind: 'Service', name: 'legacy-web' }],
  },
}))

vi.mock('@/i18n', () => ({
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))

vi.mock('./detail-shell', () => ({
  WorkloadDetailShell: ({
    extraOverview,
    resource,
  }: {
    extraOverview: (detail: unknown) => React.ReactNode
    resource: keyof typeof details
  }) => <>{extraOverview(details[resource])}</>,
}))

vi.mock('./workload-relations', () => ({
  WorkloadPodsCard: ({ pods = [] }: { pods?: unknown[] }) => <div>{`Pods ${pods.length}`}</div>,
  WorkloadRelationsCard: ({ resources = [] }: { resources?: unknown[] }) => (
    <div>{`Relations ${resources.length}`}</div>
  ),
}))

let container: HTMLDivElement | null = null
let root: ReturnType<typeof createRoot> | null = null

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({
      matches: false,
      media: '',
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})

afterEach(async () => {
  if (root) {
    await act(async () => root?.unmount())
  }
  container?.remove()
  container = null
  root = null
})

async function render(node: React.ReactNode) {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  await act(async () => root?.render(<MemoryRouter>{node}</MemoryRouter>))
  return container
}

describe('replica workload details', () => {
  it('renders ReplicaSet status and relationships', async () => {
    const view = await render(
      <ControllerOverview
        detail={{ ...details.replicasets, createdAt: '2026-01-01' }}
        resource="replicasets"
        title="ReplicaSet"
      />,
    )

    expect(view.textContent).toContain('ReplicaSet 状态')
    expect(view.querySelector('dl')?.textContent).toContain('期望3')
    expect(view.querySelector('dl')?.textContent).toContain('就绪2')
    expect(view.textContent).toContain('app=web')
    expect(view.textContent).toContain('Pods 1')
  })

  it('renders ReplicationController current replicas and relationships', async () => {
    const view = await render(
      <ControllerOverview
        detail={{ ...details.replicationcontrollers, createdAt: '2026-01-01' }}
        resource="replicationcontrollers"
        title="ReplicationController"
      />,
    )

    expect(view.textContent).toContain('ReplicationController 状态')
    expect(view.textContent).toContain('当前2')
    expect(view.querySelector('dl')?.textContent).toContain('期望2')
    expect(view.querySelector('dl')?.textContent).toContain('就绪1')
    expect(view.textContent).toContain('Pods 2')
  })
})
