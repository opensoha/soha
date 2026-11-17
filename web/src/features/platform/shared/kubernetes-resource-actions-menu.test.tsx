/** @vitest-environment jsdom */
import { act, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { expect, it, vi } from 'vitest'
import { KubernetesResourceActionsMenu } from './kubernetes-resource-actions-menu'

const popup = vi.hoisted(() => ({ renders: 0, open: false }))
vi.mock('antd', async (original) => ({
  ...(await original<typeof import('antd')>()),
  Dropdown: ({ children, open }: { children: ReactNode; open: boolean }) => {
    popup.renders += 1
    popup.open = open
    return <>{children}</>
  },
}))

it('constructs no dropdown for untouched rows and opens the first requested menu', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const host = document.createElement('div')
  document.body.append(host)
  const root = createRoot(host)
  const onSelect = vi.fn()
  try {
    await act(async () =>
      root.render(
        <>
          {Array.from({ length: 20 }, (_, i) => (
            <KubernetesResourceActionsMenu
              key={i}
              name={`pod-${i}`}
              resourceLabel="Pod"
              localeCode="zh_CN"
              items={[{ key: 'logs', label: '日志' }]}
              onSelect={onSelect}
            />
          ))}
        </>,
      ),
    )
    expect(popup.renders).toBe(0)
    const trigger = host.querySelector<HTMLButtonElement>('[aria-label="更多操作：pod-0"]')!
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
    await act(async () => trigger.click())
    expect(popup.renders).toBe(1)
    expect(popup.open).toBe(true)
    expect(onSelect).not.toHaveBeenCalled()
    expect(
      host.querySelector('[aria-label="更多操作：pod-0"]')?.getAttribute('aria-expanded'),
    ).toBe('true')
  } finally {
    await act(async () => root.unmount())
    host.remove()
    vi.unstubAllGlobals()
  }
})
