import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { observabilityKeys } from '../keys'
import { invalidateAlertIntegrations } from './mutations'

describe('alert integration mutations', () => {
  it('invalidates the canonical domain and legacy overview cache', async () => {
    const queryClient = new QueryClient()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue(undefined)

    await invalidateAlertIntegrations(queryClient)

    expect(invalidate).toHaveBeenCalledWith({ queryKey: observabilityKeys.integrations.all })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: observabilityKeys.legacy.monitoringOverviewIntegrations,
    })
  })
})
