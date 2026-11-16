/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { MutationFeedback, OperationFeedback } from './request-feedback'
import { useOperationMutation } from './use-operation-mutation'

const notices = vi.hoisted(() => ({ open: vi.fn(), destroy: vi.fn() }))
vi.mock('antd', async () => {
  const actual = await vi.importActual<typeof import('antd')>('antd')
  return { ...actual, App: { ...actual.App, useApp: () => ({ notification: notices }) } }
})

let root: ReturnType<typeof createRoot>
let container: HTMLDivElement
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})
const tick = async (ms = 0) => act(async () => vi.advanceTimersByTimeAsync(ms))

it('delays short submissions and updates one floating notice without occupying the page', async () => {
  await act(async () => root.render(<OperationFeedback pending label="保存" target="配置" />))
  await tick(399)
  expect(notices.open).not.toHaveBeenCalled()
  await tick(1)
  expect(container.textContent).toBe('')
  const key = notices.open.mock.calls[0][0].key
  expect(notices.open).toHaveBeenLastCalledWith(expect.objectContaining({ key, duration: 0 }))
  await act(async () =>
    root.render(
      <OperationFeedback label="保存" target="配置" receipt={{ message: '配置已保存' }} />,
    ),
  )
  await tick()
  expect(notices.open).toHaveBeenLastCalledWith(
    expect.objectContaining({
      key,
      type: 'success',
      title: '配置已保存',
      duration: 3,
    }),
  )
})

it('does not extend success duration or resurrect a closed notice on parent updates', async () => {
  const render = () =>
    act(async () =>
      root.render(
        <OperationFeedback
          label="提交"
          receipt={{ message: '任务已提交', evidence: <a href="/tasks">查看任务</a> }}
        />,
      ),
    )
  await render()
  await tick()
  expect(notices.open.mock.lastCall?.[0].duration).toBe(8)
  await tick(2_000)
  await render()
  await tick()
  expect(notices.open.mock.lastCall?.[0].duration).toBe(6)
  notices.open.mock.lastCall?.[0].onClose()
  const opened = notices.open.mock.calls.length
  await render()
  await tick()
  expect(notices.open).toHaveBeenCalledTimes(opened)
})

it('cleans up a notice only after its last presentation owner leaves', async () => {
  const feedback = (
    <OperationFeedback
      notificationKey="shared-action"
      label="保存"
      receipt={{ message: '已保存' }}
    />
  )
  await act(async () =>
    root.render(
      <>
        {feedback}
        {feedback}
      </>,
    ),
  )
  await tick()
  notices.destroy.mockClear()
  await act(async () => root.render(<>{feedback}</>))
  expect(notices.destroy).not.toHaveBeenCalledWith('shared-action')
  await act(async () => root.render(null))
  expect(notices.destroy).toHaveBeenCalledWith('shared-action')
})

it('keeps duplicate mutation presentations closed when another owner rerenders', async () => {
  const mutation = { isPending: false, isSuccess: true, error: null, submittedAt: 42 }
  const render = () => {
    const feedback = (
      <MutationFeedback
        operations={[
          {
            mutation,
            label: '目录同步',
            target: 'directory-1',
            receipt: { message: '已返回同步运行记录', evidence: <a href="/runs">查看运行记录</a> },
          },
        ]}
      />
    )
    return act(async () =>
      root.render(
        <>
          {feedback}
          {feedback}
        </>,
      ),
    )
  }
  await render()
  await tick()
  expect(new Set(notices.open.mock.calls.map(([notice]) => notice.key)).size).toBe(1)
  notices.open.mock.lastCall?.[0].onClose()
  const count = notices.open.mock.calls.length
  await render()
  await tick()
  expect(notices.open).toHaveBeenCalledTimes(count)
})

it.each([true, false])(
  'closing feedback preserves the correct submission fence (rejected=%s)',
  async (rejected) => {
    vi.useRealTimers()
    const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
    const error = new ApiError(rejected ? 503 : 0, 'request unavailable', {
      method: 'POST',
      requestOutcome: rejected ? 'rejected' : undefined,
      code: rejected ? 'auth_refresh_unavailable' : undefined,
    })
    const submit = vi.fn(async (_input: { id: string }) => {
      throw error
    })
    function useProbe() {
      return useOperationMutation(
        { mutationFn: submit },
        { label: ['打开应用', 'Open app'], target: (v) => v.id },
      )
    }
    let operation: ReturnType<typeof useProbe>
    function Probe() {
      operation = useProbe()
      return operation.feedback
    }
    try {
      await act(async () =>
        root.render(
          <QueryClientProvider client={client}>
            <Probe />
          </QueryClientProvider>,
        ),
      )
      const original = { id: 'app' }
      await act(async () => {
        await operation.mutateAsync(original).catch(() => undefined)
      })
      await vi.waitFor(() => expect(notices.open).toHaveBeenCalled())
      expect(notices.open.mock.lastCall?.[0].duration).toBe(0)
      await act(async () => {
        notices.open.mock.lastCall?.[0].onClose()
      })
      expect(operation!.blocked).toBe(!rejected)
      await act(async () => operation.mutate({ id: 'retry' }))
      await vi.waitFor(() => expect(submit).toHaveBeenCalledTimes(rejected ? 2 : 1))
      if (!rejected) {
        await act(async () => operation.reset())
        expect(operation!.blocked).toBe(true)
        await act(async () => operation.resolveUnknown(original))
        await vi.waitFor(() => expect(operation!.blocked).toBe(false))
      }
    } finally {
      client.clear()
    }
  },
)
