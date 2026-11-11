/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { Form, type FormInstance } from 'antd'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, it, vi } from 'vitest'
import { BuildpacksFields } from './buildpacks-fields'
import { deliveryApi } from '../api'

vi.mock('../api', () => ({ deliveryApi: { applications: { buildpacksCapability: vi.fn() } } }))

it('pins available platform settings without changing a saved builder, and rejects invalid variables and credentials', async () => {
  ;(globalThis as Record<string, unknown>).IS_REACT_ACT_ENVIRONMENT = true
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
  window.matchMedia = vi
    .fn()
    .mockReturnValue({ matches: false, addListener: vi.fn(), removeListener: vi.fn() })
  const configuration = {
    builderImage: `builder@sha256:${'a'.repeat(64)}`,
    runImage: `run@sha256:${'b'.repeat(64)}`,
    platform: 'linux/arm64' as const,
  }
  vi.mocked(deliveryApi.applications.buildpacksCapability).mockResolvedValue({
    ready: true,
    reason: '',
    providerKind: 'buildpacks_runner',
    packVersion: '0.40.9',
    configuration,
  })
  let form!: FormInstance
  function Harness() {
    ;[form] = Form.useForm()
    return (
      <Form
        form={form}
        initialValues={{
          source: { config: { buildpacks: { builderImage: '', processType: 'web' } } },
        }}
      >
        <BuildpacksFields applicationId="app" form={form} prefix={['source']} />
      </Form>
    )
  }
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const path = ['source', 'config']
  vi.useFakeTimers()
  try {
    await act(async () => {
      root.render(
        <QueryClientProvider client={client}>
          <Harness />
        </QueryClientProvider>,
      )
    })
    await act(async () => {
      await vi.advanceTimersByTimeAsync(20)
    })
    expect(deliveryApi.applications.buildpacksCapability).toHaveBeenCalledWith('app')
    expect(form.getFieldValue([...path, 'buildpacks'])).toEqual({
      ...configuration,
      processType: 'web',
    })
    await act(async () => {
      form.setFieldValue([...path, 'buildpacks', 'builderImage'], 'saved-builder')
      form.setFieldValue([...path, 'variables'], { BP_JVM_VERSION: '21' })
      form.setFieldValue([...path, 'secretRefs', 'GIT_PASSWORD'], 'soha://secrets/git-password')
      await client.invalidateQueries()
    })
    expect(form.getFieldValue([...path, 'buildpacks', 'builderImage'])).toBe('saved-builder')
    await act(async () => {
      await expect(form.validateFields()).resolves.toBeDefined()
    })
    await act(async () => {
      form.setFieldValue([...path, 'variables'], { BP_JVM_VERSION: 21 })
      form.setFieldValue([...path, 'secretRefs', 'GIT_PASSWORD'], 'plaintext-token')
      await expect(form.validateFields()).rejects.toMatchObject({
        errorFields: expect.arrayContaining([
          expect.objectContaining({ name: [...path, 'variables'] }),
          expect.objectContaining({ name: [...path, 'secretRefs', 'GIT_PASSWORD'] }),
        ]),
      })
    })
    vi.mocked(deliveryApi.applications.buildpacksCapability).mockResolvedValue({
      ready: false,
      reason: 'runner offline',
      providerKind: 'buildpacks_runner',
      packVersion: '0.40.9',
    })
    await act(async () => {
      await client.invalidateQueries()
      await vi.advanceTimersByTimeAsync(20)
    })
    expect(container.textContent).toContain('runner offline')
    expect(form.getFieldValue([...path, 'buildpacks', 'builderImage'])).toBe('saved-builder')
    expect(
      [...container.querySelectorAll('button')].find(
        (button) => button.textContent === '使用当前平台配置',
      )?.disabled,
    ).toBe(true)
  } finally {
    try {
      await act(async () => root.unmount())
      client.clear()
      // Drain delayed form feedback before jsdom tears down its window.
      await act(async () => vi.runOnlyPendingTimersAsync())
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      vi.useRealTimers()
      container.remove()
      vi.unstubAllGlobals()
    }
  }
})
