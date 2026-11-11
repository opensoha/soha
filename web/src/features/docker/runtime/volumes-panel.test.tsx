/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query'
import { ApiError } from '@/services/api-error'
import { DockerProjectVolumesPanel } from './volumes-panel'
import type { DockerRuntimePanelProps } from './shared'

vi.mock('@tanstack/react-query', async (original) => ({
  ...(await original<typeof import('@tanstack/react-query')>()),
  useQuery: vi.fn(),
}))
const roots: ReturnType<typeof createRoot>[] = []
beforeEach(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))
function render(files: object, props: Partial<DockerRuntimePanelProps> = {}) {
  vi.mocked(useQuery).mockImplementation((options) => {
    if (options.queryKey.includes('volumes'))
      return { data: [{ target: '/data', source: 'volume-1' }], isFetching: false } as never
    if (options.queryKey.includes('volume-files')) return { isFetching: false, ...files } as never
    return { data: undefined, isFetching: false } as never
  })
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  act(() =>
    root.render(
      <QueryClientProvider client={client}>
        <DockerProjectVolumesPanel
          enabled
          projectId="project"
          serviceName="service"
          serviceOptions={[{ label: 'service', value: 'service' }]}
          onServiceChange={() => {}}
          {...props}
        />
      </QueryClientProvider>,
    ),
  )
  return container
}
afterEach(() => {
  roots.splice(0).forEach((root) => act(() => root.unmount()))
  document.body.innerHTML = ''
  vi.clearAllMocks()
  vi.unstubAllGlobals()
})
describe('volume directory read evidence', () => {
  it.each([
    { servicesLoading: true, servicesError: false, label: '正在读取服务' },
    { servicesLoading: false, servicesError: true, label: '服务列表读取失败' },
    { servicesLoading: false, servicesError: false, label: '没有可用服务' },
  ])('shows $label instead of an empty volume result without services', (state) => {
    const container = render({}, { ...state, serviceOptions: [] })
    expect(container.textContent).toContain(state.label)
    expect(container.textContent).not.toContain('没有可浏览的卷')
    expect(vi.mocked(useQuery).mock.calls.every(([options]) => options.enabled === false)).toBe(
      true,
    )
  })
  it('distinguishes an unreadable directory from a real empty directory', () => {
    const failed = render({ error: new ApiError(503, 'offline'), isError: true, refetch: vi.fn() })
    expect(failed.textContent).toContain('目录读取失败')
    expect(failed.textContent).not.toContain('暂无文件')
    expect(render({ data: { items: [] } }).textContent).toContain('暂无文件')
  })
  it.each(['', 'removed-service'])(
    'requires a valid service before browsing or refreshing: %s',
    (serviceName) => {
      const container = render({}, { serviceName })
      expect(container.textContent).toContain('请选择可用服务')
      expect(container.textContent).not.toContain('没有可浏览的卷')
      expect(container.querySelector('.ant-select-disabled')).not.toBeNull()
      expect(container.querySelector<HTMLButtonElement>('button')!.disabled).toBe(true)
      expect(vi.mocked(useQuery).mock.calls.every(([options]) => options.enabled === false)).toBe(
        true,
      )
    },
  )
  it('keeps cached directory contents visible during refresh and hides denied content', () => {
    const data = { items: [{ name: 'report.txt', path: '/report.txt', kind: 'file' }] }
    const cached = render({
      data,
      isFetching: true,
      isError: true,
      error: new ApiError(503, 'offline'),
      refetch: vi.fn(),
    })
    expect(cached.textContent).toContain('report.txt')
    const denied = render({
      data,
      isError: true,
      error: new ApiError(403, 'denied'),
      refetch: vi.fn(),
    })
    expect(denied.textContent).not.toContain('report.txt')
    expect(denied.textContent).toContain('无访问权限')
  })
})
