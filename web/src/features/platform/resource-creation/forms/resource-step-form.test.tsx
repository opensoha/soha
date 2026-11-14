// @vitest-environment jsdom

import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { podQueries } from '@/features/platform/workloads/pods/queries'
import { toScopeKey } from '@/types'
import { getResourceFormDefinition } from './definitions'
import type { ServiceFormValues } from './types'

async function flushAsyncWork() {
  await act(async () => {
    await Promise.resolve()
    await vi.advanceTimersByTimeAsync(0)
  })
}

async function selectOption(select: Element | undefined, optionLabel: string) {
  expect(select).toBeDefined()
  await act(async () => {
    select
      ?.querySelector('.ant-select-content')
      ?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
  })
  await flushAsyncWork()
  const option = Array.from(document.body.querySelectorAll('.ant-select-item-option')).find(
    (item) => item.textContent === optionLabel,
  )
  expect(option).toBeDefined()
  await act(async () => {
    option?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }))
    option?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  })
  await flushAsyncWork()
}

describe('resource step form', () => {
  let container: HTMLDivElement | undefined
  let root: ReturnType<typeof createRoot> | undefined

  beforeAll(() => {
    class ResizeObserverMock {
      observe() {}
      unobserve() {}
      disconnect() {}
    }

    ;(
      globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }
    ).IS_REACT_ACT_ENVIRONMENT = true
    vi.stubGlobal('ResizeObserver', ResizeObserverMock)
    window.matchMedia = vi.fn().mockImplementation((query: string) => ({
      addEventListener: vi.fn(),
      addListener: vi.fn(),
      dispatchEvent: vi.fn(),
      matches: false,
      media: query,
      onchange: null,
      removeEventListener: vi.fn(),
      removeListener: vi.fn(),
    }))
  })

  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(async () => {
    try {
      await act(async () => {
        root?.unmount()
        await vi.runOnlyPendingTimersAsync()
      })
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      vi.useRealTimers()
      document.body.innerHTML = ''
      root = undefined
      container = undefined
    }
  })

  it('renders shared steps and navigation for a workload definition', async () => {
    const definition = getResourceFormDefinition('Deployment')
    const value = definition?.defaultValues({ namespace: 'minio' })
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)

    await act(async () => {
      root?.render(
        definition?.renderForm({
          namespaceOptions: ['minio', 'platform'],
          value,
          onChange: vi.fn(),
          onSubmit: vi.fn(),
        }),
      )
    })
    await flushAsyncWork()

    expect(container.textContent).toContain('基本信息')
    expect(container.textContent).toContain('工作负载')
    expect(container.textContent).toContain('Pod 模板')
    expect(container.textContent).toContain('下一步')
    expect(container.querySelector('#namespace')?.getAttribute('role')).toBe('combobox')
    expect(container.querySelector('.ant-select-content')?.getAttribute('title')).toBe('minio')
    expect(container.textContent).not.toContain('切换到 YAML')
  })

  it('adapts Service fields and fills editable selectors from a Pod', async () => {
    const definition = getResourceFormDefinition('Service')
    expect(definition).toBeDefined()
    const value = {
      ...(definition?.defaultValues({ namespace: 'team-a' }) as ServiceFormValues),
      name: 'api-service',
      type: 'NodePort' as const,
    }
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    })
    const onChange = vi.fn()
    queryClient.setQueryData(podQueries.list(toScopeKey('cluster-a', 'team-a')).queryKey, [
      {
        ageSeconds: 60,
        labels: { tier: 'backend', app: 'api' },
        name: 'api-0',
        namespace: 'team-a',
        phase: 'Running',
        readyContainers: '1/1',
        restarts: 0,
      },
    ])
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)

    await act(async () => {
      root?.render(
        <QueryClientProvider client={queryClient}>
          {definition?.renderForm({
            clusterId: 'cluster-a',
            namespaceOptions: ['team-a'],
            value,
            onChange,
            onSubmit: vi.fn(),
          })}
        </QueryClientProvider>,
      )
    })
    await flushAsyncWork()
    const nextButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === '下一步',
    )
    await act(async () => nextButton?.click())
    await flushAsyncWork()

    expect(container.querySelector('[aria-label="NodePort"]')).not.toBeNull()
    expect(container.textContent).not.toContain('外部 DNS 名称')
    const selects = Array.from(container.querySelectorAll('.ant-select'))
    const podSelect = selects.find((select) =>
      select.textContent?.includes('选择 Pod 自动填入标签'),
    )
    await selectOption(podSelect, 'api-0 · 2 个标签')

    expect(
      Array.from(container.querySelectorAll<HTMLInputElement>('[aria-label="Pod 选择器 key"]')).map(
        (input) => input.value,
      ),
    ).toEqual(['app', 'tier'])
    expect(
      Array.from(
        container.querySelectorAll<HTMLInputElement>('[aria-label="Pod 选择器 value"]'),
      ).map((input) => input.value),
    ).toEqual(['api', 'backend'])

    const typeSelect = selects.find(
      (select) => select.querySelector('.ant-select-content')?.getAttribute('title') === 'NodePort',
    )
    await selectOption(typeSelect, 'ExternalName')

    expect(container.textContent).toContain('外部 DNS 名称')
    expect(container.textContent).not.toContain('从 Pod 填充选择器')
    expect(container.textContent).not.toContain('添加端口')
    const externalNameInput = container.querySelector<HTMLInputElement>(
      'input[placeholder="database.example.com"]',
    )
    await act(async () => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set?.call(
        externalNameInput,
        'database.example.com',
      )
      externalNameInput?.dispatchEvent(new Event('input', { bubbles: true }))
    })
    await flushAsyncWork()

    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({
        externalName: 'database.example.com',
        ports: value.ports,
        type: 'ExternalName',
      }),
    )
  })
})
