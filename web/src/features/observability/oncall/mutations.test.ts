import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { observabilityKeys } from '../keys'
import { observabilityOncallMutations } from './mutations'

describe('on-call mutations', () => {
  it('invalidates the matching capability key', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
    const options = observabilityOncallMutations.updateRotation(queryClient)
    await options.onSuccess?.(undefined, {} as never, undefined, {} as never)
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: observabilityKeys.oncall.rotations(),
    })
  })
})
