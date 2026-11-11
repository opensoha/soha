/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useInfiniteQuery } from '@tanstack/react-query'
import { ApiError } from '@/services/api-error'
import { ComputeRelationsPanel, computeResourceIdentity } from './resource-relations-panel'

vi.mock('@tanstack/react-query', async (original) => ({
  ...(await original<typeof import('@tanstack/react-query')>()),
  useInfiniteQuery: vi.fn(),
}))
vi.mock('@/features/auth', () => ({
  usePermissionSnapshot: () => ({
    data: { data: { permissionKeys: [], visibleMenuIds: [], visibleMenus: [] } },
  }),
}))
const roots: ReturnType<typeof createRoot>[] = []
const refetch = vi.fn()
const vm = { domain: 'virtualization', kind: 'vm', id: 'same-id', displayName: 'vm-demo' }
const host = {
  domain: 'container_runtime',
  kind: 'runtime_host',
  id: 'same-id',
  displayName: 'host-demo',
}
const project = {
  domain: 'container_runtime',
  kind: 'project',
  id: 'project-1',
  displayName: 'project-demo',
}
const data = {
  resource: vm,
  relations: [
    {
      from: vm,
      to: host,
      type: 'runs_on',
      source: 'persisted',
      observedAt: '2026-10-14T00:00:00Z',
      stale: true,
    },
    {
      from: project,
      to: host,
      type: 'runs_on',
      source: 'derived',
      observedAt: '2026-10-14T00:00:00Z',
    },
  ],
}
function query(overrides = {}) {
  return {
    data,
    isError: false,
    error: null,
    isLoading: false,
    isFetching: false,
    refetch,
    ...overrides,
  }
}
function render(id = 'same-id') {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  const update = (resourceId = id) =>
    act(() =>
      root.render(
        <MemoryRouter>
          <ComputeRelationsPanel domain="virtualization" kind="vm" resourceId={resourceId} />
        </MemoryRouter>,
      ),
    )
  update()
  return { container, update }
}
afterEach(() => {
  roots.splice(0).forEach((root) => act(() => root.unmount()))
  document.body.innerHTML = ''
  vi.clearAllMocks()
})
describe('compute relationship exploration', () => {
  it('distinguishes identities by domain and kind, even when IDs are equal', () => {
    expect(computeResourceIdentity(vm as never)).not.toBe(computeResourceIdentity(host as never))
  })
  it('selects actual neighboring records without additional requests and preserves selection on refresh', () => {
    vi.mocked(useInfiniteQuery).mockReturnValue(query() as never)
    const { container, update } = render()
    act(() =>
      container
        .querySelector<HTMLButtonElement>('button[aria-label="选中关联对象 vm-demo"]')!
        .click(),
    )
    expect(container.querySelectorAll('.soha-compute-relation-row')).toHaveLength(1)
    expect(container.textContent).toContain('数据陈旧')
    expect(container.querySelectorAll('a')).toHaveLength(0)
    expect(refetch).not.toHaveBeenCalled()
    vi.mocked(useInfiniteQuery).mockReturnValue(query({ isFetching: true }) as never)
    update()
    expect(container.querySelector('button[aria-pressed="true"]')?.textContent).toBe('vm-demo')
    update('different-resource')
    expect(container.querySelector('button[aria-pressed="true"]')?.textContent).toBe('全部关系')
  })
  it('retains cached evidence after a server failure, hides it after denial, and clears removed selection', () => {
    vi.mocked(useInfiniteQuery).mockReturnValue(query() as never)
    const { container, update } = render()
    act(() =>
      container
        .querySelector<HTMLButtonElement>('button[aria-label="选中关联对象 host-demo"]')!
        .click(),
    )
    vi.mocked(useInfiniteQuery).mockReturnValue(
      query({ isError: true, error: new ApiError(503, 'offline') }) as never,
    )
    update()
    expect(container.textContent).toContain('上次读取结果')
    expect(container.querySelectorAll('.soha-compute-relation-row')).toHaveLength(2)
    vi.mocked(useInfiniteQuery).mockReturnValue(
      query({ data: { resource: vm, relations: [] } }) as never,
    )
    update()
    expect(container.querySelector('.soha-compute-relation-selected')).toBeNull()
    vi.mocked(useInfiniteQuery).mockReturnValue(
      query({ isError: true, error: new ApiError(403, 'denied') }) as never,
    )
    update()
    expect(container.textContent).not.toContain('host-demo')
    expect(container.textContent).toContain('无访问权限')
  })
  it('does not label a partial empty batch as an empty relationship graph', () => {
    vi.mocked(useInfiniteQuery).mockReturnValue(
      query({ data: { resource: vm, relations: [], nextCursor: 'next' } }) as never,
    )
    const { container } = render()
    expect(container.textContent).toContain('不代表完整关系链')
    expect(container.textContent).not.toContain('暂无资源关系')
  })
})
