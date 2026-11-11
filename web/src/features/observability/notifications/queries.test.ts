import { describe, expect, it } from 'vitest'
import { observabilityKeys } from '../keys'
import { observabilityNotificationQueries } from './queries'

describe('notification query options', () => {
  it('uses canonical keys for every page dependency', () => {
    expect(observabilityNotificationQueries.channels().queryKey).toEqual(
      observabilityKeys.notifications.channels(),
    )
    expect(observabilityNotificationQueries.policies().queryKey).toEqual(
      observabilityKeys.notifications.policies(),
    )
    expect(observabilityNotificationQueries.routes().queryKey).toEqual(
      observabilityKeys.notifications.routes(),
    )
    expect(observabilityNotificationQueries.oncallSchedules().queryKey).toEqual(
      observabilityKeys.notifications.oncallSchedules(),
    )
  })
})
