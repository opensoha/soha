/** @vitest-environment jsdom */
import { ApiError, emitApiError } from '@/services/api-error'
import { App, ConfigProvider } from 'antd'
import { act, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Link, MemoryRouter } from 'react-router-dom'
import { expect, it, vi } from 'vitest'
import { GlobalApiErrorHandler } from './global-api-error-handler'
import { OperationFeedback, ReadFeedback } from './request-feedback'

it('replaces a floating read failure with regional denial without a duplicate fallback', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
  }))
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  let showError: (error: ApiError) => void = () => {}
  function Read() {
    const [error, setError] = useState<ApiError | null>(null)
    showError = setError
    return (
      <ReadFeedback
        label="记录"
        query={{ data: [], error, isError: Boolean(error), isFetching: false }}
      />
    )
  }
  try {
    await act(async () =>
      root.render(
        <ConfigProvider theme={{ token: { motion: false } }}>
          <MemoryRouter>
            <App>
              <GlobalApiErrorHandler />
              <Read />
            </App>
          </MemoryRouter>
        </ConfigProvider>,
      ),
    )
    for (const status of [503, 403]) {
      const error = new ApiError(status, 'read unavailable', { path: '/records' })
      await act(async () => {
        emitApiError(error)
        showError(error)
      })
      await vi.waitFor(() =>
        expect(document.querySelectorAll('.soha-read-feedback-notification')).toHaveLength(
          status === 503 ? 1 : 0,
        ),
      )
    }
    expect(container.textContent).toContain('记录无访问权限')
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  }
})

it('removes retry when the consumer no longer permits the read', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const refetch = vi.fn()
  const query = {
    data: ['cached'],
    error: new ApiError(503, 'old failure', { method: 'GET' }),
    isError: true,
    isFetching: false,
    refetch,
  }
  try {
    const render = (canRead: boolean) =>
      act(async () =>
        root.render(
          <MemoryRouter>
            <App>
              <ReadFeedback canRead={canRead} label="记录" query={query} />
            </App>
          </MemoryRouter>,
        ),
      )
    await render(true)
    await vi.waitFor(() =>
      expect(document.body.querySelector('[aria-label="重试读取记录"]')).not.toBeNull(),
    )
    await render(false)
    await vi.waitFor(() =>
      expect(document.body.querySelector('[aria-label="重试读取记录"]')).toBeNull(),
    )
    expect(container.textContent).toBe('')
    expect(refetch).not.toHaveBeenCalled()
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  }
})

it('uses one floating owner and preserves task links without adding a page alert', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const error = new ApiError(503, '登录状态暂时无法恢复，请稍后重试。', {
    code: 'auth_refresh_unavailable',
    requestOutcome: 'rejected',
    method: 'POST',
    path: '/portal/applications/app/launch',
    requestId: 'req-launch',
  })
  const inspect = vi.fn()
  const render = (failed: boolean) =>
    act(async () =>
      root.render(
        <MemoryRouter>
          <App>
            <GlobalApiErrorHandler />
            <OperationFeedback
              label="打开应用"
              target="Console"
              error={failed ? error : null}
              description={failed ? '保留原应用与请求上下文' : undefined}
              actions={failed ? <button onClick={inspect}>核对原请求</button> : undefined}
              receipt={
                failed
                  ? undefined
                  : {
                      message: '任务已提交',
                      evidence: <Link to="/compute/tasks/operations?taskId=task-1">查看任务</Link>,
                    }
              }
            />
          </App>
        </MemoryRouter>,
      ),
    )
  try {
    await render(true)
    await act(async () => emitApiError(error))
    await vi.waitFor(() =>
      expect(document.querySelectorAll('.ant-notification-notice')).toHaveLength(1),
    )
    expect(container.querySelector('.ant-alert')).toBeNull()
    expect(document.body.textContent).toContain('req-launch')
    expect(document.body.textContent).not.toContain('请求结果不明')
    expect(document.body.textContent).toContain('保留原应用与请求上下文')
    await act(async () =>
      Array.from(document.querySelectorAll('button'))
        .find((button) => button.textContent === '核对原请求')!
        .click(),
    )
    expect(inspect).toHaveBeenCalledTimes(1)
    const details = document.querySelector<HTMLDetailsElement>('.soha-operation-feedback details')!
    expect(details.open).toBe(false)
    await act(async () => details.querySelector('summary')!.click())
    expect(details.open).toBe(true)
    await render(false)
    await vi.waitFor(() =>
      expect(document.body.querySelector('a[href*="taskId=task-1"]')).not.toBeNull(),
    )
    expect(document.querySelectorAll('.ant-notification-notice')).toHaveLength(1)
  } finally {
    await act(async () => root.unmount())
    container.remove()
    vi.unstubAllGlobals()
  }
})
