/** @vitest-environment jsdom */

import { act, useMemo } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { expect, it, vi } from 'vitest'
import { ApiError } from '@/services/api-error'
import { useOperationMutation } from './use-operation-mutation'

it('resolves uncertainty only for the exact outstanding submission', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  const root = createRoot(document.createElement('div'))
  const submit = vi.fn(async (_input: { requestId: string }) => {
    throw new ApiError(0, 'response lost', { method: 'POST' })
  })
  function useProbe() {
    return useOperationMutation(
      { mutationFn: submit },
      { label: ['请求', 'Request'], target: (input) => input.requestId },
    )
  }
  let operation: ReturnType<typeof useProbe>
  function Probe() {
    operation = useProbe()
    return null
  }
  const waitForState = (assertion: () => void) =>
    vi.waitFor(async () => {
      await act(async () => new Promise((resolve) => setTimeout(resolve, 0)))
      assertion()
    })
  try {
    await act(async () =>
      root.render(
        <QueryClientProvider client={client}>
          <Probe />
        </QueryClientProvider>,
      ),
    )
    const original = { requestId: 'a' }
    await act(async () => operation.mutate(original))
    await waitForState(() => expect(operation.outcomeUnknown).toBe(true))
    const previousResolver = operation!.resolveUnknown
    await act(async () => {
      operation.reset()
      operation.resolveUnknown({ ...original })
      operation.mutate({ requestId: 'blocked' })
    })
    expect(submit).toHaveBeenCalledTimes(1)
    await act(async () => operation.resolveUnknown(original))
    await waitForState(() => expect(operation.blocked).toBe(false))
    const next = { requestId: 'b' }
    await act(async () => operation.mutate(next))
    await waitForState(() => expect(operation.outcomeUnknown).toBe(true))
    await act(async () => previousResolver(original))
    expect(operation!.blocked).toBe(true)
    await act(async () => operation.resolveUnknown(next))
    await waitForState(() => expect(operation.blocked).toBe(false))
  } finally {
    await act(async () => root.unmount())
    client.clear()
    vi.unstubAllGlobals()
  }
})

it('keeps reset and dependent columns stable across refresh renders without bypassing the pending fence', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  const root = createRoot(document.createElement('div'))
  let complete!: () => void
  const submit = vi.fn(
    () =>
      new Promise<void>((resolve) => {
        complete = resolve
      }),
  )
  let operation!: ReturnType<typeof useProbe>
  let builds = 0
  let targetName = 'pod'
  function useProbe() {
    const submittedName = targetName
    return useOperationMutation(
      { mutationFn: submit },
      { label: ['保存', 'Save'], target: () => submittedName },
    )
  }
  function Probe({ refresh }: { refresh: number }) {
    operation = useProbe()
    useMemo(() => {
      builds += 1
      return [{ reset: operation.reset }]
    }, [operation.reset])
    return <span>{refresh}</span>
  }
  const render = (refresh: number) =>
    act(async () =>
      root.render(
        <QueryClientProvider client={client}>
          <Probe refresh={refresh} />
        </QueryClientProvider>,
      ),
    )
  const settle = () => act(async () => new Promise((resolve) => setTimeout(resolve, 0)))
  try {
    await render(0)
    const reset = operation.reset
    const mutate = operation.mutate
    const mutateAsync = operation.mutateAsync
    const feedback = operation.feedback
    targetName = 'current pod'
    await render(1)
    await render(2)
    expect(operation.reset).toBe(reset)
    expect(operation.mutate).toBe(mutate)
    expect(operation.mutateAsync).toBe(mutateAsync)
    expect(operation.feedback).toBe(feedback)
    expect(builds).toBe(1)
    await act(async () => mutate())
    await vi.waitFor(async () => {
      await settle()
      expect(operation.isPending).toBe(true)
    })
    expect(operation.feedback.props.target).toBe('current pod')
    await act(async () => {
      await expect(mutateAsync()).rejects.toThrow('原请求尚待核对')
    })
    await act(async () => reset())
    expect(operation.isPending).toBe(true)
    expect(submit).toHaveBeenCalledOnce()
    await act(async () => complete())
    await vi.waitFor(async () => {
      await settle()
      expect(operation.isSuccess).toBe(true)
    })
    await act(async () => reset())
    await vi.waitFor(async () => {
      await settle()
      expect(operation.isIdle).toBe(true)
    })
    expect(operation.reset).toBe(reset)
    expect(builds).toBe(1)
  } finally {
    await act(async () => root.unmount())
    client.clear()
    vi.unstubAllGlobals()
  }
})
