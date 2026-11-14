/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { WorkloadsReplicaSetsPage } from './list-page'

vi.mock('react-router-dom', () => ({ useNavigate: () => vi.fn() }))
vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/i18n')>()),
  useI18n: () => ({ localeCode: 'zh_CN' as const }),
}))
vi.mock('@/stores/platform-scope-store', () => ({
  usePlatformScopeStore: () => 'cluster-a',
}))
vi.mock('../shared/list-controls', () => ({
  renderWorkloadNameLink: (name: string) => name,
}))
vi.mock('../shared/replica-controller-list', () => ({
  ReplicaControllerListPage: ({
    columns,
    label,
  }: {
    columns: Array<{ title?: ReactNode }>
    label: string
  }) => (
    <div>
      <span>{label}</span>
      {columns.map((column, index) => (
        <span key={index}>{column.title}</span>
      ))}
    </div>
  ),
}))

describe('WorkloadsReplicaSetsPage i18n', () => {
  beforeAll(() => Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true }))
  afterEach(() => document.body.replaceChildren())

  it('translates ordinary column labels while retaining the Kubernetes kind', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => root.render(<WorkloadsReplicaSetsPage />))

    expect(container.textContent).toContain('ReplicaSets')
    expect(container.textContent).toContain('就绪')
    expect(container.textContent).toContain('期望')
    expect(container.textContent).toContain('可用')
    expect(container.textContent).toContain('时长')
    expect(container.textContent).not.toContain('Ready')

    await act(async () => root.unmount())
  })
})
