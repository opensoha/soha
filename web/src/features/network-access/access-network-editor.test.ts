import type { NetworkAccessNetworkInput } from '@opensoha/contracts/gen/ts/sohaapi'
import { describe, expect, it } from 'vitest'
import { accessNetworkInput } from './access-network-editor'

const wired: NetworkAccessNetworkInput = {
  siteId: 'site',
  name: ' Wired office ',
  accessMedium: 'wired',
  kind: 'standard',
  enabled: true,
  nasBindingIds: ['nas'],
  portStart: 0,
  portEnd: 0,
  ssid: 'previous Wi-Fi',
  bssids: ['00:11:22:33:44:55'],
  vlanId: 20,
  filterId: ' staff ',
  sessionTimeoutSeconds: 300,
  priority: 10,
  autoConnect: false,
  rules: [
    {
      effect: 'allow',
      subjects: { users: [], teams: [], tags: [] },
      scheduleStartMinute: 0,
      scheduleEndMinute: 360,
    },
  ],
}

describe('access network form payload', () => {
  it('preserves zero port and UTC boundary values and omits old Wi-Fi selectors', () => {
    const input = accessNetworkInput('site', wired)
    expect(input).toMatchObject({
      name: 'Wired office',
      portStart: 0,
      portEnd: 0,
      filterId: 'staff',
      rules: [{ scheduleStartMinute: 0, scheduleEndMinute: 360 }],
    })
    expect(input).not.toHaveProperty('ssid')
    expect(input).not.toHaveProperty('bssids')
  })

  it('saves strict-online configuration as a disabled draft', () => {
    const input = accessNetworkInput('site', { ...wired, kind: 'strict_online' })
    expect(input.enabled).toBe(false)
    expect(accessNetworkInput('site', wired).enabled).toBe(true)
  })

  it('clears wired and RADIUS data when switching a draft to open guest bootstrap', () => {
    const input = accessNetworkInput('site', {
      ...wired,
      kind: 'open_guest_bootstrap',
      ssid: 'Guest',
    })
    expect(input).toMatchObject({
      kind: 'open_guest_bootstrap',
      accessMedium: 'wifi',
      ssid: 'Guest',
      nasBindingIds: [],
      rules: [],
    })
    for (const key of ['portStart', 'portEnd', 'vlanId', 'filterId', 'sessionTimeoutSeconds']) {
      expect(input).not.toHaveProperty(key)
    }
    expect(input.bssids).toBeUndefined()
  })
})
