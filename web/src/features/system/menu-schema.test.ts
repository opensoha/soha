/** @vitest-environment jsdom */

import { describe, expect, it } from 'vitest'
import { buildMenuSectionOptions, normalizeMenuSection, resolveMenuSectionLabel } from './menu-schema'

describe('menu section schema helpers', () => {
  it('normalizes legacy display labels to canonical section keys', () => {
    expect(normalizeMenuSection('Dashboard')).toBe('platform')
    expect(normalizeMenuSection('observe')).toBe('ops')
    expect(normalizeMenuSection('Delivery')).toBe('deliver')
    expect(normalizeMenuSection('control')).toBe('control')
  })

  it('renders canonical labels for normalized aliases', () => {
    expect(resolveMenuSectionLabel('observe')).toBe('Observe')
    expect(resolveMenuSectionLabel('Dashboard')).toBe('Dashboard')
    expect(resolveMenuSectionLabel('control')).toBe('control')
  })

  it('only keeps active section options and removes duplicate aliases', () => {
    expect(
      buildMenuSectionOptions(['platform', 'Dashboard', 'observe', 'ops', 'control', 'deliver']),
    ).toEqual([
      { value: 'platform', label: 'Dashboard' },
      { value: 'ops', label: 'Observe' },
      { value: 'deliver', label: 'Delivery' },
      { value: 'control', label: 'control' },
    ])
  })
})
