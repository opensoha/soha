import { describe, expect, it } from 'vitest'
import {
  computeModuleDraft,
  computeModuleState,
  isComputeChildConfig,
} from './compute-module-model'
import type { RuntimeConfigItem } from './types'

const children = [
  { key: 'modules.docker.enabled', effectiveValue: false },
  { key: 'modules.virtualization.enabled', effectiveValue: false },
] as RuntimeConfigItem[]

describe('compute module model', () => {
  it('derives disabled, partial, and enabled parent states from the two children', () => {
    expect(computeModuleState(children, {})).toBe('disabled')
    expect(computeModuleState(children, { 'modules.docker.enabled': true })).toBe('partial')
    expect(
      computeModuleState(children, {
        'modules.docker.enabled': true,
        'modules.virtualization.enabled': true,
      }),
    ).toBe('enabled')
  })

  it('writes both real child keys for a parent action', () => {
    expect(computeModuleDraft(false)).toEqual({
      'modules.docker.enabled': false,
      'modules.virtualization.enabled': false,
    })
    expect(isComputeChildConfig('modules.docker.enabled')).toBe(true)
    expect(isComputeChildConfig('modules.ai.enabled')).toBe(false)
  })
})
