/** @vitest-environment jsdom */

import type { ReactNode } from 'react'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { ResourceStreamStatus } from './resource-stream-status'

const captured = vi.hoisted(() => ({
  badgeStatus: '',
  tooltipTitle: '',
}))

vi.mock('antd', async (importOriginal) => ({
  ...(await importOriginal<typeof import('antd')>()),
  Badge: ({ status, text }: { status: string; text?: ReactNode }) => {
    captured.badgeStatus = status
    return <span>{text}</span>
  },
  Tooltip: ({ children, title }: { children: ReactNode; title: string }) => {
    captured.tooltipTitle = title
    return children
  },
}))

describe('ResourceStreamStatus', () => {
  beforeAll(() => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  })

  afterEach(() => {
    captured.badgeStatus = ''
    captured.tooltipTitle = ''
    document.body.replaceChildren()
  })

  it('shows a compact accessible dot and keeps details in the tooltip', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(
        <ResourceStreamStatus
          status="live"
          lastEventAt="2026-08-23T08:00:00Z"
          localeCode="zh_CN"
        />,
      )
    })

    const indicator = container.querySelector<HTMLElement>('[role="status"]')
    expect(container.textContent).toBe('')
    expect(indicator?.getAttribute('aria-label')).toContain('实时')
    expect(indicator?.getAttribute('aria-label')).toContain('更新于')
    expect(captured.tooltipTitle).toBe(indicator?.getAttribute('aria-label'))
    expect(captured.badgeStatus).toBe('success')

    await act(async () => root.unmount())
  })

  it('uses an error dot for degraded polling', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)

    await act(async () => {
      root.render(<ResourceStreamStatus status="polling" localeCode="en_US" />)
    })

    expect(captured.badgeStatus).toBe('error')
    expect(container.querySelector('[role="status"]')?.getAttribute('aria-label')).toBe(
      'Polling Fallback',
    )

    await act(async () => root.unmount())
  })
  it('shows an explicit live label only when requested and keeps fallback status truthful', async () => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    await act(async () =>
      root.render(<ResourceStreamStatus showLabel status="live" localeCode="zh_CN" />),
    )
    expect(container.textContent).toBe('实时更新')
    await act(async () =>
      root.render(<ResourceStreamStatus showLabel status="degraded" localeCode="zh_CN" />),
    )
    expect(container.textContent).not.toContain('实时更新')
    expect(captured.badgeStatus).toBe('error')
    await act(async () => root.unmount())
  })

  it.each([
    [true, true, 'warning', 'Refreshing'],
    [false, true, 'error', 'Refresh failed; retry'],
  ] as const)(
    'prioritizes actual read feedback while retaining the stream state',
    async (refreshing, refreshFailed, tone, label) => {
      const container = document.createElement('div')
      const root = createRoot(container)
      await act(async () =>
        root.render(
          <ResourceStreamStatus
            status="live"
            localeCode="en_US"
            refreshing={refreshing}
            refreshFailed={refreshFailed}
          />,
        ),
      )
      expect(captured.badgeStatus).toBe(tone)
      expect(captured.tooltipTitle).toContain(label)
      expect(captured.tooltipTitle).toContain('Live')
      expect(container.textContent).toBe('')
      await act(async () => root.unmount())
    },
  )
})
