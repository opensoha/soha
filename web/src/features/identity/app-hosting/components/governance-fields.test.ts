import { describe, expect, it } from 'vitest'
import type { AppHostingSettings } from '../types'
import {
  buildGovernanceDraft,
  buildGovernancePayload,
  governanceDraftError,
} from './governance-fields'

const legacySettings = { maxAppsPerOwner: 3 } as AppHostingSettings

describe('application hosting governance draft', () => {
  it('preserves legacy defaults and only sends the configured quota dimensions', () => {
    const draft = buildGovernanceDraft(legacySettings)
    expect(draft.maxDynamicAppsPerOwner).toBe(3)
    expect(draft.dataCapacityWarningMiB).toBe(200)
    draft.quotaOverrides.push({ rowKey: 'new-0', scope: 'team', id: ' finance ', maxDynamicApps: 25 })
    expect(governanceDraftError(draft)).toBeUndefined()
    expect(JSON.parse(JSON.stringify(buildGovernancePayload(draft)))).toMatchObject({
      maxDynamicAppsPerOwner: 3,
      dataCapacityWarningMiB: 200,
      quotaOverrides: [{ scope: 'team', id: 'finance', maxDynamicApps: 25 }],
    })
  })

  it('blocks empty or duplicate overrides and out-of-range runtime values', () => {
    const draft = buildGovernanceDraft(legacySettings)
    draft.quotaOverrides.push({ rowKey: 'a', scope: 'role', id: 'auditor' })
    expect(governanceDraftError(draft)).toContain('至少设置一项额度')
    draft.quotaOverrides[0].maxStaticApps = 4
    draft.quotaOverrides.push({ rowKey: 'b', scope: 'role', id: 'auditor', maxDynamicApps: 3 })
    expect(governanceDraftError(draft)).toContain('需要唯一')
    draft.quotaOverrides.pop()
    draft.runtimeLimits.maxReplicas = 11
    expect(governanceDraftError(draft)).toContain('最大副本数')
  })
})
