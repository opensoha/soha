/** @vitest-environment jsdom */

import { act, type ReactNode } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { PlatformResourceOverview } from './resource-overview'

vi.mock('@/i18n', () => ({
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback?: string) => fallback ?? _key,
  }),
}))

vi.mock('@/utils/time', () => ({
  formatAgeSeconds: (value?: number) => (value === undefined ? '-' : `age:${value}`),
  formatRelativeTime: (value?: string) => (value ? `created:${value}` : '-'),
}))

const roots: Root[] = []

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: vi.fn().mockImplementation(() => ({
      matches: false,
      media: '',
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  })
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
})

afterEach(async () => {
  await act(async () => {
    for (const root of roots.splice(0)) root.unmount()
  })
  document.body.innerHTML = ''
})

async function renderOverview(node: ReactNode) {
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  roots.push(root)
  await act(async () => root.render(node))
  return container
}

describe('PlatformResourceOverview', () => {
  it('renders common facts and non-empty metadata through one shared layout', async () => {
    const container = await renderOverview(
      <PlatformResourceOverview
        ageSeconds={60}
        annotations={{ owner: '' }}
        facts={[{ key: 'status', label: 'Status', value: 'Ready' }]}
        labels={{ app: 'demo', '': 'ignored' }}
        name="api"
        namespace="default"
      />,
    )

    expect(container.textContent).toContain('Nameapi')
    expect(container.textContent).toContain('Namespacedefault')
    expect(container.textContent).toContain('Created Atage:60')
    expect(container.textContent).toContain('StatusReady')
    expect(container.textContent).toContain('Labelsapp:demo')
    expect(container.textContent).toContain('注解owner:-')
    expect(container.textContent).not.toContain('ignored')
    expect(container.querySelectorAll('.soha-workload-kv-item[title]')).toHaveLength(0)
  })

  it('omits namespace when the resource is cluster scoped', async () => {
    const container = await renderOverview(
      <PlatformResourceOverview createdAt="2026-07-18T00:00:00Z" name="fast" />,
    )

    expect(container.textContent).not.toContain('Namespace')
    expect(container.textContent).toContain('created:2026-07-18T00:00:00Z')
    expect(container.querySelector('.soha-workload-metadata-stack')).toBeNull()
  })
})
