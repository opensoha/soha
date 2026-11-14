import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { observabilityKeys } from '../keys'
import { invalidateAlertRules } from './mutations'

describe('alert rule mutations', () => {
  it('invalidates the canonical rule root', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
    await invalidateAlertRules(queryClient)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: observabilityKeys.rules.all })
  })
})
