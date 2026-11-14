import type { ReactElement } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AppErrorBoundary } from './app-error-boundary'

describe('AppErrorBoundary', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('logs structured metadata without exposing the raw render error', () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const boundary = new AppErrorBoundary({ children: null })
    const secret = 'token=super-secret'

    boundary.componentDidCatch(new Error(secret))
    boundary.state = { error: new Error(secret) }

    expect(consoleError).toHaveBeenCalledOnce()
    const line = String(consoleError.mock.calls[0]?.[0])
    expect(line).not.toContain(secret)
    expect(JSON.parse(line)).toMatchObject({
      level: 'error',
      component: 'react',
      service: 'soha-web',
      event: 'ui.render.failed',
      message: 'Unhandled React render error',
      error_type: 'Error',
    })

    const fallback = boundary.render() as ReactElement<{
      children: ReactElement<{ subTitle: string }>
    }>
    expect(fallback.props.children.props.subTitle).toBe('当前页面发生未处理错误，请重新加载。')
  })
})
