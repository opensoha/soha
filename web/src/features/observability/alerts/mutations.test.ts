import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { observabilityKeys } from '../keys'
import { invalidateAlertHealingRuns, invalidateAlerts } from './mutations'

describe('alert mutations', () => {
  it('invalidates list/detail root and both healing views', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)
    await invalidateAlerts(queryClient)
    await invalidateAlertHealingRuns(queryClient, 'event-1')
    expect(invalidate).toHaveBeenCalledWith({ queryKey: observabilityKeys.alerts.all })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: observabilityKeys.alerts.healingRuns('event-1'),
    })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: observabilityKeys.healing.runs() })
  })
})
