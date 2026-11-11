import { describe, expect, it } from 'vitest'
import { dashboardTemplateJSON, dashboardTemplates } from './templates'

describe('dashboard templates', () => {
  it('provides bounded executable templates and keeps unsupported PVE truthful', () => {
    const available = dashboardTemplates.filter((item) => item.available)
    expect(available.map((item) => item.key)).toEqual([
      'kubernetes-workloads',
      'kubevirt-vmi',
      'service-red',
    ])
    for (const item of available) {
      const raw = dashboardTemplateJSON(item.key)
      expect(raw?.length).toBeLessThan(2 * 1024 * 1024)
      const parsed = JSON.parse(raw!)
      expect(parsed.tags).toContain('soha-template')
      expect(parsed.panels.length).toBeGreaterThan(0)
      expect(
        parsed.panels.every((panel: { targets: unknown[] }) => panel.targets.length === 1),
      ).toBe(true)
    }
    expect(dashboardTemplates.find((item) => item.key === 'pve-resources')).toMatchObject({
      available: false,
      reason: expect.stringContaining('Provider API'),
    })
  })
})
