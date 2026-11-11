import { describe, expect, it } from 'vitest'
import { loginProviderTagColor } from './login-provider'

describe('login provider metadata colors', () => {
  it('uses a category color rather than a success status for every named provider', () => {
    for (const provider of ['password', 'oidc', 'saml', 'feishu', 'desktop-handoff', 'custom']) {
      expect(loginProviderTagColor(provider)).toBe('blue')
    }
    expect(loginProviderTagColor('  ')).toBe('default')
    expect(loginProviderTagColor()).toBe('default')
  })
})
