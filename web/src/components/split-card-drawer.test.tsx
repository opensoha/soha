/** @vitest-environment jsdom */

import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, useNavigate } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAuthStore } from '@/stores/auth-store'
import { SplitCardDrawerHost, useSplitCardDrawerStore } from './split-card-drawer'

vi.mock('@/i18n', () => ({
  useI18n: () => ({
    localeCode: 'zh_CN' as const,
    t: (_key: string, fallback = '') => fallback,
  }),
}))

function RouteHarness() {
  const navigate = useNavigate()
  return (
    <div>
      <button type="button" data-testid="go-clusters" onClick={() => navigate('/clusters')}>
        Go Clusters
      </button>
      <SplitCardDrawerHost />
    </div>
  )
}

describe('SplitCardDrawerHost', () => {
  let container: HTMLDivElement
  let root: Root

  beforeAll(() => {
    ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  })

  beforeEach(() => {
    useSplitCardDrawerStore.getState().resetSplitCard()
  })

  afterEach(async () => {
    if (root) {
      await act(async () => {
        root.unmount()
      })
    }
    container?.remove()
    document.body.innerHTML = ''
    useSplitCardDrawerStore.getState().resetSplitCard()
  })

  it('clears payload on closeSplitCard so cached JSX is not retained', async () => {
    const onClose = vi.fn()
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)

    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/']}>
          <RouteHarness />
        </MemoryRouter>,
      )
    })

    await act(async () => {
      useSplitCardDrawerStore.getState().openSplitCard({
        title: '明细面板',
        body: <div>敏感集群数据</div>,
        onClose,
      })
    })

    expect(container.textContent).toContain('敏感集群数据')
    const closeBtn = container.querySelector<HTMLButtonElement>('button[aria-label="关闭右侧卡片"]')
    expect(closeBtn).not.toBeNull()

    await act(async () => {
      closeBtn!.click()
    })

    expect(container.textContent).not.toContain('敏感集群数据')
    expect(useSplitCardDrawerStore.getState().open).toBe(false)
    expect(useSplitCardDrawerStore.getState().payload).toBeNull()
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes and purges payload automatically when navigating to another route', async () => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)

    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/']}>
          <RouteHarness />
        </MemoryRouter>,
      )
    })

    await act(async () => {
      useSplitCardDrawerStore.getState().openSplitCard({
        title: '明细面板',
        body: <div>仅当前页详情</div>,
      })
    })

    expect(container.textContent).toContain('仅当前页详情')
    const navBtn = container.querySelector<HTMLButtonElement>('button[data-testid="go-clusters"]')
    expect(navBtn).not.toBeNull()

    await act(async () => {
      navBtn!.click()
    })

    expect(container.textContent).not.toContain('仅当前页详情')
    expect(useSplitCardDrawerStore.getState().open).toBe(false)
    expect(useSplitCardDrawerStore.getState().payload).toBeNull()
  })

  it('never leaks drawer content across auth sessionGeneration transitions', async () => {
    container = document.createElement('div')
    document.body.appendChild(container)
    root = createRoot(container)

    await act(async () => {
      root.render(
        <MemoryRouter initialEntries={['/']}>
          <RouteHarness />
        </MemoryRouter>,
      )
    })

    await act(async () => {
      useSplitCardDrawerStore.getState().openSplitCard({
        title: '租户 A 明细',
        body: <div>User A secret pod list</div>,
      })
    })

    expect(container.textContent).toContain('User A secret pod list')

    await act(async () => {
      useAuthStore.setState((state) => ({ sessionGeneration: state.sessionGeneration + 1 }))
    })

    expect(container.textContent).not.toContain('User A secret pod list')
    expect(useSplitCardDrawerStore.getState().open).toBe(false)
    expect(useSplitCardDrawerStore.getState().payload).toBeNull()
  })
})
