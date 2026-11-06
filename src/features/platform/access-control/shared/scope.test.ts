import { expect, it } from 'vitest'
import { resolveAccessControlNamespace } from './scope'

it('uses resource URL identity before the list filter and retains the selection fallback', () => {
  expect(resolveAccessControlNamespace('selected-team', 'resource-team')).toBe('resource-team')
  expect(resolveAccessControlNamespace(null, 'resource-team')).toBe('resource-team')
  expect(resolveAccessControlNamespace('selected-team', null)).toBe('selected-team')
  expect(resolveAccessControlNamespace(null, null)).toBe('')
})
