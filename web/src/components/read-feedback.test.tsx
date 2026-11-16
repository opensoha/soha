/** @vitest-environment jsdom */
import { ApiError } from '@/services/api-error'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { ReadFeedback, useReadRefreshFeedback } from './request-feedback'

const notices = vi.hoisted(() => ({ warning: vi.fn(), destroy: vi.fn() }))
vi.mock('antd', async (importOriginal) => {
  const actual = await importOriginal<typeof import('antd')>()
  return { ...actual, App: { ...actual.App, useApp: () => ({ notification: notices }) } }
})

let container: HTMLDivElement
let root: ReturnType<typeof createRoot>
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.clearAllMocks()
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})
afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
  vi.unstubAllGlobals()
})

const error = new ApiError(503, 'upstream unavailable', { requestId: 'read-preview-id' })
const failed = { isError: true, isFetching: false, error }

it('keeps initial failure compact and diagnostic details collapsed without a floating notice', async () => {
  await act(async () => root.render(<ReadFeedback label="节点容量" query={failed} />))
  expect(container.querySelector('.ant-alert')).toBeNull()
  expect(container.querySelector('[role="alert"]')?.textContent).toContain('节点容量读取失败')
  const details = container.querySelector('details')!
  expect(details.open).toBe(false)
  expect(details.textContent).toContain('read-preview-id')
  await act(async () => details.querySelector('summary')!.click())
  expect(details.open).toBe(true)
  expect(notices.warning).not.toHaveBeenCalled()
})

it('marks readable empty cache stale but hides protected cache and retry on denial', async () => {
  await act(async () =>
    root.render(<ReadFeedback label="节点" query={{ ...failed, data: [], refetch: vi.fn() }} />),
  )
  expect(container.querySelector('[role="status"].is-stale')).not.toBeNull()
  expect(container.textContent).toContain('正在展示上次读取结果')
  await act(async () =>
    root.render(
      <ReadFeedback
        label="节点"
        query={{
          ...failed,
          data: ['protected'],
          refetch: vi.fn(),
          error: new ApiError(403, 'denied'),
        }}
      />,
    ),
  )
  expect(container.querySelector('.is-stale')).toBeNull()
  expect(container.querySelector('button')).toBeNull()
  expect(container.textContent).toContain('节点无访问权限')
  expect(notices.warning).not.toHaveBeenCalled()
})

it('coalesces explicit refresh failures, blocks duplicate clicks and clears the notice after recovery', async () => {
  let finish!: (value: unknown) => void
  const refetch = vi.fn(
    () =>
      new Promise((resolve) => {
        finish = resolve
      }),
  )
  let refresh!: ReturnType<typeof useReadRefreshFeedback>
  function Refresh() {
    refresh = useReadRefreshFeedback()
    return null
  }
  await act(async () => root.render(<Refresh />))
  let pending!: Promise<void>
  await act(async () => {
    pending = refresh([
      { label: '工作负载', refetch },
      {
        label: '节点容量',
        refetch: () =>
          Promise.resolve([
            { isError: true, error: new ApiError(403, 'denied') },
            { isError: true, error },
          ]),
      },
    ])
    await refresh([{ label: '重复刷新', refetch }])
  })
  expect(refetch).toHaveBeenCalledTimes(1)
  await act(async () => {
    finish({ isError: false })
    await pending
  })
  expect(notices.warning).toHaveBeenCalledTimes(1)
  expect(notices.warning).toHaveBeenCalledWith(
    expect.objectContaining({ title: '刷新未完成', duration: 0 }),
  )
  const key = notices.warning.mock.calls[0][0].key
  await act(async () => refresh([{ label: '节点容量', refetch: () => ({ isError: false }) }]))
  expect(notices.destroy).toHaveBeenCalledWith(key)
})

it('does not show a late notice after its region unmounts', async () => {
  let finish!: (value: unknown) => void
  const refetch = () =>
    new Promise((resolve) => {
      finish = resolve
    })
  let refresh!: ReturnType<typeof useReadRefreshFeedback>
  function Refresh() {
    refresh = useReadRefreshFeedback()
    return null
  }
  await act(async () => root.render(<Refresh />))
  const pending = refresh([{ label: '节点', refetch }])
  await act(async () => root.render(null))
  await act(async () => {
    finish({ isError: true, error })
    await pending
  })
  expect(notices.warning).not.toHaveBeenCalled()
})
