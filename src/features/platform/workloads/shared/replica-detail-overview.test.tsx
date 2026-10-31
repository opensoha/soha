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

    expect(view.textContent).toContain('ReplicaSet 仍有副本未就绪')
    expect(view.querySelector('.soha-workload-overview-facts')?.textContent).toContain('期望3')
    expect(view.querySelector('.soha-workload-overview-facts')?.textContent).toContain('就绪2')
    expect(view.querySelector('.soha-workload-overview-facts')?.textContent).not.toContain('当前')
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

    expect(view.textContent).toContain('ReplicationController 仍有副本未就绪')
    expect(view.textContent).toContain('当前2')
    expect(view.querySelector('.soha-workload-overview-facts')?.textContent).toContain('期望2')
    expect(view.querySelector('.soha-workload-overview-facts')?.textContent).toContain('就绪1')
    expect(view.textContent).toContain('Pods 2')
  })

  it('aligns StatefulSet overview regions without inventing Deployment-only data', async () => {
    const view = await render(
      <ControllerOverview
        resource="statefulsets"
        title="StatefulSet"
        detail={{
          name: 'database',
          namespace: 'apps',
          createdAt: '2026-01-01',
          desiredReplicas: 3,
          currentReplicas: 3,
          readyReplicas: 3,
          currentRevision: 'database-old',
          updateRevision: 'database-new',
          updateStrategy: 'RollingUpdate',
          serviceName: 'database-headless',
          labels: { app: 'database' },
          annotations: { owner: 'team' },
          pods: [{ name: 'database-0' }],
        }}
      />,
    )
    const main = view.querySelector('.soha-workload-overview > div')!
    const aside = view.querySelector('.soha-workload-overview > aside')!
    expect(main.textContent).toContain('当前副本已就绪，版本仍有差异')
    expect(main.textContent).toContain('副本与发布')
    expect(main.textContent).toContain('Pods 1')
    expect(aside.textContent).toContain('运行配置')
    expect(aside.textContent).toContain('database-headless')
    expect(aside.textContent).toContain('database-old')
    expect(aside.textContent).toContain('database-new')
    expect(view.querySelectorAll('.soha-k8s-detail-metadata')).toHaveLength(1)
    expect(view.querySelectorAll('.soha-k8s-detail-metadata [aria-expanded="false"]')).toHaveLength(
      2,
    )
    for (const absent of ['当前模板', '运行条件', '已更新', '可用', 'ReplicaSet', '更新完成']) {
      expect(view.textContent).not.toContain(absent)
    }
  })

  it.each([
    [{ desiredReplicas: 0, currentReplicas: 0, readyReplicas: 0 }, '已缩容至 0 个副本', '0'],
    [{}, 'StatefulSet 就绪数据尚未返回', '—'],
    [{ desiredReplicas: 3, readyReplicas: 1 }, 'StatefulSet 仍有副本未就绪', '3'],
  ])(
    'keeps zero, unavailable and unready replica states distinct',
    async (counts, message, expected) => {
      const view = await render(
        <ControllerOverview
          detail={{ name: 'database', namespace: 'apps', createdAt: '2026-01-01', ...counts }}
          resource="statefulsets"
          title="StatefulSet"
        />,
      )
      expect(view.textContent).toContain(message)
      expect(view.querySelector('.soha-workload-overview-facts dd')?.textContent).toBe(expected)
    },
  )
})
