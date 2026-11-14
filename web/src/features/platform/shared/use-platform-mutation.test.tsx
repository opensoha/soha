import { useOperationMutation } from '@/components/use-operation-mutation'
/** @vitest-environment jsdom */
import { ApiError } from '@/services/api-error'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  platformBatchUnknown,
  platformResourceTarget,
  usePlatformMutation,
} from './use-platform-mutation'

vi.mock('@/components/request-feedback', () => ({
  OperationFeedback: ({ target, error }: { target: string; error?: Error }) => (
    <div>
      {target} {error?.message}
    </div>
  ),
  requestErrorMessage: (error: Error) => error.message,
}))

const clients: QueryClient[] = []
const roots: ReturnType<typeof createRoot>[] = []
vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true)
afterEach(async () => {
  await act(async () => roots.splice(0).forEach((root) => root.unmount()))
  clients.splice(0).forEach((client) => client.clear())
  document.body.innerHTML = ''
})

async function mount<T>(operation: () => T) {
  let result: T
  const container = document.createElement('div')
  document.body.append(container)
  const root = createRoot(container)
  roots.push(root)
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  clients.push(client)
  function Consumer() {
    result = operation()
    return null
  }
  await act(async () =>
    root.render(
      <QueryClientProvider client={client}>
        <Consumer />
      </QueryClientProvider>,
    ),
  )
  return () => result!
}

const target = { scope: { clusterId: 'cluster-a', namespace: 'team-a' }, name: 'app-a' }
const apiError = (status: number) =>
  new ApiError(status, 'synthetic failure', {
    method: 'PUT',
    path: '/test',
    requestId: 'request-a',
  })

describe.each([
  ['Platform', usePlatformMutation],
  ['Shared scenes', useOperationMutation],
] as const)('%s operation continuity', (_, useSceneMutation) => {
  it('fences same-tick duplicates and keeps an uncertain original request locked after reset', async () => {
    let reject!: (error: Error) => void
    const write = vi.fn(
      (_variables: typeof target) =>
        new Promise<void>((_resolve, failure) => {
          reject = failure
        }),
    )
    const read = await mount(() =>
      useSceneMutation(
        { mutationFn: write },
        { label: ['更新', 'Update'], target: platformResourceTarget },
      ),
    )
    await act(async () => {
      read().mutate(target)
      read().mutate(target)
    })
    expect(write).toHaveBeenCalledTimes(1)
    await act(async () => {
      reject(apiError(503))
    })
    await vi.waitFor(async () => {
      await act(async () => undefined)
      expect(read().outcomeUnknown).toBe(true)
    })
    await act(async () => {
      read().reset()
      read().mutate({ ...target, name: 'app-b' })
    })
    expect(write).toHaveBeenCalledTimes(1)
    expect(read().variables).toEqual(target)
  })

  it('allows an explicit corrected submission after a known rejection and preserves owner callbacks', async () => {
    const write = vi.fn().mockRejectedValueOnce(apiError(409)).mockResolvedValueOnce('saved')
    const invalidated = vi.fn()
    const read = await mount(() =>
      useSceneMutation(
        { mutationFn: write, onSuccess: invalidated },
        { label: ['更新', 'Update'], target: platformResourceTarget },
      ),
    )
    await act(async () => {
      await read()
        .mutateAsync(target)
        .catch(() => undefined)
    })
    expect(read().blocked).toBe(false)
    await act(async () => {
      await read().mutateAsync({ ...target, name: 'app-b' })
    })
    expect(write).toHaveBeenCalledTimes(2)
    expect(invalidated).toHaveBeenCalledTimes(1)
    expect(read().variables?.name).toBe('app-b')
  })

  it('allows read-only preflight retry after a transport failure', async () => {
    const preflight = vi
      .fn()
      .mockRejectedValueOnce(apiError(503))
      .mockResolvedValueOnce({ ready: true })
    const read = await mount(() =>
      useSceneMutation(
        { mutationFn: preflight },
        { label: ['预检', 'Preflight'], target: platformResourceTarget, readOnly: true },
      ),
    )
    await act(async () => {
      await read()
        .mutateAsync(target)
        .catch(() => undefined)
    })
    expect(read().outcomeUnknown).toBe(false)
    await act(async () => {
      await read().mutateAsync(target)
    })
    expect(preflight).toHaveBeenCalledTimes(2)
  })

  it('keeps a partially uncertain batch locked even though settled results were returned', async () => {
    const results: PromiseSettledResult<void>[] = [
      { status: 'fulfilled', value: undefined },
      { status: 'rejected', reason: apiError(503) },
    ]
    const write = vi.fn().mockResolvedValue(results)
    const read = await mount(() =>
      useSceneMutation(
        { mutationFn: write },
        {
          label: ['批量删除', 'Delete batch'],
          target: platformResourceTarget,
          resultError: platformBatchUnknown,
        },
      ),
    )
    await act(async () => {
      await read().mutateAsync(target)
    })
    expect(read().isSuccess).toBe(true)
    expect(read().blocked).toBe(true)
    await act(async () => read().mutate(target))
    expect(write).toHaveBeenCalledTimes(1)
  })
})
