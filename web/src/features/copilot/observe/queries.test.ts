import { describe, expect, it } from 'vitest'
import { observeKeys } from './keys'
import { observeQueries } from './queries'

describe('observeQueries', () => {
  it('keeps legacy-compatible query keys behind the canonical factory', () => {
    expect(observeQueries.overview.sessions().queryKey).toEqual(observeKeys.overview.sessions())
    expect(observeQueries.operations.tasks().queryKey).toEqual(observeKeys.operations.tasks())
    expect(observeQueries.operations.runs().queryKey).toEqual(observeKeys.operations.runs())
    expect(observeQueries.operations.catalog().queryKey).toEqual(observeKeys.operations.catalog())
    expect(observeQueries.tools.catalog().queryKey).toEqual(observeKeys.tools.catalog())
    expect(observeQueries.tools.session('session-1').queryKey).toEqual(
      observeKeys.tools.session('session-1'),
    )
  })

  it('disables permission and identifier-dependent queries until usable', () => {
    expect(observeQueries.operations.policies(false).enabled).toBe(false)
    expect(observeQueries.operations.policies(true).enabled).toBe(true)
    expect(observeQueries.tools.session(undefined).enabled).toBe(false)
    expect(observeQueries.tools.session('session-1').enabled).toBe(true)
  })
})
