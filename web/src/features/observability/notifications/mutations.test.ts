import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { observabilityKeys } from '../keys'
import { observabilityNotificationMutations } from './mutations'

describe('notification mutations', () => {
  it('invalidates both compatibility routes and canonical policies after route writes', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
    const options = observabilityNotificationMutations.createRoute(queryClient)

    await options.onSuccess?.(undefined, {} as never, undefined, {} as never)

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: observabilityKeys.notifications.routes(),
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: observabilityKeys.notifications.policies(),
    })
  })
})
