/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { describe, expect, it, vi } from 'vitest'
import { ResourceEventsTimeline } from './resource-events-timeline'

vi.mock('@/i18n', async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useI18n: () => ({ localeCode: 'zh_CN' }),
}))

describe('ResourceEventsTimeline', () => {
  it('retains event evidence, count, scope and time while distinguishing warnings', async () => {
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
    vi.stubGlobal(
      'matchMedia',
      vi.fn(() => ({
        matches: false,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
      })),
    )
    const container = document.createElement('div')
    document.body.appendChild(container)
    const root = createRoot(container)
    try {
      await act(async () =>
        root.render(
          <ResourceEventsTimeline
            events={[
              {
                name: 'warning',
                namespace: 'monitoring',
                type: 'Warning',
                reason: 'Unhealthy',
                message: 'Readiness probe failed',
                count: 3,
                ageSeconds: 90,
                involvedKind: 'Pod',
                involvedName: 'api-1',
              },
              {
                name: 'normal',
                type: 'Normal',
                reason: 'Started',
                message: 'Container started',
                count: 1,
                ageSeconds: 60,
              },
            ]}
          />,
        ),
      )
      const warning = container.querySelector('.soha-events-timeline-item.is-warning')
      expect(warning?.textContent).toContain('Unhealthy')
      expect(warning?.textContent).toContain('Readiness probe failed')
      expect(warning?.textContent).toContain('3 次')
      expect(warning?.textContent).toContain('Pod / api-1')
      expect(warning?.textContent).toContain('monitoring')
      expect(container.querySelectorAll('time[datetime]')).toHaveLength(2)
      expect(
        container.querySelector('.soha-events-timeline-item.is-ongoing')?.textContent,
      ).toContain('Started')
      await act(async () =>
        root.render(<ResourceEventsTimeline events={[]} emptyDescription="No recent events" />),
      )
      expect(container.textContent).toContain('No recent events')
      expect(container.querySelector('.soha-events-timeline-shell')).toBeNull()
    } finally {
      await act(async () => root.unmount())
      container.remove()
      vi.unstubAllGlobals()
    }
  })
})
