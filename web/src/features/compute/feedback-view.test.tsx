import { ReadFeedback } from '@/components/request-feedback'
/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { ConfigProvider } from 'antd'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { ComputeOperationFeedback } from './feedback'

const roots: ReturnType<typeof createRoot>[] = []
function render(node: ReactNode) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  act(() =>
    root.render(
      <ConfigProvider theme={{ token: { motion: false } }}>
        <MemoryRouter>{node}</MemoryRouter>
      </ConfigProvider>,
    ),
  )
  return container
}
afterEach(() => {
  roots.splice(0).forEach((root) => act(() => root.unmount()))
  document.body.innerHTML = ''
})
describe('compute visible feedback', () => {
  it('keeps a submitting request separate from a task outcome and honors disabled motion', () => {
    const container = render(<ComputeOperationFeedback pending label="启动虚拟机" target="vm-1" />)
    expect(container.textContent).toContain('正在提交请求')
    expect(container.textContent).not.toContain('已完成')
    expect(container.querySelector('[data-motion-disabled="true"]')).not.toBeNull()
  })
  it('offers evidence only with task permission and preserves the exact task ID', () => {
    const denied = render(
      <ComputeOperationFeedback
        label="启动"
        receipt={{ message: '任务已提交', taskId: 'task/id', domain: 'virtualization' }}
      />,
    )
    expect(denied.textContent).toContain('任务进度不可见')
    expect(denied.querySelector('a')).toBeNull()
    const allowed = render(
      <ComputeOperationFeedback
        label="启动"
        receipt={{
          message: '任务已提交',
          taskId: 'task/id',
          domain: 'virtualization',
          canViewTask: true,
        }}
      />,
    )
    expect(allowed.querySelector('a')?.getAttribute('href')).toContain('taskId=task%2Fid')
  })
  it('keeps every accepted task in a batch accessible', () => {
    const container = render(
      <ComputeOperationFeedback
        label="批量同步"
        receipt={{
          message: '任务已提交',
          taskIds: ['first', 'second'],
          domain: 'virtualization',
          canViewTask: true,
        }}
      />,
    )
    const links = [...container.querySelectorAll('a')].map((link) => link.getAttribute('href'))
    expect(links).toHaveLength(2)
    expect(links[0]).toContain('taskId=first')
    expect(links[1]).toContain('taskId=second')
  })
  it('distinguishes uncertain writes from failed preflight reads', () => {
    const error = new ApiError(503, 'response lost')
    expect(render(<ComputeOperationFeedback label="保存" error={error} />).textContent).toContain(
      '请求结果不明',
    )
    expect(
      render(<ComputeOperationFeedback label="预检" error={error} readOnly />).textContent,
    ).toContain('操作请求失败')
  })
  it('provides regional retry for cached failures and distinguishes expired authentication', () => {
    const refetch = vi.fn()
    const failed = render(
      <ReadFeedback
        label="主机"
        query={{
          data: [],
          error: new ApiError(503, 'offline', { requestId: 'req-regional' }),
          isError: true,
          isFetching: false,
          refetch,
        }}
      />,
    )
    expect(failed.textContent).toContain('上次读取结果')
    expect(failed.textContent).toContain('req-regional')
    act(() => failed.querySelector<HTMLButtonElement>('button')!.click())
    expect(refetch).toHaveBeenCalledOnce()
    const auth = render(
      <ReadFeedback
        label="主机"
        query={{
          data: ['protected'],
          error: new ApiError(401, 'expired'),
          isError: true,
          isFetching: false,
          refetch,
        }}
      />,
    )
    expect(auth.textContent).toContain('登录状态失效')
    expect(auth.querySelector('button')).toBeNull()
  })
})
