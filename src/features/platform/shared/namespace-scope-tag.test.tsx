/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { NamespaceScopeTag } from './namespace-scope-tag'

vi.mock('@/i18n', () => ({ useI18n: () => ({ localeCode: 'zh_CN' }) }))

afterEach(() => usePlatformScopeStore.setState({ clusterId: null, namespace: null }))

async function render(namespace?: string | null) {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true })
  const container = document.createElement('div')
  document.body.appendChild(container)
  const root = createRoot(container)
  let location = { pathname: '', search: '' }
  function LocationObserver() {
    location = useLocation()
    return null
  }
  await act(async () =>
    root.render(
      <MemoryRouter
        initialEntries={['/workloads/deployments?clusterId=old&namespace=old&view=compact']}
      >
        <NamespaceScopeTag namespace={namespace} />
        <LocationObserver />
      </MemoryRouter>,
    ),
  )
  return {
    container,
    get location() {
      return location
    },
    async cleanup() {
      await act(async () => root.unmount())
      container.remove()
    },
  }
}

it('switches the shared namespace and URL while retaining cluster and other filters', async () => {
  usePlatformScopeStore.setState({ clusterId: 'cluster-a', namespace: null })
  const view = await render('monitoring')
  try {
    const button = view.container.querySelector('button')!
    expect(button.getAttribute('aria-label')).toBe('切换到命名空间 monitoring')
    expect(button.querySelector('.ant-tag')?.textContent).toBe('monitoring')
    await act(async () => button.click())
    expect(usePlatformScopeStore.getState()).toMatchObject({
      clusterId: 'cluster-a',
      namespace: 'monitoring',
    })
    const params = new URLSearchParams(view.location.search)
    expect(params.get('namespace')).toBe('monitoring')
    expect(params.get('clusterId')).toBe('cluster-a')
    expect(params.get('view')).toBe('compact')
    expect(view.location.pathname).toBe('/workloads/deployments')
  } finally {
    await view.cleanup()
  }
})

it('keeps a missing namespace as non-interactive text', async () => {
  const view = await render(null)
  try {
    expect(view.container.querySelector('button')).toBeNull()
    expect(view.container.textContent).toBe('—')
    expect(usePlatformScopeStore.getState().namespace).toBeNull()
  } finally {
    await view.cleanup()
  }
})
