import { ReadFeedback } from '@/components/request-feedback'
/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router-dom'
import { App, ConfigProvider } from 'antd'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { ComputeMutationFeedback, ComputeOperationFeedback } from './feedback'

const roots: ReturnType<typeof createRoot>[] = []
function render(node: ReactNode) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  act(() =>
    root.render(
      <ConfigProvider theme={{ token: { motion: false } }}>
        <MemoryRouter>
          <App>{node}</App>
        </MemoryRouter>
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
  it('keeps a submitting request separate from a task outcome and honors disabled motion', async () => {
    render(<ComputeOperationFeedback pending label="启动虚拟机" target="vm-1" />)
    await vi.waitFor(() =>
      expect(document.querySelector('.soha-operation-feedback')).not.toBeNull(),
    )
    const container = document.querySelector('.soha-operation-feedback')!
    expect(container.textContent).toContain('正在提交请求')
    expect(container.textContent).not.toContain('已完成')
    expect(container.querySelector('[data-motion-disabled="true"]')).not.toBeNull()
  })
  it('shares one failed save notice between the drawer and its active editor', async () => {
    const operations = [
      {
        label: '保存节点池',
        mutation: {
          isPending: false,
          isSuccess: false,
          error: new ApiError(409, 'revision conflict'),
          submittedAt: 123,
        },
      },
    ]
    render(
      <>
        <ComputeMutationFeedback operations={operations} />
        <ComputeMutationFeedback operations={operations} />
      </>,
    )
    await vi.waitFor(() =>
      expect(document.querySelectorAll('.soha-operation-feedback')).toHaveLength(1),
    )
    expect(document.body.textContent).toContain('revision conflict')
  })
  it('offers evidence only with task permission and preserves the exact task ID', async () => {
    render(
      <ComputeOperationFeedback
        label="启动"
        receipt={{ message: '任务已提交', taskId: 'task/id', domain: 'virtualization' }}
      />,
    )
    await vi.waitFor(() =>
      expect(document.querySelector('.soha-operation-feedback')).not.toBeNull(),
    )
    const denied = document.querySelector('.soha-operation-feedback')!
    expect(denied.textContent).toContain('任务进度不可见')
    expect(denied.querySelector('a')).toBeNull()
    render(
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
    await vi.waitFor(() =>
      expect(document.querySelector('.soha-operation-feedback a')?.getAttribute('href')).toContain(
        'taskId=task%2Fid',
      ),
    )
  })
  it('keeps every accepted task in a batch accessible', async () => {
    render(
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
    await vi.waitFor(() =>
      expect(document.querySelectorAll('.soha-operation-feedback a')).toHaveLength(2),
    )
    const links = [...document.querySelectorAll('.soha-operation-feedback a')].map((link) =>
      link.getAttribute('href'),
    )
    expect(links).toHaveLength(2)
    expect(links[0]).toContain('taskId=first')
    expect(links[1]).toContain('taskId=second')
  })
  it('distinguishes uncertain writes from failed preflight reads', async () => {
    const error = new ApiError(503, 'response lost')
    render(<ComputeOperationFeedback label="保存" error={error} />)
    await vi.waitFor(() =>
      expect(document.querySelector('.soha-operation-feedback')?.textContent).toContain(
        '请求结果不明',
      ),
    )
    render(<ComputeOperationFeedback label="预检" error={error} readOnly />)
    await vi.waitFor(() => expect(document.body.textContent).toContain('操作请求失败'))
  })
  it('provides floating retry for cached failures and distinguishes expired authentication', async () => {
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
    await vi.waitFor(() =>
      expect(document.body.querySelector('.soha-read-feedback-notification')).not.toBeNull(),
    )
    expect(failed.textContent).not.toContain('上次读取结果')
    expect(document.body.textContent).toContain('上次读取结果')
    expect(document.body.textContent).toContain('req-regional')
    await act(async () =>
      document.body.querySelector<HTMLButtonElement>('[aria-label="重试读取主机"]')!.click(),
    )
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
