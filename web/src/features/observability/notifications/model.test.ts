import { describe, expect, it } from 'vitest'
import {
  buildNotificationChannelPayload,
  buildNotificationPolicyPayload,
  buildNotificationRoutePayload,
  buildNotificationSilencePayload,
  buildNotificationTemplatePayload,
} from './model'

describe('notification payload model', () => {
  it('parses policy, template and channel form-only JSON fields', () => {
    expect(
      buildNotificationPolicyPayload({
        name: 'Critical Pager',
        matchers: '{"severity":"critical"}',
        processorChain: ['template_render', 'webhook_update'],
        channelRefs: ['channel-slack'],
        oncallRef: 'schedule-primary',
        sendResolved: true,
        cooldownSeconds: 300,
        enabled: true,
      }),
    ).toEqual({
      name: 'Critical Pager',
      matchers: { severity: 'critical' },
      processorChain: ['template_render', 'webhook_update'],
      channelRefs: ['channel-slack'],
      oncallRef: 'schedule-primary',
      sendResolved: true,
      cooldownSeconds: 300,
      enabled: true,
    })
    expect(
      buildNotificationTemplatePayload({
        name: 'Webhook Body',
        templateType: 'generic_json',
        contentType: 'application/json',
        bodyTemplate: '{"alert":"{{ .alert.title }}"}',
        headers: '{"X-Soha":"true"}',
        queryParams: '{"dryRun":"false"}',
        samplePayload: '{"alert":{"title":"CPU High"}}',
        enabled: true,
      }),
    ).toMatchObject({
      headers: { 'X-Soha': 'true' },
      queryParams: { dryRun: 'false' },
      samplePayload: { alert: { title: 'CPU High' } },
    })
    expect(
      buildNotificationChannelPayload({
        name: 'Ops Webhook',
        channelType: 'webhook',
        config: '{"url":"https://hooks.local/ops","method":"POST"}',
        enabled: false,
      }),
    ).toEqual({
      name: 'Ops Webhook',
      channelType: 'webhook',
      config: { url: 'https://hooks.local/ops', method: 'POST' },
      enabled: false,
    })
  })

  it('normalizes compatibility route lists and silence ISO times', () => {
    expect(
      buildNotificationRoutePayload({
        name: 'Critical Route',
        matchers: '{"severity":"critical"}',
        channelIds: 'channel-slack, channel-email',
        enabled: true,
      }),
    ).toEqual({
      name: 'Critical Route',
      matchers: { severity: 'critical' },
      channelIds: ['channel-slack', 'channel-email'],
      enabled: true,
    })
    expect(
      buildNotificationSilencePayload({
        name: 'Maintenance',
        matchers: '{"service":"checkout"}',
        reason: 'planned maintenance',
        startsAt: '2026-05-06T10:00:00Z',
        endsAt: '2026-05-06T11:00:00Z',
        enabled: true,
      }),
    ).toEqual({
      name: 'Maintenance',
      matchers: { service: 'checkout' },
      reason: 'planned maintenance',
      startsAt: '2026-05-06T10:00:00.000Z',
      endsAt: '2026-05-06T11:00:00.000Z',
      enabled: true,
    })
  })
})
