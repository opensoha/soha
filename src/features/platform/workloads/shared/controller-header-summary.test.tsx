/** @vitest-environment jsdom */
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { ControllerHeaderFacts, ControllerStatusTag } from './controller-overview'
import type { WorkloadMeta } from './detail-shell'
import type { WorkloadKind } from './types'

const language = vi.hoisted(() => ({ localeCode: 'zh_CN' }))
vi.mock('@/i18n', () => ({ useI18n: () => ({ ...language }) }))
const base: WorkloadMeta = { name: 'database', namespace: 'apps', createdAt: '' }
let host: HTMLDivElement
let root: ReturnType<typeof createRoot>
beforeAll(() => vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true))
afterEach(async () => {
  await act(async () => root?.unmount())
  host?.remove()
  language.localeCode = 'zh_CN'
})
async function render(resource: WorkloadKind, detail: Partial<WorkloadMeta> = {}) {
  host = document.createElement('div')
  root = createRoot(host)
  await act(async () =>
    root.render(
      <>
        <ControllerStatusTag resource={resource} title={resource} detail={{ ...base, ...detail }} />
        <ControllerHeaderFacts resource={resource} detail={{ ...base, ...detail }} />
      </>,
    ),
  )
  return host
}

describe('controller header observations', () => {
  it.each([
    [
      'statefulsets',
      {
        desiredReplicas: 2,
        currentReplicas: 2,
        readyReplicas: 2,
        currentRevision: 'database-v2',
        updateRevision: 'database-v2',
      },
    ],
    [
      'daemonsets',
      { desiredNumber: 2, currentNumber: 2, readyNumber: 2, updatedNumber: 2, availableNumber: 2 },
    ],
    ['replicasets', { desiredReplicas: 2, readyReplicas: 2, availableReplicas: 2 }],
    [
      'replicationcontrollers',
      { desiredReplicas: 2, currentReplicas: 2, readyReplicas: 2, availableReplicas: 2 },
    ],
  ] as const)(
    'uses the shared healthy label for complete %s observations',
    async (resource, fields) => {
      const view = await render(resource, fields)
      expect(view.querySelector('.soha-status-tag')?.textContent).toBe('健康')
    },
  )

  it('keeps unavailable DaemonSet replicas visible even after ready and updated reach target', async () => {
    const view = await render('daemonsets', {
      desiredNumber: 2,
      currentNumber: 2,
      readyNumber: 2,
      updatedNumber: 2,
      availableNumber: 1,
    })
    expect(view.querySelector('.soha-status-tag')?.textContent).toBe('部分不可用')
  })

  it.each(['replicasets', 'replicationcontrollers'] as const)(
    'keeps %s availability separate from readiness',
    async (resource) => {
      const view = await render(resource, {
        desiredReplicas: 2,
        currentReplicas: 2,
        readyReplicas: 2,
        availableReplicas: 1,
      })
      expect(view.querySelector('.soha-status-tag')?.textContent).toBe('部分不可用')
    },
  )

  it('does not claim ReplicationController health without a current count', async () => {
    const view = await render('replicationcontrollers', {
      desiredReplicas: 2,
      readyReplicas: 2,
      availableReplicas: 2,
    })
    expect(view.querySelector('.soha-status-tag')?.textContent).toBe('副本就绪')
  })

  it.each([
    ['statefulsets', { desiredReplicas: 3, readyReplicas: 3 }, '副本就绪', 'success'],
    ['daemonsets', { desiredNumber: 10, readyNumber: 8 }, '副本未就绪', 'warning'],
    ['replicasets', { desiredReplicas: 2, readyReplicas: 2 }, '副本就绪', 'success'],
    ['replicationcontrollers', { desiredReplicas: 2, readyReplicas: 1 }, '副本未就绪', 'warning'],
    ['jobs', { active: 1, failed: 2, succeeded: 0, completions: 1 }, '执行中', 'processing'],
    ['jobs', { active: 0, failed: 2, succeeded: 0, completions: 1 }, '有失败记录', 'warning'],
    ['jobs', { active: 0, succeeded: 2, completions: 2 }, '完成数达标', 'success'],
    ['cronjobs', { suspend: true }, '已暂停', 'default'],
    ['cronjobs', { suspend: false }, '调度已启用', 'success'],
  ] as const)(
    'labels actual %s state without claiming overall health',
    async (resource, fields, label, tone) => {
      const view = await render(resource, fields)
      const tag = view.querySelector('.soha-status-tag')!
      expect(tag.textContent).toBe(label)
      expect(tag.classList.contains(`ant-tag-${tone}`)).toBe(true)
      expect(view.textContent).not.toContain('健康')
    },
  )

  it('shows differing StatefulSet revisions with full identity and keeps strategy', async () => {
    const current = `database-${'a'.repeat(80)}`
    const target = `database-${'b'.repeat(80)}`
    const view = await render('statefulsets', {
      desiredReplicas: 3,
      readyReplicas: 3,
      currentRevision: current,
      updateRevision: target,
      updateStrategy: 'OnDelete',
    })
    expect(view.querySelector('.soha-status-tag')?.textContent).toBe('版本有差异')
    expect(view.querySelector(`[title="${current}"]`)?.textContent).toContain('当前版本')
    expect(view.querySelector(`[title="${target}"]`)?.textContent).toContain('目标版本')
    expect(view.textContent).toContain('3 / 3 就绪')
    expect(view.textContent).toContain('OnDelete')
    expect(view.textContent).not.toContain('更新中')
  })

  it('shows a single current revision when both revisions match', async () => {
    const view = await render('statefulsets', {
      desiredReplicas: 1,
      readyReplicas: 1,
      currentRevision: 'database-abc123',
      updateRevision: 'database-abc123',
    })
    expect(view.querySelectorAll('.soha-controller-header-revision')).toHaveLength(1)
    expect(view.textContent).toContain('当前版本 abc123')
  })

  it('shows DaemonSet updated counts and never invents a revision', async () => {
    const view = await render('daemonsets', {
      desiredNumber: 10,
      readyNumber: 10,
      updatedNumber: 8,
      updateStrategy: 'RollingUpdate',
    })
    expect(view.textContent).toContain('10 / 10 就绪')
    expect(view.textContent).toContain('已更新 8 / 10')
    expect(view.querySelector('.soha-status-tag')?.textContent).toBe('部分未更新')
    expect(view.textContent).toContain('RollingUpdate')
    expect(view.textContent).not.toContain('版本')
  })

  it('uses a ReplicaSet revision only when its returned annotation supplies one', async () => {
    const view = await render('replicasets', {
      desiredReplicas: 1,
      readyReplicas: 1,
      annotations: { 'deployment.kubernetes.io/revision': '12' },
    })
    expect(view.textContent).toContain('版本 12')
    expect(view.querySelector('[title="deployment.kubernetes.io/revision"]')).not.toBeNull()
  })

  it.each([
    {},
    { desiredReplicas: Number.NaN, readyReplicas: Number.POSITIVE_INFINITY },
    { desiredReplicas: -1, readyReplicas: -1 },
  ])('keeps missing and invalid observations unknown: %j', async (fields) => {
    const view = await render('statefulsets', fields)
    expect(view.querySelector('.soha-status-tag')?.textContent).toBe('就绪未知')
    expect(view.textContent).toContain('— / — 就绪')
    expect(view.textContent).not.toContain('版本')
  })

  it('does not claim scaling completed merely because the desired count is zero', async () => {
    const view = await render('statefulsets', {
      desiredReplicas: 0,
      readyReplicas: 2,
      currentReplicas: 2,
    })
    expect(view.querySelector('.soha-status-tag')?.textContent).toBe('期望为 0')
    expect(view.textContent).toContain('2 / 0 就绪')
    expect(view.textContent).not.toContain('已缩容')
  })

  it('keeps CronJob schedule and active Jobs visible in English', async () => {
    language.localeCode = 'en_US'
    const view = await render('cronjobs', {
      suspend: false,
      schedule: '*/5 * * * *',
      activeJobs: 0,
      timeZone: 'UTC',
    })
    expect(view.textContent).toContain('Scheduling enabled')
    expect(view.textContent).toContain('Schedule */5 * * * *')
    expect(view.textContent).toContain('Active Jobs 0')
    expect(view.textContent).toContain('UTC')
  })
})
