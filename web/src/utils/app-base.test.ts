import { describe, expect, it } from 'vitest'
import { appBrowserPath, appRouterPath } from './app-base'
describe('deployment paths', () => {
  it('keeps root deployments compatible', () => {
    expect(appBrowserPath('/auth/device', '/')).toBe('/auth/device')
    expect(appRouterPath('/auth/device', '/')).toBe('/auth/device')
  })
  it('preserves query/hash and only strips an exact deployment prefix', () => {
    expect(appBrowserPath('/auth/device?tab=1#top', '/team/')).toBe('/team/auth/device?tab=1#top')
    expect(appRouterPath('/team/auth/device?tab=1#top', '/team/')).toBe('/auth/device?tab=1#top')
    expect(appRouterPath('/teammate/projects', '/team/')).toBe('/teammate/projects')
    expect(appRouterPath('/team', '/team/')).toBe('/')
  })
})
