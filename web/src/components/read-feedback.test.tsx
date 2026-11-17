/** @vitest-environment jsdom */
import { ApiError, isApiErrorPresented } from '@/services/api-error'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ReadFeedback, useReadRefreshFeedback } from './request-feedback'
import { QueryClient, QueryClientProvider, QueryObserver } from '@tanstack/react-query'

const notices = vi.hoisted(() => ({ open: vi.fn(), destroy: vi.fn() }))
vi.mock('antd', async (importOriginal) => {
  const actual = await importOriginal<typeof import('antd')>()
  return { ...actual, App: { ...actual.App, useApp: () => ({ notification: notices }) } }
})
let root: ReturnType<typeof createRoot>
let noticeRoot: ReturnType<typeof createRoot>
let container: HTMLDivElement
let floating: HTMLDivElement
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  vi.clearAllMocks()
  container = document.createElement('div')
  floating = document.createElement('div')
  document.body.append(container, floating)
  root = createRoot(container)
  noticeRoot = createRoot(floating)
  notices.open.mockImplementation((options) =>
    noticeRoot.render(
      <div className={options.className}>
        <div>{options.title}</div>
        {options.description}
        <button
          onClick={() => {
            options.onClose()
            noticeRoot.render(null)
          }}
        >
          关闭提示
        </button>
      </div>,
    ),
  )
  notices.destroy.mockImplementation(() => noticeRoot.render(null))
})
afterEach(async () => {
  await act(async () => root.unmount())
  await act(async () => noticeRoot.unmount())
  container.remove()
  floating.remove()
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
const failure = (status = 503, path = '/records', message = 'upstream unavailable') =>
  new ApiError(status, message, { path, method: 'GET', requestId: 'read-preview-id' })
const error = failure()
const failed = { isError: true, isFetching: false, error }
const tick = (time: number) =>
  act(async () => {
    await vi.advanceTimersByTimeAsync(time)
  })
const click = (name: string) =>
  act(async () => {
    Array.from(floating.querySelectorAll('button'))
      .find((button) => button.textContent === name)!
      .click()
  })

it('floats read failures with collapsed details and leaves no page or composer footprint', async () => {
  await act(async () =>
    root.render(<ReadFeedback label="运行配置" query={{ ...failed, refetch: vi.fn() }} />),
  )
  expect(container.innerHTML).toBe('')
  expect(floating.textContent).toContain('运行配置读取失败')
  expect(floating.textContent).toContain('15 秒后自动重试')
  const details = floating.querySelector('details')!
  expect(details.open).toBe(false)
  expect(details.textContent).toContain('read-preview-id')
  await act(async () => details.querySelector('summary')!.click())
  expect(details.open).toBe(true)
})

it('retries at 15/30/60 seconds and then pauses rather than polling forever', async () => {
  const refetch = vi.fn(async () => ({ isError: true, error }))
  await act(async () => root.render(<ReadFeedback label="记录" query={{ ...failed, refetch }} />))
  await tick(14_999)
  expect(refetch).not.toHaveBeenCalled()
  await tick(1)
  expect(refetch).toHaveBeenCalledTimes(1)
  expect(floating.textContent).toContain('30 秒后自动重试')
  await tick(30_000)
  expect(refetch).toHaveBeenCalledTimes(2)
  await tick(60_000)
  expect(refetch).toHaveBeenCalledTimes(3)
  await tick(120_000)
  expect(refetch).toHaveBeenCalledTimes(3)
  expect(floating.textContent).toContain('自动重试已暂停')
})

it('keeps its deadline while parents rerender faster than the countdown tick', async () => {
  const refetch = vi.fn(async () => ({ isError: true, error }))
  const render = () =>
    act(async () => root.render(<ReadFeedback label="记录" query={{ ...failed, refetch }} />))
  await render()
  for (let index = 0; index < 30; index++) {
    await tick(500)
    await render()
  }
  expect(refetch).toHaveBeenCalledTimes(1)
})

it('retries immediately only once, and closing while pending stops the retry chain', async () => {
  let finish!: (result: unknown) => void
  const refetch = vi.fn(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  await act(async () => root.render(<ReadFeedback label="记录" query={{ ...failed, refetch }} />))
  await click('立即重试')
  await click('立即重试')
  expect(refetch).toHaveBeenCalledTimes(1)
  await click('关闭提示')
  await act(async () => {
    finish({ isError: true, error })
    await Promise.resolve()
  })
  await tick(120_000)
  expect(refetch).toHaveBeenCalledTimes(1)
  expect(floating.innerHTML).toBe('')
})

it('does not reopen a dismissed episode for new background status/messages or request IDs', async () => {
  const refetch = vi.fn(async () => ({ isError: true, error }))
  const render = (readError: Error) =>
    act(async () =>
      root.render(<ReadFeedback label="记录" query={{ ...failed, error: readError, refetch }} />),
    )
  await render(error)
  await click('关闭提示')
  const opened = notices.open.mock.calls.length
  const nextError = failure(502, '/records', 'another upstream error')
  await render(nextError)
  expect(notices.open).toHaveBeenCalledTimes(opened)
  expect(isApiErrorPresented(nextError)).toBe(true)
  expect(
    isApiErrorPresented(new ApiError(401, 'expired', { method: 'GET', path: '/records' })),
  ).toBe(false)
  expect(
    isApiErrorPresented(new ApiError(503, 'write failed', { method: 'POST', path: '/records' })),
  ).toBe(false)
  await tick(120_000)
  expect(refetch).not.toHaveBeenCalled()
  await act(async () => root.render(null))
  expect(isApiErrorPresented(nextError)).toBe(false)
})

it.each([401, 403])(
  'keeps %s in a compact region and stops any scheduled retry',
  async (status) => {
    const refetch = vi.fn()
    await act(async () =>
      root.render(
        <ReadFeedback label="记录" query={{ ...failed, data: ['protected'], refetch }} />,
      ),
    )
    await act(async () =>
      root.render(
        <ReadFeedback
          label="记录"
          query={{ ...failed, data: ['protected'], refetch, error: failure(status) }}
        />,
      ),
    )
    expect(container.querySelector('[role="alert"].soha-read-feedback')).not.toBeNull()
    expect(container.textContent).toContain(status === 401 ? '登录状态失效' : '无访问权限')
    expect(container.querySelector('button')).toBeNull()
    expect(floating.innerHTML).toBe('')
    await tick(120_000)
    expect(refetch).not.toHaveBeenCalled()
  },
)

it('shares one notice and one retry across duplicate query presenters; withdrawal cancels it', async () => {
  const refetch = vi.fn(async () => ({ isError: true, error }))
  const render = (allowed: boolean) =>
    act(async () =>
      root.render(
        <>
          <ReadFeedback label="记录" query={{ ...failed, refetch }} canRead={allowed} />
          <ReadFeedback
            label="记录副本"
            query={{ ...failed, error: failure(), refetch }}
            canRead={allowed}
          />
        </>,
      ),
    )
  await render(true)
  expect(floating.querySelectorAll('.soha-read-feedback-notification')).toHaveLength(1)
  await click('立即重试')
  expect(refetch).toHaveBeenCalledTimes(1)
  await render(false)
  await tick(120_000)
  expect(refetch).toHaveBeenCalledTimes(1)
  expect(floating.innerHTML).toBe('')
})

it('pauses automatic retries in the background and removes the notice after recovery', async () => {
  const refetch = vi.fn(async () => ({ isError: false }))
  await act(async () => root.render(<ReadFeedback label="记录" query={{ ...failed, refetch }} />))
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
  await act(async () => document.dispatchEvent(new Event('visibilitychange')))
  await tick(20_000)
  expect(refetch).not.toHaveBeenCalled()
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
  await act(async () => document.dispatchEvent(new Event('visibilitychange')))
  await tick(1_000)
  expect(refetch).toHaveBeenCalledTimes(1)
  expect(floating.innerHTML).toBe('')
  await act(async () => root.render(<ReadFeedback label="记录" query={{ ...failed, refetch }} />))
  expect(floating.innerHTML).toBe('')
})

it('does not automatically replay known POST operations, disabled queries or unmounted reads', async () => {
  const refetch = vi.fn()
  await act(async () =>
    root.render(
      <ReadFeedback
        label="预检"
        query={{
          ...failed,
          refetch,
          error: new ApiError(503, 'failed', { method: 'POST', path: '/preview' }),
        }}
      />,
    ),
  )
  await tick(120_000)
  expect(refetch).not.toHaveBeenCalled()
  await act(async () =>
    root.render(<ReadFeedback label="记录" query={{ ...failed, refetch, isEnabled: false }} />),
  )
  expect(floating.innerHTML).toBe('')
  await act(async () => root.render(<ReadFeedback label="记录" query={{ ...failed, refetch }} />))
  await act(async () => root.render(null))
  await tick(120_000)
  expect(refetch).not.toHaveBeenCalled()
})

it('merges explicit refresh errors, retains a 503 after a 403 and deduplicates retry callbacks', async () => {
  const refetch = vi.fn(
    async (): Promise<unknown> => [
      { isError: true, error: failure(403, '/denied') },
      { isError: true, error },
      { isError: true, error: failure(503, '/other-records') },
    ],
  )
  let refresh!: ReturnType<typeof useReadRefreshFeedback>
  function Refresh() {
    refresh = useReadRefreshFeedback()
    return null
  }
  await act(async () => root.render(<Refresh />))
  await act(async () => refresh([{ label: '记录', refetch }]))
  expect(floating.querySelectorAll('.soha-read-feedback-notification')).toHaveLength(1)
  expect(floating.textContent).toContain('read-preview-id')
  expect(floating.textContent).not.toContain('/denied')
  await click('立即重试')
  expect(refetch).toHaveBeenCalledTimes(2)
  refetch.mockResolvedValue([{ isError: false }])
  await click('立即重试')
  expect(floating.innerHTML).toBe('')
})

it('drops a late explicit failure after its Query scope loses active observers', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  let reject!: (error: Error) => void
  const request = new Promise<unknown>((_resolve, rejectRequest) => {
    reject = rejectRequest
  })
  const options = { queryKey: ['scope-a'], queryFn: () => request, retry: false }
  const observer = new QueryObserver(client, options)
  const unsubscribe = observer.subscribe(() => {})
  let refresh!: ReturnType<typeof useReadRefreshFeedback>
  function Refresh() {
    refresh = useReadRefreshFeedback()
    return null
  }
  try {
    await act(async () =>
      root.render(
        <QueryClientProvider client={client}>
          <Refresh />
        </QueryClientProvider>,
      ),
    )
    let finishOther!: (result: unknown) => void
    const other = new Promise((resolve) => {
      finishOther = resolve
    })
    const pending = refresh([
      { label: 'scope-a', refetch: () => client.fetchQuery(options) },
      { label: 'another-read', refetch: () => other },
    ])
    observer.setOptions({ queryKey: ['scope-b'], queryFn: async () => [], retry: false })
    const lateError = failure(503, '/scope-a')
    await act(async () => {
      reject(lateError)
      await vi.waitFor(() => expect(client.getQueryState(['scope-a'])?.error).toBe(lateError))
    })
    client.removeQueries({ queryKey: ['scope-a'] })
    await act(async () => {
      finishOther({ isError: false })
      await pending
    })
    expect(floating.innerHTML).toBe('')
  } finally {
    unsubscribe()
    client.clear()
  }
})
