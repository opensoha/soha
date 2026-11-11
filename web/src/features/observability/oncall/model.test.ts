import dayjs from 'dayjs'
import { describe, expect, it } from 'vitest'
import {
  buildOnCallAssignmentPayload,
  buildOnCallEscalationPolicyPayload,
  buildOnCallRotationPayload,
  buildOnCallSchedulePayload,
  buildRotationConfigWithOverride,
} from './model'

describe('on-call model', () => {
  it('builds schedule and typed rotation config payloads', () => {
    expect(
      buildOnCallSchedulePayload({
        name: 'Primary schedule',
        timeZone: 'Asia/Shanghai',
        description: 'platform team',
        enabled: true,
      }),
    ).toEqual({
      name: 'Primary schedule',
      timeZone: 'Asia/Shanghai',
      description: 'platform team',
      enabled: true,
    })
    expect(
      buildOnCallRotationPayload(
        {
          name: 'Daily primary',
          scheduleId: 'schedule-primary',
          participants: ['alice', 'bob'],
          rotationMode: 'custom',
          shiftHours: 6,
          startAt: dayjs('2026-05-06T10:00:00Z'),
          enabled: true,
        },
        { keepExisting: true },
      ),
    ).toMatchObject({
      rotationConfig: {
        keepExisting: true,
        shiftHours: 6,
        rotationMinutes: 360,
        startAt: '2026-05-06T10:00:00.000Z',
      },
    })
  })

  it('preserves escalation metadata and parses assignment matchers', () => {
    expect(
      buildOnCallEscalationPolicyPayload(
        {
          name: 'Critical escalation',
          steps: [
            {
              scheduleId: 'schedule-primary',
              delayMinutes: 5,
              role: 'sre',
              description: 'page SRE',
            },
          ],
          enabled: true,
        },
        [
          {
            scheduleId: 'old',
            delayMinutes: 0,
            role: 'dev',
            description: 'old',
            auditTag: 'keep',
          },
        ],
      ),
    ).toMatchObject({ steps: [{ auditTag: 'keep', scheduleId: 'schedule-primary' }] })
    expect(
      buildOnCallAssignmentPayload({
        name: 'Checkout critical',
        matchers: '{"clusterId":"prod-a"}',
        targetType: 'escalation',
        targetRef: 'policy-critical',
        groupBy: ['alertName', 'service'],
        enabled: true,
      }),
    ).toMatchObject({
      matchers: { clusterId: 'prod-a' },
      targetType: 'escalation',
      groupBy: ['alertName', 'service'],
    })
  })

  it('updates and clears date overrides without losing rotation config', () => {
    const withOverride = buildRotationConfigWithOverride(
      { shiftHours: 24, overrides: { '2026-05-06': ['alice'] } },
      '2026-05-07',
      ['bob'],
    )
    expect(withOverride).toEqual({
      shiftHours: 24,
      overrides: { '2026-05-06': ['alice'], '2026-05-07': ['bob'] },
    })
    expect(buildRotationConfigWithOverride(withOverride, '2026-05-06', [])).toEqual({
      shiftHours: 24,
      overrides: { '2026-05-07': ['bob'] },
    })
  })
})
