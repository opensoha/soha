import { describe, expect, it } from 'vitest'

import { normalizeBrandingSettings, resolveBrandingLogos } from '@/utils/branding'

describe('normalizeBrandingSettings', () => {
  it('uses the packaged primary mark when logo settings are empty', () => {
    const branding = normalizeBrandingSettings({
      loginLogoUrl: ' ',
      expandedLogoUrl: '',
      collapsedLogoUrl: '',
      faviconUrl: '',
    })

    expect([
      branding.loginLogoUrl,
      branding.expandedLogoUrl,
      branding.collapsedLogoUrl,
      branding.faviconUrl,
    ]).toEqual(['/logo.svg', '/logo.svg', '/logo.svg', '/logo.svg'])
  })
})

describe('resolveBrandingLogos', () => {
  const branding = normalizeBrandingSettings({
    expandedLogoUrl: '/light.svg',
    collapsedLogoUrl: '/compact.svg',
    darkExpandedLogoUrl: ' /dark.svg ',
    darkCollapsedLogoUrl: ' /dark-compact.svg ',
  })

  it('selects theme variants independently for expanded and compact brands', () => {
    expect(resolveBrandingLogos(branding, 'light')).toEqual({
      expandedLogoUrl: '/light.svg',
      collapsedLogoUrl: '/compact.svg',
    })
    expect(resolveBrandingLogos(branding, 'dark')).toEqual({
      expandedLogoUrl: '/dark.svg',
      collapsedLogoUrl: '/dark-compact.svg',
    })
    expect(resolveBrandingLogos({ ...branding, darkCollapsedLogoUrl: '' }, 'dark')).toEqual({
      expandedLogoUrl: '/dark.svg',
      collapsedLogoUrl: '/compact.svg',
    })
  })

  it('preserves older settings and cleared overrides in dark mode', () => {
    const older = normalizeBrandingSettings({
      expandedLogoUrl: '/light.svg',
      collapsedLogoUrl: '/compact.svg',
    })
    expect(resolveBrandingLogos(older, 'dark')).toEqual(resolveBrandingLogos(older, 'light'))
    expect(resolveBrandingLogos(normalizeBrandingSettings(), 'dark')).toEqual({
      expandedLogoUrl: '/logo.svg',
      collapsedLogoUrl: '/logo.svg',
    })
  })
})
