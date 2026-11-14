/** @vitest-environment jsdom */

import { act } from 'react'
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
