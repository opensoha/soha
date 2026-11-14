/** @vitest-environment jsdom */

import { describe, expect, it } from 'vitest'
import { normalizeLocalReturnTo, shouldUseDocumentNavigation } from './return-to'

describe('normalizeLocalReturnTo', () => {
  it('accepts local absolute paths and same-origin URLs as local paths', () => {
    expect(normalizeLocalReturnTo('/portal/applications/app-1?tab=overview#top')).toBe(
      '/portal/applications/app-1?tab=overview#top',
    )
    expect(normalizeLocalReturnTo(`${window.location.origin}/portal/applications/app-1`)).toBe(
      '/portal/applications/app-1',
    )
  })

  it('rejects external, protocol-relative, backslash, and control-character values', () => {
    for (const value of [
      '//evil.example/path',
      'https://evil.example/path',
      '/portal\nnext',
      '/portal?next=%0A',
      '/\\evil',
      '/%5Cevil',
    ]) {
      expect(normalizeLocalReturnTo(value)).toBeNull()
    }
  })
})

describe('shouldUseDocumentNavigation', () => {
  it('uses document navigation for server protocol callback paths', () => {
    expect(shouldUseDocumentNavigation('/oauth2/authorize?client_id=portal')).toBe(true)
    expect(shouldUseDocumentNavigation('/api/v1/provider/proxy/callback?state=resume')).toBe(true)
    expect(shouldUseDocumentNavigation('/portal/applications/app-1')).toBe(false)
  })
})

describe('path deployment return targets', () => {
  it('converts browser callback targets into router paths without losing query or fragment', () => {
    expect(normalizeLocalReturnTo('/team/auth/device', '/team/')).toBe('/auth/device')
    expect(normalizeLocalReturnTo('/team/projects?p=1#selection', '/team/')).toBe(
      '/projects?p=1#selection',
    )
    expect(normalizeLocalReturnTo('/auth/device', '/team/')).toBe('/auth/device')
    expect(normalizeLocalReturnTo('//other.example.test/team/', '/team/')).toBeNull()
    expect(normalizeLocalReturnTo('/team/../auth/device', '/team/')).toBe('/auth/device')
  })
  it('recognizes server callbacks below the deployment prefix', () => {
    expect(
      shouldUseDocumentNavigation('/team/api/v1/provider/proxy/callback?state=resume', '/team/'),
    ).toBe(true)
    expect(shouldUseDocumentNavigation('/team/oauth2/authorize?client_id=portal', '/team/')).toBe(
      true,
    )
    expect(shouldUseDocumentNavigation('/team/auth/device', '/team/')).toBe(false)
  })
})
